package backtest

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func day(s string) time.Time { t, _ := time.Parse(Day, s); return t }

func flat(from string, n int, v float64) map[string]float64 {
	m := map[string]float64{}
	for i := 0; i < n; i++ {
		m[day(from).AddDate(0, 0, i).Format(Day)] = v
	}
	return m
}

func near(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-6*math.Max(1, math.Abs(want)) {
		t.Errorf("%s = %.9f, want %.9f", name, got, want)
	}
}

func run(t *testing.T, in Input, s Series) *Result {
	t.Helper()
	res, err := Run(in, s)
	if err != nil {
		t.Fatal(err)
	}
	sum := 0.0
	for _, v := range res.Contributions {
		sum += v
	}
	near(t, "contributions sum", sum, res.FinalUSD-res.InitialUSD)
	return res
}

var rules = Rules{DriftBps: 500, BasisLeverage: 1, SlippageBps: 10}

func TestLendingCompoundsToAPY(t *testing.T) {
	in := Input{Asset: "USDC", Amount: 1000, From: day("2026-01-01"), To: day("2027-01-01"),
		Legs: []Leg{{Kind: "lending", Venue: "aave-v3", Weight: 1}}, Rules: rules}
	res := run(t, in, Series{APY: map[string]map[string]float64{"lending:aave-v3": flat("2025-12-01", 500, 0.05)}})
	near(t, "final", res.FinalUSD, 1050)
	near(t, "apy", res.APYUSD, 0.05)
	near(t, "yield", res.Contributions["lending:aave-v3"], 50)
	if res.Days != 365 || len(res.Curve) != 366 || res.Rebalances != 0 || res.CostsUSD != 0 {
		t.Fatalf("%+v", res)
	}
}

func TestPriceMoveInAssetAndUSD(t *testing.T) {
	price := map[string]float64{"2026-01-01": 2000, "2026-01-02": 1000, "2026-01-03": 3000}
	in := Input{Asset: "WETH", Amount: 2, From: day("2026-01-01"), To: day("2026-01-03"),
		Legs: []Leg{{Kind: "lending", Venue: "aave-v3", Weight: 1}}, Rules: rules}
	res := run(t, in, Series{Price: price, APY: map[string]map[string]float64{"lending:aave-v3": flat("2026-01-01", 3, 0)}})
	near(t, "final usd", res.FinalUSD, 6000)
	near(t, "final asset", res.FinalAsset, 2)
	near(t, "hodl", res.HodlFinalUSD, 6000)
	near(t, "dd usd", res.MaxDrawdownUSD, 0.5)
	near(t, "dd asset", res.MaxDrawdownAsset, 0)
	near(t, "price contrib", res.Contributions["price"], 2000)
}

func TestBasisFundingAndCosts(t *testing.T) {
	in := Input{Asset: "USDC", Amount: 1000, From: day("2026-03-01"), To: day("2026-03-11"),
		Legs: []Leg{{Kind: "basis", PerpID: 20, Weight: 1}}, Rules: rules}
	res := run(t, in, Series{Funding: map[int]map[string]float64{20: flat("2026-03-01", 11, 0.0001)}})
	bps := (10 + PerplTakerBps) * 0.5 // leverage 1: half spot, half collateral
	v := 1000 * (1 - bps/1e4) * math.Pow(1+0.0001*0.5, 10)
	near(t, "final", res.FinalUSD, v*(1-bps/1e4))
	near(t, "exit", res.ExitCostUSD, v*bps/1e4)
	if !res.Curve[0].BasisActive {
		t.Fatal("no gate: basis should be active")
	}
}

