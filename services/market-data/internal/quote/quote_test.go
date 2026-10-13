package quote

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/streams"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

// WETH_USDC params from exchangeInfo: pricePrecision 1e4, sizePrecision 1e10, minSize 0.002 WETH.
var weth = streams.MarketInfo{Market: "0xa6afd386135b7d41a6c40c525abc4a1019b0d132", BaseDecimals: 18, QuoteDecimals: 6,
	PricePrecision: d("10000"), SizePrecision: d("10000000000"), MinSize: d("20000000")}

var book = streams.BookSnapshot{
	Bids: []streams.Level{{d("2000"), d("1")}, {d("1990"), d("1")}, {d("0.01"), d("10")}},
	Asks: []streams.Level{{d("2000"), d("1")}, {d("2010"), d("1")}},
}

func TestBuyWalk(t *testing.T) {
	// $3000: 1 WETH @2000 + 0.4975124378 @2010 (floored to 1e-10)
	q, err := Market(weth, book, "buy", d("3000000000"), 100, 50)
	if err != nil {
		t.Fatal(err)
	}
	if q.OrderSize != "30000000" || q.ExpectedOut != "1497512437800000000" || q.Partial {
		t.Fatalf("%+v", q)
	}
	if q.MinOut != "1482537313422000000" { // 1% slippage
		t.Fatal(q.MinOut)
	}
	if q.Fillable != "3000000000" { // 2999.999999978 spent, ceil to atoms
		t.Fatal(q.Fillable)
	}
	if q.ImpactBps != 17 || !q.AvgPx.Round(4).Equal(d("2003.3223")) {
		t.Fatal(q.ImpactBps, q.AvgPx)
	}
	// avg ≤ 2010 (50 bps) holds for the whole book: 4010 USDC
	if q.MaxSafeAmount != "4010000000" {
		t.Fatal("maxSafe", q.MaxSafeAmount)
	}
}

func TestBuyPartialAndCap(t *testing.T) {
	q, err := Market(weth, book, "buy", d("10000000000"), 0, 25) // $10k vs $4010 of asks
	if err != nil {
		t.Fatal(err)
	}
	if !q.Partial || q.Fillable != "4010000000" || q.ExpectedOut != "2000000000000000000" || q.MinOut != q.ExpectedOut {
		t.Fatalf("%+v", q)
	}
	// cap 25 bps → avg ≤ 2005: (2000 + 2010x)/(1+x) = 2005 → x = 1 WETH... avg exactly 2005 at x=1, so full book again
	if q.MaxSafeAmount != "4010000000" {
		t.Fatal(q.MaxSafeAmount)
	}
	q, _ = Market(weth, book, "buy", d("10000000000"), 0, 10) // avg ≤ 2002 → x = 2/8 = 0.25
	if q.MaxSafeAmount != "2502500000" {
		t.Fatal("cap 10", q.MaxSafeAmount)
	}
}

func TestSellStaleBids(t *testing.T) {
	// 3 WETH sweeps into the 0.01 bid: the thin-book case minOut/maxSafe exist for
	q, err := Market(weth, book, "sell", d("3000000000000000000"), 50, 100)
	if err != nil {
		t.Fatal(err)
	}
	if q.OrderSize != "30000000000" || q.ExpectedOut != "3990010000" || q.Partial || q.ImpactBps != 3350 {
		t.Fatalf("%+v", q)
	}
	// 100 bps → avg ≥ 1980: whole 2 top levels (avg 1995) ok, the 0.01 level allows x = 30/1979.99
	want := d("2").Add(d("30").Div(d("1979.99")).Div(d("1e-10")).Floor().Mul(d("1e-10"))).Shift(18)
	if q.MaxSafeAmount != want.String() {
		t.Fatal(q.MaxSafeAmount, want)
	}
	q, _ = Market(weth, book, "sell", d("20000000000000000000"), 0, 100)
	if !q.Partial || q.Fillable != "12000000000000000000" {
		t.Fatalf("partial sell %+v", q)
	}
}

func TestErrors(t *testing.T) {
	if _, err := Market(weth, book, "buy", d("1000000"), 0, 50); !errors.Is(err, ErrMinSize) { // $1 < 0.002 WETH
		t.Fatal("min size buy", err)
	}
	if _, err := Market(weth, book, "sell", d("1000000000000000"), 0, 50); !errors.Is(err, ErrMinSize) {
		t.Fatal("min size sell", err)
	}
	if _, err := Market(weth, book, "sell", d("2000000000000000"), 0, 50); err != nil { // exactly minSize
		t.Fatal(err)
	}
	if _, err := Market(weth, streams.BookSnapshot{}, "buy", d("1"), 0, 50); !errors.Is(err, ErrUnavailable) {
		t.Fatal("empty", err)
	}
	for _, side := range []string{"mint", ""} {
		if _, err := Market(weth, book, side, d("1"), 0, 50); !errors.Is(err, ErrBadInput) {
			t.Fatal(side, err)
		}
	}
	if _, err := Market(weth, book, "buy", d("0"), 0, 50); !errors.Is(err, ErrBadInput) {
		t.Fatal("zero")
	}
}

func TestMonUnits(t *testing.T) {
	// MON_USDC: pricePrecision 1e8 so quoteSize = USD × 1e8; minSize 200 MON
	mon := streams.MarketInfo{Market: "0x065c9d28e428a0db40191a54d33d5b7c71a9c394", BaseDecimals: 18, QuoteDecimals: 6,
		PricePrecision: d("100000000"), SizePrecision: d("10000000000"), MinSize: d("2000000000000")}
	b := streams.BookSnapshot{Asks: []streams.Level{{d("0.02705"), d("100000")}}}
	q, err := Market(mon, b, "buy", d("10000000"), 0, 50) // $10
	if err != nil {
		t.Fatal(err)
	}
	if q.OrderSize != "1000000000" || q.ExpectedOut != "369685767097900000000" /* 369.6857670979 MON, floored to 1e-10 */ {
		t.Fatalf("%+v", q)
	}
}
