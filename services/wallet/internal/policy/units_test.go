package policy

import (
	"math/big"
	"testing"
	"time"
)

func n(s string) *big.Int {
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		panic(s)
	}
	return v
}

// The same fixtures the executor asserts in venues/kuru.rs. If either side
// drifts by a power of ten, one of the two test suites goes red.
func TestScalesMatchTheExecutor(t *testing.T) {
	ms, err := LoadMarkets("../../../../executor/kuru-markets.json")
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]Market{}
	for _, m := range ms {
		by[m.ID] = m
	}

	tests := []struct {
		market, price, quantity string
		wantBaseAtoms           string
		wantQuoteNotional       string
	}{
		// 0.5 WETH @ 2750.00 = 1375.00 USDC
		{"WETH-USDC", "275000", "5000000000", "500000000000000000", "1375000000"},
		// the multiplier is 1: book quantity already is the raw atom count
		{"CBBTC-USDC", "8600000", "100000", "100000", "86000000"},
		{"XAUT0-USDC", "432500", "10000", "10000", "43250000"},
		// 100 MON @ 0.025 = 2.50 USDC
		{"MON-USDC", "25000", "10000000000", "100000000000000000000", "2500000"},
	}
	for _, tt := range tests {
		t.Run(tt.market, func(t *testing.T) {
			m, ok := by[tt.market]
			if !ok {
				t.Fatalf("market %s is not on the allowlist", tt.market)
			}
			base, err := RawBaseAtoms(m, n(tt.quantity))
			if err != nil {
				t.Fatal(err)
			}
			if base.Cmp(n(tt.wantBaseAtoms)) != 0 {
				t.Fatalf("raw base atoms = %s, want %s", base, tt.wantBaseAtoms)
			}
			quote, err := QuoteNotional(m, n(tt.price), n(tt.quantity))
			if err != nil {
				t.Fatal(err)
			}
			if quote.Cmp(n(tt.wantQuoteNotional)) != 0 {
				t.Fatalf("quote notional = %s, want %s", quote, tt.wantQuoteNotional)
			}
		})
	}
}

// The shipped allowlist must carry the multiplier the executor validates, or a
// maker order would be sized against a guess.
func TestAllowlistCarriesEveryScale(t *testing.T) {
	ms, err := LoadMarkets("../../../../executor/kuru-markets.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range ms {
		mult, sizePrec := n(m.BaseSizeMultiplier), n(m.SizePrecision)
		want := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(m.BaseDecimals)), nil)
		if new(big.Int).Mul(mult, sizePrec).Cmp(want) != 0 {
			t.Fatalf("market %s: base_size_multiplier %s * size_precision %s != 10^%d",
				m.ID, mult, sizePrec, m.BaseDecimals)
		}
		if n(m.MinQuoteNotional).Sign() == 0 || n(m.PricePrecision).Sign() == 0 {
			t.Fatalf("market %s is missing a scale", m.ID)
		}
	}
}

// Both caps are raw atoms of the token the order spends. A cap read as anything
// else lets an order through that is 1e6 too large, which is the whole reason
// this test exists.
func TestCapsAreDenominatedInSpentAtoms(t *testing.T) {
	d := Delegation{
		Venues: []string{"kuru"}, Markets: []string{"MON-USDC"},
		// 10 USDC of quote atoms per order, 100 in total
		MaxNotionalPerSlice: "10000000", MaxNotionalTotal: "100000000",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	tests := []struct {
		name     string
		notional string
		reject   string
	}{
		{"a 10 USDC bid is exactly at the cap", "10000000", ""},
		{"one atom over is refused whole", "10000001", "over_slice_limit"},
		// the same order read as a display amount would be 1e6 too small and pass
		{"10 USDC misread as a display amount", "10", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rej := EvaluateIntent(d, Intent{Venue: "kuru", MarketID: "MON-USDC", Notional: n(tt.notional)},
				new(big.Int), time.Now())
			switch {
			case tt.reject == "" && rej != nil:
				t.Fatalf("refused with %s: %s", rej.Code, rej.Reason)
			case tt.reject != "" && (rej == nil || rej.Code != tt.reject):
				t.Fatalf("rejection = %v, want %s", rej, tt.reject)
			}
		})
	}
}