func TestFundingGateParksInBestLending(t *testing.T) {
	f := flat("2026-03-01", 10, -0.0002)
	for d, v := range flat("2026-03-06", 5, 0.0003) {
		f[d] = v
	}
	r := rules
	r.GateLookbackDays, r.GateAPR, r.DriftBps = 1, 0, 100
	in := Input{Asset: "USDC", Amount: 1000, From: day("2026-03-01"), To: day("2026-03-10"),
		Legs: []Leg{{Kind: "lending", Venue: "aave-v3", Weight: 0.5}, {Kind: "basis", PerpID: 20, Weight: 0.5}}, Rules: r}
	res := run(t, in, Series{
		APY: map[string]map[string]float64{
			"lending:aave-v3":   flat("2026-01-01", 100, 0.03),
			"lending:morpho":    flat("2026-01-01", 100, 0.08),
			"lending:neverland": {"2026-03-01": 0.01, "2026-03-03": 0.5}, // becomes best while parked: must not trigger a hop
		},
		Funding: map[int]map[string]float64{20: f},
	})
	for i, p := range res.Curve {
		if want := i >= 5; p.BasisActive != want {
			t.Fatalf("day %d active=%v", i, p.BasisActive)
		}
	}
	if res.Rebalances != 1 {
		t.Fatalf("rebalances %d, want 1 (gate open on day 5; parking is sticky)", res.Rebalances)
	}
	if res.Contributions["lending:morpho"] <= 0 || res.Contributions["lending:neverland"] != 0 {
		t.Fatalf("parked weight should earn in morpho only: %v", res.Contributions)
	}
	// basis earns only on days 6..9 (after rebalance on day 5), never pays the negative funding
	if c := res.Contributions["basis:20"]; c <= 0 {
		t.Fatalf("basis contrib %v", c)
	}
}

func TestGateWithoutDataStaysClosed(t *testing.T) {
	r := rules
	r.GateLookbackDays = 7
	in := Input{Asset: "USDC", Amount: 100, From: day("2026-03-01"), To: day("2026-03-02"),
		Legs: []Leg{{Kind: "basis", PerpID: 20, Weight: 1}}, Rules: r}
	res := run(t, in, Series{Funding: map[int]map[string]float64{20: {"2026-03-02": 0.01}}})
	if res.From != "2026-03-02" || res.Curve[0].BasisActive != true {
		t.Fatalf("%+v", res)
	}
	res = run(t, in, Series{Funding: map[int]map[string]float64{20: {"2026-03-01": -0.01, "2026-03-02": -0.01}}})
	if res.Curve[0].BasisActive || res.FinalUSD != 100 {
		t.Fatalf("closed gate with no lending should hold idle: %+v", res.Curve)
	}
}

func TestDriftRebalanceAndMinInterval(t *testing.T) {
	price := map[string]float64{}
	for i := 0; i < 5; i++ {
		price[day("2026-05-01").AddDate(0, 0, i).Format(Day)] = 1000 * float64(i+1)
	}
	mk := func(drift float64, minDays int) *Result {
		r := rules
		r.DriftBps, r.MinIntervalDays = drift, minDays
		in := Input{Asset: "WETH", Amount: 1, From: day("2026-05-01"), To: day("2026-05-05"),
			Legs: []Leg{{Kind: "lending", Venue: "aave-v3", Weight: 0.5}, {Kind: "basis", PerpID: 20, Weight: 0.5}}, Rules: r}
		return run(t, in, Series{Price: price,
			APY:     map[string]map[string]float64{"lending:aave-v3": flat("2026-05-01", 5, 0)},
			Funding: map[int]map[string]float64{20: flat("2026-05-01", 5, 0)}})
	}
	if n := mk(1e9, 0).Rebalances; n != 0 {
		t.Fatalf("wide band rebalanced %d", n)
	}
	if n := mk(100, 0).Rebalances; n != 4 {
		t.Fatalf("tight band rebalances %d, want 4", n)
	}
	if n := mk(100, 2).Rebalances; n != 2 {
		t.Fatalf("min interval 2: rebalances %d, want 2", n)
	}
	// no rebalance: lending half ×5, basis half flat
	entry := 500 * (10 + PerplTakerBps) * 0.5 / 1e4
	near(t, "unrebalanced", mk(1e9, 0).Curve[4].USD, (1000-entry)/2*6)
}

func TestStakingExitHaircut(t *testing.T) {
	in := Input{Asset: "MON", Amount: 1000, From: day("2026-01-01"), To: day("2026-01-02"),
		Legs: []Leg{{Kind: "staking", Venue: "shmonad", Weight: 1}}, Rules: rules}
	res := run(t, in, Series{Price: flat("2026-01-01", 2, 0.02), APY: map[string]map[string]float64{"staking:shmonad": flat("2026-01-01", 2, 0)}})
	near(t, "final asset", res.FinalAsset, 991)
}

