// Package backtest simulates an Earn strategy day by day over historical series. Pure: no I/O.
package backtest

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
)

const (
	Day           = "2006-01-02"
	PerplTakerBps = 3.45
)

var (
	Assets  = map[string]bool{"USDC": true, "AUSD": true, "WETH": true, "cbBTC": true, "MON": true, "XAUt0": true}
	Stables = map[string]bool{"USDC": true, "AUSD": true}
	// Perpl perpId (1 BTC, 10 MON, 20 ETH) -> max leverage.
	MaxLeverage = map[int]float64{1: 15, 10: 10, 20: 12}
	// Instant-exit haircuts: shMON redeem ~0.9%, Kintsu 10 bps.
	exitBps = map[string]float64{"staking:shmonad": 90, "staking:kintsu": 10}
)

type Leg struct {
	Kind   string // lending | staking | lp | basis
	Venue  string // lending/staking venue; lp: kuru-activevault
	PerpID int    // basis
	Weight float64
}

func (l Leg) Key() string {
	if l.Kind == "basis" {
		return fmt.Sprintf("basis:%d", l.PerpID)
	}
	return l.Kind + ":" + l.Venue
}

type Rules struct {
	DriftBps         float64
	MinIntervalDays  int
	GateAPR          float64 // basis leg active only while trailing funding APR >= this
	GateLookbackDays int     // 0 disables the gate
	BasisLeverage    float64
	SlippageBps      float64 // Kuru spot swap cost (taker fee is 0 on V1)
	LPAssumedAPR     float64
}

type Input struct {
	Asset    string
	Amount   float64 // in asset units
	From, To time.Time
	Legs     []Leg
	Rules    Rules
}

// Series: all keyed by day "2006-01-02".
type Series struct {
	Price   map[string]float64            // asset USD close; ignored for stables
	APY     map[string]map[string]float64 // "lending:<venue>" | "staking:<venue>" -> apy fraction; every lending key is a gate parking candidate
	Funding map[int]map[string]float64    // perpId -> sum of interval rates that day (shorts receive when > 0)
}

type Coverage struct {
	Series string `json:"series"`
	First  string `json:"first"`
	Last   string `json:"last"`
	Days   int    `json:"days"`
	Note   string `json:"note,omitempty"`
}

type Point struct {
	Day         string
	USD, Asset  float64
	HodlUSD     float64
	BasisActive bool
}

type Result struct {
	Asset              string
	From, To           string // effective window after clamping to data coverage
	Days               int
	Coverage           []Coverage
	Estimated          bool // LP leg uses an assumed APR, not venue history
	InitialUSD         float64
	FinalUSD           float64 // after exit costs
	FinalAsset         float64
	APYUSD, APYAsset   float64
	MaxDrawdownUSD     float64
	MaxDrawdownAsset   float64
	HodlFinalUSD       float64
	HodlAPYUSD         float64
	HodlMaxDrawdownUSD float64
	Rebalances         int
	CostsUSD           float64 // rebalance + entry + exit
	ExitCostUSD        float64
	Contributions      map[string]float64 // per source yield, plus "price" and "costs"; sums to FinalUSD-InitialUSD
	Curve              []Point
}

type Error struct{ Code, Msg string }

func (e *Error) Error() string { return e.Code + ": " + e.Msg }

func bad(f string, a ...any) error { return &Error{"bad_request", fmt.Sprintf(f, a...)} }

