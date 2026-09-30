package sched

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/config"
	"github.com/snehendu098/sweem-terminal/internal/httpx"
	"github.com/snehendu098/sweem-terminal/internal/planner"
)

var d = decimal.RequireFromString

const fixtures = "../../../../testdata/contracts/executor/"

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(fixtures + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func testDep(t *testing.T) *config.Deployments143 {
	dep, err := config.LoadDeployments143("../../../../contracts/deployments/143.json")
	if err != nil {
		t.Fatal(err)
	}
	return dep
}

// fakeQ prices markets at fixed USDC prices; minOut = expected × (1 − slip).
type fakeQ struct {
	dep *config.Deployments143
	mu  sync.Mutex
	px  map[string]string
}

func (f *fakeQ) Quote(_ context.Context, ob, side string, amount decimal.Decimal, slip int) (planner.Quote, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var sym string
	for s, m := range f.dep.Kuru.Markets {
		if strings.EqualFold(m.Orderbook, ob) {
			sym = s
		}
	}
	px, dec := d(f.px[sym]), int32(f.dep.Tokens[f.dep.Kuru.Markets[sym].Base].Decimals)
	out := amount.Shift(-dec).Mul(px).Shift(6).Floor()
	if side == "buy" {
		out = amount.Shift(-6).Div(px).Shift(dec).Floor()
	}
	minOut := out.Mul(decimal.NewFromInt(int64(10_000 - slip))).Div(decimal.NewFromInt(10_000)).Floor()
	return planner.Quote{Amount: amount.String(), Fillable: amount.String(), ExpectedOut: out.String(), MinOut: minOut.String(),
		AvgPx: px.String(), MaxSafeAmount: "1e30"}, nil
}

type fakeChain struct {
	mu     sync.Mutex
	wallet map[string]decimal.Decimal
	bals   []planner.Balance
	perp   Perp
}

func (f *fakeChain) Wallet(_ context.Context, _, sym string) (decimal.Decimal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.wallet[sym], nil
}

func (f *fakeChain) Balances(context.Context, string, planner.Strategy, []planner.LegState, []planner.Peer) ([]planner.Balance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bals, nil
}

func (f *fakeChain) Live(context.Context, string, planner.Strategy, []planner.LegState, []planner.Peer) (planner.Live, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return planner.Live{Bals: f.bals, Wallet: f.wallet, PerpLot: f.perp.Lot, PerpFree: f.perp.Free}, nil
}

func (f *fakeChain) Perp(context.Context, string, int) (Perp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.perp, nil
}

// fakeExecutor is the executor's steps route: internal auth, request byte-equal to the fixture shape, fixture response.
type fakeExecutor struct {
	*httptest.Server
	mu   sync.Mutex
	ids  []string
	fail bool
}

func newFakeExecutor(t *testing.T) *fakeExecutor {
	f := &fakeExecutor{}
	want := strings.TrimSpace(string(fixture(t, "user_steps.request.json")))
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct{ ExecutionID string }
		json.Unmarshal(body, &req)
		if strings.Replace(want, "6f1c2a4e-2b7d-4c1e-9a0b-3f5d8e7c1a20", req.ExecutionID, 1) != string(body) {
			t.Errorf("request %s != fixture %s", body, want)
		}
		f.mu.Lock()
		f.ids = append(f.ids, req.ExecutionID)
		fail := f.fail
		f.mu.Unlock()
		if fail {
			w.WriteHeader(http.StatusConflict)
			w.Write(fixture(t, "error.response.json"))
			return
		}
		w.Write(fixture(t, "user_steps.response.json"))
	})
	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/user/steps", httpx.RequireInternal("s3cret", "api", "scheduler")(h))
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func (f *fakeExecutor) IDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.ids...)
}

