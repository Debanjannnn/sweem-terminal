package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/snehendu098/sweem-terminal/services/api/internal/privy"
)

func ptr[T any](v T) *T { return &v }

const basket = "0x00000000000000000000000000000000000000AA"

func TestValidate(t *testing.T) {
	rec := func(mut func(*ScheduleInput)) ScheduleInput {
		in := ScheduleInput{Basket: basket, Kind: "recurring", AmountUSDC: ptr("10.50"), IntervalSeconds: ptr(int64(60)),
			MaxSlippageBps: 50, MaxPerRunUSDC: "10.5", StopNavPerShare: ptr("1"), RedeemBps: ptr(5)}
		if mut != nil {
			mut(&in)
		}
		return in
	}
	sl := func(mut func(*ScheduleInput)) ScheduleInput {
		in := ScheduleInput{Basket: basket, Kind: "stop_loss", StopNavPerShare: ptr("0.95"), RedeemBps: ptr(10000),
			MaxSlippageBps: 1000, MaxPerRunUSDC: "100", AmountUSDC: ptr("1"), IntervalSeconds: ptr(int64(1))}
		if mut != nil {
			mut(&in)
		}
		return in
	}
	cases := []struct {
		in   ScheduleInput
		want string
	}{
		{rec(nil), ""},
		{sl(nil), ""},
		{rec(func(i *ScheduleInput) { i.Basket = "0xzz" }), "invalid_basket"},
		{rec(func(i *ScheduleInput) { i.Kind = "dca" }), "invalid_kind"},
		{rec(func(i *ScheduleInput) { i.AmountUSDC = nil }), "invalid_amount_usdc"},
		{rec(func(i *ScheduleInput) { i.AmountUSDC = ptr("0") }), "invalid_amount_usdc"},
		{rec(func(i *ScheduleInput) { i.AmountUSDC = ptr("1e3x") }), "invalid_amount_usdc"},
		{rec(func(i *ScheduleInput) { i.IntervalSeconds = ptr(int64(59)) }), "invalid_interval_seconds"},
		{rec(func(i *ScheduleInput) { i.IntervalSeconds = nil }), "invalid_interval_seconds"},
		{rec(func(i *ScheduleInput) { i.MaxPerRunUSDC = "10.49" }), "max_per_run_below_amount"},
		{rec(func(i *ScheduleInput) { i.MaxPerRunUSDC = "" }), "invalid_max_per_run_usdc"},
		{rec(func(i *ScheduleInput) { i.MaxSlippageBps = 0 }), "invalid_max_slippage_bps"},
		{rec(func(i *ScheduleInput) { i.MaxSlippageBps = 1001 }), "invalid_max_slippage_bps"},
		{sl(func(i *ScheduleInput) { i.StopNavPerShare = ptr("-1") }), "invalid_stop_nav_per_share"},
		{sl(func(i *ScheduleInput) { i.StopNavPerShare = nil }), "invalid_stop_nav_per_share"},
		{sl(func(i *ScheduleInput) { i.RedeemBps = ptr(0) }), "invalid_redeem_bps"},
		{sl(func(i *ScheduleInput) { i.RedeemBps = ptr(10001) }), "invalid_redeem_bps"},
	}
	for i, tc := range cases {
		if got := tc.in.Validate(); got != tc.want {
			t.Errorf("case %d: got %q want %q", i, got, tc.want)
		}
	}
	in := rec(nil)
	in.Validate()
	if in.Basket != strings.ToLower(basket) || *in.AmountUSDC != "10.5" || in.StopNavPerShare != nil || in.RedeemBps != nil {
		t.Errorf("recurring not normalized: %+v", in)
	}
	in = sl(nil)
	in.Validate()
	if in.AmountUSDC != nil || in.IntervalSeconds != nil {
		t.Errorf("stop_loss not normalized: %+v", in)
	}
}

type fakeAuth struct{}

func (fakeAuth) Verify(_ context.Context, tok string) (string, error) {
	if tok == "good" {
		return "did:privy:u1", nil
	}
	return "", errors.New("bad token")
}

type fakePrivy struct{ err error }

func (f fakePrivy) VerifyDelegation(context.Context, string, string, string) error { return f.err }

// fakeStore embeds the interface: any method a test did not expect panics.
type fakeStore struct {
	Store
	calls []string
	err   error
}