func validate(in Input) error {
	if !Assets[in.Asset] {
		return bad("unknown asset %q", in.Asset)
	}
	if !(in.Amount > 0) {
		return bad("amount must be > 0")
	}
	if in.From.After(in.To) {
		return bad("from after to")
	}
	if len(in.Legs) == 0 {
		return bad("no legs")
	}
	r, sum, seen := in.Rules, 0.0, map[string]bool{}
	for _, l := range in.Legs {
		if !(l.Weight >= 0) {
			return bad("negative weight")
		}
		if seen[l.Key()] {
			return bad("duplicate leg %s", l.Key())
		}
		seen[l.Key()] = true
		sum += l.Weight
		switch l.Kind {
		case "lending":
			if l.Venue == "" {
				return bad("lending leg needs venue")
			}
		case "staking", "lp":
			if in.Asset != "MON" {
				return bad("%s legs need asset MON", l.Kind)
			}
			if l.Kind == "staking" && exitBps[l.Key()] == 0 {
				return bad("unknown staking venue %q", l.Venue)
			}
			if l.Kind == "lp" && l.Venue != "kuru-activevault" {
				return bad("unknown lp venue %q", l.Venue)
			}
		case "basis":
			max, ok := MaxLeverage[l.PerpID]
			if !ok {
				return bad("unknown perpId %d", l.PerpID)
			}
			if r.BasisLeverage < 1 || r.BasisLeverage > max {
				return bad("basisLeverage must be in [1,%g] for perp %d", max, l.PerpID)
			}
		default:
			return bad("unknown leg kind %q", l.Kind)
		}
	}
	if math.Abs(sum-1) > 1e-9 {
		return bad("weights sum to %g, want 1", sum)
	}
	if r.DriftBps < 0 || r.MinIntervalDays < 0 || r.GateLookbackDays < 0 || r.SlippageBps < 0 {
		return bad("rules must be non-negative")
	}
	return nil
}

func span(m map[string]float64) (first, last string, n int) {
	for d := range m {
		if first == "" || d < first {
			first = d
		}
		if d > last {
			last = d
		}
	}
	return first, last, len(m)
}

// dense forward-fills m over n days from start, seeded with the last value on or before start (NaN before any data).
func dense(m map[string]float64, start time.Time, n int) []float64 {
	out, cur, seedDay, s0 := make([]float64, n), math.NaN(), "", start.Format(Day)
	for d, v := range m {
		if d <= s0 && d > seedDay {
			seedDay, cur = d, v
		}
	}
	for i := range out {
		if v, ok := m[start.AddDate(0, 0, i).Format(Day)]; ok {
			cur = v
		}
		out[i] = cur
	}
	return out
}