func TestExecutorContract(t *testing.T) {
	f := newFakeExecutor(t)
	x := planner.Executor{URL: f.URL, Secret: "s3cret", Caller: "scheduler", HTTP: http.DefaultClient}
	var want planner.Outcome
	json.Unmarshal(fixture(t, "user_steps.response.json"), &want)
	got, err := x.Run(context.Background(), "6f1c2a4e-2b7d-4c1e-9a0b-3f5d8e7c1a20")
	if err != nil || !reflect.DeepEqual(got, want) || got.Status != "confirmed" || got.StepsDone != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	var failed planner.Outcome
	if err := json.Unmarshal(fixture(t, "user_steps_failed.response.json"), &failed); err != nil || *failed.Error != "chain_error" {
		t.Fatal(failed, err)
	}
	f.fail = true
	var pe *planner.Error
	if _, err := x.Run(context.Background(), "x"); !errors.As(err, &pe) || pe.Code != "delegation_inactive" {
		t.Fatalf("err = %v", err)
	}
	if _, err := (planner.Executor{URL: f.URL, Secret: "wrong", Caller: "scheduler", HTTP: http.DefaultClient}).Run(context.Background(), "x"); !errors.As(err, &pe) || pe.Detail != "executor 401" {
		t.Fatalf("bad secret: %v", err)
	}
}

func TestSlotAndNextRun(t *testing.T) {
	iv := int64(3600)
	next := time.Unix(1_000_000_800, 0)
	if s := Slot(next, iv); s != 1_000_000_800/3600 {
		t.Fatal(s)
	}
	if n := NextRun(next, iv, next); Slot(n, iv) != Slot(next, iv)+1 {
		t.Fatal(n)
	}
	now := next.Add(5*time.Hour + 30*time.Minute) // late: skip to first future slot, no backfill
	if n := NextRun(next, iv, now); !n.Equal(next.Add(6 * time.Hour)) {
		t.Fatal(n)
	}
	if n := NextRun(next, iv, next.Add(3*time.Hour)); !n.Equal(next.Add(4 * time.Hour)) {
		t.Fatal(n)
	}
}

func TestHit(t *testing.T) {
	cases := []struct {
		kind, v, trig string
		want          bool
	}{
		{"take_profit", "4000", "4000", true}, {"take_profit", "3999", "4000", false},
		{"stop_loss", "2999", "3000", true}, {"stop_loss", "3001", "3000", false},
		{"stop_loss", "0", "3000", false}, {"take_profit", "5", "0", false},
	}
	for _, c := range cases {
		if Hit(c.kind, d(c.v), d(c.trig)) != c.want {
			t.Errorf("%+v", c)
		}
	}
}

func TestDrift(t *testing.T) {
	if got := Drift([]decimal.Decimal{d("60"), d("40")}, []int{5000, 5000}); got != 1000 {
		t.Fatal(got)
	}
	if got := Drift([]decimal.Decimal{d("0"), d("0")}, []int{5000, 5000}); got != 0 {
		t.Fatal(got)
	}
	if got := Drift([]decimal.Decimal{d("30"), d("30"), d("40")}, []int{3000, 3000, 4000}); got != 0 {
		t.Fatal(got)
	}
}

func TestGateHysteresis(t *testing.T) {
	th, band := d("0.05"), d("0.02")
	cases := []struct {
		open bool
		apr  string
		want bool
	}{
		{true, "0.05", true}, {true, "0.0499", false}, // open: stays until below threshold
		{false, "0.06", false}, {false, "0.07", false}, {false, "0.0701", true}, // closed: reopens only above threshold+band
	}
	for _, c := range cases {
		if Gate(c.open, d(c.apr), th, band) != c.want {
			t.Errorf("%+v", c)
		}
	}
}

