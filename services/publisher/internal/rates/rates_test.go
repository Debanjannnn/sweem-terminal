package rates

import (
	"math/big"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func near(t *testing.T, name string, got decimal.Decimal, want float64) {
	t.Helper()
	if g := got.InexactFloat64(); g < want-1e-4 || g > want+1e-4 {
		t.Fatalf("%s: got %s want %v", name, got, want)
	}
}

func TestAaveAPY(t *testing.T) {
	ray, _ := new(big.Int).SetString("206648e474914a84fcdd53", 16) // live Aave USDC currentLiquidityRate
	near(t, "usdc", AaveAPY(ray), 0.039946)                        // 3.9169% APR → 3.9946% APY
	near(t, "zero", AaveAPY(big.NewInt(0)), 0)
	near(t, "10% apr", AaveAPY(new(big.Int).Mul(big.NewInt(1e17), big.NewInt(1e9))), 0.105171)
}

func TestTrailingAPY(t *testing.T) {
	p0, _ := new(big.Int).SetString("1627431839957253111", 10) // live shMON 1 day apart
	p1, _ := new(big.Int).SetString("1627930493471995245", 10)
	near(t, "shmon", TrailingAPY(p0, p1, 24*time.Hour), 0.118312)
	near(t, "flat", TrailingAPY(p0, p0, time.Hour), 0)
	near(t, "zero p0", TrailingAPY(big.NewInt(0), p1, time.Hour), 0)
	near(t, "loss", TrailingAPY(big.NewInt(100), big.NewInt(99), 365*24*time.Hour), -0.01)
}

func TestFundingAPR(t *testing.T) {
	// 10e-6 per 2580s interval, the live ETH rate
	near(t, "one", FundingAPR([]int64{10}, 2580*time.Second), 0.122233)
	near(t, "7d", FundingAPR([]int64{10, -10, 5}, 7*24*time.Hour), 5e-6*365/7)
	near(t, "empty", FundingAPR(nil, 0), 0)
	if slug("Morpho sbAUSD-Tori-Mon") != "morpho-sbausd-tori-mon" || slug("Euler k3USDC") != "euler-k3usdc" {
		t.Fatal("slug")
	}
}

func TestVaultNAV(t *testing.T) {
	// live ActiveVault per-share amounts: 422830.93… MON + 12257.041924 USDC at mid 0.027
	b, _ := new(big.Int).SetString("422830933386785523991986", 10)
	nav := VaultNAV(b, big.NewInt(12257041924), decimal.RequireFromString("0.027"), 18, 6)
	if nav.String() != "23673477125" { // 11416.435201 + 12257.041924 USD
		t.Fatal(nav)
	}
}
