package planner

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/config"
)

type SourceParams struct {
	Leverage string `json:"leverage,omitempty"` // basis only, e.g. "2"
}

type Source struct {
	Kind      string       `json:"kind"`  // lending | staking | lp | basis
	Venue     string       `json:"venue"` // aave | neverland | <4626 slug> | shmonad | kuru-activevault | perpl
	WeightBps int          `json:"weightBps"`
	Params    SourceParams `json:"params"`
}

type Gate struct {
	APR          string `json:"apr"`
	LookbackDays int    `json:"lookbackDays"`
}

type Rules struct {
	DriftBps            int         `json:"driftBps,omitempty"`
	MinRebalanceSeconds int64       `json:"minRebalanceSeconds,omitempty"`
	FundingGate         *Gate       `json:"fundingGate,omitempty"`
	RateSwitch          *RateSwitch `json:"rateSwitch,omitempty"`
}

// RateSwitch: the scheduler moves lending weight to a venue whose APY beats the current one by > MinGain (fraction).
type RateSwitch struct {
	MinGain string `json:"minGain"`
}

type Strategy struct {
	Asset   string   `json:"asset"`
	Sources []Source `json:"sources"`
	Rules   Rules    `json:"rules"`
}

// LegState is what a source holds for one position (stored in strategy_positions.legs). Raw atoms; EntryPx display USDC/unit.
type LegState struct {
	Kind       string `json:"kind"`
	Venue      string `json:"venue"`
	Asset      string `json:"asset"`                // asset atoms supplied (basis: spot kept)
	Collateral string `json:"collateral,omitempty"` // basis: AUSD atoms in Perpl
	Lot        string `json:"lot,omitempty"`        // basis: short size in perp lot units
	EntryPx    string `json:"entryPx,omitempty"`    // basis
}

// Balance is a source's live state for one position, same order as Strategy.Sources.
// Value: asset atoms the source returns on exit (lp: MON part; basis: spot + collateral in asset terms). Shares: vault/shMON/ActiveVault share atoms.
type Balance struct {
	Value  string `json:"value"`
	Shares string `json:"shares"`
}

type Plan struct {
	Steps   []Step     `json:"steps"`
	FeeUSDC string     `json:"feeUsdc,omitempty"` // atoms
	Legs    []LegState `json:"legs,omitempty"`
}

var Assets = map[string]bool{"USDC": true, "WETH": true, "cbBTC": true, "MON": true, "XAUt0": true}

// perp: Perpl market hedging a Kuru spot asset; decimals from app.perpl.xyz/api/v1/pub/context (verified 2026-09-30).
type perp struct {
	Name           string
	PriceDec, Size int32
	MaxLev         int64
}

var perps = map[string]perp{"WETH": {"ETH", 2, 3, 12}, "cbBTC": {"BTC", 1, 5, 15}, "MON": {"MON", 6, 0, 10}}

const (
	perplSlipBps   = 100 // IOC limit through spot; Perpl caps market slippage at 100 bps
	poolFeeBps     = 5   // 3pool
	shmonHaircut   = 100 // instant redeem ~0.93%
	perplMinOpen   = 10_000_000
	perplExitBufBp = 50 // withdraw estimate buffer on notional
)

// Slug is the venue id of a 4626 vault: "Euler k3USDC" → "euler-k3usdc" (same as publisher rates).
func Slug(name string) string { return strings.ToLower(strings.Join(strings.Fields(name), "-")) }

func (p *Planner) vault(slug string) (config.Vault, bool) {
	for _, v := range p.Dep.Lending.ERC4626 {
		if Slug(v.Name) == slug {
			return v, true
		}
	}
	return config.Vault{}, false
}

func d(s string) decimal.Decimal { v, _ := decimal.NewFromString(s); return v }

func (s Source) leverage() decimal.Decimal { return d(s.Params.Leverage) }

