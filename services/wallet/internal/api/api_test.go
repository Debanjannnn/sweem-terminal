package api

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/privyauth"
	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/executor"
	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/policy"
	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/store"
)

const (
	appID          = "app-1"
	did            = "did:privy:alice"
	internalSecret = "internal-secret-value"
)

// fakeStore is the whole Store interface in memory. Hand-rolled on purpose:
// the handlers are what is under test, not a driver.
type fakeStore struct {
	users       map[string]store.User
	delegations map[string]policy.Delegation
	intents     map[string]store.Intent
	execs       map[string]store.Execution
	schedules   map[string]store.Schedule
	slices      map[string]store.Slice
	kuru        map[string]store.KuruAccount

	insertAttempts int
	pingErr        error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		users:       map[string]store.User{},
		delegations: map[string]policy.Delegation{},
		intents:     map[string]store.Intent{},
		execs:       map[string]store.Execution{},
		schedules:   map[string]store.Schedule{},
		slices:      map[string]store.Slice{},
		kuru:        map[string]store.KuruAccount{},
	}
}

func sliceKey(id string, i int) string { return id + ":" + strconv.Itoa(i) }

func (f *fakeStore) Ping(context.Context) error { return f.pingErr }

func (f *fakeStore) UpsertUser(_ context.Context, id, wallet, walletID string) (store.User, error) {
	// mirrors the unique index on users.wallet_address
	for _, u := range f.users {
		if u.ID != id && strings.EqualFold(u.WalletAddress, wallet) {
			return store.User{}, store.ErrWalletTaken
		}
	}
	u := store.User{ID: id, WalletAddress: wallet, PrivyWalletID: walletID, CreatedAt: time.Now()}
	f.users[id] = u
	return u, nil
}

func (f *fakeStore) User(_ context.Context, id string) (store.User, error) {
	u, ok := f.users[id]
	if !ok {
		return store.User{}, store.ErrNotFound
	}
	return u, nil
}

func (f *fakeStore) PutDelegation(_ context.Context, d policy.Delegation) (policy.Delegation, error) {
	f.delegations[d.UserID] = d
	return d, nil
}

func (f *fakeStore) Delegation(_ context.Context, userID string) (policy.Delegation, error) {
	d, ok := f.delegations[userID]
	if !ok {
		return policy.Delegation{}, store.ErrNotFound
	}
	return d, nil
}

func (f *fakeStore) RevokeDelegation(_ context.Context, userID string) error {
	d, ok := f.delegations[userID]
	if !ok || d.RevokedAt != nil {
		return store.ErrNotFound
	}
	now := time.Now()
	d.RevokedAt = &now
	f.delegations[userID] = d
	return nil
}

func (f *fakeStore) SpentTotal(_ context.Context, userID string) (string, error) {
	total := 0
	for _, in := range f.intents {
		if in.UserID == userID && in.State != "failed" && (in.OrderType == "market" || in.OrderType == "limit") {
			n, _ := strconv.Atoi(in.Size)
			total += n
		}
	}
	return strconv.Itoa(total), nil
}

func (f *fakeStore) PutKuruAccount(_ context.Context, ka store.KuruAccount) error {
	// mirrors the upsert: signer_expiry is not in its update list
	if old, ok := f.kuru[ka.UserID]; ok {
		ka.SignerExpiry = old.SignerExpiry
	}
	ka.UpdatedAt = time.Now()
	f.kuru[ka.UserID] = ka
	return nil
}

func (f *fakeStore) KuruAccount(_ context.Context, userID string) (store.KuruAccount, error) {
	ka, ok := f.kuru[userID]
	if !ok {
		return store.KuruAccount{}, store.ErrNotFound
	}
	return ka, nil
}

func (f *fakeStore) SetSignerExpiry(_ context.Context, userID string, expiry int64) error {
	ka, ok := f.kuru[userID]
	if !ok {
		return store.ErrNotFound
	}
	ka.SignerExpiry = expiry
	f.kuru[userID] = ka
	return nil
}

func (f *fakeStore) InsertIntent(_ context.Context, in store.Intent) (bool, error) {
	f.insertAttempts++
	if _, ok := f.intents[in.Key]; ok {
		return false, nil
	}
	in.CreatedAt = time.Now()
	f.intents[in.Key] = in
	return true, nil
}

func (f *fakeStore) Intent(_ context.Context, key string) (store.Intent, error) {
	in, ok := f.intents[key]
	if !ok {
		return store.Intent{}, store.ErrNotFound
	}
	return in, nil
}

