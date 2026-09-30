// Package quote prices Kuru V1 wallet market orders (placeAndExecuteMarketBuy/Sell) against the publisher's top-20 book.
package quote

import (
	"errors"

	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/streams"
)

var (
	ErrBadInput    = errors.New("bad_request")
	ErrMinSize     = errors.New("below_min_size")
	ErrUnavailable = errors.New("unavailable") // empty book side
)

var bps = decimal.NewFromInt(10_000)

// Quote amounts are raw integer strings: buy in = USDC atoms, out = base atoms; sell the reverse.
type Quote struct {
	Market        streams.Addr `json:"market"`
	Side          string       `json:"side"`
	Amount        string       `json:"amount"`
	Fillable      string       `json:"fillable"` // part of amount the book absorbs
	Partial       bool         `json:"partial"`
	ExpectedOut   string       `json:"expectedOut"`
	MinOut        string       `json:"minOut"`
	OrderSize     string       `json:"orderSize"` // first arg of the Kuru call: buy quoteSize (USD×pricePrecision), sell size (sizePrecision units)
	AvgPx         streams.Dec  `json:"avgPx"`
	BestPx        streams.Dec  `json:"bestPx"`
	ImpactBps     int64        `json:"impactBps"` // avgPx vs best level
	MaxSafeAmount string       `json:"maxSafeAmount"`
	MaxImpactBps  int64        `json:"maxImpactBps"`
}

// Market quotes a buy (amount = USDC atoms) or sell (amount = base atoms) walking the book.
func Market(m streams.MarketInfo, b streams.BookSnapshot, side string, amount decimal.Decimal, slippageBps, maxImpactBps int64) (Quote, error) {
	if !amount.IsPositive() || slippageBps < 0 || slippageBps > 10_000 || maxImpactBps < 0 || m.SizePrecision.IsZero() || m.PricePrecision.IsZero() {
		return Quote{}, ErrBadInput
	}
	qd, bd := int32(m.QuoteDecimals), int32(m.BaseDecimals)
	step := decimal.NewFromInt(1).Div(m.SizePrecision)
	levels := b.Asks
	if side == "sell" {
		levels = b.Bids
	} else if side != "buy" {
		return Quote{}, ErrBadInput
	}
	if len(levels) == 0 || !levels[0][0].IsPositive() {
		return Quote{}, ErrUnavailable
	}
	q := Quote{Market: m.Market, Side: side, Amount: amount.String(), BestPx: levels[0][0], MaxImpactBps: maxImpactBps}

	var in, base, quote decimal.Decimal // display units
	var exhausted bool
	if side == "buy" {
		qs := amount.Shift(-qd).Mul(m.PricePrecision).Floor()
		q.OrderSize = qs.String()
		in = qs.Div(m.PricePrecision)
		base, quote, exhausted = walk(levels, in, step, true)
	} else {
		in = amount.Shift(-bd).Div(step).Floor().Mul(step)
		q.OrderSize = in.Mul(m.SizePrecision).String()
		base, quote, exhausted = walk(levels, in, step, false)
	}
	if base.LessThan(m.MinSize.Div(m.SizePrecision)) || !base.IsPositive() {
		return Quote{}, ErrMinSize
	}
	var out decimal.Decimal
	if side == "buy" {
		q.Fillable, out = quote.Shift(qd).Ceil().String(), base.Shift(bd).Floor()
	} else {
		q.Fillable, out = base.Shift(bd).String(), quote.Shift(qd).Floor()
	}
	q.Partial = exhausted
	q.ExpectedOut = out.String()
	q.MinOut = out.Mul(bps.Sub(decimal.NewFromInt(slippageBps))).Div(bps).Floor().String()
	q.AvgPx = quote.DivRound(base, 18)
	q.ImpactBps = q.AvgPx.Sub(q.BestPx).Abs().Mul(bps).Div(q.BestPx).Round(0).IntPart()
	safe := maxSafe(levels, step, maxImpactBps, side == "buy")
	if side == "buy" {
		q.MaxSafeAmount = safe.Shift(qd).Floor().String()
	} else {
		q.MaxSafeAmount = safe.Shift(bd).Floor().String()
	}
	return q, nil
}

// walk consumes levels until `in` (quote for buys, base for sells) runs out; sizes snap to step. exhausted = the book ran out first.
func walk(levels []streams.Level, in, step decimal.Decimal, buy bool) (base, quote decimal.Decimal, exhausted bool) {
	rem := in
	for _, lv := range levels {
		px, sz := lv[0], lv[1]
		if !px.IsPositive() {
			continue
		}
		take := sz
		if buy {
			take = decimal.Min(sz, rem.Div(px).Div(step).Floor().Mul(step))
		} else {
			take = decimal.Min(sz, rem)
		}
		if !take.IsPositive() {
			return base, quote, false
		}
		base, quote = base.Add(take), quote.Add(px.Mul(take))
		if buy {
			rem = rem.Sub(px.Mul(take))
		} else {
			rem = rem.Sub(take)
		}
		if take.LessThan(sz) {
			return base, quote, false
		}
	}
	return base, quote, rem.IsPositive()
}