// Validate checks a strategy against what the executor can run.
func (p *Planner) Validate(s Strategy) error {
	if !Assets[s.Asset] {
		return fail("invalid_asset", "asset %q", s.Asset)
	}
	if len(s.Sources) == 0 || len(s.Sources) > 6 {
		return fail("invalid_sources", "need 1..6 sources")
	}
	sum, seen, basis := 0, map[string]bool{}, 0
	for _, src := range s.Sources {
		key := src.Kind + ":" + src.Venue
		if src.WeightBps <= 0 || seen[key] {
			return fail("invalid_sources", "bad weight or duplicate %s", key)
		}
		seen[key], sum = true, sum+src.WeightBps
		if err := p.validSource(s.Asset, src); err != nil {
			return err
		}
		if src.Kind == "basis" {
			basis++
		}
	}
	if sum != 10_000 {
		return fail("invalid_sources", "weights sum %d, want 10000", sum)
	}
	if basis > 1 {
		return fail("invalid_sources", "at most one basis source")
	}
	if r := s.Rules; r.DriftBps < 0 || r.MinRebalanceSeconds < 0 || (r.FundingGate != nil && (r.FundingGate.LookbackDays < 1 || d(r.FundingGate.APR).IsNegative())) {
		return fail("invalid_rules", "negative or bad funding gate")
	}
	if r := s.Rules.RateSwitch; r != nil && !d(r.MinGain).IsPositive() {
		return fail("invalid_rules", "rateSwitch.minGain must be > 0")
	}
	return nil
}

func (p *Planner) validSource(asset string, src Source) error {
	bad := func(why string) error { return fail("unsupported_source", "%s:%s %s", src.Kind, src.Venue, why) }
	switch src.Kind {
	case "lending":
		switch src.Venue {
		case "aave", "neverland":
			pool := p.Dep.Lending.AaveV3
			if src.Venue == "neverland" {
				pool = p.Dep.Lending.Neverland
			}
			if pool.ATokens[lendAsset(asset)] == "" {
				return bad("no reserve for " + asset)
			}
		default:
			v, ok := p.vault(src.Venue)
			if !ok || v.Asset != lendAsset(asset) {
				return bad("unknown vault for " + asset)
			}
		}
	case "staking":
		if src.Venue != "shmonad" || asset != "MON" {
			return bad("staking is shmonad on MON only")
		}
	case "lp":
		if src.Venue != "kuru-activevault" || asset != "MON" {
			return bad("lp is kuru-activevault on MON only")
		}
	case "basis":
		pp, ok := perps[asset]
		l := src.leverage()
		if src.Venue != "perpl" || !ok {
			return bad("basis is perpl on WETH/cbBTC/MON")
		}
		if l.LessThan(decimal.NewFromInt(1)) || l.GreaterThan(decimal.NewFromInt(pp.MaxLev)) || !l.Mul(decimal.NewFromInt(100)).IsInteger() {
			return fail("invalid_sources", "basis leverage must be 1..%d with ≤2 decimals", pp.MaxLev)
		}
	default:
		return bad("unknown kind")
	}
	return nil
}

// lendAsset: MON is lent as WMON.
func lendAsset(asset string) string {
	if asset == "MON" {
		return "WMON"
	}
	return asset
}

// supply puts amt atoms of the strategy asset into a lending/staking source (MON wrapped first for lending).
func (p *Planner) supply(asset string, src Source, amt decimal.Decimal) []Step {
	a, la := amt.String(), lendAsset(asset)
	var st Step
	switch {
	case src.Kind == "staking":
		return []Step{{Action: "shmonad.deposit", Amount: a}}
	case src.Venue == "aave":
		st = Step{Action: "lend.supply", Venue: "aaveV3", Asset: la, Amount: a}
	case src.Venue == "neverland":
		st = Step{Action: "lend.supply", Venue: "neverland", Asset: la, Amount: a}
	default:
		v, _ := p.vault(src.Venue)
		st = Step{Action: "vault.deposit", Vault: v.Vault, Amount: a}
	}
	if la != asset {
		return []Step{{Action: "wmon.wrap", Amount: a}, st}
	}
	return []Step{st}
}

