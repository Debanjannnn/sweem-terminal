package nav

import (
	"math/big"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/streams"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }
func n(s string) *big.Int        { v, _ := new(big.Int).SetString(s, 10); return v }

func TestCompute(t *testing.T) {
	now := time.UnixMilli(1_000_000)
	fresh := now.UnixMilli()
	legs := []Leg{
		// 1000 MON (18dp) @ one-sided book → ref 0.025
		{Market: "0xm", Balance: n("1000000000000000000000"), Decimals: 18, Ref: &streams.RefPrice{Mid: decimal.Zero, Ref: d("0.025"), Ts: fresh}},
		// 0.001 cbBTC (8dp) @ mid 85000
		{Market: "0xb", Balance: n("100000"), Decimals: 8, Ref: &streams.RefPrice{Mid: d("85000"), Ref: d("84000"), Ts: fresh}},
	}
	// 10 USDC idle, 100 shares
	p := Compute("0xB", 7, now, 10*time.Second, legs, n("10000000"), n("100000000000000000000"))
	if p.Stale || p.TotalNav.String() != "120" || p.NavPerShare.String() != "1.2" || p.Supply.String() != "100" {
		t.Fatalf("%+v", p)
	}
	if p.Components[0].Value.String() != "25" || p.Components[1].Balance.String() != "0.001" || p.Components[1].Value.String() != "85" {
		t.Fatalf("%+v", p.Components)
	}

	old := *legs[1].Ref
	old.Ts = fresh - 11_000
	if !Compute("0xb", 7, now, 10*time.Second, []Leg{{Balance: n("1"), Decimals: 8, Ref: &old}}, n("0"), n("1")).Stale {
		t.Fatal("old ref must be stale")
	}
	dev := *legs[1].Ref
	dev.Deviated = true
	if !Compute("0xb", 7, now, 10*time.Second, []Leg{{Balance: n("1"), Decimals: 8, Ref: &dev}}, n("0"), n("1")).Stale {
		t.Fatal("deviated must be stale")
	}
	if !Compute("0xb", 7, now, 10*time.Second, []Leg{{Balance: n("1"), Decimals: 8}}, n("0"), n("1")).Stale {
		t.Fatal("missing ref must be stale")
	}
	z := Compute("0xb", 7, now, 10*time.Second, legs, n("0"), n("0"))
	if !z.Stale || !z.NavPerShare.IsZero() || z.TotalNav.String() != "110" {
		t.Fatalf("supply 0: %+v", z)
	}
}
