package quote

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/streams"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

const (
	mon  = streams.Addr("0xfdbe356828c8f5a5d5ed4f69dde0816f4058ef61")
	weth = streams.Addr("0xa9c2936656a7d2143720bcd91ba8506200b7cbe7")
	btc  = streams.Addr("0x5bdea6f9f9aba34f4ecb9b865646a792b835ef7f")
)

// Same basket and books as contracts/test/fork/SweemBasket.fork.t.sol: 40/30/30, makers ±0.1% around center.
func forkState() State {
	info := func(m streams.Addr, pp, sp string, bd int, fee string) streams.MarketInfo {
		return streams.MarketInfo{Market: m, BaseDecimals: bd, QuoteDecimals: 6, PricePrecision: d(pp), SizePrecision: d(sp), TakerFeePps: d(fee)}
	}
	book := func(m streams.Addr, bid, ask, sz string) streams.BookSnapshot {
		return streams.BookSnapshot{Market: m, Bids: []streams.Level{{d(bid), d(sz)}}, Asks: []streams.Level{{d(ask), d(sz)}}}
	}
	return State{
		Basket: streams.BasketInfo{Components: []streams.Component{{Market: mon, WeightBps: 4000}, {Market: weth, WeightBps: 3000}, {Market: btc, WeightBps: 3000}}},
		Markets: map[streams.Addr]streams.MarketInfo{
			mon:  info(mon, "1000000", "100000000", 18, "7000"),
			weth: info(weth, "100", "10000000000", 18, "0"), // 0 → default 7000
			btc:  info(btc, "100", "100000000", 8, "7000"),
		},
		Books: map[streams.Addr]streams.BookSnapshot{
			mon:  book(mon, "0.024975", "0.025025", "1000000"),
			weth: book(weth, "2747.25", "2752.75", "100"),
			btc:  book(btc, "84065.85", "84234.15", "5"),
		},
	}
}

// Expected values hand-computed with Python Decimal from the contract + Kuru notional formulas.
func TestFirstMintSharesEqualSpent(t *testing.T) {
	r, err := Mint(forkState(), d("1000000000"), 50)
	if err != nil {
		t.Fatal(err)
	}
	if r.ExpectedOut != "999979999000000000000" || r.MinOut != "994980099005000000000" {
		t.Fatalf("got %s / %s", r.ExpectedOut, r.MinOut)
	}
	want := [][2]string{{"399999999", "15972827172821193000000"}, {"299990000", "108905639751030000"}, {"299990000", "355900"}}
	for i, l := range r.Legs {
		if l.In != want[i][0] || l.Out != want[i][1] {
			t.Fatalf("leg %d: %+v", i, l)
		}
	}
	if r.Legs[0].ImpactBps != 0 {
		t.Fatalf("single-level fill has no impact: %d", r.Legs[0].ImpactBps)
	}
}

func afterFirstMint() State {
	s := forkState()
	s.Nav = streams.NavPoint{Supply: d("999.979999"), Components: []streams.NavComponent{
		{Market: mon, Balance: d("15972.827172821193"), Value: d("399.320679320529825")},
		{Market: weth, Balance: d("0.10890563975103"), Value: d("299.4905093153325")},
		{Market: btc, Balance: d("0.003559"), Value: d("299.48985")},
	}}
	return s
}

func TestSecondMintMinRatio(t *testing.T) {
	r, err := Mint(afterFirstMint(), d("500000000"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if r.ExpectedOut != "499987189778170834504" || r.MinOut != r.ExpectedOut {
		t.Fatalf("got %+v", r)
	}
}

func TestRedeemHalf(t *testing.T) {
	r, err := Redeem(afterFirstMint(), d("499989999500000000000"), 100)
	if err != nil {
		t.Fatal(err)
	}
	if r.ExpectedOut != "498291630" || r.MinOut != "493308713" {
		t.Fatalf("got %+v", r)
	}
	if r.Legs[0].In != "7986413586410000000000" { // floorToLot: 1e-8 MON steps
		t.Fatalf("mon leg %+v", r.Legs[0])
	}
}

func TestWalkMultipleLevels(t *testing.T) {
	s := forkState()
	b := s.Books[btc]
	b.Asks = []streams.Level{{d("100"), d("1")}, {d("200"), d("10")}}
	s.Books[btc] = b
	cost, base, err := buy(s.Markets[btc], b.Asks, d("300"))
	if err != nil || !cost.Equal(d("300")) || !base.Equal(d("2")) {
		t.Fatalf("%s %s %v", cost, base, err)
	}
	l := mkLeg(btc, cost, base, cost, base, b.Asks)
	if l.ImpactBps != 5000 { // avg 150 vs best 100
		t.Fatalf("impact %d", l.ImpactBps)
	}
}

func TestErrors(t *testing.T) {
	s := forkState()
	b := s.Books[weth]
	b.Asks = nil
	s.Books[weth] = b
	if _, err := Mint(s, d("1000000000"), 0); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("empty asks: %v", err)
	}
	if _, err := Mint(forkState(), d("1"), 0); !errors.Is(err, ErrBadInput) {
		t.Fatalf("dust: %v", err) // 1 atom allocs round to 0 → contract ZeroAmount
	}
	if _, err := Redeem(afterFirstMint(), d("999979999000000000001"), 0); !errors.Is(err, ErrBadInput) {
		t.Fatalf("over supply: %v", err)
	}
	s = forkState()
	delete(s.Books, btc)
	if _, err := Mint(s, d("1000000000"), 0); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing book: %v", err)
	}
}

func TestStale(t *testing.T) {
	s := forkState()
	s.Nav.Stale = true // publisher marks supply-0 NAV stale
	if r, _ := Mint(s, d("1000000000"), 0); r.Stale {
		t.Fatal("first mint quote must not inherit supply-0 staleness")
	}
	s = afterFirstMint()
	s.Nav.Stale = true
	if r, _ := Mint(s, d("500000000"), 0); !r.Stale {
		t.Fatal("mint with supply uses NAV values")
	}
	if r, _ := Redeem(s, d("1000000000000000000"), 0); !r.Stale {
		t.Fatal("redeem stale")
	}
}
