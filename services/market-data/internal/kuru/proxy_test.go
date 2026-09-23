package kuru

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/chains"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/config"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/finality"
)

// Live, pinned. Kuru upgrades behind an ERC-1967 proxy, and an upgrade changes
// which ABI actually answers. Pinning it here makes that a red test instead of
// a production surprise. Skips when the RPC is unreachable.
const (
	liveOrderBookProxy = "0x5bdea6f9f9aba34f4ecb9b865646a792b835ef7f"
	liveOrderBookImpl  = "0xe20f57e673d7f254279c19270862a3d1e6f5b0d4"
)

func TestImplementationDecode(t *testing.T) {
	cases := []struct {
		name, slot string
		wantErr    bool
		want       string
	}{
		{
			name: "address lives in the low 20 bytes",
			slot: "0x000000000000000000000000e20f57e673d7f254279c19270862a3d1e6f5b0d4",
			want: liveOrderBookImpl,
		},
		{name: "empty slot means it is not a proxy", slot: "0x" + strings.Repeat("0", 64), wantErr: true},
		{name: "short word is rejected", slot: "0x1234", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"` + tc.slot + `"}`))
			}))
			defer srv.Close()
			c := New(chains.MonadTestnet)
			c.rpcURL = srv.URL
			got, err := c.Implementation(context.Background(), liveOrderBookProxy)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && got != tc.want {
				t.Errorf("implementation = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestLiveImplementationPin(t *testing.T) {
	rpc := config.GetEnv("MONAD_RPC_URL_10143", "https://testnet-rpc.monad.xyz")
	c := New(chains.MonadTestnet)
	c.rpcURL = rpc
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	got, err := c.Implementation(ctx, liveOrderBookProxy)
	if err != nil {
		t.Skipf("monad testnet rpc unreachable: %v", err)
	}
	if got != liveOrderBookImpl {
		t.Fatalf("OrderBook implementation moved to %s (was %s): re-verify every selector we call before shipping", got, liveOrderBookImpl)
	}
}

// The depth snapshot comes from the gateway now, not an eth_call. /l2Book and
// /bbo are NotFound; only /api/depth and /api/bbo answer.
func TestLiveGatewayDepthAnswers(t *testing.T) {
	t.Setenv("KURU_API_URL_10143", "")
	t.Setenv("KURU_API_URL", "")
	c := New(chains.MonadTestnet)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	if _, err := c.Markets(ctx); err != nil {
		t.Skipf("kuru testnet api unreachable: %v", err)
	}
	b, err := c.Book(ctx, liveOrderBookProxy, 10)
	if err != nil {
		t.Skipf("kuru gateway unreachable: %v", err)
	}
	if b.MarketID != liveOrderBookProxy {
		t.Fatalf("book market = %s", b.MarketID)
	}
	if len(b.Bids)+len(b.Asks) == 0 {
		t.Error("cbBTC/USDC answered with an empty book, which no longer matches the chain")
	}
	if _, err := c.BBO(ctx, liveOrderBookProxy, finality.Final); err != nil {
		t.Errorf("gateway /api/bbo: %v", err)
	}
}
