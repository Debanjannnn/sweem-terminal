package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/config"
	"github.com/snehendu098/sweem-terminal/internal/planner"
	"github.com/snehendu098/sweem-terminal/internal/streams"
	"github.com/snehendu098/sweem-terminal/services/api/internal/privy"
)

func ptr[T any](v T) *T { return &v }

func TestValidate(t *testing.T) {
	const uid = "6f1c1b1e-0000-4000-8000-000000000000"
	dca := func(mut func(*ScheduleInput)) ScheduleInput {
		in := ScheduleInput{Kind: "dca", Target: Target{"market", "WETH"}, AmountUSDC: ptr("10.50"), IntervalSeconds: ptr(int64(60)),
			MaxSlippageBps: 50, MaxPerRunUSDC: "10.5", TriggerPrice: ptr("1"), SellBps: ptr(5)}
		if mut != nil {
			mut(&in)
		}
		return in
	}
	sl := func(mut func(*ScheduleInput)) ScheduleInput {
		in := ScheduleInput{Kind: "stop_loss", Target: Target{"basket", uid}, TriggerPrice: ptr("95"), SellBps: ptr(10000),
			MaxSlippageBps: 1000, MaxPerRunUSDC: "100", AmountUSDC: ptr("1")}
		if mut != nil {
			mut(&in)
		}
		return in
	}
	rb := ScheduleInput{Kind: "rebalance", Target: Target{"position", uid}, IntervalSeconds: ptr(int64(3600)), MaxSlippageBps: 50, MaxPerRunUSDC: "1000"}
	cases := []struct {
		in   ScheduleInput
		want string
	}{
		{dca(nil), ""},
		{sl(nil), ""},
		{rb, ""},
		{sl(func(i *ScheduleInput) { i.Kind = "take_profit" }), ""},
		{dca(func(i *ScheduleInput) { i.Target = Target{"basket", "nope"} }), "invalid_target"},
		{dca(func(i *ScheduleInput) { i.Target = Target{"position", uid} }), "invalid_target"},
		{dca(func(i *ScheduleInput) { i.Target = Target{"pool", "x"} }), "invalid_target"},
		{dca(func(i *ScheduleInput) { i.Kind = "recurring" }), "invalid_kind"},
		{dca(func(i *ScheduleInput) { i.AmountUSDC = nil }), "invalid_amount_usdc"},
		{dca(func(i *ScheduleInput) { i.AmountUSDC = ptr("0") }), "invalid_amount_usdc"},
		{dca(func(i *ScheduleInput) { i.IntervalSeconds = ptr(int64(59)) }), "invalid_interval_seconds"},
		{dca(func(i *ScheduleInput) { i.MaxPerRunUSDC = "10.49" }), "max_per_run_below_amount"},
		{dca(func(i *ScheduleInput) { i.MaxSlippageBps = 0 }), "invalid_max_slippage_bps"},
		{sl(func(i *ScheduleInput) { i.TriggerPrice = ptr("-1") }), "invalid_trigger_price"},
		{sl(func(i *ScheduleInput) { i.SellBps = ptr(0) }), "invalid_sell_bps"},
		{sl(func(i *ScheduleInput) { i.Kind = "rebalance"; i.IntervalSeconds = ptr(int64(60)) }), "invalid_drift_bps"},
		{func() ScheduleInput { x := rb; x.Target = Target{"market", "WETH"}; return x }(), "invalid_target"},
	}
	for i, tc := range cases {
		if got := tc.in.Validate(); got != tc.want {
			t.Errorf("case %d: got %q want %q", i, got, tc.want)
		}
	}
	in := dca(nil)
	in.Validate()
	if *in.AmountUSDC != "10.5" || in.TriggerPrice != nil || in.SellBps != nil {
		t.Errorf("dca not normalized: %+v", in)
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
	mu      sync.Mutex
	calls   []string
	execs   []NewExec
	err     error
	wallet  string
	status  string // position status
	strat   Strategy
	settled []string
}

func (f *fakeStore) log(s string) { f.mu.Lock(); f.calls = append(f.calls, s); f.mu.Unlock() }

func (f *fakeStore) EnsureUser(_ context.Context, did string) error {
	f.log("ensure " + did)
	return nil
}
func (f *fakeStore) PutDelegation(_ context.Context, did, wid, addr, q string) (Delegation, error) {
	f.log(strings.Join([]string{"put", did, wid, addr, q}, " "))
	return Delegation{ID: "d1", WalletAddress: addr, Active: true}, f.err
}
func (f *fakeStore) Wallet(context.Context, string) (string, error) {
	if f.wallet == "" {
		return "", ErrNoDelegation
	}
	return f.wallet, nil
}
func (f *fakeStore) InsertExecution(_ context.Context, _ string, e NewExec) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.execs = append(f.execs, e)
	return "e1", f.err
}
func (f *fakeStore) FailExecution(_ context.Context, id, code string) error {
	f.log("fail " + id + " " + code)
	return nil
}
func (f *fakeStore) CreateSchedule(_ context.Context, _ string, in ScheduleInput) (Schedule, error) {
	return Schedule{ID: "s1", Kind: in.Kind, Target: in.Target}, f.err
}
func (f *fakeStore) BasketDef(_ context.Context, _, id string) (Basket, error) {
	return Basket{ID: id, Legs: []planner.Leg{{Market: "WETH", WeightBps: 5000}, {Market: "MON", WeightBps: 5000}}}, f.err
}
func (f *fakeStore) SaveBasket(_ context.Context, did, id string, in BasketInput) (Basket, error) {
	return Basket{ID: "b1", Owner: did, Name: in.Name, Legs: in.Legs}, f.err
}
func (f *fakeStore) Strategy(_ context.Context, id, _ string) (Strategy, error) {
	return f.strat, f.err
}
func (f *fakeStore) Strategies(context.Context, string, bool) ([]Strategy, error) {
	return []Strategy{f.strat}, f.err
}
func (f *fakeStore) HasPerplAccount(context.Context, string, string) (bool, error) { return false, nil }
func (f *fakeStore) OpenPosition(_ context.Context, did, wallet string, st Strategy, principal string, plan planner.Plan, fee, owed string) (string, string, error) {
	f.log(strings.Join([]string{"open", did, wallet, st.ID, principal, fee, owed}, " "))
	f.mu.Lock()
	f.execs = append(f.execs, NewExec{Kind: "deploy", Steps: plan.Steps})
	f.mu.Unlock()
	return "p1", "e2", f.err
}
func (f *fakeStore) SettlePosition(_ context.Context, id, status string) error {
	f.mu.Lock()
	f.settled = append(f.settled, id+" "+status)
	f.mu.Unlock()
	return nil
}
func (f *fakeStore) Position(context.Context, string, string) (Position, planner.Strategy, error) {
	return Position{ID: "p1", Status: f.status, Wallet: f.wallet, Legs: []planner.LegState{{Kind: "lending", Venue: "aave", Asset: "1000000"}}}, f.strat.Strategy, f.err
}
func (f *fakeStore) Peers(context.Context, string, string) ([]planner.Peer, error) { return nil, nil }
func (f *fakeStore) StartWithdraw(_ context.Context, _, _ string, e NewExec) (string, error) {
	f.mu.Lock()
	f.execs = append(f.execs, e)
	f.mu.Unlock()
	return "e3", f.err
}
func (f *fakeStore) SetBacktest(context.Context, string, json.RawMessage) error {
	f.log("backtest")
	return nil
}

