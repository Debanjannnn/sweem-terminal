// Package book holds the venue-neutral L2 order book. Every number is a WAD
// decimal string: the browser, Postgres numeric(78,0) and the contracts all
// agree on that and none of them agree on float64.
package book

import (
	"math/big"
	"sort"
)

type Level struct {
	Price string `json:"price"`
	Size  string `json:"size"`
}

type Book struct {
	MarketID string  `json:"marketId"`
	Venue    string  `json:"venue"`
	Bids     []Level `json:"bids"`
	Asks     []Level `json:"asks"`
	Ts       int64   `json:"ts"`
}

func Int(s string) *big.Int {
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return new(big.Int)
	}
	return n
}

// Mid is the touch midpoint as a WAD string, or "" when either side is empty.
func (b *Book) Mid() string {
	if len(b.Bids) == 0 || len(b.Asks) == 0 {
		return ""
	}
	sum := new(big.Int).Add(Int(b.Bids[0].Price), Int(b.Asks[0].Price))
	return sum.Rsh(sum, 1).String()
}

// Apply merges an L2 delta. A level with size zero is a removal, which is how
// Perpl retires a price (it sends o:0 and we translate that to size 0).
func (b *Book) Apply(bids, asks []Level) {
	b.Bids = merge(b.Bids, bids, true)
	b.Asks = merge(b.Asks, asks, false)
}

// Trim caps each side at depth levels, best first.
func (b *Book) Trim(depth int) {
	if len(b.Bids) > depth {
		b.Bids = b.Bids[:depth]
	}
	if len(b.Asks) > depth {
		b.Asks = b.Asks[:depth]
	}
}

func merge(side, delta []Level, desc bool) []Level {
	if len(delta) == 0 {
		return Sort(side, desc)
	}
	byPrice := make(map[string]string, len(side)+len(delta))
	order := make([]string, 0, len(side)+len(delta))
	for _, l := range side {
		key := Int(l.Price).String()
		if _, seen := byPrice[key]; !seen {
			order = append(order, key)
		}
		byPrice[key] = l.Size
	}
	for _, l := range delta {
		key := Int(l.Price).String()
		if _, seen := byPrice[key]; !seen {
			order = append(order, key)
		}
		byPrice[key] = l.Size
	}
	out := make([]Level, 0, len(order))
	for _, p := range order {
		size := byPrice[p]
		if Int(size).Sign() <= 0 || Int(p).Sign() <= 0 {
			continue
		}
		out = append(out, Level{Price: p, Size: size})
	}
	return Sort(out, desc)
}

// Sort orders levels best-first: bids descending, asks ascending.
func Sort(side []Level, desc bool) []Level {
	sort.SliceStable(side, func(i, j int) bool {
		c := Int(side[i].Price).Cmp(Int(side[j].Price))
		if desc {
			return c > 0
		}
		return c < 0
	})
	return side
}
