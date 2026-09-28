package market

import (
	"math/big"
	"sort"

	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/streams"
)

const depth = 20

// book is one market's full L2 in raw units, rebuilt from an l2Delta snapshot.
type book struct {
	synced  bool
	bridged bool
	seq     uint64
	bids    map[int64]*big.Int
	asks    map[int64]*big.Int
}

func (b *book) snapshot(f *frame) {
	b.bids, b.asks = map[int64]*big.Int{}, map[int64]*big.Int{}
	for _, l := range f.Bids {
		b.bids[l.Px] = l.Sz
	}
	for _, l := range f.Asks {
		b.asks[l.Px] = l.Sz
	}
	b.synced, b.bridged, b.seq = true, false, f.Seq
}

// apply reports changed=false for frames already covered by the snapshot, gap=true when continuity breaks (§6.3).
func (b *book) apply(f *frame) (changed, gap bool) {
	if !b.synced || f.Seq <= b.seq {
		return false, false
	}
	if b.bridged && f.Prev != b.seq || !b.bridged && f.Prev > b.seq {
		b.synced = false
		return false, true
	}
	for _, u := range f.Update {
		side := b.asks
		if u.Buy {
			side = b.bids
		}
		if u.Sz.Sign() == 0 {
			delete(side, u.Px)
		} else {
			side[u.Px] = u.Sz
		}
	}
	b.bridged, b.seq = true, f.Seq
	return true, false
}

func top(side map[int64]*big.Int, desc bool, pp, sp decimal.Decimal) []streams.Level {
	px := make([]int64, 0, len(side))
	for p := range side {
		px = append(px, p)
	}
	sort.Slice(px, func(i, j int) bool { return (px[i] > px[j]) == desc })
	if len(px) > depth {
		px = px[:depth]
	}
	out := make([]streams.Level, len(px))
	for i, p := range px {
		out[i] = streams.Level{decimal.NewFromInt(p).Div(pp), decimal.NewFromBigInt(side[p], 0).Div(sp)}
	}
	return out
}

// levels returns top-20 display levels: bids high→low, asks low→high.
func (b *book) levels(m streams.MarketInfo) (bids, asks []streams.Level) {
	return top(b.bids, true, m.PricePrecision, m.SizePrecision), top(b.asks, false, m.PricePrecision, m.SizePrecision)
}
