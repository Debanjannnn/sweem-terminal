// Package quote mirrors SweemBasket.mint/redeem against the publisher's book snapshots.
package quote

import (
	"errors"
	"strings"

	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/streams"
)

var (
	ErrBadInput    = errors.New("bad_request")
	ErrUnavailable = errors.New("unavailable") // missing book/market/liquidity
)

const (
	shareDecimals = 18
	defaultFeePps = 7000 // live getMarketParams value on every testnet market
	ppsDigits     = 7    // fee = amount * pps / 1e7
)

var bps = decimal.NewFromInt(10_000)

type Leg struct {
	Market    streams.Addr `json:"market"`
	In        string       `json:"in"`  // raw atoms of input token
	Out       string       `json:"out"` // raw atoms of output token, after taker fee
	AvgPx     streams.Dec  `json:"avgPx"`
	ImpactBps int64        `json:"impactBps"` // avgPx vs best level on the walked side
}

type Result struct {
	ExpectedOut string `json:"expectedOut"`
	MinOut      string `json:"minOut"`
	Legs        []Leg  `json:"legs"`
	Stale       bool   `json:"stale"`
}

// State is everything the contract reads, as the publisher last saw it (display units).
type State struct {
	Basket  streams.BasketInfo
	Nav     streams.NavPoint
	Markets map[streams.Addr]streams.MarketInfo
	Books   map[streams.Addr]streams.BookSnapshot
}

type leg struct {
	info streams.MarketInfo
	book streams.BookSnapshot
	w    decimal.Decimal
	held decimal.Decimal
	val  decimal.Decimal
}

func (s State) legs() ([]leg, error) {
	bal := map[streams.Addr]streams.NavComponent{}
	for _, c := range s.Nav.Components {
		bal[streams.Addr(strings.ToLower(string(c.Market)))] = c
	}
	out := make([]leg, 0, len(s.Basket.Components))
	for _, c := range s.Basket.Components {
		m := streams.Addr(strings.ToLower(string(c.Market)))
		info, ok := s.Markets[m]
		book, ok2 := s.Books[m]
		if !ok || !ok2 {
			return nil, ErrUnavailable
		}
		out = append(out, leg{info, book, decimal.NewFromInt(int64(c.WeightBps)), bal[m].Balance, bal[m].Value})
	}
	return out, nil
}

// Mint mirrors SweemBasket.mint for usdcRaw (6dp integer) and returns shares (18dp integer).
func Mint(s State, usdcRaw decimal.Decimal, slippageBps int64) (Result, error) {
	legs, err := s.legs()
	if err != nil {
		return Result{}, err
	}
	if len(legs) == 0 || !usdcRaw.IsPositive() {
		return Result{}, ErrBadInput
	}
	qd := int32(legs[0].info.QuoteDecimals)
	usdc := usdcRaw.Shift(-qd)
	supply := s.Nav.Supply

	total := decimal.Zero
	for _, l := range legs {
		total = total.Add(l.val)
	}
	if supply.IsPositive() && !total.IsPositive() {
		return Result{}, ErrUnavailable // contract divides by zero
	}

	res := Result{Legs: []Leg{}}
	spent := decimal.Zero
	var shares *decimal.Decimal
	for _, l := range legs {
		var alloc decimal.Decimal
		if supply.IsZero() {
			alloc = floorDiv(usdc.Mul(l.w), bps, qd)
		} else {
			alloc = floorDiv(usdc.Mul(l.val), total, qd)
		}
		if alloc.IsZero() {
			continue
		}
		cost, base, err := buy(l.info, l.book.Asks, alloc)
		if err != nil {
			return Result{}, err
		}
		bd := int32(l.info.BaseDecimals)
		// ponytail: taker fee modelled as a haircut on base received; confirm against a fork mint if quotes drift
		bought := base.Sub(base.Mul(fee(l.info)).Shift(-ppsDigits).RoundCeil(bd))
		spent = spent.Add(cost)
		res.Legs = append(res.Legs, mkLeg(l.info.Market, cost.Shift(qd), bought.Shift(bd), cost, base, l.book.Asks))
		if supply.IsPositive() && l.held.IsPositive() {
			sh := floorDiv(supply.Mul(bought), l.held, shareDecimals)
			if shares == nil || sh.LessThan(*shares) {
				shares = &sh
			}
		}
	}
	out := decimal.Zero
	if supply.IsZero() {
		out = spent // shares = spent * 1e12, i.e. 1 share per USDC
	} else if shares != nil {
		out = *shares
	}
	if !out.IsPositive() {
		return Result{}, ErrBadInput // contract reverts ZeroAmount
	}
	return finish(res, out.Shift(shareDecimals), slippageBps), nil
}