// takeOut withdraws amt asset atoms from a lending/staking source (all = everything, by shares); returns the guaranteed asset out.
func (p *Planner) takeOut(asset string, src Source, amt decimal.Decimal, b Balance, all bool) ([]Step, decimal.Decimal) {
	shares := d(b.Shares)
	if !all && d(b.Value).IsPositive() {
		shares = shares.Mul(amt).Div(d(b.Value)).Floor()
	}
	a, la := amt.String(), lendAsset(asset)
	var st Step
	switch {
	case src.Kind == "staking":
		return []Step{{Action: "shmonad.redeem", Shares: shares.String()}}, amt.Mul(decimal.NewFromInt(10_000 - shmonHaircut)).Div(bps).Floor()
	case src.Venue == "aave" || src.Venue == "neverland":
		venue := map[string]string{"aave": "aaveV3", "neverland": "neverland"}[src.Venue]
		st = Step{Action: "lend.withdraw", Venue: venue, Asset: la, Amount: a}
	default:
		v, _ := p.vault(src.Venue)
		st = Step{Action: "vault.withdraw", Vault: v.Vault, Amount: a}
		if all {
			st = Step{Action: "vault.redeem", Vault: v.Vault, Shares: shares.String()}
		}
	}
	if la != asset {
		return []Step{st, {Action: "wmon.unwrap", Amount: a}}, amt
	}
	return []Step{st}, amt
}

func bpsOf(x decimal.Decimal, w int) decimal.Decimal {
	return x.Mul(decimal.NewFromInt(int64(w))).Div(bps).Floor()
}

// openBasis: spot atoms already in the wallet, usdc atoms → AUSD collateral → short `spot` at px.
func (p *Planner) openBasis(asset string, src Source, spot, usdc decimal.Decimal, px decimal.Decimal, hasAccount bool, slip int) ([]Step, LegState, error) {
	pp := perps[asset]
	ausd := usdc.Mul(decimal.NewFromInt(int64(10_000 - slip - poolFeeBps))).Div(bps).Floor()
	lot := spot.Shift(pp.Size - int32(p.decimals(asset))).Floor()
	if !lot.IsPositive() || !ausd.IsPositive() {
		return nil, LegState{}, fail("below_min_size", "basis leg too small to hedge")
	}
	steps := []Step{{Action: "stable.swap", From: "USDC", To: "AUSD", Amount: usdc.String(), MinOut: ausd.String()}}
	if hasAccount {
		steps = append(steps, Step{Action: "perpl.deposit", Amount: ausd.String()})
	} else {
		if ausd.LessThan(decimal.NewFromInt(perplMinOpen)) {
			return nil, LegState{}, fail("below_min_size", "Perpl account needs ≥10 AUSD collateral")
		}
		steps = append(steps, Step{Action: "perpl.createAccount", Amount: ausd.String()})
	}
	limit := px.Mul(decimal.NewFromInt(10_000 - perplSlipBps)).Div(bps).Shift(pp.PriceDec).Floor()
	steps = append(steps, Step{Action: "perpl.order", Perp: pp.Name, Kind: "openShort", Price: limit.String(), Lot: lot.String(),
		LeverageHdths: int(src.leverage().Mul(decimal.NewFromInt(100)).IntPart())})
	return steps, LegState{Kind: src.Kind, Venue: src.Venue, Asset: spot.String(), Collateral: ausd.String(), Lot: lot.String(), EntryPx: px.String()}, nil
}

