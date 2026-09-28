package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWKSURL is not on docs.privy.io (docs say copy the key from the dashboard) but is what Privy's SDKs fetch; verified live.
func JWKSURL(appID string) string { return "https://auth.privy.io/api/v1/apps/" + appID + "/jwks.json" }

type Verifier struct {
	url     string
	parser  *jwt.Parser
	mu      sync.Mutex
	keys    map[string]*ecdsa.PublicKey
	fetched time.Time
}

// New fetches the key set once so a bad app id fails at boot, not on the first request.
func New(ctx context.Context, appID, jwksURL string) (*Verifier, error) {
	if appID == "" {
		return nil, fmt.Errorf("privy app id empty")
	}
	v := &Verifier{url: jwksURL, parser: jwt.NewParser(
		jwt.WithValidMethods([]string{"ES256"}),
		jwt.WithIssuer("privy.io"),
		jwt.WithAudience(appID),
		jwt.WithExpirationRequired(),
	)}
	return v, v.refresh(ctx)
}

// Verify returns the Privy DID in sub.
func (v *Verifier) Verify(ctx context.Context, token string) (string, error) {
	var c jwt.RegisteredClaims
	_, err := v.parser.ParseWithClaims(token, &c, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		return v.keySet(ctx, kid), nil
	})
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(c.Subject, "did:privy:") {
		return "", fmt.Errorf("sub %q is not a privy did", c.Subject)
	}
	return c.Subject, nil
}

func (v *Verifier) keySet(ctx context.Context, kid string) jwt.VerificationKeySet {
	v.mu.Lock()
	defer v.mu.Unlock()
	if k := v.keys[kid]; k != nil {
		return jwt.VerificationKeySet{Keys: []jwt.VerificationKey{k}}
	}
	// unknown kid: Privy may have rotated; throttled so junk tokens can't hammer the endpoint
	if time.Since(v.fetched) > time.Minute {
		_ = v.refreshLocked(ctx)
	}
	var set jwt.VerificationKeySet
	for _, k := range v.keys {
		set.Keys = append(set.Keys, k)
	}
	return set
}

func (v *Verifier) refresh(ctx context.Context) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.refreshLocked(ctx)
}

func (v *Verifier) refreshLocked(ctx context.Context) error {
	v.fetched = time.Now()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch privy jwks: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("privy jwks: status %d", resp.StatusCode)
	}
	var body struct {
		Keys []struct{ Kid, Kty, Crv, X, Y string } `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return fmt.Errorf("decode privy jwks: %w", err)
	}
	keys := map[string]*ecdsa.PublicKey{}
	for _, k := range body.Keys {
		if k.Kty != "EC" || k.Crv != "P-256" {
			continue
		}
		x, err1 := base64.RawURLEncoding.DecodeString(k.X)
		y, err2 := base64.RawURLEncoding.DecodeString(k.Y)
		if err1 != nil || err2 != nil || len(x) != 32 || len(y) != 32 {
			return fmt.Errorf("privy jwks: bad key %q", k.Kid)
		}
		pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), append(append([]byte{4}, x...), y...))
		if err != nil {
			return fmt.Errorf("privy jwks key %q: %w", k.Kid, err)
		}
		keys[k.Kid] = pub
	}
	if len(keys) == 0 {
		return fmt.Errorf("privy jwks: no P-256 keys")
	}
	v.keys = keys
	return nil
}
