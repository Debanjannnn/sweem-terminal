package privyauth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const appID = "app-123"

func newKey(t *testing.T) (*ecdsa.PrivateKey, string) {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&k.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return k, string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

func sign(t *testing.T, k *ecdsa.PrivateKey, method jwt.SigningMethod, claims jwt.Claims) string {
	t.Helper()
	s, err := jwt.NewWithClaims(method, claims).SignedString(k)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func goodClaims() jwt.RegisteredClaims {
	return jwt.RegisteredClaims{
		Issuer:    issuer,
		Audience:  jwt.ClaimStrings{appID},
		Subject:   "did:privy:abc123",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}
}

func TestVerify(t *testing.T) {
	key, pub := newKey(t)
	other, _ := newKey(t)
	v, err := NewVerifier(appID, pub)
	if err != nil {
		t.Fatal(err)
	}

	noExp := goodClaims()
	noExp.ExpiresAt = nil
	expired := goodClaims()
	expired.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Minute))
	wrongIss := goodClaims()
	wrongIss.Issuer = "evil.io"
	wrongAud := goodClaims()
	wrongAud.Audience = jwt.ClaimStrings{"other-app"}
	notDID := goodClaims()
	notDID.Subject = "user_42"

	tests := []struct {
		name    string
		token   string
		wantDID string
	}{
		{"valid", sign(t, key, jwt.SigningMethodES256, goodClaims()), "did:privy:abc123"},
		{"wrong signing key", sign(t, other, jwt.SigningMethodES256, goodClaims()), ""},
		{"missing exp", sign(t, key, jwt.SigningMethodES256, noExp), ""},
		{"expired", sign(t, key, jwt.SigningMethodES256, expired), ""},
		{"wrong issuer", sign(t, key, jwt.SigningMethodES256, wrongIss), ""},
		{"wrong audience", sign(t, key, jwt.SigningMethodES256, wrongAud), ""},
		{"subject is not a DID", sign(t, key, jwt.SigningMethodES256, notDID), ""},
		{"empty", "", ""},
		{"garbage", "not.a.jwt", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := v.Verify(tt.token)
			if tt.wantDID == "" {
				if err == nil {
					t.Fatalf("accepted a bad token, got did %q", got)
				}
				return
			}
			if err != nil || got != tt.wantDID {
				t.Fatalf("got %q, %v", got, err)
			}
		})
	}
}

// alg swaps are the classic bypass: only ES256 may be accepted
func TestAlgSwapsRejected(t *testing.T) {
	_, pub := newKey(t)
	v, err := NewVerifier(appID, pub)
	if err != nil {
		t.Fatal(err)
	}
	none, err := jwt.NewWithClaims(jwt.SigningMethodNone, goodClaims()).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	hmac, err := jwt.NewWithClaims(jwt.SigningMethodHS256, goodClaims()).SignedString([]byte(pub))
	if err != nil {
		t.Fatal(err)
	}
	for name, tok := range map[string]string{"none": none, "hs256 with the public key": hmac} {
		if _, err := v.Verify(tok); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
}

func TestMiddlewareAllowingInternal(t *testing.T) {
	key, pub := newKey(t)
	v, err := NewVerifier(appID, pub)
	if err != nil {
		t.Fatal(err)
	}
	internal := InternalAuth{Secret: "s3cret"}
	var seen string
	h := v.MiddlewareAllowingInternal(internal, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, _ = DIDFromContext(r.Context())
	}))
	valid := sign(t, key, jwt.SigningMethodES256, goodClaims())

	tests := []struct {
		name     string
		headers  map[string]string
		wantCode int
		wantDID  string
	}{
		{"jwt path", map[string]string{"Authorization": "Bearer " + valid}, 200, "did:privy:abc123"},
		{"lowercase bearer", map[string]string{"Authorization": "bearer " + valid}, 200, "did:privy:abc123"},
		{"no credentials", nil, 401, ""},
		{"bad jwt", map[string]string{"Authorization": "Bearer nope"}, 401, ""},
		{"internal acting", map[string]string{HeaderInternalSecret: "s3cret", HeaderActingUser: "did:privy:sched"}, 200, "did:privy:sched"},
		{"internal wrong secret", map[string]string{HeaderInternalSecret: "wrong", HeaderActingUser: "did:privy:sched"}, 401, ""},
		{"internal without acting user", map[string]string{HeaderInternalSecret: "s3cret"}, 401, ""},
		{"internal acting user not a DID", map[string]string{HeaderInternalSecret: "s3cret", HeaderActingUser: "admin"}, 401, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seen = ""
			r := httptest.NewRequest("GET", "/", nil)
			for k, val := range tt.headers {
				r.Header.Set(k, val)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tt.wantCode {
				t.Fatalf("status %d want %d", w.Code, tt.wantCode)
			}
			if seen != tt.wantDID {
				t.Fatalf("did %q want %q", seen, tt.wantDID)
			}
		})
	}
}