// closeBasis closes part (bps) of the short and withdraws that share of the estimated collateral back to USDC.
// Returns the steps, guaranteed USDC out and the leg left afterwards.
func (p *Planner) closeBasis(asset string, leg LegState, px decimal.Decimal, part, slip int) ([]Step, decimal.Decimal, LegState) {
	pp := perps[asset]
	lot := bpsOf(d(leg.Lot), part)
	if !lot.IsPositive() {
		return nil, decimal.Zero, leg
	}
	units := lot.Shift(-pp.Size)
	// (collateral + short PnL) × part − taker fee − buffer on the closed notional, in AUSD atoms
	pnl := d(leg.EntryPx).Sub(px).Mul(d(leg.Lot).Shift(-pp.Size)).Shift(6)
	fees := units.Mul(px).Shift(6).Mul(decimal.NewFromFloat(3.45 + perplExitBufBp)).Div(bps)
	est := bpsOf(d(leg.Collateral).Add(pnl), part).Sub(fees).Floor()
	limit := px.Mul(decimal.NewFromInt(10_000 + perplSlipBps)).Div(bps).Shift(pp.PriceDec).Ceil()
	steps := []Step{{Action: "perpl.order", Perp: pp.Name, Kind: "closeShort", Price: limit.String(), Lot: lot.String(), LeverageHdths: 100}}
	left := leg
	left.Lot = d(leg.Lot).Sub(lot).String()
	left.Collateral = d(leg.Collateral).Sub(bpsOf(d(leg.Collateral), part)).String()
	left.Asset = d(leg.Asset).Sub(bpsOf(d(leg.Asset), part)).String()
	if part == 10_000 {
		left = LegState{Kind: leg.Kind, Venue: leg.Venue, Asset: "0"}
	}
	if !est.IsPositive() {
		return steps, decimal.Zero, left
	}
	out := est.Mul(decimal.NewFromInt(int64(10_000 - slip - poolFeeBps))).Div(bps).Floor()
	return append(steps,
		Step{Action: "perpl.withdraw", Amount: est.String()},
		Step{Action: "stable.swap", From: "AUSD", To: "USDC", Amount: est.String(), MinOut: out.String()}), out, left
}

// Deploy opens a position with usdc atoms: fee to treasury first, then per source (buy asset on Kuru once, supply/stake/LP/hedge).
func (p *Planner) Deploy(ctx context.Context, s Strategy, usdc decimal.Decimal, hasPerplAccount bool, slip int) (Plan, error) {
	if err := p.Validate(s); err != nil {
		return Plan{}, err
	}
	if p.Dep.Treasury == "" {
		return Plan{}, fail("treasury_unset", "treasury missing in deployments")
	}
	fee := bpsOf(usdc, FeeBps)
	if !fee.IsPositive() {
		return Plan{}, fail("below_min_size", "amount too small")
	}
	net := usdc.Sub(fee)
	steps := []Step{{Action: "fee.transfer", Amount: fee.String()}}
	allocs, buys := make([]decimal.Decimal, len(s.Sources)), make([]decimal.Decimal, len(s.Sources))
	total := decimal.Zero
	for i, src := range s.Sources {
		allocs[i] = bpsOf(net, src.WeightBps)
		if s.Asset == "USDC" {
			continue
		}
		switch src.Kind {
		case "lp":
			buys[i] = allocs[i].Div(decimal.NewFromInt(2)).Floor()
		case "basis":
			l := src.leverage()
			buys[i] = allocs[i].Mul(l).Div(l.Add(decimal.NewFromInt(1))).Floor()
		default:
			buys[i] = allocs[i]
		}
		total = total.Add(buys[i])
	}
	got, px := decimal.Zero, decimal.Zero
	if total.IsPositive() {
		q, err := p.quote(ctx, s.Asset, "buy", total, slip)
		if err != nil {
			return Plan{}, err
		}
		got, px = d(q.MinOut), d(q.AvgPx)
		steps = append(steps, Step{Action: "kuru.marketBuy", Market: s.Asset, Quote: total.String(), MinOut: q.MinOut})
	}
	legs := make([]LegState, len(s.Sources))
	for i, src := range s.Sources {
		amt := allocs[i]
		if s.Asset != "USDC" {
			amt = got.Mul(buys[i]).Div(total).Floor()
		}
		legs[i] = LegState{Kind: src.Kind, Venue: src.Venue, Asset: amt.String()}
		switch src.Kind {
		case "lp":
			steps = append(steps, Step{Action: "activeVault.deposit", Base: amt.String(), Quote: allocs[i].Sub(buys[i]).String()})
		case "basis":
			bs, leg, err := p.openBasis(s.Asset, src, amt, allocs[i].Sub(buys[i]), px, hasPerplAccount, slip)
			if err != nil {
				return Plan{}, err
			}
			steps, legs[i] = append(steps, bs...), leg
		default:
			steps = append(steps, p.supply(s.Asset, src, amt)...)
		}
	}
	steps, err := p.check(p.Dep, steps)
	return Plan{Steps: steps, FeeUSDC: fee.String(), Legs: legs}, err
}