// fakeQ quotes WETH at 4000 and MON at 0.03 USDC, minOut = out × (1 − slip).
type fakeQ struct{}

func (fakeQ) Quote(_ context.Context, ob, side string, amount decimal.Decimal, slip int) (planner.Quote, error) {
	px, dec := decimal.NewFromInt(4000), int32(18)
	if ob == "0x065c9d28e428a0db40191a54d33d5b7c71a9c394" {
		px = decimal.RequireFromString("0.03")
	}
	out := amount.Shift(-6).Div(px).Shift(dec).Floor()
	if side == "sell" {
		out = amount.Shift(-dec).Mul(px).Shift(6).Floor()
	}
	min := out.Mul(decimal.NewFromInt(int64(10_000 - slip))).Div(decimal.NewFromInt(10_000)).Floor()
	return planner.Quote{Amount: amount.String(), ExpectedOut: out.String(), MinOut: min.String(), AvgPx: px.String(), MaxSafeAmount: "1e30"}, nil
}

type fakeExec struct {
	mu  sync.Mutex
	ids []string
	out string
	err error
}

func (f *fakeExec) Run(_ context.Context, id string) (planner.Outcome, error) {
	f.mu.Lock()
	f.ids = append(f.ids, id)
	f.mu.Unlock()
	return planner.Outcome{ExecutionID: id, Status: f.out}, f.err
}

