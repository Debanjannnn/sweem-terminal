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
	"time"

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
	last    *LastWithdraw
}

func (f *fakeStore) LastWithdraw(context.Context, string) (LastWithdraw, bool, error) {
	if f.last == nil {
		return LastWithdraw{}, false, nil
	}
	return *f.last, true, nil
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
func (f *fakeStore) Executions(_ context.Context, _ string, before *time.Time) ([]Execution, error) {
	if before != nil {
		f.log("before " + before.UTC().Format(time.RFC3339Nano))
	}
	return []Execution{}, f.err
}
func (f *fakeStore) Earnings(context.Context, string) (Earnings, error) {
	return Earnings{OwedUSDC: "1.5", PaidUSDC: "0", FeesPaidUSDC: "0.4", Rows: []Earning{{ExecutionID: "e1", StrategyID: "st1", FeeUSDC: "3", CreatorOwedUSDC: "1.5"}}}, f.err
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
		{"executions before", "GET", "/api/v1/me/executions?before=2026-10-01T12:00:00.123456%2B02:00", "good", "", nil, nil, "", "", 200, "[]"},
		{"executions bad before", "GET", "/api/v1/me/executions?before=yesterday", "good", "", nil, nil, "", "", 400, "bad_request"},
		{"earnings", "GET", "/api/v1/me/earnings", "good", "", nil, nil, "", "", 200,
			`{"owedUsdc":"1.5","paidUsdc":"0","feesPaidUsdc":"0.4","rows":[{"executionId":"e1","strategyId":"st1","feeUsdc":"3","creatorOwedUsdc":"1.5","paid":false,"createdAt":"0001-01-01T00:00:00Z"}]}`},
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
		case "executions before":
			if st.calls[1] != "before 2026-10-01T10:00:00.123456Z" {
				t.Errorf("before: %v", st.calls)
			}
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
			if !strings.Contains(string(hist.got), `"legs":[{"kind":"lending","venue":"aave","weight":"1"}]`) || !strings.Contains(string(hist.got), `"from":"2026-01-01"`) {
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

// a failed withdraw resumes from step_done with every step capped by live state; nothing left closes the position.
func TestWithdrawResume(t *testing.T) {
	const uid = "6f1c1b1e-0000-4000-8000-000000000000"
	plan := []planner.Step{{Action: "lend.withdraw", Venue: "aaveV3", Asset: "USDC", Amount: "9000000"}}
	cases := []struct {
		name   string
		last   LastWithdraw
		bal    int64
		code   int
		has    string
		settle string
	}{
		{"in flight", LastWithdraw{Status: "submitted", Steps: plan}, 5_000_000, 409, "withdraw_in_progress", ""},
		{"resume capped", LastWithdraw{Status: "failed", Steps: plan}, 5_000_000, 202, `"resumed":true,"steps":[{"action":"lend.withdraw","venue":"aaveV3","asset":"USDC","amount":"5000000"}]`, uid + " closed"},
		{"step already done", LastWithdraw{Status: "failed", Steps: plan, StepDone: 1}, 5_000_000, 200, `"status":"closed"`, uid + " closed"},
		{"landed before crash", LastWithdraw{Status: "failed", Steps: plan}, 0, 200, `"status":"closed"`, uid + " closed"},
	}
	for _, tc := range cases {
		st := &fakeStore{wallet: "0x00000000000000000000000000000000000000bb", status: "withdrawing", strat: usdcStrat, last: &tc.last}
		ex := &fakeExec{out: "failed"}
		srv := &Server{Store: st, Auth: fakeAuth{}, Plan: &planner.Planner{Dep: testDep(t), Q: fakeQ{}, MaxStepUSD: 1000}, Exec: ex, Chain: fakeChain{tc.bal}}
		mux := http.NewServeMux()
		srv.Routes(mux)
		req := httptest.NewRequest("POST", "/api/v1/me/positions/"+uid+"/withdraw", nil)
		req.Header.Set("Authorization", "Bearer good")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		srv.bg.Wait()
		if w.Code != tc.code || !strings.Contains(w.Body.String(), tc.has) {
			t.Errorf("%s: %d %s", tc.name, w.Code, w.Body)
		}
		// a failed run keeps the position withdrawing, never back to open
		want := tc.settle
		if tc.code == 202 {
			want = uid + " withdrawing"
		}
		if (want == "") != (len(st.settled) == 0) || (want != "" && st.settled[0] != want) {
			t.Errorf("%s: settled %v want %q", tc.name, st.settled, want)
		}
	}
}

func (f *fakeStore) CreateStrategy(_ context.Context, did string, in StrategyInput) (Strategy, error) {
	f.strat = Strategy{ID: "st2", Creator: did, Name: in.Name, Strategy: in.Strategy}
	return f.strat, f.err
}

// multi-token strategy: stored with sleeves, backtested as history's sleeves body, APY weighted per sleeve; 1 sleeve folds.
func TestStrategySleeves(t *testing.T) {
	multi := `{"name":"mix","sleeves":[{"asset":"USDC","weightBps":4000,"sources":[{"kind":"lending","venue":"aave","weightBps":10000}]},
		{"asset":"WETH","weightBps":6000,"sources":[{"kind":"basis","venue":"perpl","weightBps":10000,"params":{"leverage":"2"}}]}],"rules":{"driftBps":300}}`
	one := `{"name":"one","sleeves":[{"asset":"USDC","weightBps":10000,"sources":[{"kind":"lending","venue":"aave","weightBps":10000}]}],"rules":{}}`
	run := func(method, path, body string) (*fakeStore, *fakeHistory, int, string) {
		st := &fakeStore{strat: usdcStrat}
		hist := &fakeHistory{status: 422, body: `{"error":"no_data"}`}
		srv := &Server{Store: st, Auth: fakeAuth{}, Plan: &planner.Planner{Dep: testDep(t), Q: fakeQ{}, MaxStepUSD: 1000}, Rates: fakeRates{}, History: hist}
		if method == "GET" {
			st.strat.Strategy = planner.Strategy{}
			json.Unmarshal([]byte(multi), &st.strat.Strategy)
		}
		mux := http.NewServeMux()
		srv.Routes(mux)
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer good")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return st, hist, w.Code, w.Body.String()
	}
	st, hist, code, body := run("POST", "/api/v1/strategies", multi)
	if code != 201 || len(st.strat.Sleeves) != 2 || !strings.Contains(body, `"sleeves":[{"asset":"USDC","weightBps":4000`) {
		t.Fatalf("multi create: %d %s", code, body)
	}
	var got map[string]any
	json.Unmarshal(hist.got, &got)
	want := `{"amount":"1000","sleeves":[` +
		`{"amount":"1","asset":"USDC","legs":[{"kind":"lending","venue":"aave","weight":"1"}],"rules":{"driftBps":"300"},"weight":"0.4"},` +
		`{"amount":"1","asset":"WETH","legs":[{"kind":"basis","perpId":20,"venue":"","weight":"1"}],"rules":{"basisLeverage":"2","driftBps":"300"},"weight":"0.6"}]}`
	if b, _ := json.Marshal(got); string(b) != want {
		t.Errorf("history body:\n got %s\nwant %s", b, want)
	}
	if st, _, code, _ = run("POST", "/api/v1/strategies", one); code != 201 || st.strat.Asset != "USDC" || len(st.strat.Sources) != 1 || st.strat.Sleeves != nil {
		t.Errorf("one sleeve must fold: %d %+v", code, st.strat)
	}
	if _, _, code, body = run("POST", "/api/v1/strategies", strings.Replace(multi, "4000", "3000", 1)); code != 400 || !strings.Contains(body, "invalid_sleeves") {
		t.Errorf("bad weights: %d %s", code, body)
	}
	// aave 4% on the 40% USDC sleeve; no ETH funding rate → 0
	if _, _, code, body = run("GET", "/api/v1/strategies", ""); code != 200 || !strings.Contains(body, `"apy":{"estimate":"0.016000","sources":[{"asset":"USDC","kind":"lending","venue":"aave","apy":"0.040000"},{"asset":"WETH","kind":"basis","venue":"perpl","apy":null}]}`) {
		t.Errorf("multi apy: %d %s", code, body)
	}
}
