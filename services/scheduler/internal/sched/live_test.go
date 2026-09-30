//go:build live

package sched

import (
	"context"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/ethclient"
)

// Reads a real third-party ETH short on mainnet Perpl (account 533, seen via getPositions). Read-only.
func TestLivePerp(t *testing.T) {
	url := os.Getenv("RPC_URL")
	if url == "" {
		url = "https://rpc.monad.xyz"
	}
	c, err := ethclient.Dial(url)
	if err != nil {
		t.Fatal(err)
	}
	dep := testDep(t)
	r := RPC{C: c, Dep: dep}
	p, err := r.Perp(context.Background(), "0xaAcFd199e3e5c1f750978a3b6ca4E9150571B9A2", dep.Perpl.PerpIds["ETH"])
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%+v", p)
	if p.Account != 533 || !p.Lot.IsPositive() || p.PriceDec != 2 || p.LotDec != 3 || !p.MaintHdths.IsPositive() || !p.MarkValid {
		t.Fatalf("%+v", p)
	}
	t.Logf("guard %+v", Guard(p, d("0.03"), d("0")))
	if none, err := r.Perp(context.Background(), "0x0000000000000000000000000000000000000001", 20); err != nil || none.Account != 0 {
		t.Fatal(none, err)
	}
}
