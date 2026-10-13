//go:build live

package perpl

import (
	"context"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/ethclient"
)

const exchange = "0x34B6552d57a35a1D042CcAe1951BD1C370112a6F"

// Read-only mainnet reads: market state for ETH (perp 20) and a real third-party short (account 533).
func TestLiveReads(t *testing.T) {
	url := os.Getenv("RPC_URL")
	if url == "" {
		url = "https://rpc.monad.xyz"
	}
	c, err := ethclient.Dial(url)
	if err != nil {
		t.Fatal(err)
	}
	r := Reader{C: c, Exchange: exchange}
	m, err := r.Market(context.Background(), 20)
	if err != nil || !m.Mark.IsPositive() || !m.Maint.IsPositive() || !m.Init.GreaterThan(m.Maint) {
		t.Fatalf("%+v %v", m, err)
	}
	t.Logf("%+v", m)
	p, err := r.Position(context.Background(), "0xaAcFd199e3e5c1f750978a3b6ca4E9150571B9A2", 20)
	if err != nil || !p.Lot.IsPositive() || !p.Entry.IsPositive() {
		t.Fatalf("%+v %v", p, err)
	}
	t.Logf("%+v liq %s guard %s", p, p.LiqPrice(p.Maint()), p.LiqPrice(p.Maint().Add(d("0.03"))))
}
