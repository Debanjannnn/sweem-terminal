// Package quote walks a book to price a size. Pure big.Int, so the result can
// be handed to a contract call unchanged.
package quote

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/wad"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/book"
)

type Quote struct {
	MarketID string `json:"marketId"`
	Venue    string `json:"venue"`
	Side     string `json:"side"`
	Filled   string `json:"filled"`
	Notional string `json:"notional"`
	AvgPrice string `json:"avgPrice"`
	// always positive-adverse: distance of avgPrice from the touch
	ImpactBps int64 `json:"impactBps"`
	// true when the book was too thin to fill the whole size
	Partial bool `json:"partial"`
}

// FromBook fills sizeWad against the resting side. buy/long takes asks,
// sell/short takes bids.
func FromBook(b book.Book, side, sizeWad string) (Quote, error) {
	var levels []book.Level
	switch strings.ToLower(strings.TrimSpace(side)) {
	case "buy", "long":
		side, levels = "buy", b.Asks
	case "sell", "short":
		side, levels = "sell", b.Bids
	default:
		return Quote{}, fmt.Errorf("side %q must be buy, sell, long or short", side)
	}
	size, ok := new(big.Int).SetString(strings.TrimSpace(sizeWad), 10)
	if !ok || size.Sign() <= 0 {
		return Quote{}, fmt.Errorf("size %q must be a positive WAD integer", sizeWad)
	}

	remaining := new(big.Int).Set(size)
	filled, notional := new(big.Int), new(big.Int)
	touch := new(big.Int)
	if len(levels) > 0 {
		touch = book.Int(levels[0].Price)
	}
	for _, l := range levels {
		if remaining.Sign() <= 0 {
			break
		}
		price, avail := book.Int(l.Price), book.Int(l.Size)
		take := avail
		if take.Cmp(remaining) > 0 {
			take = remaining
		}
		notional.Add(notional, wad.MulWad(take, price))
		filled.Add(filled, take)
		remaining.Sub(remaining, take)
	}

	q := Quote{
		MarketID: b.MarketID, Venue: b.Venue, Side: side,
		Filled: filled.String(), Notional: notional.String(), AvgPrice: "0",
		Partial: remaining.Sign() > 0,
	}
	if filled.Sign() == 0 || touch.Sign() == 0 {
		return q, nil
	}
	avg := wad.DivWad(notional, filled)
	q.AvgPrice = avg.String()
	bps := new(big.Int).Sub(avg, touch)
	bps.Mul(bps, big.NewInt(10_000))
	bps.Quo(bps, touch)
	if side == "sell" {
		bps.Neg(bps)
	}
	q.ImpactBps = bps.Int64()
	return q, nil
}