func (f *fakeStore) Intents(_ context.Context, userID string, _ int) ([]store.Intent, error) {
	out := []store.Intent{}
	for _, in := range f.intents {
		if in.UserID == userID {
			out = append(out, in)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func (f *fakeStore) SetOrderRef(_ context.Context, key, orderID string, slot *int) error {
	in, ok := f.intents[key]
	if !ok {
		return store.ErrNotFound
	}
	in.OrderID = orderID
	if slot != nil {
		in.SlotIdx = slot
	}
	f.intents[key] = in
	return nil
}

func (f *fakeStore) SetIntentState(_ context.Context, key, state string) error {
	in, ok := f.intents[key]
	if !ok {
		return store.ErrNotFound
	}
	in.State = state
	f.intents[key] = in
	return nil
}

func (f *fakeStore) RecordExecution(_ context.Context, e store.Execution) error {
	e.SubmittedAt = time.Now()
	f.execs[e.IntentKey] = e
	return nil
}

func (f *fakeStore) Execution(_ context.Context, key string) (store.Execution, error) {
	e, ok := f.execs[key]
	if !ok {
		return store.Execution{}, store.ErrNotFound
	}
	return e, nil
}

func (f *fakeStore) Executions(_ context.Context, userID string, _ int) ([]store.Execution, error) {
	out := []store.Execution{}
	for key, e := range f.execs {
		if f.intents[key].UserID == userID {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fakeStore) CreateSchedule(_ context.Context, sc store.Schedule, slices []store.Slice) (store.Schedule, error) {
	for _, existing := range f.schedules {
		if existing.UserID == sc.UserID && existing.IdempotencyKey == sc.IdempotencyKey {
			return existing, nil
		}
	}
	sc.ID = fmt.Sprintf("00000000-0000-4000-8000-%012d", len(f.schedules)+1)
	sc.State, sc.CreatedAt = "active", time.Now()
	for _, sl := range slices {
		sl.ScheduleID, sl.State = sc.ID, "pending"
		f.slices[sliceKey(sc.ID, sl.Index)] = sl
		sc.Slices = append(sc.Slices, sl)
	}
	f.schedules[sc.ID] = sc
	return sc, nil
}

func (f *fakeStore) Schedule(_ context.Context, id, userID string) (store.Schedule, error) {
	sc, ok := f.schedules[id]
	if !ok || sc.UserID != userID {
		return store.Schedule{}, store.ErrNotFound
	}
	return sc, nil
}

func (f *fakeStore) Schedules(_ context.Context, userID string) ([]store.Schedule, error) {
	out := []store.Schedule{}
	for _, sc := range f.schedules {
		if sc.UserID == userID {
			out = append(out, sc)
		}
	}
	return out, nil
}

func (f *fakeStore) CancelSchedule(_ context.Context, id, userID string) error {
	sc, ok := f.schedules[id]
	if !ok || sc.UserID != userID || sc.State == "cancelled" {
		return store.ErrNotFound
	}
	sc.State = "cancelled"
	f.schedules[id] = sc
	return nil
}

func (f *fakeStore) PauseScheduleRevoked(_ context.Context, id string) error {
	sc, ok := f.schedules[id]
	if !ok {
		return store.ErrNotFound
	}
	sc.State = "paused_revoked"
	f.schedules[id] = sc
	return nil
}

func (f *fakeStore) DispatchTarget(_ context.Context, id string, index int, userID string) (store.Schedule, store.Slice, error) {
	sc, ok := f.schedules[id]
	sl, hasSlice := f.slices[sliceKey(id, index)]
	if !ok || !hasSlice || sc.UserID != userID {
		return store.Schedule{}, store.Slice{}, store.ErrNotFound
	}
	return sc, sl, nil
}

func (f *fakeStore) MarkSlice(_ context.Context, id string, index int, state, intentKey, skipReason string) error {
	sl, ok := f.slices[sliceKey(id, index)]
	if !ok {
		return store.ErrNotFound
	}
	sl.State, sl.IntentKey, sl.SkipReason = state, intentKey, skipReason
	f.slices[sliceKey(id, index)] = sl
	return nil
}

// fakeExecutor records every write call and the path it arrived on, so a test
// can prove a retry did not place a second order and that a limit order was
// never flattened into a swap.
type fakeExecutor struct {
	srv        *httptest.Server
	calls      int
	ensures    int
	paths      []string
	bodies     []map[string]any
	status     string
	errMsg     string
	registered bool
	authorized bool
	authNonce  string
	signer     string
	markets    []policy.Market
	// perpl: what /perpl/account answers, and extra info on every write
	perplAccount executor.PerplAccount
	info         json.RawMessage
}

// mirrors the MON-USDC entry of executor/kuru-markets.json
func fakeMarkets() []policy.Market {
	return []policy.Market{{
		ID: "MON-USDC", ChainID: 10143,
		MarketAddress: "0xfdbe356828c8f5a5d5ed4f69dde0816f4058ef61",
		BaseToken:     "0x0000000000000000000000000000000000000000",
		QuoteToken:    "0xee0722ead54f1b4fe97be399be43bc0226a6f97e",
		BaseDecimals:  18, QuoteDecimals: 6,
		PricePrecision: "1000000", SizePrecision: "100000000",
		BaseSizeMultiplier: "10000000000", TickSize: "1",
		MinQuoteNotional: "10000000", MaxQuoteNotional: "5000000000000",
	}}
}

func newFakeExecutor(t *testing.T) *fakeExecutor {
	t.Helper()
	f := &fakeExecutor{status: "submitted", registered: true, authorized: true,
		authNonce: "6", markets: fakeMarkets()}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		enc := json.NewEncoder(w)
		switch r.URL.Path {
		case "/markets":
			_ = enc.Encode(map[string]any{"markets": f.markets})
			return
		case "/perpl/account":
			_ = enc.Encode(f.perplAccount)
			return
		case "/account/ensure":
			f.ensures++
			id := 0
			if f.registered {
				id = 7
			}
			_ = enc.Encode(executor.Account{
				ChainID: 10143, AccountCore: "0x6384e9b2Bf3b65e1535403a0A543b5FDA905eE22",
				UserID: int64(id), Registered: f.registered,
				SignerAuthorized: f.authorized, AuthNonce: f.authNonce, Signer: f.signer,
			})
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if f.status == "submitted" {
			switch r.URL.Path {
			case "/account/deposit":
				f.registered = true // the first deposit mints the kuru account id
			case "/account/authorize":
				f.authorized = true
			case "/account/revoke":
				f.authorized = false
			}
		}
		f.calls++
		f.paths = append(f.paths, r.URL.Path)
		f.bodies = append(f.bodies, body)
		key, _ := body["intent_key"].(string)
		_ = enc.Encode(executor.RouteResponse{
			IntentKey: key,
			TxHash:    "0xtx" + key[:4],
			Status:    f.status,
			Error:     f.errMsg,
			Info:      f.info,
		})
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeExecutor) sawOnly(t *testing.T, path string) {
	t.Helper()
	if len(f.paths) != 1 || f.paths[0] != path {
		t.Fatalf("executor saw %v, want exactly [%s]", f.paths, path)
	}
}

type harness struct {
	handler http.Handler
	store   *fakeStore
	exec    *fakeExecutor
	key     *ecdsa.PrivateKey
	auth    *privyauth.Verifier
}

func newHarness(t *testing.T, opts ...func(*Server)) *harness {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := privyauth.NewVerifier(appID, string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})))
	if err != nil {
		t.Fatal(err)
	}
	fs, fe := newFakeStore(), newFakeExecutor(t)
	s := &Server{
		Store:              fs,
		Executor:           executor.New(fe.srv.URL),
		Auth:               verifier,
		Internal:           privyauth.InternalAuth{Secret: internalSecret},
		Log:                slog.New(slog.NewTextHandler(io.Discard, nil)),
		ChainID:            10143,
		DefaultSlippageBps: 50,
	}
	for _, o := range opts {
		o(s)
	}
	return &harness{handler: s.Routes(), store: fs, exec: fe, key: key, auth: verifier}
}

func (h *harness) token(t *testing.T, subject string) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.RegisteredClaims{
		Issuer:    "privy.io",
		Subject:   subject,
		Audience:  jwt.ClaimStrings{appID},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	})
	signed, err := tok.SignedString(h.key)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func (h *harness) do(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	return h.send(t, method, path, body, map[string]string{"Authorization": "Bearer " + h.token(t, did)})
}

func (h *harness) send(t *testing.T, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	return rec
}

func (h *harness) registered(t *testing.T) {
	t.Helper()
	if _, err := h.store.UpsertUser(context.Background(), did, "0x1111111111111111111111111111111111111111", "pw-1"); err != nil {
		t.Fatal(err)
	}
}

func (h *harness) delegated(t *testing.T, perSlice, total string) {
	t.Helper()
	if _, err := h.store.PutDelegation(context.Background(), policy.Delegation{
		UserID:              did,
		SignerID:            "signer-1",
		Venues:              []string{"kuru"},
		Markets:             []string{"MON-USDC"},
		MaxNotionalPerSlice: perSlice,
		MaxNotionalTotal:    total,
		ExpiresAt:           time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
}

// stale ages a cached row past accountFresh so the next read refreshes.
func stale(ka store.KuruAccount) store.KuruAccount {
	ka.UpdatedAt = time.Now().Add(-2 * accountFresh)
	return ka
}

func body(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var env struct {
		Data  map[string]any `json:"data"`
		Error map[string]any `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("response %q is not JSON: %v", rec.Body.String(), err)
	}
	if env.Error != nil {
		return env.Error
	}
	return env.Data
}

// Every /v1 route must refuse an unauthenticated caller. Handlers would
// nil-panic on a missing user, so a 401 proves the middleware stopped it.
func TestRoutesRequireAuth(t *testing.T) {
	h := newHarness(t)
	routes := []struct{ method, path string }{
		{http.MethodGet, "/v1/me"},
		{http.MethodPost, "/v1/me"},
		{http.MethodGet, "/v1/delegation"},
		{http.MethodPut, "/v1/delegation"},
		{http.MethodDelete, "/v1/delegation"},
		{http.MethodPost, "/v1/orders"},
		{http.MethodGet, "/v1/orders"},
		{http.MethodDelete, "/v1/orders/MON-USDC/3"},
		{http.MethodGet, "/v1/account"},
		{http.MethodPost, "/v1/account/deposit"},
		{http.MethodPost, "/v1/account/authorize"},
		{http.MethodDelete, "/v1/account/authorize"},
		{http.MethodGet, "/v1/portfolio"},
		{http.MethodGet, "/v1/executions"},
		{http.MethodGet, "/v1/schedules"},
		{http.MethodPost, "/v1/schedules"},
		{http.MethodGet, "/v1/schedules/00000000-0000-4000-8000-000000000001"},
		{http.MethodDelete, "/v1/schedules/00000000-0000-4000-8000-000000000001"},
		{http.MethodPost, "/v1/internal/slices/00000000-0000-4000-8000-000000000001/0/dispatch"},
	}
	for _, rt := range routes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			if rec := h.send(t, rt.method, rt.path, "{}", nil); rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
		})
	}
}

func TestHealthIsPublic(t *testing.T) {
	h := newHarness(t)
	if rec := h.send(t, http.MethodGet, "/health", "", nil); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

// The internal secret opens exactly two doors; it must not stand in for a
// token anywhere else.
func TestInternalSecretOpensOnlyItsOwnRoutes(t *testing.T) {
	h := newHarness(t)
	headers := map[string]string{
		privyauth.HeaderInternalSecret: internalSecret,
		privyauth.HeaderActingUser:     did,
	}
	closed := []struct{ method, path string }{
		{http.MethodPost, "/v1/orders"},
		{http.MethodPost, "/v1/account/deposit"},
		{http.MethodPost, "/v1/account/authorize"},
		{http.MethodGet, "/v1/me"},
		{http.MethodPut, "/v1/delegation"},
		{http.MethodPost, "/v1/schedules"},
		{http.MethodGet, "/v1/portfolio"},
	}
	for _, rt := range closed {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			if rec := h.send(t, rt.method, rt.path, "{}", headers); rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401: the internal secret must not reach this route", rec.Code)
			}
		})
	}

	t.Run("GET /v1/delegation is open to it", func(t *testing.T) {
		rec := h.send(t, http.MethodGet, "/v1/delegation?user="+did, "", headers)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404 (authenticated, no delegation yet)", rec.Code)
		}
	})

	t.Run("a wrong secret is refused", func(t *testing.T) {
		rec := h.send(t, http.MethodGet, "/v1/delegation", "", map[string]string{
			privyauth.HeaderInternalSecret: "wrong",
			privyauth.HeaderActingUser:     did,
		})
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})
}

// Reading another user's delegation is an internal-only power.
func TestDelegationUserParamNeedsInternalSecret(t *testing.T) {
	h := newHarness(t)
	h.registered(t)
	h.delegated(t, "100", "1000")
	rec := h.do(t, http.MethodGet, "/v1/delegation?user=did:privy:bob", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestAWalletBelongsToOneSignIn(t *testing.T) {
	h := newHarness(t)
	const me = `{"wallet_address":"0x1111111111111111111111111111111111111111"}`
	if rec := h.do(t, http.MethodPost, "/v1/me", me); rec.Code != http.StatusOK {
		t.Fatalf("first claim: %d %s", rec.Code, rec.Body)
	}
	rec := h.send(t, http.MethodPost, "/v1/me", me,
		map[string]string{"Authorization": "Bearer " + h.token(t, "did:privy:mallory")})
	if rec.Code != http.StatusConflict {
		t.Fatalf("second claim: %d %s, want 409", rec.Code, rec.Body)
	}
}
