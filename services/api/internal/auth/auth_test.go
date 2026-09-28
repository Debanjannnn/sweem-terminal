package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func jwk(kid string, k *ecdsa.PrivateKey) map[string]string {
	b := k.PublicKey.X.FillBytes(make([]byte, 32))
	c := k.PublicKey.Y.FillBytes(make([]byte, 32))
	return map[string]string{"kid": kid, "kty": "EC", "crv": "P-256", "alg": "ES256",
		"x": base64.RawURLEncoding.EncodeToString(b), "y": base64.RawURLEncoding.EncodeToString(c)}
}

func TestVerify(t *testing.T) {
	good, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	decoy, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"keys": []any{jwk("decoy", decoy), jwk("k1", good)}})
	}))
	defer srv.Close()
	v, err := New(context.Background(), "app", srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	sign := func(key *ecdsa.PrivateKey, kid string, mut func(jwt.MapClaims)) string {
		c := jwt.MapClaims{"iss": "privy.io", "aud": "app", "sub": "did:privy:abc", "sid": "s",
			"iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix()}
		if mut != nil {
			mut(c)
		}
		tok := jwt.NewWithClaims(jwt.SigningMethodES256, c)
		if kid != "" {
			tok.Header["kid"] = kid
		}
		s, err := tok.SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

	ctx := context.Background()
	did, err := v.Verify(ctx, sign(good, "k1", nil))
	if err != nil || did != "did:privy:abc" {
		t.Fatalf("valid token: %q %v", did, err)
	}
	if _, err := v.Verify(ctx, sign(good, "", nil)); err != nil {
		t.Fatalf("no kid should try all keys: %v", err)
	}
	hs, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"iss": "privy.io", "aud": "app", "sub": "did:privy:abc", "exp": time.Now().Add(time.Hour).Unix()}).SignedString([]byte("x"))

	bad := map[string]string{
		"wrong key":    sign(other, "k1", nil),
		"kid mismatch": sign(good, "decoy", nil),
		"wrong aud":    sign(good, "k1", func(c jwt.MapClaims) { c["aud"] = "other" }),
		"wrong iss":    sign(good, "k1", func(c jwt.MapClaims) { c["iss"] = "evil" }),
		"expired":      sign(good, "k1", func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Minute).Unix() }),
		"no exp":       sign(good, "k1", func(c jwt.MapClaims) { delete(c, "exp") }),
		"non-did sub":  sign(good, "k1", func(c jwt.MapClaims) { c["sub"] = "bob" }),
		"hs256":        hs,
		"garbage":      "a.b.c",
		"empty":        "",
	}
	for name, tok := range bad {
		if _, err := v.Verify(ctx, tok); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