// Withdraw unwinds every source and sells the asset to USDC. No fee.
func (p *Planner) Withdraw(ctx context.Context, s Strategy, legs []LegState, bals []Balance, slip int) (Plan, error) {
	return p.WithdrawBps(ctx, s, legs, bals, 10_000, slip)
}

// WithdrawBps unwinds part (bps) of every source pro rata and sells that asset to USDC (position TP/SL). Plan.Legs = what remains.
func (p *Planner) WithdrawBps(ctx context.Context, s Strategy, legs []LegState, bals []Balance, part, slip int) (Plan, error) {
	if len(legs) != len(s.Sources) || len(bals) != len(s.Sources) || part < 1 || part > 10_000 {
		return Plan{}, fail("bad_request", "legs/balances do not match sources or bps out of 1..10000")
	}
	all := part == 10_000
	var steps []Step
	sell := decimal.Zero
	basis := -1
	out := make([]LegState, len(legs))
	for i, src := range s.Sources {
		b := bals[i]
		out[i] = legs[i]
		out[i].Asset = d(legs[i].Asset).Sub(bpsOf(d(legs[i].Asset), part)).String()
		switch src.Kind {
		case "lp":
			if sh := bpsOf(d(b.Shares), part); sh.IsPositive() {
				steps = append(steps, Step{Action: "activeVault.withdraw", Shares: sh.String()})
				sell = sell.Add(bpsOf(d(b.Value), part))
			}
		case "basis":
			basis = i
			sell = sell.Add(bpsOf(d(legs[i].Asset), part))
		default:
			if v := bpsOf(d(b.Value), part); v.IsPositive() {
				st, got := p.takeOut(s.Asset, src, v, b, all)
				steps, sell = append(steps, st...), sell.Add(got)
			}
		}
	}
	var q Quote
	var err error
	if s.Asset != "USDC" && sell.IsPositive() {
		q, err = p.quote(ctx, s.Asset, "sell", sell, slip)
		if err != nil && !isCode(err, "below_min_size") {
			return Plan{}, err
		}
	}
	if basis >= 0 && d(legs[basis].Lot).IsPositive() {
		px := d(q.AvgPx)
		if !px.IsPositive() {
			return Plan{}, fail("quote_unavailable", "no %s price to close the hedge", s.Asset)
		}
		var bs []Step
		bs, _, out[basis] = p.closeBasis(s.Asset, legs[basis], px, part, slip)
		steps = append(steps, bs...)
	}
	if err == nil && q.MinOut != "" {
		steps = append(steps, Step{Action: "kuru.marketSell", Market: s.Asset, Size: sell.String(), MinOut: q.MinOut})
	}
	steps, err = p.check(p.Dep, steps)
	return Plan{Steps: steps, Legs: out}, err
}