func Run(in Input, s Series) (*Result, error) {
	if err := validate(in); err != nil {
		return nil, err
	}
	stable := Stables[in.Asset]
	type req struct {
		name string
		m    map[string]float64
	}
	var reqs []req
	if !stable {
		reqs = append(reqs, req{"price:" + in.Asset, s.Price})
	}
	for _, l := range in.Legs {
		switch l.Kind {
		case "lending", "staking":
			reqs = append(reqs, req{"rate:" + l.Key() + ":" + in.Asset, s.APY[l.Key()]})
		case "basis":
			reqs = append(reqs, req{fmt.Sprintf("funding:%d", l.PerpID), s.Funding[l.PerpID]})
		}
	}
	from, to := in.From.Format(Day), in.To.Format(Day)
	res := &Result{Asset: in.Asset, Contributions: map[string]float64{"price": 0, "costs": 0}}
	for _, r := range reqs {
		first, last, n := span(r.m)
		if n == 0 {
			return nil, &Error{"no_data", "no data for " + r.name}
		}
		c := Coverage{Series: r.name, First: first, Last: last, Days: n}
		if first > from {
			from, c.Note = first, "starts after requested window; window clamped"
		}
		if last < to {
			to, c.Note = last, strings.TrimPrefix(c.Note+"; ends before requested window; window clamped", "; ")
		}
		res.Coverage = append(res.Coverage, c)
	}
	if from > to {
		return nil, &Error{"no_data", "series do not overlap the requested window"}
	}
	start, _ := time.Parse(Day, from)
	end, _ := time.Parse(Day, to)
	N := int(end.Sub(start).Hours()/24) + 1
	res.From, res.To, res.Days = from, to, N-1

	price := make([]float64, N)
	if stable {
		for i := range price {
			price[i] = 1
		}
	} else {
		price = dense(s.Price, start, N)
	}
	apy := map[string][]float64{}
	var lendKeys []string
	for k, m := range s.APY {
		apy[k] = dense(m, start, N)
		if strings.HasPrefix(k, "lending:") {
			lendKeys = append(lendKeys, k)
		}
	}
	slices.Sort(lendKeys)

	perpOf := map[string]int{}
	for _, l := range in.Legs {
		perpOf[l.Key()] = l.PerpID
	}
	r := in.Rules
	L := r.BasisLeverage
	spotShare := L / (L + 1) // capital split: spot s + collateral s/L
	daily := func(annual float64) float64 { return math.Pow(1+annual, 1.0/365) - 1 }

	gateOpen := func(perp, i int) bool {
		if r.GateLookbackDays == 0 {
			return true
		}
		sum, n := 0.0, 0
		for j := 0; j < r.GateLookbackDays; j++ {
			if v, ok := s.Funding[perp][start.AddDate(0, 0, i-j).Format(Day)]; ok {
				sum, n = sum+v, n+1
			}
		}
		return n > 0 && sum/float64(n)*365 >= r.GateAPR
	}
	parked := map[string]string{} // basis leg -> venue chosen when its gate closed; sticky until it reopens
	targets := func(i int) (map[string]float64, bool) {
		w, active := map[string]float64{}, false
		for _, l := range in.Legs {
			if l.Kind != "basis" || gateOpen(l.PerpID, i) {
				w[l.Key()] += l.Weight
				active = active || l.Kind == "basis"
				delete(parked, l.Key())
				continue
			}
			if _, ok := parked[l.Key()]; !ok {
				park, best := "idle", math.Inf(-1)
				for _, k := range lendKeys {
					if v := apy[k][i]; v > best {
						park, best = k, v
					}
				}
				parked[l.Key()] = park
			}
			w[parked[l.Key()]] += l.Weight
		}
		return w, active
	}
	costBps := func(k string, out bool) float64 {
		switch kind, _, _ := strings.Cut(k, ":"); kind {
		case "basis":
			return (r.SlippageBps + PerplTakerBps) * spotShare
		case "lp":
			return r.SlippageBps / 2
		case "staking":
			if out {
				return exitBps[k]
			}
		}
		return 0
	}
	pos := map[string]float64{"idle": in.Amount * price[0]}
	total := func() float64 {
		t := 0.0
		for _, v := range pos {
			t += v
		}
		return t
	}
	rebalance := func(w map[string]float64) {
		t, cost := total(), 0.0
		for k := range pos {
			if d := w[k]*t - pos[k]; d < 0 {
				cost -= d * costBps(k, true) / 1e4
			}
		}
		for k := range w {
			if d := w[k]*t - pos[k]; d > 0 {
				cost += d * costBps(k, false) / 1e4
			}
		}
		t -= cost
		res.Contributions["costs"] -= cost
		res.CostsUSD += cost
		pos = map[string]float64{}
		for k, x := range w {
			pos[k] = x * t
		}
	}

	res.InitialUSD = in.Amount * price[0]
	w, active := targets(0)
	rebalance(w)
	last := 0
	for i := 0; i < N; i++ {
		if i > 0 {
			pr := price[i] / price[i-1]
			dayKey := start.AddDate(0, 0, i).Format(Day)
			for k, v := range pos {
				kind, _, _ := strings.Cut(k, ":")
				v1, y := v*pr, 0.0
				switch kind {
				case "lending", "staking":
					if a := apy[k][i]; !math.IsNaN(a) {
						y = v1 * daily(a)
					}
				case "lp":
					v1 = v * math.Sqrt(pr) // 50/50 constant-product value vs price
					y = v1 * daily(r.LPAssumedAPR)
				case "basis":
					v1, y = v, v*s.Funding[perpOf[k]][dayKey]*spotShare // hedged: no price PnL
				}
				res.Contributions["price"] += v1 - v
				if y != 0 {
					res.Contributions[k] += y
				}
				pos[k] = v1 + y
			}
			w, active = targets(i)
			if i-last >= r.MinIntervalDays {
				t, drift := total(), 0.0
				for k := range w {
					drift = math.Max(drift, math.Abs(pos[k]/t-w[k]))
				}
				for k := range pos {
					drift = math.Max(drift, math.Abs(pos[k]/t-w[k]))
				}
				if drift*1e4 > r.DriftBps {
					rebalance(w)
					res.Rebalances++
					last = i
				}
			}
		}
		t := total()
		res.Curve = append(res.Curve, Point{
			Day: start.AddDate(0, 0, i).Format(Day), USD: t, Asset: t / price[i],
			HodlUSD: in.Amount * price[i], BasisActive: active,
		})
	}

	for k, v := range pos {
		res.ExitCostUSD += v * costBps(k, true) / 1e4
	}
	res.CostsUSD += res.ExitCostUSD
	res.Contributions["costs"] -= res.ExitCostUSD
	pEnd := price[N-1]
	res.FinalUSD = total() - res.ExitCostUSD
	res.FinalAsset = res.FinalUSD / pEnd
	res.HodlFinalUSD = in.Amount * pEnd
	if N > 1 {
		yrs := float64(N-1) / 365
		res.APYUSD = math.Pow(res.FinalUSD/res.InitialUSD, 1/yrs) - 1
		res.APYAsset = math.Pow(res.FinalAsset/in.Amount, 1/yrs) - 1
		res.HodlAPYUSD = math.Pow(res.HodlFinalUSD/res.InitialUSD, 1/yrs) - 1
	}
	res.MaxDrawdownUSD = maxDD(res.Curve, func(p Point) float64 { return p.USD })
	res.MaxDrawdownAsset = maxDD(res.Curve, func(p Point) float64 { return p.Asset })
	res.HodlMaxDrawdownUSD = maxDD(res.Curve, func(p Point) float64 { return p.HodlUSD })
	for _, l := range in.Legs {
		res.Estimated = res.Estimated || l.Kind == "lp"
	}
	return res, nil
}

