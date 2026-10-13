package perpl

import (
	"testing"

	"github.com/shopspring/decimal"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func TestGateHysteresis(t *testing.T) {
	th, band := d("0.05"), d("0.02")
	cases := []struct {
		open bool
		apr  string
		want bool
	}{
		{true, "0.05", true}, {true, "0.0499", false}, // open: stays until below threshold
		{false, "0.06", false}, {false, "0.07", false}, {false, "0.0701", true}, // closed: reopens only above threshold+band
	}
	for _, c := range cases {
		if Gate(c.open, d(c.apr), th, band) != c.want {
			t.Errorf("%+v", c)
		}
	}
}

func TestGuard(t *testing.T) {
	// 1 ETH short (lot 1000, 3 lot dec) at 2600.00 (2 price dec), 1300 AUSD collateral, maintenance 20x = 5%
	p := func(mark int64) Perp {
		pnl := decimal.NewFromInt(260000 - mark).Mul(d("10000")) // (entry − mark) × 1 ETH in 6dp
		return Perp{Account: 7, Deposit: d("1300000000"), PnL: pnl, Mark: decimal.NewFromInt(mark), Lot: d("1000"),
			PriceDec: 2, LotDec: 3, MaintHdths: d("2000"), MarkValid: true}
	}
	buf, rich := d("0.03"), d("500000000")
	for _, mark := range []int64{260000, 350000} { // 50%, 11.4% margin
		if g := Guard(p(mark), buf, rich); g.Fire {
			t.Fatal(mark, g)
		}
	}
	// mark 3700: equity 200 / 3700 = 5.4% < 8%; target 11% × 3700 = 407
	cases := []struct {
		free, wallet, topUp, cut, released string
	}{
		{"0", "500000000", "209090910", "0", "0"},          // wallet covers 207 / 0.99 swap haircut
		{"50000000", "500000000", "158585859", "0", "0"},   // free balance first: 157 / 0.99
		{"300000000", "0", "0", "0", "0"},                  // free balance alone covers it
		{"0", "100000000", "100000000", "266", "52215800"}, // keep 1000 × (200 + 99) / 407 = 734; re-add (200 − 3.7) × 0.266
		{"0", "500000", "0", "509", "99916700"},            // dust wallet: keep 1000 × 200 / 407 = 491
	}
	for _, c := range cases {
		pp := p(370000)
		pp.Free = d(c.free)
		g := Guard(pp, buf, d(c.wallet))
		if !g.Fire || g.CloseAll || !g.TopUp.Equal(d(c.topUp)) || !g.LotCut.Equal(d(c.cut)) || !g.Released.Equal(d(c.released)) {
			t.Errorf("%+v: %+v", c, g)
		}
	}
	if g := Guard(p(390000), buf, decimal.Zero); !g.Fire || !g.CloseAll { // equity 0: last resort
		t.Errorf("wiped: %+v", g)
	}
	stale := p(370000)
	stale.MarkValid = false
	if Guard(stale, buf, rich).Fire {
		t.Fatal("invalid mark fired")
	}
	closed := p(370000)
	closed.Lot = decimal.Zero
	if Guard(closed, buf, rich).Fire {
		t.Fatal("no position fired")
	}
}

func TestPerplABI(t *testing.T) {
	for _, m := range []string{"getAccountByAddr", "getPosition", "getPerpetualInfo", "getMarginFractions"} {
		if _, ok := ABI.Methods[m]; !ok {
			t.Fatal(m)
		}
	}
	out := ABI.Methods["getPosition"].Outputs
	if len(out) != 3 || out[0].Type.TupleRawNames == nil {
		t.Fatalf("%+v", out)
	}
}

func TestLiqPrice(t *testing.T) {
	// 1 ETH short at 2600, 1300 AUSD collateral (2x): (1300 + 2600) / 1.05
	p := Perp{Deposit: d("1300000000"), Mark: d("260000"), Lot: d("1000"), PriceDec: 2, LotDec: 3, MaintHdths: d("2000")}
	if got := p.LiqPrice(p.Maint()).StringFixed(2); got != "3714.29" {
		t.Fatal(got)
	}
	if got := p.LiqPrice(p.Maint().Add(d("0.03"))).StringFixed(2); got != "3611.11" { // guard: 3900 / 1.08
		t.Fatal(got)
	}
}
