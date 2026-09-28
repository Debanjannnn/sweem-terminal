// Package mm is the seeder's quoting strategy: ladder construction, requote decision, and the loop.
package mm

import (
	"math"
	"time"

	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/streams"
	"github.com/snehendu098/sweem-terminal/services/seeder/internal/executor"
)

type Params struct {
	Levels         int
	SpreadBps      int64 // first level distance from ref
	StepBps        int64 // extra distance per further level
	LevelUSD       decimal.Decimal
	MinNotionalUSD decimal.Decimal // Kuru minQuoteNotional (10 USDC on testnet)
}

// Inventory is what one market's ladder may use: raw token atoms and a per-side USD cap.
type Inventory struct {
	Base, Quote decimal.Decimal
	SideUSD     decimal.Decimal
}

var (
	bps     = decimal.NewFromInt(10_000)
	feeRoom = decimal.RequireFromString("1.01") // bids reserve quote plus maker fee headroom
	maxU32  = decimal.NewFromInt(math.MaxUint32)
)

func pow10(n int) decimal.Decimal { return decimal.New(1, int32(n)) }

// QuoteRaw is Kuru's notional: floor(floor(price*size/sizePrecision) * 10^quoteDecimals / pricePrecision).
func QuoteRaw(m streams.MarketInfo, price, size decimal.Decimal) decimal.Decimal {
	return price.Mul(size).Div(m.SizePrecision).Floor().Mul(pow10(m.QuoteDecimals)).Div(m.PricePrecision).Floor()
}

// Ladder builds GTC bids then asks around ref (display quote-per-base) in the market's integer units.
func Ladder(m streams.MarketInfo, ref decimal.Decimal, p Params, inv Inventory) []executor.Order {
	if !ref.IsPositive() || !m.TickSize.IsPositive() || !m.PricePrecision.IsPositive() || !m.SizePrecision.IsPositive() {
		return nil
	}
	minQuote := p.MinNotionalUSD.Mul(pow10(m.QuoteDecimals))
	var out []executor.Order
	for _, side := range []string{"buy", "sell"} {
		budget := inv.SideUSD
		quoteLeft, baseLeft := inv.Quote, inv.Base
		for i := 0; i < p.Levels; i++ {
			off := decimal.NewFromInt(p.SpreadBps + int64(i)*p.StepBps).Div(bps)
			var px decimal.Decimal
			if side == "buy" {
				px = ref.Mul(decimal.NewFromInt(1).Sub(off)).Mul(m.PricePrecision).Div(m.TickSize).Floor().Mul(m.TickSize)
			} else {
				px = ref.Mul(decimal.NewFromInt(1).Add(off)).Mul(m.PricePrecision).Div(m.TickSize).Ceil().Mul(m.TickSize)
			}
			if !px.IsPositive() || px.GreaterThan(maxU32) {
				continue
			}
			usd := decimal.Min(p.LevelUSD, budget)
			size := usd.Mul(m.PricePrecision).Div(px).Mul(m.SizePrecision).Floor()
			if side == "buy" {
				size = decimal.Min(size, quoteLeft.Div(feeRoom).Mul(m.PricePrecision).Mul(m.SizePrecision).Div(px.Mul(pow10(m.QuoteDecimals))).Floor())
			} else {
				size = decimal.Min(size, baseLeft.Mul(m.SizePrecision).Div(pow10(m.BaseDecimals)).Floor())
			}
			notional := QuoteRaw(m, px, size)
			if !size.IsPositive() || size.LessThan(m.MinSize) || notional.LessThan(minQuote) {
				continue
			}
			budget = budget.Sub(notional.Div(pow10(m.QuoteDecimals)))
			if side == "buy" {
				quoteLeft = quoteLeft.Sub(notional.Mul(feeRoom))
			} else {
				baseLeft = baseLeft.Sub(size.Mul(pow10(m.BaseDecimals)).Div(m.SizePrecision))
			}
			out = append(out, executor.Order{Side: side, Price: px.String(), Size: size.String(), Tif: "gtc"})
		}
	}
	return out
}

// Due reports whether to requote: never quoted, ref moved more than thresholdBps, or every has elapsed.
func Due(lastRef, ref decimal.Decimal, sinceLast, every time.Duration, thresholdBps int64) bool {
	if lastRef.IsZero() || sinceLast >= every {
		return true
	}
	return ref.Sub(lastRef).Abs().Mul(bps).Div(lastRef).GreaterThan(decimal.NewFromInt(thresholdBps))
}