func TestTargets(t *testing.T) {
	srcs := []planner.Source{{Kind: "lending", Venue: "aave", WeightBps: 4000}, {Kind: "lending", Venue: "euler-k3weth", WeightBps: 3000}, {Kind: "basis", Venue: "perpl", WeightBps: 3000}}
	apy := []decimal.Decimal{d("0.03"), d("0.06"), decimal.Zero}
	g := func(x string) *decimal.Decimal { v := d(x); return &v }
	cases := []struct {
		on      bool
		minGain *decimal.Decimal
		want    []int
	}{
		{true, nil, []int{4000, 3000, 3000}},
		{false, nil, []int{4000, 6000, 0}},         // gated basis parks in best lending
		{true, g("0.02"), []int{0, 7000, 3000}},    // aave loses by 3% > 2%
		{true, g("0.05"), []int{4000, 3000, 3000}}, // gain too small after costs
	}
	for _, c := range cases {
		if got := Targets(srcs, []bool{false, false, c.on}, apy, c.minGain); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%+v: %v", c, got)
		}
	}
	noLend := []planner.Source{{Kind: "staking", Venue: "shmonad", WeightBps: 5000}, {Kind: "basis", Venue: "perpl", WeightBps: 5000}}
	if got := Targets(noLend, []bool{false, false}, []decimal.Decimal{decimal.Zero, decimal.Zero}, nil); !reflect.DeepEqual(got, []int{5000, 5000}) {
		t.Errorf("no lending: %v", got)
	}
}

func TestGuard(t *testing.T) {
	// 1 ETH short (lot 1000, 3 lot dec) at 2600.00 (2 price dec), 1300 AUSD collateral, maintenance 20x = 5%
	p := func(mark int64) Perp {
		pnl := decimal.NewFromInt(260000 - mark).Mul(d("10000")) // (entry − mark) × 1 ETH in 6dp
		return Perp{Account: 7, Deposit: d("1300000000"), PnL: pnl, Mark: decimal.NewFromInt(mark), Lot: d("1000"),
			PriceDec: 2, LotDec: 3, MaintHdths: d("2000"), MarkValid: true}
	}
	buf, rich := d("0.03"), d("500000000")
	for _, mark := range []int64{260000, 350000} { // 50%, 11.4% margin
		if g := Guard(p(mark), buf, rich); g.Fire {
			t.Fatal(mark, g)
		}
	}
	// mark 3700: equity 200 / 3700 = 5.4% < 8%; target 11% × 3700 = 407
	cases := []struct {
		free, wallet, topUp, cut, released string
	}{
		{"0", "500000000", "209090910", "0", "0"},          // wallet covers 207 / 0.99 swap haircut
		{"50000000", "500000000", "158585859", "0", "0"},   // free balance first: 157 / 0.99
		{"300000000", "0", "0", "0", "0"},                  // free balance alone covers it
		{"0", "100000000", "100000000", "266", "52215800"}, // keep 1000 × (200 + 99) / 407 = 734; re-add (200 − 3.7) × 0.266
		{"0", "500000", "0", "509", "99916700"},            // dust wallet: keep 1000 × 200 / 407 = 491
	}
	for _, c := range cases {
		pp := p(370000)
		pp.Free = d(c.free)
		g := Guard(pp, buf, d(c.wallet))
		if !g.Fire || g.CloseAll || !g.TopUp.Equal(d(c.topUp)) || !g.LotCut.Equal(d(c.cut)) || !g.Released.Equal(d(c.released)) {
			t.Errorf("%+v: %+v", c, g)
		}
	}
	if g := Guard(p(390000), buf, decimal.Zero); !g.Fire || !g.CloseAll { // equity 0: last resort
		t.Errorf("wiped: %+v", g)
	}
	stale := p(370000)
	stale.MarkValid = false
	if Guard(stale, buf, rich).Fire {
		t.Fatal("invalid mark fired")
	}
	closed := p(370000)
	closed.Lot = decimal.Zero
	if Guard(closed, buf, rich).Fire {
		t.Fatal("no position fired")
	}
}

func TestPerplABI(t *testing.T) {
	for _, m := range []string{"getAccountByAddr", "getPosition", "getPerpetualInfo", "getMarginFractions"} {
		if _, ok := perplABI.Methods[m]; !ok {
			t.Fatal(m)
		}
	}
	out := perplABI.Methods["getPosition"].Outputs
	if len(out) != 3 || out[0].Type.TupleRawNames == nil {
		t.Fatalf("%+v", out)
	}
}