// ResizeBasis changes the Perpl hedge of a basis leg without touching spot (liquidation guard / re-hedge).
// lotDelta: perp lot units, < 0 reduces the short, > 0 adds to it (at the source leverage). collateralDelta: > 0 USDC atoms added
// as AUSD collateral, < 0 AUSD atoms withdrawn back to USDC. Plan.Legs = the updated leg.
func (p *Planner) ResizeBasis(ctx context.Context, s Strategy, leg LegState, lotDelta, collateralDelta decimal.Decimal, slip int) (Plan, error) {
	var src *Source
	for i := range s.Sources {
		if s.Sources[i].Kind == "basis" {
			src = &s.Sources[i]
		}
	}
	pp, ok := perps[s.Asset]
	if src == nil || !ok {
		return Plan{}, fail("bad_request", "strategy has no basis source")
	}
	if !lotDelta.IsInteger() || d(leg.Lot).Add(lotDelta).IsNegative() || d(leg.Collateral).Add(decimal.Min(collateralDelta, decimal.Zero)).IsNegative() {
		return Plan{}, fail("bad_request", "resize beyond the position")
	}
	var steps []Step
	out := leg
	if collateralDelta.IsPositive() {
		cs, err := p.AddCollateral(collateralDelta, slip)
		if err != nil {
			return Plan{}, err
		}
		steps = append(steps, cs...)
		out.Collateral = d(leg.Collateral).Add(d(cs[0].MinOut)).String()
	}
	if !lotDelta.IsZero() {
		q, err := p.quote(ctx, s.Asset, "buy", decimal.New(100, 6), slip) // $100 probe for the spot price
		if err != nil {
			return Plan{}, err
		}
		px := d(q.AvgPx)
		kind, f, lot := "openShort", decimal.NewFromInt(10_000-perplSlipBps), lotDelta
		lev := int(src.leverage().Mul(decimal.NewFromInt(100)).IntPart())
		if lotDelta.IsNegative() {
			kind, f, lot, lev = "closeShort", decimal.NewFromInt(10_000+perplSlipBps), lotDelta.Neg(), 100
		}
		limit := px.Mul(f).Div(bps).Shift(pp.PriceDec)
		if kind == "openShort" {
			limit = limit.Floor()
		} else {
			limit = limit.Ceil()
		}
		steps = append(steps, Step{Action: "perpl.order", Perp: pp.Name, Kind: kind, Price: limit.String(), Lot: lot.String(), LeverageHdths: lev})
		out.Lot = d(leg.Lot).Add(lotDelta).String()
	}
	if collateralDelta.IsNegative() {
		amt := collateralDelta.Neg()
		min := amt.Mul(decimal.NewFromInt(int64(10_000 - slip - poolFeeBps))).Div(bps).Floor()
		steps = append(steps, Step{Action: "perpl.withdraw", Amount: amt.String()},
			Step{Action: "stable.swap", From: "AUSD", To: "USDC", Amount: amt.String(), MinOut: min.String()})
		out.Collateral = d(leg.Collateral).Sub(amt).String()
	}
	steps, err := p.check(p.Dep, steps)
	return Plan{Steps: steps, Legs: []LegState{out}}, err
}

// AddCollateral tops up the Perpl account from USDC (liquidation guard).
func (p *Planner) AddCollateral(usdc decimal.Decimal, slip int) ([]Step, error) {
	ausd := usdc.Mul(decimal.NewFromInt(int64(10_000 - slip - poolFeeBps))).Div(bps).Floor()
	if !ausd.IsPositive() {
		return nil, fail("bad_request", "amount too small")
	}
	return p.check(p.Dep, []Step{
		{Action: "stable.swap", From: "USDC", To: "AUSD", Amount: usdc.String(), MinOut: ausd.String()},
		{Action: "perpl.deposit", Amount: ausd.String()},
	})
}