// maxDD returns the largest peak-to-trough fall as a fraction (0.2 = -20%).
func maxDD(c []Point, f func(Point) float64) float64 {
	peak, dd := math.Inf(-1), 0.0
	for _, p := range c {
		v := f(p)
		peak = math.Max(peak, v)
		dd = math.Max(dd, 1-v/peak)
	}
	return dd
}

// Combine merges per-sleeve results into one USD portfolio: sleeve i is scaled to usd*w[i] at its start,
// curves are summed over the days every sleeve has, and the asset view equals the USD view (asset "MULTI").
// Sleeves should share one window (the caller aligns them); a misaligned start makes InitialUSD != usd.
func Combine(rs []*Result, w []float64, usd float64) *Result {
	out := &Result{Asset: "MULTI", InitialUSD: usd, Contributions: map[string]float64{}}
	seen := map[string]int{}
	for _, r := range rs {
		for _, p := range r.Curve {
			seen[p.Day]++
		}
	}
	for i, r := range rs {
		k := usd * w[i] / r.InitialUSD
		out.FinalUSD += k * r.FinalUSD
		out.HodlFinalUSD += k * r.HodlFinalUSD
		out.CostsUSD += k * r.CostsUSD
		out.ExitCostUSD += k * r.ExitCostUSD
		out.Rebalances += r.Rebalances
		out.Estimated = out.Estimated || r.Estimated
		out.Coverage = append(out.Coverage, r.Coverage...)
		for c, v := range r.Contributions {
			out.Contributions[r.Asset+":"+c] += k * v
		}
		j := 0
		for _, p := range r.Curve {
			if seen[p.Day] != len(rs) {
				continue
			}
			if i == 0 {
				out.Curve = append(out.Curve, Point{Day: p.Day})
			}
			c := &out.Curve[j]
			c.USD += k * p.USD
			c.HodlUSD += k * p.HodlUSD
			c.BasisActive = c.BasisActive || p.BasisActive
			j++
		}
	}
	for i := range out.Curve {
		out.Curve[i].Asset = out.Curve[i].USD
	}
	if n := len(out.Curve); n > 0 {
		out.From, out.To, out.Days = out.Curve[0].Day, out.Curve[n-1].Day, n-1
	}
	out.FinalAsset = out.FinalUSD
	if out.Days > 0 {
		yrs := float64(out.Days) / 365
		out.APYUSD = math.Pow(out.FinalUSD/usd, 1/yrs) - 1
		out.HodlAPYUSD = math.Pow(out.HodlFinalUSD/usd, 1/yrs) - 1
	}
	out.APYAsset = out.APYUSD
	out.MaxDrawdownUSD = maxDD(out.Curve, func(p Point) float64 { return p.USD })
	out.MaxDrawdownAsset = out.MaxDrawdownUSD
	out.HodlMaxDrawdownUSD = maxDD(out.Curve, func(p Point) float64 { return p.HodlUSD })
	return out
}