// fakeChain returns the same balance for every call.
type fakeChain struct{ bal int64 }

func (f fakeChain) CallContract(context.Context, ethereum.CallMsg, *big.Int) ([]byte, error) {
	return common.LeftPadBytes(big.NewInt(f.bal).Bytes(), 32), nil
}
func (f fakeChain) BalanceAt(context.Context, common.Address, *big.Int) (*big.Int, error) {
	return big.NewInt(f.bal), nil
}

type fakeRates struct{}

func (fakeRates) Rates(context.Context) (streams.RatesSnapshot, error) {
	return streams.RatesSnapshot{Rates: []streams.RateInfo{{Source: "lending", Venue: "aave", Asset: "USDC", Apy: decimal.RequireFromString("0.04")}}}, nil
}

type fakeHistory struct {
	status int
	body   string
	got    []byte
}

func (f *fakeHistory) Backtest(_ context.Context, body []byte) (int, []byte, error) {
	f.got = body
	return f.status, []byte(f.body), nil
}

func testDep(t *testing.T) *config.Deployments143 {
	dep, err := config.LoadDeployments143("../../../../contracts/deployments/143.json")
	if err != nil {
		t.Fatal(err)
	}
	dep.Treasury = "0x7777777777777777777777777777777777777777"
	return dep
}

var usdcStrat = Strategy{ID: "st1", Creator: "did:privy:creator", Name: "s", Public: true,
	Strategy: planner.Strategy{Asset: "USDC", Sources: []planner.Source{{Kind: "lending", Venue: "aave", WeightBps: 10000}}}}

