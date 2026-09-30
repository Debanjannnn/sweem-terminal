// Package planner turns trade/earn intents into executor step plans (testdata/contracts/executor/user_steps.plan.json shape).
// No I/O of its own: prices come through Quoter, addresses from 143.json.
package planner

import (
	"context"
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/config"
)

const (
	FeeBps     = 40 // open fee → treasury
	CreatorBps = 20 // of the fee, owed to a followed strategy's creator
	MaxSteps   = 32 // executor MAX_STEPS
)

// Step is one executor step; omitempty keeps each action's JSON exactly its fixture fields.
type Step struct {
	Action        string   `json:"action"`
	Market        string   `json:"market,omitempty"`
	Quote         string   `json:"quote,omitempty"`
	Size          string   `json:"size,omitempty"`
	Token         string   `json:"token,omitempty"`
	Side          string   `json:"side,omitempty"`
	Price         string   `json:"price,omitempty"`
	PostOnly      *bool    `json:"postOnly,omitempty"`
	OrderIDs      []string `json:"orderIds,omitempty"`
	Hops          []Hop    `json:"hops,omitempty"`
	From          string   `json:"from,omitempty"`
	To            string   `json:"to,omitempty"`
	Venue         string   `json:"venue,omitempty"`
	Asset         string   `json:"asset,omitempty"`
	Vault         string   `json:"vault,omitempty"`
	Amount        string   `json:"amount,omitempty"`
	Shares        string   `json:"shares,omitempty"`
	Base          string   `json:"base,omitempty"`
	MinOut        string   `json:"minOut,omitempty"`
	Perp          string   `json:"perp,omitempty"`
	Kind          string   `json:"kind,omitempty"`
	Lot           string   `json:"lot,omitempty"`
	LeverageHdths int      `json:"leverageHdths,omitempty"`
}

type Hop struct {
	Market string `json:"market"`
	IsBuy  bool   `json:"isBuy"`
}

// Quote is market-data's GET /api/v1/quote/{orderbook} response (raw atoms; AvgPx display USDC per unit).
type Quote struct {
	Amount        string `json:"amount"`
	Fillable      string `json:"fillable"`
	Partial       bool   `json:"partial"`
	ExpectedOut   string `json:"expectedOut"`
	MinOut        string `json:"minOut"`
	AvgPx         string `json:"avgPx"`
	MaxSafeAmount string `json:"maxSafeAmount"`
}

type Quoter interface {
	Quote(ctx context.Context, orderbook, side string, amount decimal.Decimal, slippageBps int) (Quote, error)
}

// Error carries an api error code (tasks/api-v2.md).
type Error struct{ Code, Detail string }

func (e *Error) Error() string { return e.Code + ": " + e.Detail }

func fail(code, format string, a ...any) error { return &Error{code, fmt.Sprintf(format, a...)} }

type Planner struct {
	Dep        *config.Deployments143
	Q          Quoter
	MaxStepUSD int64 // must equal executor MAX_STEP_USD
}

var (
	bps     = decimal.NewFromInt(10_000)
	stables = map[string]bool{"USDC": true, "AUSD": true, "USDT0": true}
)

func pow10(n uint8) decimal.Decimal { return decimal.New(1, int32(n)) }

func (p *Planner) market(sym string) (config.Market, error) {
	m, ok := p.Dep.Kuru.Markets[sym]
	if !ok {
		return m, fail("bad_request", "unknown market %s", sym)
	}
	return m, nil
}

func (p *Planner) decimals(sym string) uint8 { return p.Dep.Tokens[sym].Decimals }

// ToAtoms converts a human amount of token sym to floored raw atoms.
func (p *Planner) ToAtoms(sym string, human decimal.Decimal) (decimal.Decimal, error) {
	t, ok := p.Dep.Tokens[sym]
	if !ok {
		return decimal.Zero, fail("bad_request", "unknown token %s", sym)
	}
	return human.Shift(int32(t.Decimals)).Floor(), nil
}

