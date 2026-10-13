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
	"github.com/snehendu098/sweem-terminal/internal/perpl"
	"github.com/snehendu098/sweem-terminal/internal/planner"
	"github.com/snehendu098/sweem-terminal/internal/streams"
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
	perp   perpl.Perp
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
	return planner.Live{Bals: f.bals, Wallet: f.wallet, PerpLot: map[string]decimal.Decimal{"ETH": f.perp.Lot, "BTC": f.perp.Lot, "MON": f.perp.Lot}, PerpFree: f.perp.Free}, nil
}

func (f *fakeChain) Perp(context.Context, string, int) (perpl.Perp, error) {
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

func TestFundingAPRNoLookback(t *testing.T) { // days == 0 never touches the DB
	snap := streams.RatesSnapshot{Funding: []streams.FundingInfo{{PerpID: 20, AprShort7d: d("0.2")}}}
	s := &Scheduler{}
	if apr, ok, err := s.fundingAPR(context.Background(), snap, 20, 0); err != nil || !ok || !apr.Equal(d("0.2")) {
		t.Fatal(apr, ok, err)
	}
	if _, ok, _ := s.fundingAPR(context.Background(), snap, 30, 0); ok {
		t.Fatal("unknown perp")
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
