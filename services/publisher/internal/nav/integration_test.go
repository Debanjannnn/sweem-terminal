//go:build integration

package nav

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/chain"
	"github.com/snehendu098/sweem-terminal/internal/config"
	"github.com/snehendu098/sweem-terminal/internal/pg"
	"github.com/snehendu098/sweem-terminal/internal/streams"
	"github.com/snehendu098/sweem-terminal/services/publisher/internal/registry"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// Registry + NAV for BLUE from live chain into real Redis/Postgres.
func TestLiveBlueNav(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	dep, err := config.LoadDeployments("../../../../contracts/deployments/10143.json")
	if err != nil {
		t.Fatal(err)
	}
	eth, err := chain.Dial(ctx, env("RPC_URL", "https://testnet-rpc.monad.xyz"))
	if err != nil {
		t.Fatal(err)
	}
	opt, _ := redis.ParseURL(env("REDIS_URL", "redis://localhost:6380"))
	rdb := redis.NewClient(opt)
	db, err := pg.Open(ctx, env("DATABASE_URL", "postgres://sweem:sweem@localhost:5433/sweem?sslmode=disable"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	reg := &registry.Registry{ETH: eth, Factory: common.HexToAddress(dep.Factory), RDB: rdb}
	if err := reg.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	blue := strings.ToLower(dep.Baskets["BLUE"])
	bs, err := streams.Get[[]streams.BasketInfo](ctx, rdb, streams.RegistryBaskets)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, b := range bs {
		found = found || string(b.Address) == blue && b.Symbol == "BLUE" && len(b.Components) > 0
	}
	ms, err := streams.Get[[]streams.MarketInfo](ctx, rdb, streams.RegistryMarkets)
	if !found || err != nil || len(ms) == 0 {
		t.Fatalf("registry: %v %+v %+v", err, bs, ms)
	}

	s := &Service{ETH: eth, RDB: rdb, PG: db, Registry: reg, MaxAge: 10 * time.Second,
		AccountCore: common.HexToAddress(dep.Kuru.AccountCore), USDC: common.HexToAddress(dep.Kuru.USDC)}
	if err := s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	p, err := streams.Get[streams.NavPoint](ctx, rdb, streams.NavLatest(blue))
	if err != nil {
		t.Fatal(err)
	}
	if p.Block == 0 || len(p.Components) == 0 || time.Since(time.UnixMilli(p.Ts)) > 10*time.Second {
		t.Fatalf("%+v", p)
	}
	if p.Supply.IsZero() && (!p.Stale || !p.NavPerShare.IsZero()) {
		t.Fatalf("supply 0 must be stale with navPerShare 0: %+v", p)
	}
	t.Logf("BLUE supply=%s totalNav=%s navPerShare=%s stale=%v", p.Supply, p.TotalNav, p.NavPerShare, p.Stale)

	// nav_points upsert, throwaway basket
	b := streams.Addr("0xtest" + uuid.NewString()[:8])
	t.Cleanup(func() { db.Exec(context.Background(), `delete from nav_points where basket=$1`, string(b)) })
	pt := streams.NavPoint{Basket: b, Ts: time.Now().UnixMilli(), Supply: decimal.RequireFromString("100"), TotalNav: decimal.RequireFromString("101.5"), NavPerShare: decimal.RequireFromString("1.015")}
	for i := 0; i < 3; i++ {
		if err := s.persist(ctx, pt); err != nil {
			t.Fatal(err)
		}
	}
	var cnt int
	var nps decimal.Decimal
	if err := db.QueryRow(ctx, `select count(*), max(nav_per_share)::text from nav_points where basket=$1`, string(b)).Scan(&cnt, &nps); err != nil {
		t.Fatal(err)
	}
	if cnt != 1 || nps.String() != "1.015" {
		t.Fatal(cnt, nps)
	}
}