// quote fetches a quote and enforces fill + safe size; below-min and upstream errors pass through.
func (p *Planner) quote(ctx context.Context, sym, side string, amount decimal.Decimal, slip int) (Quote, error) {
	m, err := p.market(sym)
	if err != nil {
		return Quote{}, err
	}
	if !amount.IsPositive() {
		return Quote{}, fail("bad_request", "%s %s amount must be positive", side, sym)
	}
	q, err := p.Q.Quote(ctx, strings.ToLower(m.Orderbook), side, amount, slip)
	if err != nil {
		return q, err
	}
	if q.Partial {
		return q, fail("insufficient_liquidity", "%s %s: book fills only %s of %s", side, sym, q.Fillable, amount)
	}
	if safe, err := decimal.NewFromString(q.MaxSafeAmount); err == nil && amount.GreaterThan(safe) {
		return q, fail("exceeds_safe_size", "%s %s: max safe amount %s", side, sym, q.MaxSafeAmount)
	}
	return q, nil
}

// check enforces executor limits: step count and per-step stablecoin spend.
func (p *Planner) check(d *config.Deployments143, steps []Step) ([]Step, error) {
	if len(steps) == 0 {
		return nil, fail("nothing_to_sell", "plan is empty")
	}
	if len(steps) > MaxSteps {
		return nil, fail("too_many_steps", "%d steps > %d", len(steps), MaxSteps)
	}
	limit := decimal.NewFromInt(p.MaxStepUSD).Shift(6)
	for _, s := range steps {
		if spend := stableSpend(d, s); spend.GreaterThan(limit) {
			return nil, fail("cap_exceeded", "%s spends %s stable atoms > %d USD", s.Action, spend, p.MaxStepUSD)
		}
	}
	return steps, nil
}

// stableSpend mirrors executor steps::stable_spend.
func stableSpend(d *config.Deployments143, s Step) decimal.Decimal {
	a := func(v string) decimal.Decimal { x, _ := decimal.NewFromString(v); return x }
	switch s.Action {
	case "fee.transfer", "perpl.createAccount", "perpl.deposit":
		return a(s.Amount)
	case "kuru.marketBuy":
		return a(s.Quote)
	case "activeVault.deposit":
		return a(s.Quote)
	case "kuru.marginDeposit":
		if stables[s.Token] {
			return a(s.Amount)
		}
	case "lend.supply":
		if stables[s.Asset] {
			return a(s.Amount)
		}
	case "stable.swap", "kuru.swap":
		if stables[s.From] {
			return a(s.Amount)
		}
	case "vault.deposit":
		for _, v := range d.Lending.ERC4626 {
			if strings.EqualFold(v.Vault, s.Vault) && stables[v.Asset] {
				return a(s.Amount)
			}
		}
	}
	return decimal.Zero
}

// MarketBuy spends usdc atoms on market sym.
func (p *Planner) MarketBuy(ctx context.Context, sym string, usdc decimal.Decimal, slip int) ([]Step, error) {
	q, err := p.quote(ctx, sym, "buy", usdc, slip)
	if err != nil {
		return nil, err
	}
	return p.check(p.Dep, []Step{{Action: "kuru.marketBuy", Market: sym, Quote: usdc.String(), MinOut: q.MinOut}})
}

// MarketSell sells base atoms of market sym for USDC.
func (p *Planner) MarketSell(ctx context.Context, sym string, base decimal.Decimal, slip int) ([]Step, error) {
	q, err := p.quote(ctx, sym, "sell", base, slip)
	if err != nil {
		return nil, err
	}
	return p.check(p.Dep, []Step{{Action: "kuru.marketSell", Market: sym, Size: base.String(), MinOut: q.MinOut}})
}

type Leg struct {
	Market    string `json:"market"`
	WeightBps int    `json:"weightBps"`
}

// ValidateLegs: 1..10 unique known markets, positive weights summing to 10000.
func (p *Planner) ValidateLegs(legs []Leg) error {
	if len(legs) == 0 || len(legs) > 10 {
		return fail("invalid_legs", "need 1..10 legs")
	}
	sum, seen := 0, map[string]bool{}
	for _, l := range legs {
		if _, err := p.market(l.Market); err != nil || seen[l.Market] || l.WeightBps <= 0 {
			return fail("invalid_legs", "bad leg %s", l.Market)
		}
		seen[l.Market], sum = true, sum+l.WeightBps
	}
	if sum != 10_000 {
		return fail("invalid_legs", "weights sum %d, want 10000", sum)
	}
	return nil
}

