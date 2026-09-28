package market

import (
	"encoding/binary"
	"math"
	"math/big"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/streams"
)

type wb []byte

func (w *wb) u8(v byte)    { *w = append(*w, v) }
func (w *wb) u32(v uint32) { *w = binary.BigEndian.AppendUint32(*w, v) }
func (w *wb) u64(v uint64) { *w = binary.BigEndian.AppendUint64(*w, v) }
func (w *wb) u128(v int64) { w.u64(0); w.u64(uint64(v)) }
func (w *wb) pad(n int)    { *w = append(*w, make([]byte, n)...) }
func (w *wb) addr(b byte)  { w.pad(19); w.u8(b) }
func hdr(id uint64, kind byte) *wb {
	w := &wb{}
	w.u64(id)
	*w = append(*w, "KXMD"...)
	w.u8(1)
	w.u8(kind)
	w.u8(3)
	w.u8(1)
	w.u64(285)
	return w
}

func snapBytes(seq uint64, bids, asks [][2]int64) []byte {
	w := hdr(1, kindBook)
	w.addr(0xab)
	w.u64(seq)
	w.pad(8 + 40 + 8 + 4 + 13)
	w.u32(uint32(len(bids)))
	w.u32(uint32(len(asks)))
	for _, l := range append(bids, asks...) {
		w.u128(l[0])
		w.u128(l[1])
	}
	return *w
}

func deltaBytes(seq, prev uint64, ups ...[3]int64) []byte {
	w := hdr(1, kindDelta)
	w.addr(0xab)
	w.u64(seq)
	w.u64(prev)
	w.pad(8 + 40)
	w.u32(uint32(len(ups)))
	for _, u := range ups {
		w.u8(byte(u[0]))
		w.u64(uint64(u[1]))
		w.u128(u[2])
		w.pad(16 + 16 + 4)
	}
	return *w
}

func mustDecode(t *testing.T, b []byte) *frame {
	t.Helper()
	f, err := decode(b)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestDecodeTrades(t *testing.T) {
	w := hdr(9, kindTrades)
	w.addr(0xcd)
	w.u64(10)
	w.u64(7)
	w.pad(48)
	w.u32(1)
	w.u64(1)
	w.pad(2)
	w.u8(2)
	w.u64(8604591)
	w.u128(2326)
	w.u64(1790070859)
	f := mustDecode(t, *w)
	if f.ID != 9 || f.Seq != 10 || f.Prev != 7 || len(f.Fills) != 1 || f.Fills[0].Buy || f.Fills[0].Px != 8604591 || f.Fills[0].Sz.Int64() != 2326 || f.Fills[0].Ts != 1790070859 {
		t.Fatalf("%+v %+v", f, f.Fills)
	}
	if _, err := decode((*w)[:len(*w)-3]); err == nil {
		t.Fatal("truncated frame must fail")
	}
}

func TestBookSnapshotDeltasGap(t *testing.T) {
	info := streams.MarketInfo{PricePrecision: decimal.NewFromInt(100), SizePrecision: decimal.NewFromInt(100000000)}
	var b book
	if ch, gap := b.apply(mustDecode(t, deltaBytes(5, 4, [3]int64{1, 100, 1}))); ch || gap {
		t.Fatal("delta before snapshot must be ignored")
	}
	b.snapshot(mustDecode(t, snapBytes(10, [][2]int64{{8543040, 2326}, {8530168, 703385}}, [][2]int64{{8600000, 5}})))
	// retained delta already in the snapshot
	if ch, gap := b.apply(mustDecode(t, deltaBytes(9, 8, [3]int64{1, 1, 1}))); ch || gap {
		t.Fatal("seq <= snapshot must be skipped")
	}
	// bridge: prev <= snapshot seq, sparse
	if ch, gap := b.apply(mustDecode(t, deltaBytes(13, 7, [3]int64{1, 8530168, 0}, [3]int64{2, 8590000, 9}))); !ch || gap {
		t.Fatal("bridge frame rejected")
	}
	bids, asks := b.levels(info)
	if len(bids) != 1 || bids[0][0].String() != "85430.4" || bids[0][1].String() != "0.00002326" {
		t.Fatal(bids)
	}
	if len(asks) != 2 || asks[0][0].String() != "85900" || asks[1][0].String() != "86000" {
		t.Fatal(asks)
	}
	// linked frame
	if ch, gap := b.apply(mustDecode(t, deltaBytes(20, 13, [3]int64{1, 8500000, 3}))); !ch || gap {
		t.Fatal("linked frame rejected")
	}
	// prev must equal last seq exactly
	if _, gap := b.apply(mustDecode(t, deltaBytes(25, 21))); !gap || b.synced {
		t.Fatal("gap not detected")
	}
}

func TestBookBridgeGap(t *testing.T) {
	var b book
	b.snapshot(mustDecode(t, snapBytes(10, nil, nil)))
	if _, gap := b.apply(mustDecode(t, deltaBytes(12, 11))); !gap {
		t.Fatal("bridge with prev > snapshot seq is a gap")
	}
}

func TestTopDepth(t *testing.T) {
	b := book{bids: map[int64]*big.Int{}, asks: map[int64]*big.Int{}}
	for i := int64(1); i <= 30; i++ {
		b.bids[i] = big.NewInt(1)
		b.asks[100+i] = big.NewInt(1)
	}
	one := decimal.NewFromInt(1)
	bids, asks := b.levels(streams.MarketInfo{PricePrecision: one, SizePrecision: one})
	if len(bids) != 20 || bids[0][0].IntPart() != 30 || bids[19][0].IntPart() != 11 || len(asks) != 20 || asks[0][0].IntPart() != 101 {
		t.Fatal(bids, asks)
	}
}

func TestEWMA(t *testing.T) {
	var e ewma
	t0 := time.Unix(0, 0)
	e.observe(0, t0, time.Minute)
	if e.ref != 0 {
		t.Fatal("zero price must be ignored")
	}
	e.observe(100, t0, time.Minute)
	if e.ref != 100 || e.vol != 0 {
		t.Fatal(e)
	}
	e.observe(110, t0.Add(time.Minute), time.Minute)
	a := 1 - math.Exp(-1)
	if math.Abs(e.ref-(100+10*a)) > 1e-9 || math.Abs(e.vol-0.1*a) > 1e-9 {
		t.Fatal(e)
	}
	if !deviated(106, 100, 500) || deviated(104, 100, 500) || deviated(0, 100, 500) {
		t.Fatal("deviation")
	}
}

func TestDiff(t *testing.T) {
	have := map[string]*mstate{"0xaa": {}, "0xbb": {}}
	added, removed := diff(have, []streams.MarketInfo{{Market: "0xBB"}, {Market: "0xcc"}})
	if len(added) != 1 || added[0].Market != "0xcc" || len(removed) != 1 || removed[0] != "0xaa" {
		t.Fatal(added, removed)
	}
}