// Redeem mirrors SweemBasket.redeem for sharesRaw (18dp integer) and returns USDC (6dp integer).
func Redeem(s State, sharesRaw decimal.Decimal, slippageBps int64) (Result, error) {
	legs, err := s.legs()
	if err != nil {
		return Result{}, err
	}
	shares := sharesRaw.Shift(-shareDecimals)
	supply := s.Nav.Supply
	if len(legs) == 0 || !shares.IsPositive() || shares.GreaterThan(supply) {
		return Result{}, ErrBadInput
	}
	qd := int32(legs[0].info.QuoteDecimals)
	res := Result{Legs: []Leg{}}
	total := decimal.Zero
	for _, l := range legs {
		bd := int32(l.info.BaseDecimals)
		amt := floorDiv(l.held.Mul(shares), supply, bd)
		amt = amt.Sub(amt.Mod(step(l.info))) // floorToLot
		if amt.IsZero() {
			continue
		}
		usdc, sold, err := sell(l.info, l.book.Bids, amt)
		if err != nil {
			return Result{}, err
		}
		got := usdc.Sub(usdc.Mul(fee(l.info)).Shift(-ppsDigits).RoundCeil(qd))
		total = total.Add(got)
		res.Legs = append(res.Legs, mkLeg(l.info.Market, sold.Shift(bd), got.Shift(qd), usdc, sold, l.book.Bids))
	}
	if !total.IsPositive() {
		return Result{}, ErrBadInput
	}
	return finish(res, total.Shift(qd), slippageBps), nil
}

// buy spends up to budget USDC walking asks; returns USDC spent and base bought (display).
func buy(m streams.MarketInfo, asks []streams.Level, budget decimal.Decimal) (cost, base decimal.Decimal, err error) {
	if len(asks) == 0 {
		return cost, base, ErrUnavailable
	}
	st := step(m)
	rem := budget
	for _, lv := range asks {
		px, sz := lv[0], lv[1]
		if !px.IsPositive() {
			continue
		}
		s := decimal.Min(sz, floorDiv(rem, px.Mul(st), 0).Mul(st))
		c := notional(m, px, s)
		if !s.IsPositive() {
			break
		}
		base, cost, rem = base.Add(s), cost.Add(c), rem.Sub(c)
		if s.LessThan(sz) {
			break
		}
	}
	return cost, base, nil
}

// sell walks bids with amt base; returns USDC received and base actually sold (display).
func sell(m streams.MarketInfo, bids []streams.Level, amt decimal.Decimal) (usdc, sold decimal.Decimal, err error) {
	if len(bids) == 0 {
		return usdc, sold, ErrUnavailable
	}
	for _, lv := range bids {
		s := decimal.Min(lv[1], amt.Sub(sold))
		if !s.IsPositive() {
			break
		}
		usdc, sold = usdc.Add(notional(m, lv[0], s)), sold.Add(s)
	}
	return usdc, sold, nil
}

// notional is Kuru's quoteRaw = floor(floor(price*size/sizePrecision) * 10^qd / pricePrecision), in display units.
func notional(m streams.MarketInfo, px, sz decimal.Decimal) decimal.Decimal {
	return px.Mul(sz).RoundFloor(digits(m.PricePrecision)).RoundFloor(int32(m.QuoteDecimals))
}

func step(m streams.MarketInfo) decimal.Decimal { return decimal.New(1, -digits(m.SizePrecision)) }

// digits assumes Kuru precisions are powers of ten (true on every live market).
func digits(p decimal.Decimal) int32 { return int32(len(p.BigInt().String()) - 1) }

func fee(m streams.MarketInfo) decimal.Decimal {
	if m.TakerFeePps.IsZero() {
		return decimal.NewFromInt(defaultFeePps)
	}
	return m.TakerFeePps
}

// floorDiv is a/b truncated to places, exact (Div alone rounds at 16 digits).
func floorDiv(a, b decimal.Decimal, places int32) decimal.Decimal {
	q, _ := a.QuoRem(b, places)
	return q
}

func mkLeg(m streams.Addr, in, out, quote, base decimal.Decimal, side []streams.Level) Leg {
	l := Leg{Market: m, In: in.String(), Out: out.String()}
	if base.IsPositive() {
		l.AvgPx = floorDiv(quote, base, 18)
		best := side[0][0]
		l.ImpactBps = l.AvgPx.Sub(best).Abs().Mul(bps).Div(best).Round(0).IntPart()
	}
	return l
}

func finish(r Result, out decimal.Decimal, slippageBps int64) Result {
	r.ExpectedOut = out.String()
	r.MinOut = floorDiv(out.Mul(decimal.NewFromInt(10_000-slippageBps)), bps, 0).String()
	return r
}