// Rebalance moves value toward targetBps (same order as sources). Lending/staking move partially; basis only toggles
// fully off (target 0) or on (was off, target > 0); lp is never moved. Empty Steps = nothing to do.
func (p *Planner) Rebalance(ctx context.Context, s Strategy, legs []LegState, bals []Balance, targetBps []int, hasPerplAccount bool, slip int) (Plan, error) {
	n := len(s.Sources)
	if len(legs) != n || len(bals) != n || len(targetBps) != n {
		return Plan{}, fail("bad_request", "legs/balances/targets do not match sources")
	}
	total, sum := decimal.Zero, 0
	for i := range s.Sources {
		total, sum = total.Add(d(bals[i].Value)), sum+targetBps[i]
	}
	if sum != 10_000 {
		return Plan{}, fail("bad_request", "targets sum %d", sum)
	}
	out := append([]LegState(nil), legs...)
	var steps []Step
	freed := decimal.Zero
	deltas := make([]decimal.Decimal, n)
	px := func() (decimal.Decimal, error) {
		q, err := p.quote(ctx, s.Asset, "buy", decimal.New(100, 6), slip) // $100 probe for the spot price
		return d(q.AvgPx), err
	}
	for i, src := range s.Sources {
		cur := d(bals[i].Value)
		deltas[i] = bpsOf(total, targetBps[i]).Sub(cur)
		switch {
		case src.Kind == "lp" || deltas[i].Sign() >= 0:
		case src.Kind == "basis":
			if targetBps[i] != 0 || !d(legs[i].Lot).IsPositive() {
				deltas[i] = decimal.Zero // resize unsupported: toggle only
				continue
			}
			spot, err := px()
			if err != nil {
				return Plan{}, err
			}
			bs, usdc, _ := p.closeBasis(s.Asset, legs[i], spot, 10_000, slip)
			steps, freed = append(steps, bs...), freed.Add(d(legs[i].Asset))
			if usdc.IsPositive() {
				q, err := p.quote(ctx, s.Asset, "buy", usdc, slip)
				if err != nil {
					return Plan{}, err
				}
				steps, freed = append(steps, Step{Action: "kuru.marketBuy", Market: s.Asset, Quote: usdc.String(), MinOut: q.MinOut}), freed.Add(d(q.MinOut))
			}
			out[i] = LegState{Kind: src.Kind, Venue: src.Venue, Asset: "0"}
		default:
			st, got := p.takeOut(s.Asset, src, deltas[i].Neg(), bals[i], false)
			steps, freed = append(steps, st...), freed.Add(got)
			out[i].Asset = decimal.Max(decimal.Zero, d(legs[i].Asset).Add(deltas[i])).String()
		}
	}
	// basis switching on takes its share first; the rest of freed is split over under-weight sources by delta
	want := decimal.Zero
	for i, src := range s.Sources {
		if deltas[i].IsPositive() && src.Kind == "basis" {
			if d(legs[i].Lot).IsPositive() {
				deltas[i] = decimal.Zero
				continue
			}
			alloc := decimal.Min(deltas[i], freed)
			l := src.leverage()
			sellAmt := alloc.Div(l.Add(decimal.NewFromInt(1))).Floor()
			q, err := p.quote(ctx, s.Asset, "sell", sellAmt, slip)
			if err != nil {
				return Plan{}, err
			}
			bs, leg, err := p.openBasis(s.Asset, src, alloc.Sub(sellAmt), d(q.MinOut), d(q.AvgPx), hasPerplAccount, slip)
			if err != nil {
				return Plan{}, err
			}
			steps = append(steps, Step{Action: "kuru.marketSell", Market: s.Asset, Size: sellAmt.String(), MinOut: q.MinOut})
			steps, out[i], freed = append(steps, bs...), leg, freed.Sub(alloc)
			deltas[i] = decimal.Zero
		}
		if deltas[i].IsPositive() && src.Kind != "lp" {
			want = want.Add(deltas[i])
		}
	}
	for i, src := range s.Sources {
		if !deltas[i].IsPositive() || src.Kind == "lp" || !want.IsPositive() {
			continue
		}
		amt := decimal.Min(deltas[i], freed.Mul(deltas[i]).Div(want).Floor())
		if amt.IsPositive() {
			steps = append(steps, p.supply(s.Asset, src, amt)...)
			out[i].Asset = d(legs[i].Asset).Add(amt).String()
		}
	}
	if len(steps) == 0 {
		return Plan{Legs: out}, nil
	}
	steps, err := p.check(p.Dep, steps)
	return Plan{Steps: steps, Legs: out}, err
}

// Attribute nets a basket's holdings per market from its confirmed executions' steps: Σ marketBuy minOut − Σ marketSell size (base atoms).
func Attribute(plans [][]byte) map[string]decimal.Decimal {
	out := map[string]decimal.Decimal{}
	for _, raw := range plans {
		var steps []Step
		if json.Unmarshal(raw, &steps) != nil {
			continue
		}
		for _, s := range steps {
			switch s.Action {
			case "kuru.marketBuy":
				out[s.Market] = out[s.Market].Add(d(s.MinOut))
			case "kuru.marketSell":
				out[s.Market] = out[s.Market].Sub(d(s.Size))
			}
		}
	}
	for m, v := range out {
		if !v.IsPositive() {
			delete(out, m)
		}
	}
	return out
}