func TestHandlers(t *testing.T) {
	const wallet = `0x00000000000000000000000000000000000000Bb`
	const uid = "6f1c1b1e-0000-4000-8000-000000000000"
	sched := `{"kind":"dca","target":{"type":"market","id":"WETH"},"amountUsdc":"5","intervalSeconds":3600,"maxSlippageBps":50,"maxPerRunUsdc":"5"}`
	cases := []struct {
		name, method, path, tok, body string
		privyErr, storeErr            error
		wallet, posStatus             string
		code                          int
		bodyHas                       string
	}{
		{"markets", "GET", "/api/v1/markets", "", "", nil, nil, "", "", 200, `"symbol":"WETH","orderbook":"0xa6afd386135b7d41a6c40c525abc4a1019b0d132"`},
		{"no token", "GET", "/api/v1/me", "", "", nil, nil, "", "", 401, "unauthorized"},
		{"bad token", "GET", "/api/v1/me/schedules", "nope", "", nil, nil, "", "", 401, "unauthorized"},
		{"delegation ok", "POST", "/api/v1/me/delegation", "good", `{"walletId":"w1","walletAddress":"` + wallet + `"}`, nil, nil, "", "", 200, `"walletAddress":"0x00000000000000000000000000000000000000bb"`},
		{"delegation bad addr", "POST", "/api/v1/me/delegation", "good", `{"walletId":"w1","walletAddress":"0x1"}`, nil, nil, "", "", 400, "bad_request"},
		{"delegation not owned", "POST", "/api/v1/me/delegation", "good", `{"walletId":"w1","walletAddress":"` + wallet + `"}`, privy.ErrNotOwned, nil, "", "", 403, "wallet_not_owned"},
		{"privy down", "POST", "/api/v1/me/delegation", "good", `{"walletId":"w1","walletAddress":"` + wallet + `"}`, errors.New("timeout"), nil, "", "", 502, "privy_unavailable"},
		{"schedule ok", "POST", "/api/v1/me/schedules", "good", sched, nil, nil, "", "", 201, `"target":{"type":"market","id":"WETH"}`},
		{"schedule bad market", "POST", "/api/v1/me/schedules", "good", strings.Replace(sched, "WETH", "PEPE", 1), nil, nil, "", "", 400, "invalid_target"},
		{"schedule no delegation", "POST", "/api/v1/me/schedules", "good", sched, nil, ErrNoDelegation, "", "", 409, "delegation_inactive"},
		{"schedule garbage", "POST", "/api/v1/me/schedules", "good", `{`, nil, nil, "", "", 400, "bad_request"},
		{"trade no delegation", "POST", "/api/v1/me/trade", "good", `{"market":"WETH","side":"buy","amount":"100"}`, nil, nil, "", "", 409, "delegation_inactive"},
		{"trade bad side", "POST", "/api/v1/me/trade", "good", `{"market":"WETH","side":"hold","amount":"100"}`, nil, nil, wallet, "", 400, "bad_request"},
		{"trade cap", "POST", "/api/v1/me/trade", "good", `{"market":"WETH","side":"buy","amount":"1000.000001"}`, nil, nil, wallet, "", 422, "cap_exceeded"},
		{"trade buy", "POST", "/api/v1/me/trade", "good", `{"market":"WETH","side":"buy","amount":"100","slippageBps":50}`, nil, nil, wallet, "", 202,
			`"steps":[{"action":"kuru.marketBuy","market":"WETH","quote":"100000000","minOut":"24875000000000000"}]`},
		{"basket bad legs", "POST", "/api/v1/me/baskets", "good", `{"name":"x","legs":[{"market":"WETH","weightBps":9000}]}`, nil, nil, "", "", 400, "invalid_legs"},
		{"basket create", "POST", "/api/v1/me/baskets", "good", `{"name":" Blue ","legs":[{"market":"WETH","weightBps":10000}],"public":true}`, nil, nil, "", "", 201, `"name":"Blue"`},
		{"basket buy", "POST", "/api/v1/me/baskets/" + uid + "/buy", "good", `{"amountUsdc":"10"}`, nil, nil, wallet, "", 202, `"kind":"basket_buy"`},
		{"basket buy missing", "POST", "/api/v1/me/baskets/" + uid + "/buy", "good", `{"amountUsdc":"10"}`, nil, ErrNotFound, wallet, "", 404, "not_found"},
		{"order below min", "POST", "/api/v1/me/orders", "good", `{"market":"WETH","side":"buy","price":"3600","size":"0.001"}`, nil, nil, wallet, "", 422, "below_min_size"},
		{"deploy", "POST", "/api/v1/me/strategies/" + uid + "/deploy", "good", `{"amountUsdc":"100"}`, nil, nil, wallet, "", 202, `"positionId":"p1","steps":[{"action":"fee.transfer","amount":"400000"},{"action":"lend.supply","venue":"aaveV3","asset":"USDC","amount":"99600000"}]`},
		{"deploy bad amount", "POST", "/api/v1/me/strategies/" + uid + "/deploy", "good", `{"amountUsdc":"-1"}`, nil, nil, wallet, "", 400, "bad_request"},
		{"withdraw not open", "POST", "/api/v1/me/positions/" + uid + "/withdraw", "good", ``, nil, nil, wallet, "closing", 409, "position_not_open"},
		{"withdraw", "POST", "/api/v1/me/positions/" + uid + "/withdraw", "good", ``, nil, nil, wallet, "open", 202, `{"action":"lend.withdraw","venue":"aaveV3","asset":"USDC","amount":"5000000"}`},
		{"strategies apy", "GET", "/api/v1/strategies", "", "", nil, nil, "", "", 200, `"apy":{"estimate":"0.040000"`},
		{"backtest proxy error", "POST", "/api/v1/strategies/" + uid + "/backtest", "", `{"from":"2026-01-01"}`, nil, nil, "", "", 422, `"no_data"`},
		{"bad id", "GET", "/api/v1/me/executions/xyz", "good", "", nil, nil, "", "", 400, "bad_request"},
	}
	for _, tc := range cases {
		st := &fakeStore{err: tc.storeErr, wallet: strings.ToLower(tc.wallet), status: tc.posStatus, strat: usdcStrat}
		ex := &fakeExec{out: "confirmed"}
		hist := &fakeHistory{status: 422, body: `{"error":"no_data","detail":"x"}`}
		srv := &Server{Store: st, Auth: fakeAuth{}, Privy: fakePrivy{tc.privyErr}, QuorumID: "q1",
			Plan: &planner.Planner{Dep: testDep(t), Q: fakeQ{}, MaxStepUSD: 1000}, Exec: ex, Chain: fakeChain{5_000_000}, Rates: fakeRates{}, History: hist}
		mux := http.NewServeMux()
		srv.Routes(mux)
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		if tc.tok != "" {
			req.Header.Set("Authorization", "Bearer "+tc.tok)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		srv.bg.Wait()
		body, _ := io.ReadAll(w.Body)
		if w.Code != tc.code || !strings.Contains(string(body), tc.bodyHas) {
			t.Errorf("%s: %d %s", tc.name, w.Code, body)
		}
		switch tc.name {
		case "delegation ok":
			if st.calls[1] != "put did:privy:u1 w1 0x00000000000000000000000000000000000000bb q1" {
				t.Errorf("put args: %v", st.calls)
			}
		case "trade buy":
			if len(st.execs) != 1 || st.execs[0].Kind != "trade" || len(ex.ids) != 1 || ex.ids[0] != "e1" {
				t.Errorf("trade: execs %+v executor %v", st.execs, ex.ids)
			}
		case "deploy":
			// follower of another creator's strategy: 0.4 fee, 0.2 owed
			if st.calls[1] != "open did:privy:u1 0x00000000000000000000000000000000000000bb st1 100 0.4 0.2" || len(st.settled) != 1 || st.settled[0] != "p1 open" {
				t.Errorf("deploy: %v %v", st.calls, st.settled)
			}
		case "withdraw":
			if len(st.settled) != 1 || st.settled[0] != uid+" closed" || st.execs[0].Kind != "withdraw" {
				t.Errorf("withdraw: %v %+v", st.settled, st.execs)
			}
		case "backtest proxy error":
			if !strings.Contains(string(hist.got), `"legs":[{"kind":"lending","venue":"aave-v3","weight":"1"}]`) || !strings.Contains(string(hist.got), `"from":"2026-01-01"`) {
				t.Errorf("history request: %s", hist.got)
			}
		}
	}
}

// executor down: the pending row is failed with executor_unavailable and the position marked failed.
func TestExecutorDown(t *testing.T) {
	st := &fakeStore{wallet: "0x00000000000000000000000000000000000000bb", strat: usdcStrat}
	srv := &Server{Store: st, Auth: fakeAuth{}, Plan: &planner.Planner{Dep: testDep(t), Q: fakeQ{}, MaxStepUSD: 1000},
		Exec: &fakeExec{err: errors.New("dial tcp: refused")}}
	mux := http.NewServeMux()
	srv.Routes(mux)
	req := httptest.NewRequest("POST", "/api/v1/me/strategies/6f1c1b1e-0000-4000-8000-000000000000/deploy", strings.NewReader(`{"amountUsdc":"10"}`))
	req.Header.Set("Authorization", "Bearer good")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	srv.bg.Wait()
	if w.Code != 202 || !strings.Contains(strings.Join(st.calls, ","), "fail e2 executor_unavailable") || st.settled[0] != "p1 failed" {
		t.Errorf("%d %v %v", w.Code, st.calls, st.settled)
	}
}
