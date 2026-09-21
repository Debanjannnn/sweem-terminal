package privyauth

import (
	"context"
	"crypto/ecdsa"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/config"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/httpx"
)

const (
	issuer    = "privy.io"
	didPrefix = "did:privy:"
)

type ctxKey struct{}

func DIDFromContext(ctx context.Context) (string, bool) {
	did, ok := ctx.Value(ctxKey{}).(string)
	return did, ok
}

type Verifier struct {
	appID  string
	key    *ecdsa.PublicKey
	parser *jwt.Parser
}

func NewVerifier(appID, verificationKeyPEM string) (*Verifier, error) {
	if appID == "" {
		return nil, fmt.Errorf("privy app id is empty, so no audience can be checked")
	}
	key, err := jwt.ParseECPublicKeyFromPEM([]byte(normalizePEM(verificationKeyPEM)))
	if err != nil {
		return nil, fmt.Errorf("parse privy verification key: %w", err)
	}
	return &Verifier{
		appID: appID,
		key:   key,
		parser: jwt.NewParser(
			jwt.WithValidMethods([]string{"ES256"}),
			jwt.WithIssuer(issuer),
			jwt.WithAudience(appID),
			jwt.WithExpirationRequired(),
		),
	}, nil
}

// Verify returns the Privy DID carried in sub.
func (v *Verifier) Verify(token string) (string, error) {
	var claims jwt.RegisteredClaims
	if _, err := v.parser.ParseWithClaims(token, &claims, func(*jwt.Token) (any, error) { return v.key, nil }); err != nil {
		return "", fmt.Errorf("privy access token rejected: %w", err)
	}
	if !strings.HasPrefix(claims.Subject, didPrefix) {
		return "", fmt.Errorf("token subject %q is not a privy DID", claims.Subject)
	}
	return claims.Subject, nil
}

func (v *Verifier) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		did, err := v.Verify(bearer(r))
		if err != nil {
			httpx.Fail(w, http.StatusUnauthorized, "unauthorized", "The Privy access token is missing, expired or not signed for this app, so the request was refused.")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, did)))
	})
}

// MiddlewareAllowingInternal lets a trusted service act for a user without a JWT.
func (v *Verifier) MiddlewareAllowingInternal(internal InternalAuth, next http.Handler) http.Handler {
	jwtOnly := v.Middleware(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(HeaderInternalSecret) == "" {
			jwtOnly.ServeHTTP(w, r)
			return
		}
		did, err := internal.ActingUser(r)
		if err != nil {
			httpx.Fail(w, http.StatusUnauthorized, "unauthorized", "The internal secret did not match or no acting user was supplied, so the request was refused.")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, did)))
	})
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

const (
	HeaderInternalSecret = "X-Internal-Secret"
	HeaderActingUser     = "X-Acting-User"
)

type InternalAuth struct{ Secret string }

// ActingUser authenticates a service-to-service call and returns the DID it acts for.
func (a InternalAuth) ActingUser(r *http.Request) (string, error) {
	if a.Secret == "" {
		return "", fmt.Errorf("no internal secret is configured, so internal calls are disabled")
	}
	got := r.Header.Get(HeaderInternalSecret)
	if subtle.ConstantTimeCompare([]byte(got), []byte(a.Secret)) != 1 {
		return "", fmt.Errorf("the %s header does not match the configured secret", HeaderInternalSecret)
	}
	did := strings.TrimSpace(r.Header.Get(HeaderActingUser))
	if !strings.HasPrefix(did, didPrefix) {
		return "", fmt.Errorf("the %s header must carry a privy DID, got %q", HeaderActingUser, did)
	}
	return did, nil
}

type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// FetchVerificationKey prefers PRIVY_VERIFICATION_KEY and only then asks the API.
// ponytail: undocumented endpoint, env fallback is the supported path
func FetchVerificationKey(ctx context.Context, appID, appSecret string) (string, error) {
	return fetchVerificationKey(ctx, &http.Client{Timeout: 10 * time.Second}, appID, appSecret)
}

func fetchVerificationKey(ctx context.Context, d Doer, appID, appSecret string) (string, error) {
	if k := config.GetEnv("PRIVY_VERIFICATION_KEY", ""); k != "" {
		return normalizePEM(k), nil
	}
	if appID == "" || appSecret == "" {
		return "", fmt.Errorf("no PRIVY_VERIFICATION_KEY is set and app credentials are missing, so the key cannot be resolved")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.privy.io/v1/apps/"+appID, nil)
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(appID, appSecret)
	req.Header.Set("privy-app-id", appID)
	resp, err := d.Do(req)
	if err != nil {
		return "", fmt.Errorf("call privy app endpoint: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("privy app endpoint returned %d; set PRIVY_VERIFICATION_KEY instead", resp.StatusCode)
	}
	var body struct {
		VerificationKey string `json:"verification_key"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("decode privy app response: %w", err)
	}
	if body.VerificationKey == "" {
		return "", fmt.Errorf("privy app response carried no verification_key; set PRIVY_VERIFICATION_KEY instead")
	}
	return normalizePEM(body.VerificationKey), nil
}

var pemLabel = regexp.MustCompile(`-----BEGIN ([A-Z0-9 ]+)-----`)

// dashboards hand the key over on one line; rewrap the body at 64 chars
func normalizePEM(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), `\n`, "\n")
	m := pemLabel.FindStringSubmatch(s)
	if m == nil {
		return s
	}
	begin, end := "-----BEGIN "+m[1]+"-----", "-----END "+m[1]+"-----"
	body := s[strings.Index(s, begin)+len(begin):]
	if i := strings.Index(body, end); i >= 0 {
		body = body[:i]
	}
	body = strings.Join(strings.Fields(body), "")
	var b strings.Builder
	b.WriteString(begin + "\n")
	for len(body) > 64 {
		b.WriteString(body[:64] + "\n")
		body = body[64:]
	}
	if body != "" {
		b.WriteString(body + "\n")
	}
	b.WriteString(end + "\n")
	return b.String()
}