// maxSafe is the largest input (quote for buys, base for sells, display) whose average price stays within capBps of the best level.
func maxSafe(levels []streams.Level, step decimal.Decimal, capBps int64, buy bool) decimal.Decimal {
	f := bps.Add(decimal.NewFromInt(capBps))
	if !buy {
		f = bps.Sub(decimal.NewFromInt(capBps))
	}
	capPx := levels[0][0].Mul(f).Div(bps)
	var base, quote decimal.Decimal
	for _, lv := range levels {
		px, sz := lv[0], lv[1]
		if !px.IsPositive() {
			continue
		}
		nb, nq := base.Add(sz), quote.Add(px.Mul(sz))
		within := nq.LessThanOrEqual(capPx.Mul(nb))
		if !buy {
			within = nq.GreaterThanOrEqual(capPx.Mul(nb))
		}
		if within {
			base, quote = nb, nq
			continue
		}
		// partial level x solves (quote + px·x) / (base + x) = capPx
		x := capPx.Mul(base).Sub(quote).DivRound(px.Sub(capPx), 18).Div(step).Floor().Mul(step)
		if x.IsPositive() {
			base, quote = base.Add(x), quote.Add(px.Mul(x))
		}
		break
	}
	if buy {
		return quote
	}
	return base
}

type BasketLeg struct {
	Market     streams.Addr `json:"market"`
	WeightBps  int64        `json:"weightBps,omitempty"`  // buy
	AmountUsdc string       `json:"amountUsdc,omitempty"` // buy: this leg's USDC atoms
	Amount     string       `json:"amount,omitempty"`     // sell: base atoms held for the basket
	Quote      *Quote       `json:"quote,omitempty"`
	Error      string       `json:"error,omitempty"`
}

type Basket struct {
	Side            string      `json:"side"`
	AmountUsdc      string      `json:"amountUsdc,omitempty"`   // buy
	FillableUsdc    string      `json:"fillableUsdc,omitempty"` // buy
	ExpectedOutUsdc string      `json:"expectedOutUsdc,omitempty"`
	MinOutUsdc      string      `json:"minOutUsdc,omitempty"` // sell: sum of leg minOut
	Partial         bool        `json:"partial"`
	Ok              bool        `json:"ok"` // every leg quoted
	Legs            []BasketLeg `json:"legs"`
}

// BuyBasket splits amountUsdc (atoms) by weight and quotes a market buy per leg; leg i uses markets[i] and books[i].
func BuyBasket(amount decimal.Decimal, weights []int64, markets []streams.MarketInfo, books []streams.BookSnapshot, slippageBps, maxImpactBps int64) Basket {
	r := Basket{Side: "buy", AmountUsdc: amount.String(), Ok: true, Legs: []BasketLeg{}}
	fill := decimal.Zero
	for i, w := range weights {
		alloc := amount.Mul(decimal.NewFromInt(w)).Div(bps).Floor()
		leg := BasketLeg{Market: markets[i].Market, WeightBps: w, AmountUsdc: alloc.String()}
		if q, ok := r.add(&leg, markets[i], books[i], "buy", alloc, slippageBps, maxImpactBps); ok {
			fill = fill.Add(decimal.RequireFromString(q.Fillable))
		}
		r.Legs = append(r.Legs, leg)
	}
	r.FillableUsdc = fill.String()
	return r
}

// SellBasket quotes a market sell of amounts[i] (base atoms) per leg and totals the USDC out.
func SellBasket(amounts []decimal.Decimal, markets []streams.MarketInfo, books []streams.BookSnapshot, slippageBps, maxImpactBps int64) Basket {
	r := Basket{Side: "sell", Ok: true, Legs: []BasketLeg{}}
	out, minOut := decimal.Zero, decimal.Zero
	for i, a := range amounts {
		leg := BasketLeg{Market: markets[i].Market, Amount: a.String()}
		if q, ok := r.add(&leg, markets[i], books[i], "sell", a, slippageBps, maxImpactBps); ok {
			out, minOut = out.Add(decimal.RequireFromString(q.ExpectedOut)), minOut.Add(decimal.RequireFromString(q.MinOut))
		}
		r.Legs = append(r.Legs, leg)
	}
	r.ExpectedOutUsdc, r.MinOutUsdc = out.String(), minOut.String()
	return r
}

func (r *Basket) add(leg *BasketLeg, m streams.MarketInfo, b streams.BookSnapshot, side string, amt decimal.Decimal, slip, impact int64) (Quote, bool) {
	q, err := Market(m, b, side, amt, slip, impact)
	if err != nil {
		leg.Error, r.Ok = err.Error(), false
		return q, false
	}
	leg.Quote, r.Partial = &q, r.Partial || q.Partial
	return q, true
}
