//go:build integration

package rates

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/chain"
	"github.com/snehendu098/sweem-terminal/internal/config"
	"github.com/snehendu098/sweem-terminal/internal/streams"
)

// Live mainnet poll → real Redis (DB 14; routes tests own rates keys in DB 15): Aave USDC, shMON trailing, Perpl funding.
func TestLivePoll(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	t.Setenv("CHAIN_ID", "143")
	dep, err := config.LoadDeployments143("../../../../deployments/143.json")
	if err != nil {
		t.Fatal(err)
	}
	rpc := os.Getenv("MAINNET_RPC_URL")
	if rpc == "" {
		rpc = "https://rpc.monad.xyz"
	}
	eth, err := chain.Dial(ctx, rpc)
	if err != nil {
		t.Fatal(err)
	}
	opt, _ := redis.ParseURL("redis://localhost:6380")
	if u := os.Getenv("REDIS_URL"); u != "" {
		opt, _ = redis.ParseURL(u)
	}
	opt.DB = 14
	rdb := redis.NewClient(opt)
	p := &Poller{ETH: eth, RDB: rdb, Dep: dep, PerplURL: "https://app.perpl.xyz", HTTP: &http.Client{Timeout: 20 * time.Second}}
	mon := dep.Kuru.Markets["MON"].Orderbook
	lvl := func(px string) []streams.Level {
		return []streams.Level{{decimal.RequireFromString(px), decimal.NewFromInt(1)}}
	}
	if err := streams.Publish(ctx, rdb, "", streams.BookLatest(mon), streams.BookSnapshot{Bids: lvl("0.027"), Asks: lvl("0.0271")}); err != nil {
		t.Fatal(err)
	}
	defer rdb.Del(context.Background(), streams.BookLatest(mon))
	if err := p.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	aave, err := streams.Get[streams.RateInfo](ctx, rdb, streams.Rate("lending", "aave", "USDC"))
	if err != nil || aave.Apy.InexactFloat64() <= 0.001 || aave.Apy.InexactFloat64() > 0.5 {
		t.Fatalf("aave usdc %v %+v", err, aave)
	}
	sh, err := streams.Get[streams.RateInfo](ctx, rdb, streams.Rate("staking", "shmonad", "MON"))
	if err != nil || sh.Apy.InexactFloat64() <= 0 || sh.Apy.InexactFloat64() > 1 {
		t.Fatalf("shmon %v %+v", err, sh)
	}
	av, err := streams.Get[streams.RateInfo](ctx, rdb, streams.Rate("lp", "kuru-activevault", "MON"))
	if err != nil || av.Kind != "estimated" || av.Apy.IsZero() {
		t.Fatalf("activevault %v %+v", err, av)
	}
	snap, err := streams.Get[streams.RatesSnapshot](ctx, rdb, streams.RatesLatest)
	if err != nil || len(snap.Funding) != 3 {
		t.Fatalf("snapshot %v funding=%d", err, len(snap.Funding))
	}
	for _, r := range snap.Rates {
		t.Logf("%s %s %s apy=%s", r.Source, r.Venue, r.Asset, r.Apy)
	}
	for _, f := range snap.Funding {
		t.Logf("perp %d %s rate=%s aprShort=%s 7d=%s", f.PerpID, f.Name, f.Rate, f.AprShort, f.AprShort7d)
	}
	keys := []string{streams.Rates, streams.RatesLatest}
	for _, r := range snap.Rates {
		keys = append(keys, streams.Rate(r.Source, r.Venue, r.Asset))
	}
	for _, f := range snap.Funding {
		keys = append(keys, streams.Funding(f.PerpID))
	}
	rdb.Del(context.Background(), keys...)
}