func (f *fakeStore) EnsureUser(_ context.Context, did string) error {
	f.calls = append(f.calls, "ensure "+did)
	return nil
}
func (f *fakeStore) PutDelegation(_ context.Context, did, wid, addr, q string) (Delegation, error) {
	f.calls = append(f.calls, fmt.Sprintf("put %s %s %s %s", did, wid, addr, q))
	return Delegation{ID: "d1", WalletAddress: addr, Active: true}, f.err
}
func (f *fakeStore) CreateSchedule(_ context.Context, did string, in ScheduleInput) (Schedule, error) {
	f.calls = append(f.calls, "create "+in.Basket)
	return Schedule{ID: "s1", Basket: in.Basket}, f.err
}
func (f *fakeStore) DeactivateSchedule(_ context.Context, did, id string) error { return f.err }
func (f *fakeStore) Baskets(context.Context) ([]Basket, error)                  { return []Basket{}, f.err }

func TestHandlers(t *testing.T) {
	const wallet = `0x00000000000000000000000000000000000000Bb`
	sched := `{"basket":"` + basket + `","kind":"recurring","amountUsdc":"5","intervalSeconds":3600,"maxSlippageBps":50,"maxPerRunUsdc":"5"}`
	cases := []struct {
		name, method, path, tok, body string
		privyErr, storeErr            error
		code                          int
		bodyHas                       string
	}{
		{"public baskets", "GET", "/api/v1/baskets", "", "", nil, nil, 200, "[]"},
		{"no token", "GET", "/api/v1/me", "", "", nil, nil, 401, "unauthorized"},
		{"bad token", "GET", "/api/v1/me/schedules", "nope", "", nil, nil, 401, "unauthorized"},
		{"delegation ok", "POST", "/api/v1/me/delegation", "good", `{"walletId":"w1","walletAddress":"` + wallet + `"}`, nil, nil, 200, `"walletAddress":"0x00000000000000000000000000000000000000bb"`},
		{"delegation bad addr", "POST", "/api/v1/me/delegation", "good", `{"walletId":"w1","walletAddress":"0x1"}`, nil, nil, 400, "bad_request"},
		{"delegation no wallet id", "POST", "/api/v1/me/delegation", "good", `{"walletAddress":"` + wallet + `"}`, nil, nil, 400, "bad_request"},
		{"delegation not owned", "POST", "/api/v1/me/delegation", "good", `{"walletId":"w1","walletAddress":"` + wallet + `"}`, privy.ErrNotOwned, nil, 403, "wallet_not_owned"},
		{"delegation no signer", "POST", "/api/v1/me/delegation", "good", `{"walletId":"w1","walletAddress":"` + wallet + `"}`, privy.ErrNoSigner, nil, 403, "signer_missing"},
		{"privy down", "POST", "/api/v1/me/delegation", "good", `{"walletId":"w1","walletAddress":"` + wallet + `"}`, errors.New("timeout"), nil, 502, "privy_unavailable"},
		{"schedule ok", "POST", "/api/v1/me/schedules", "good", sched, nil, nil, 201, `"id":"s1"`},
		{"schedule invalid", "POST", "/api/v1/me/schedules", "good", strings.Replace(sched, `"5"}`, `"4"}`, 1), nil, nil, 400, "max_per_run_below_amount"},
		{"schedule no delegation", "POST", "/api/v1/me/schedules", "good", sched, nil, ErrNoDelegation, 409, "delegation_inactive"},
		{"schedule no basket", "POST", "/api/v1/me/schedules", "good", sched, nil, ErrNoBasket, 404, "basket_not_found"},
		{"schedule garbage", "POST", "/api/v1/me/schedules", "good", `{`, nil, nil, 400, "bad_request"},
		{"delete bad id", "DELETE", "/api/v1/me/schedules/xyz", "good", "", nil, nil, 400, "bad_request"},
		{"delete missing", "DELETE", "/api/v1/me/schedules/6f1c1b1e-0000-4000-8000-000000000000", "good", "", nil, ErrNotFound, 404, "not_found"},
		{"store error hidden", "GET", "/api/v1/baskets", "", "", nil, errors.New("pg: secret detail"), 500, `"internal"`},
	}
	for _, tc := range cases {
		st := &fakeStore{err: tc.storeErr}
		mux := http.NewServeMux()
		(&Server{Store: st, Auth: fakeAuth{}, Privy: fakePrivy{tc.privyErr}, QuorumID: "q1"}).Routes(mux)
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		if tc.tok != "" {
			req.Header.Set("Authorization", "Bearer "+tc.tok)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		body, _ := io.ReadAll(w.Body)
		if w.Code != tc.code || !strings.Contains(string(body), tc.bodyHas) {
			t.Errorf("%s: %d %s", tc.name, w.Code, body)
		}
		if tc.name == "delegation ok" && st.calls[1] != "put did:privy:u1 w1 0x00000000000000000000000000000000000000bb q1" {
			t.Errorf("put args: %v", st.calls)
		}
	}
}