func TestLPEstimated(t *testing.T) {
	r := rules
	r.SlippageBps = 0
	in := Input{Asset: "MON", Amount: 100, From: day("2026-01-01"), To: day("2026-01-02"),
		Legs: []Leg{{Kind: "lp", Venue: "kuru-activevault", Weight: 1}}, Rules: r}
	res := run(t, in, Series{Price: map[string]float64{"2026-01-01": 1, "2026-01-02": 4}})
	near(t, "lp usd", res.FinalUSD, 200) // sqrt(4) × 100, LPAssumedAPR 0
	if !res.Estimated {
		t.Fatal("lp must be flagged estimated")
	}
}

func TestCoverageClamp(t *testing.T) {
	in := Input{Asset: "WETH", Amount: 1, From: day("2026-01-01"), To: day("2026-12-31"),
		Legs: []Leg{{Kind: "lending", Venue: "aave-v3", Weight: 1}}, Rules: rules}
	res := run(t, in, Series{Price: flat("2025-12-01", 400, 3000), APY: map[string]map[string]float64{"lending:aave-v3": flat("2026-07-02", 60, 0.05)}})
	if res.From != "2026-07-02" || res.To != "2026-08-30" {
		t.Fatalf("window %s..%s", res.From, res.To)
	}
	c := res.Coverage[1]
	if c.Series != "rate:lending:aave-v3:WETH" || !strings.Contains(c.Note, "starts after") || !strings.Contains(c.Note, "ends before") {
		t.Fatalf("%+v", res.Coverage)
	}
	_, err := Run(in, Series{Price: flat("2025-12-01", 400, 3000)})
	var e *Error
	if !errors.As(err, &e) || e.Code != "no_data" {
		t.Fatal(err)
	}
	_, err = Run(in, Series{Price: flat("2025-01-01", 10, 3000), APY: map[string]map[string]float64{"lending:aave-v3": flat("2026-07-02", 60, 0.05)}})
	if !errors.As(err, &e) || e.Code != "no_data" {
		t.Fatal("non-overlap", err)
	}
}

func TestForwardFillGaps(t *testing.T) {
	in := Input{Asset: "USDC", Amount: 1000, From: day("2026-01-01"), To: day("2026-01-11"),
		Legs: []Leg{{Kind: "lending", Venue: "x", Weight: 1}}, Rules: rules}
	res := run(t, in, Series{APY: map[string]map[string]float64{"lending:x": {"2025-12-25": 0.1, "2026-01-11": 0.1}}})
	near(t, "ffill", res.FinalUSD, 1000*math.Pow(1.1, 10.0/365))
}

func TestValidation(t *testing.T) {
	ok := Input{Asset: "USDC", Amount: 1, From: day("2026-01-01"), To: day("2026-01-02"),
		Legs: []Leg{{Kind: "lending", Venue: "aave-v3", Weight: 1}}, Rules: rules}
	cases := map[string]func(*Input){
		"asset":       func(i *Input) { i.Asset = "DOGE" },
		"amount":      func(i *Input) { i.Amount = 0 },
		"window":      func(i *Input) { i.From = day("2026-02-01") },
		"weights":     func(i *Input) { i.Legs[0].Weight = 0.9 },
		"staking usd": func(i *Input) { i.Legs[0] = Leg{Kind: "staking", Venue: "shmonad", Weight: 1} },
		"perp":        func(i *Input) { i.Legs[0] = Leg{Kind: "basis", PerpID: 99, Weight: 1} },
		"leverage":    func(i *Input) { i.Legs[0] = Leg{Kind: "basis", PerpID: 10, Weight: 1}; i.Rules.BasisLeverage = 11 },
		"kind":        func(i *Input) { i.Legs[0].Kind = "farm" },
		"dup":         func(i *Input) { i.Legs = []Leg{i.Legs[0], i.Legs[0]}; i.Legs[0].Weight, i.Legs[1].Weight = 0.5, 0.5 },
	}
	for name, mod := range cases {
		in := ok
		in.Legs = append([]Leg(nil), ok.Legs...)
		mod(&in)
		var e *Error
		if _, err := Run(in, Series{}); !errors.As(err, &e) || e.Code != "bad_request" {
			t.Errorf("%s: %v", name, err)
		}
	}
}