func TestInternalAuthDisabledWithoutSecret(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set(HeaderInternalSecret, "")
	r.Header.Set(HeaderActingUser, "did:privy:x")
	if _, err := (InternalAuth{}).ActingUser(r); err == nil {
		t.Fatal("empty configured secret must not authenticate anyone")
	}
}

func TestNormalizePEM(t *testing.T) {
	_, pub := newKey(t)
	body := strings.Join(strings.Fields(strings.NewReplacer(
		"-----BEGIN PUBLIC KEY-----", "", "-----END PUBLIC KEY-----", "").Replace(pub)), "")

	tests := []struct {
		name string
		in   string
	}{
		{"already wrapped", pub},
		{"single line", "-----BEGIN PUBLIC KEY-----" + body + "-----END PUBLIC KEY-----"},
		{"escaped newlines", strings.ReplaceAll(pub, "\n", `\n`)},
		{"spaces instead of newlines", "-----BEGIN PUBLIC KEY----- " + body + " -----END PUBLIC KEY-----"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewVerifier(appID, tt.in); err != nil {
				t.Fatalf("key not parseable after normalize: %v", err)
			}
			for _, line := range strings.Split(strings.TrimSpace(normalizePEM(tt.in)), "\n") {
				if len(line) > 64 && !strings.HasPrefix(line, "-----") {
					t.Fatalf("body line longer than 64: %q", line)
				}
			}
		})
	}
}

type fakeDoer struct {
	status int
	body   string
	err    error
	gotURL string
	gotAut string
}

func (f *fakeDoer) Do(r *http.Request) (*http.Response, error) {
	f.gotURL, f.gotAut = r.URL.String(), r.Header.Get("Authorization")
	if f.err != nil {
		return nil, f.err
	}
	return &http.Response{StatusCode: f.status, Body: io.NopCloser(strings.NewReader(f.body))}, nil
}

func TestFetchVerificationKey(t *testing.T) {
	key, pub := newKey(t)
	// jwks built from this test's own key, so the round trip is what is asserted
	jwks := fmt.Sprintf(`{"keys":[{"kty":"EC","crv":"P-256","alg":"ES256","x":%q,"y":%q}]}`,
		base64.RawURLEncoding.EncodeToString(key.PublicKey.X.Bytes()),
		base64.RawURLEncoding.EncodeToString(key.PublicKey.Y.Bytes()))

	tests := []struct {
		name    string
		env     string
		doer    *fakeDoer
		wantErr bool
		wantAPI bool
	}{
		{"env wins and skips the endpoint", pub, &fakeDoer{status: 200, body: jwks}, false, false},
		{"jwks fetch", "", &fakeDoer{status: 200, body: jwks}, false, true},
		{"jwks 404", "", &fakeDoer{status: 404, body: `{}`}, true, true},
		{"jwks transport error", "", &fakeDoer{err: http.ErrHandlerTimeout}, true, true},
		{"jwks carries no ec key", "", &fakeDoer{status: 200, body: `{"keys":[]}`}, true, true},
		{"jwks bad json", "", &fakeDoer{status: 200, body: `nope`}, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PRIVY_VERIFICATION_KEY", tt.env)
			got, err := fetchVerificationKey(context.Background(), tt.doer, appID)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v", err)
			}
			if called := tt.doer.gotURL != ""; called != tt.wantAPI {
				t.Fatalf("endpoint called = %v, want %v", called, tt.wantAPI)
			}
			want := "https://auth.privy.io/api/v1/apps/" + appID + "/jwks.json"
			if tt.wantAPI && tt.doer.gotURL != want {
				t.Fatalf("url %q, want %q", tt.doer.gotURL, want)
			}
			// the jwks endpoint is public; sending app credentials to it would be a leak
			if tt.wantAPI && tt.doer.gotAut != "" {
				t.Fatal("app credentials must not be sent to the public jwks endpoint")
			}
			if err == nil {
				if _, err := NewVerifier(appID, got); err != nil {
					t.Fatalf("fetched key is not usable: %v", err)
				}
			}
		})
	}
}

func TestFetchWithoutCredentialsOrEnv(t *testing.T) {
	t.Setenv("PRIVY_VERIFICATION_KEY", "")
	d := &fakeDoer{}
	if _, err := fetchVerificationKey(context.Background(), d, ""); err == nil {
		t.Fatal("want error")
	}
	if d.gotURL != "" {
		t.Fatal("must not call the endpoint without an app id")
	}
}
