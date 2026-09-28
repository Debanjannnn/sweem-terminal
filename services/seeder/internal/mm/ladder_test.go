package mm

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/streams"
	"github.com/snehendu098/sweem-terminal/services/seeder/internal/executor"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

// live WETH/USDC params, kuru-spot-v2 §8.2
var weth = streams.MarketInfo{Market: "0xa9c2936656a7d2143720bcd91ba8506200b7cbe7", BaseDecimals: 18, QuoteDecimals: 6,
	PricePrecision: d("100"), SizePrecision: d("10000000000"), TickSize: d("1"), MinSize: d("0")}

var params = Params{Levels: 2, SpreadBps: 30, StepBps: 20, LevelUSD: d("100"), MinNotionalUSD: d("10")}

var rich = Inventory{Base: d("1e18"), Quote: d("1e10"), SideUSD: d("1000")}

func sides(os []executor.Order) (b, a []executor.Order) {
	for _, o := range os {
		if o.Side == "buy" {
			b = append(b, o)
		} else {
			a = append(a, o)
		}
	}
	return
}

func TestLadderUnits(t *testing.T) {
	got := Ladder(weth, d("3000"), params, rich)
	want := []executor.Order{
		{Side: "buy", Price: "299100", Size: "334336342", Tif: "gtc"}, // 3000*(1-0.003)=2991.00; 100/2991 WETH in 1e10 units, floored
		{Side: "buy", Price: "298500", Size: "335008375", Tif: "gtc"}, // 50bps
		{Side: "sell", Price: "300900", Size: "332336324", Tif: "gtc"},
		{Side: "sell", Price: "301500", Size: "331674958", Tif: "gtc"},
	}
	if len(got) != len(want) {
		t.Fatal(got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d: got %+v want %+v", i, got[i], want[i])
		}
	}
	if q := QuoteRaw(weth, d("299100"), d("334336342")); q.String() != "99990000" {
		t.Fatal(q) // 99.99 USDC after Kuru's double floor
	}
}

func TestLadderTickRounding(t *testing.T) {
	m := weth
	m.TickSize = d("7")
	b, a := sides(Ladder(m, d("3000.03"), params, rich))
	for _, o := range append(b, a...) {
		if d(o.Price).Mod(d("7")).Sign() != 0 {
			t.Fatal("off tick", o)
		}
	}
	// bids round down, asks up: never inside the spread
	if d(b[0].Price).GreaterThan(d("299103")) || d(a[0].Price).LessThan(d("300903")) || !d(b[0].Price).LessThan(d(a[0].Price)) {
		t.Fatal(b[0], a[0])
	}
}

func TestLadderInventoryCaps(t *testing.T) {
	// 0.01 WETH (~30 USDC) covers one ask level; 50 USDC covers one bid level
	b, a := sides(Ladder(weth, d("3000"), params, Inventory{Base: d("1e16"), Quote: d("50000000"), SideUSD: d("1000")}))
	if len(a) != 1 || a[0].Size != "100000000" {
		t.Fatal("asks", a)
	}
	if len(b) != 1 || QuoteRaw(weth, d(b[0].Price), d(b[0].Size)).Mul(feeRoom).GreaterThan(d("50000000")) {
		t.Fatal("bids", b)
	}
}

func TestLadderBudgetMinNotionalMinSize(t *testing.T) {
	b, _ := sides(Ladder(weth, d("3000"), params, Inventory{Base: d("1e18"), Quote: d("1e10"), SideUSD: d("150")}))
	if len(b) != 2 || QuoteRaw(weth, d(b[1].Price), d(b[1].Size)).GreaterThan(d("50010000")) {
		t.Fatal("budget", b)
	}
	small := params
	small.LevelUSD = d("9")
	if o := Ladder(weth, d("3000"), small, rich); o != nil {
		t.Fatal("below min notional", o)
	}
	m := weth
	m.MinSize = d("400000000") // 0.04 WETH > 100 USD
	if o := Ladder(m, d("3000"), params, rich); o != nil {
		t.Fatal("below minSize", o)
	}
	if Ladder(weth, decimal.Zero, params, rich) != nil {
		t.Fatal("zero ref")
	}
}

func TestDue(t *testing.T) {
	cases := []struct {
		last, ref string
		since     time.Duration
		want      bool
	}{
		{"0", "3000", 0, true},               // never quoted
		{"3000", "3005", time.Second, false}, // 16.7bps < 20
		{"3000", "3007", time.Second, true},  // 23bps
		{"3000", "2993", time.Second, true},
		{"3000", "3000", 31 * time.Second, true},
	}
	for _, c := range cases {
		if got := Due(d(c.last), d(c.ref), c.since, 30*time.Second, 20); got != c.want {
			t.Errorf("%+v got %v", c, got)
		}
	}
}
