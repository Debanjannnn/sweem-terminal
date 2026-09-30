package market

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/streams"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

var weth = streams.MarketInfo{Market: "0xa6afd386135b7d41a6c40c525abc4a1019b0d132", PricePrecision: d("10000"), SizePrecision: d("10000000000")}

func TestLevels(t *testing.T) {
	// raw pairs copied from a live WETH_USDC depthUpdate
	bids, asks, err := levels(weth,
		[][2]string{{"10000000000000000", "100000000000"}, {"2671090000000000000000", "7486000000"}, {"2670130000000000000000", "0"}},
		[][2]string{{"2674150000000000000000", "748650269"}, {"2672710000000000000000", "7486000000"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(bids) != 2 || !bids[0][0].Equal(d("2671.09")) || !bids[0][1].Equal(d("0.7486")) || !bids[1][0].Equal(d("0.01")) {
		t.Fatal("bids", bids)
	}
	if len(asks) != 2 || !asks[0][0].Equal(d("2672.71")) || !asks[1][1].Equal(d("0.0748650269")) {
		t.Fatal("asks", asks)
	}
	var many [][2]string
	for i := 1; i <= 30; i++ {
		many = append(many, [2]string{decimal.NewFromInt(int64(i)).Shift(18).String(), "1"})
	}
	if b, _, _ := levels(weth, many, nil); len(b) != depth || !b[0][0].Equal(d("30")) {
		t.Fatal("depth cap / order", len(b), b[0])
	}
	if _, _, err := levels(weth, [][2]string{{"x", "1"}}, nil); err == nil {
		t.Fatal("bad price accepted")
	}
}

func TestCandleApply(t *testing.T) {
	tr := func(ts int64, px, sz string) streams.Trade {
		return streams.Trade{Market: weth.Market, Ts: ts, Px: d(px), Sz: d(sz)}
	}
	base := int64(1_790_731_200_000) // minute-aligned
	var c *streams.Candle
	c = apply(c, tr(base+1_000, "10", "1"), "1m", time.Minute)
	c = apply(c, tr(base+2_000, "12", "2"), "1m", time.Minute)
	c = apply(c, tr(base+3_000, "9", "1"), "1m", time.Minute)
	c = apply(c, tr(base+4_000, "11", "0.5"), "1m", time.Minute)
	if c.T != base || !c.O.Equal(d("10")) || !c.H.Equal(d("12")) || !c.L.Equal(d("9")) || !c.C.Equal(d("11")) ||
		!c.V.Equal(d("4.5")) || !c.Q.Equal(d("48.5")) || c.N != 4 {
		t.Fatalf("%+v", c)
	}
	old := apply(c, tr(base-1, "1", "1"), "1m", time.Minute)
	if old != c || c.N != 4 {
		t.Fatal("older trade must be ignored")
	}
	next := apply(c, tr(base+60_000, "13", "1"), "1m", time.Minute)
	if next == c || next.T != base+60_000 || !next.O.Equal(d("13")) || next.N != 1 {
		t.Fatalf("new bucket %+v", next)
	}
	day := apply(nil, tr(base+5_000, "1", "1"), "1d", 24*time.Hour)
	if time.UnixMilli(day.T).UTC().Format(time.RFC3339) != "2026-09-30T00:00:00Z" {
		t.Fatal("1d bucket not UTC midnight", time.UnixMilli(day.T).UTC())
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

func TestDecodeLiveFrames(t *testing.T) {
	// verbatim mainnet frames; trade "b" is a block number, depth "b" is bids
	tr := []byte(`{"e":"trade","E":1790731958000,"T":1790731958,"s":"0xa6afd386135b7d41a6c40c525abc4a1019b0d132","t":"68392503","p":"2669390000000000000000","q":"564230269","b":109191782,"m":true,"M":true}`)
	var h wsHead
	if err := json.Unmarshal(tr, &h); err != nil || h.E != "trade" {
		t.Fatal(err, h)
	}
	got, key, err := decodeTrade(weth, h, tr)
	if err != nil || key != "0xa6afd386135b7d41a6c40c525abc4a1019b0d132:109191782:68392503:2669390000000000000000:564230269:true" || got.Ts != 1790731958000 || got.Side != "sell" || !got.Px.Equal(d("2669.39")) || !got.Sz.Equal(d("0.0564230269")) {
		t.Fatal(err, got)
	}
	buy := []byte(`{"e":"trade","E":1790731091000,"T":1790731091,"s":"0x40c49f171202f91ff5d2fae34c22dd2bfdd22af0","t":"68386784","p":"83410070000000000000000","q":"2387500","b":109188910,"m":false,"M":true}`)
	if got, _, err := decodeTrade(weth, h, buy); err != nil || got.Side != "buy" {
		t.Fatal("taker buy (m=false, M=true) must stay buy", err, got.Side)
	}
	dep := []byte(`{"e":"depthUpdate","E":1790731050000,"T":1790731050,"s":"0xa6afd386135b7d41a6c40c525abc4a1019b0d132","U":109188776,"u":109188776,"b":[["2671090000000000000000","7486000000"]],"a":[["2672710000000000000000","7486000000"]]}`)
	var w wsDepth
	if err := json.Unmarshal(dep, &w); err != nil || len(w.Bids) != 1 || len(w.Asks) != 1 {
		t.Fatal(err, w)
	}
}