// BasketBuy splits usdc atoms by weight into one wallet market buy per leg.
func (p *Planner) BasketBuy(ctx context.Context, legs []Leg, usdc decimal.Decimal, slip int) ([]Step, error) {
	if err := p.ValidateLegs(legs); err != nil {
		return nil, err
	}
	var steps []Step
	for _, l := range legs {
		alloc := usdc.Mul(decimal.NewFromInt(int64(l.WeightBps))).Div(bps).Floor()
		q, err := p.quote(ctx, l.Market, "buy", alloc, slip)
		if err != nil {
			return nil, err
		}
		steps = append(steps, Step{Action: "kuru.marketBuy", Market: l.Market, Quote: alloc.String(), MinOut: q.MinOut})
	}
	return p.check(p.Dep, steps)
}

type SellLeg struct {
	Market string
	Amount decimal.Decimal // base atoms
}

// BasketSell sells each leg to USDC; legs below Kuru's min size are left in the wallet.
func (p *Planner) BasketSell(ctx context.Context, legs []SellLeg, slip int) ([]Step, error) {
	var steps []Step
	for _, l := range legs {
		if !l.Amount.IsPositive() {
			continue
		}
		q, err := p.quote(ctx, l.Market, "sell", l.Amount, slip)
		if isCode(err, "below_min_size") {
			continue
		}
		if err != nil {
			return nil, err
		}
		steps = append(steps, Step{Action: "kuru.marketSell", Market: l.Market, Size: l.Amount.String(), MinOut: q.MinOut})
	}
	return p.check(p.Dep, steps)
}

func isCode(err error, code string) bool {
	e, ok := err.(*Error)
	return ok && e.Code == code
}

// Limit places a resting order from the margin account; fund prepends the margin deposit it needs.
// price/size are human; returns the raw price/size the order uses.
func (p *Planner) Limit(sym, side string, price, size decimal.Decimal, postOnly, fund bool) ([]Step, error) {
	m, err := p.market(sym)
	if err != nil {
		return nil, err
	}
	if side != "buy" && side != "sell" {
		return nil, fail("bad_request", "side %q", side)
	}
	tick := decimal.NewFromInt(int64(m.TickSize))
	pp, sp := decimal.NewFromInt(int64(m.PricePrecision)), decimal.NewFromInt(int64(m.SizePrecision))
	rawPx := price.Mul(pp).Div(tick).Floor().Mul(tick)
	rawSz := size.Mul(sp).Floor()
	if !rawPx.IsPositive() || rawPx.GreaterThan(decimal.NewFromInt(1<<32-1)) {
		return nil, fail("bad_request", "price out of range")
	}
	if rawSz.LessThan(decimal.NewFromInt(int64(m.MinSize))) {
		return nil, fail("below_min_size", "size below %s", decimal.NewFromInt(int64(m.MinSize)).Div(sp))
	}
	var steps []Step
	if fund {
		if side == "buy" {
			need := rawPx.Div(pp).Mul(rawSz.Div(sp)).Shift(int32(p.decimals(m.Quote))).Ceil()
			steps = append(steps, Step{Action: "kuru.marginDeposit", Token: m.Quote, Amount: need.String()})
		} else {
			need := rawSz.Div(sp).Shift(int32(p.decimals(m.Base))).Ceil()
			steps = append(steps, Step{Action: "kuru.marginDeposit", Token: m.Base, Amount: need.String()})
		}
	}
	steps = append(steps, Step{Action: "kuru.limit", Market: sym, Side: side, Price: rawPx.String(), Size: rawSz.String(), PostOnly: &postOnly})
	return p.check(p.Dep, steps)
}

func (p *Planner) Cancel(sym string, ids []string) ([]Step, error) {
	if _, err := p.market(sym); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, fail("bad_request", "no order ids")
	}
	return p.check(p.Dep, []Step{{Action: "kuru.cancel", Market: sym, OrderIDs: ids}})
}

// Margin deposits into / withdraws from the Kuru margin account (amount in atoms).
func (p *Planner) Margin(deposit bool, token string, amount decimal.Decimal) ([]Step, error) {
	if _, ok := p.Dep.Tokens[token]; !ok || !amount.IsPositive() {
		return nil, fail("bad_request", "bad token or amount")
	}
	a := "kuru.marginWithdraw"
	if deposit {
		a = "kuru.marginDeposit"
	}
	return p.check(p.Dep, []Step{{Action: a, Token: token, Amount: amount.String()}})
}
