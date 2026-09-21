// Package candle turns ticks into OHLCV. Kuru only: Perpl serves its own
// candles and theirs are authoritative.
package candle

import (
	"math/big"
	"sync"
)

// Resolutions we aggregate, in seconds. Matches what the chart offers.
var Resolutions = []int{60, 300, 900, 3600, 14400, 86400}

type Tick struct {
	MarketID string `json:"marketId"`
	Venue    string `json:"venue"`
	Price    string `json:"price"`
	Size     string `json:"size"`
	Side     string `json:"side"`
	Ts       int64  `json:"ts"`
}

type Candle struct {
	MarketID   string `json:"marketId"`
	Resolution int    `json:"resolution"`
	OpenTime   int64  `json:"openTime"`
	Open       string `json:"open"`
	High       string `json:"high"`
	Low        string `json:"low"`
	Close      string `json:"close"`
	Volume     string `json:"volume"`
}

type key struct {
	market   string
	res      int
	openTime int64
}

// Agg is safe for concurrent Add and Flush.
type Agg struct {
	mu      sync.Mutex
	res     []int
	open    map[key]*Candle
	dirty   map[key]bool
	highest map[string]int64 // newest tick seen per market, for eviction
}

func New(res ...int) *Agg {
	if len(res) == 0 {
		res = Resolutions
	}
	return &Agg{res: res, open: map[key]*Candle{}, dirty: map[key]bool{}, highest: map[string]int64{}}
}

// OpenTime floors a millisecond timestamp onto the resolution grid, in seconds.
func OpenTime(tsMillis int64, res int) int64 {
	sec := tsMillis / 1000
	if sec < 0 {
		sec -= int64(res) - 1
	}
	return sec - sec%int64(res)
}

func (a *Agg) Add(t Tick) {
	if t.Price == "" {
		return
	}
	price, ok := new(big.Int).SetString(t.Price, 10)
	if !ok || price.Sign() <= 0 {
		return
	}
	size, ok := new(big.Int).SetString(t.Size, 10)
	if !ok {
		size = new(big.Int)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if t.Ts > a.highest[t.MarketID] {
		a.highest[t.MarketID] = t.Ts
	}
	for _, r := range a.res {
		k := key{market: t.MarketID, res: r, openTime: OpenTime(t.Ts, r)}
		c := a.open[k]
		if c == nil {
			c = &Candle{
				MarketID: t.MarketID, Resolution: r, OpenTime: k.openTime,
				Open: price.String(), High: price.String(), Low: price.String(),
				Close: price.String(), Volume: "0",
			}
			a.open[k] = c
		}
		if price.Cmp(mustInt(c.High)) > 0 {
			c.High = price.String()
		}
		if price.Cmp(mustInt(c.Low)) < 0 {
			c.Low = price.String()
		}
		c.Close = price.String()
		c.Volume = new(big.Int).Add(mustInt(c.Volume), size).String()
		a.dirty[k] = true
	}
}

// Flush returns every candle touched since the last call and evicts buckets
// that closed more than one resolution ago.
func (a *Agg) Flush() []Candle {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]Candle, 0, len(a.dirty))
	for k := range a.dirty {
		if c := a.open[k]; c != nil {
			out = append(out, *c)
		}
	}
	clear(a.dirty)
	for k := range a.open {
		if a.highest[k.market] >= (k.openTime+2*int64(k.res))*1000 {
			delete(a.open, k)
		}
	}
	return out
}

func mustInt(s string) *big.Int {
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return new(big.Int)
	}
	return n
}
