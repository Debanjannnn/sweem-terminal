package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/redis/go-redis/v9"
	"github.com/snehendu098/sweem-terminal/internal/chain"
	"github.com/snehendu098/sweem-terminal/internal/config"
	"github.com/snehendu098/sweem-terminal/internal/httpx"
	"github.com/snehendu098/sweem-terminal/internal/logx"
	"github.com/snehendu098/sweem-terminal/internal/pg"
	"github.com/snehendu098/sweem-terminal/internal/streams"
	"github.com/snehendu098/sweem-terminal/services/publisher/internal/market"
	"github.com/snehendu098/sweem-terminal/services/publisher/internal/nav"
	"github.com/snehendu098/sweem-terminal/services/publisher/internal/registry"
)

func main() {
	logx.Setup("publisher")
	if err := run(); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("exit", "err", err)
		os.Exit(1)
	}
}

func run() error {
	var e config.Env
	rpcURL, dbURL, redisURL := e.Str("RPC_URL"), e.Str("DATABASE_URL"), e.Str("REDIS_URL")
	wsURL, apiURL, depPath := e.Str("KURU_GATEWAY_WS_URL"), e.Str("KURU_API_URL"), e.Str("DEPLOYMENTS_PATH")
	devBps := e.Int("REF_DEVIATION_BPS", 500)
	tau := e.Duration("REF_TAU", time.Minute)
	addr := e.Opt("ADDR", ":8081")
	if err := e.Err(); err != nil {
		return err
	}
	dep, err := config.LoadDeployments(depPath)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	eth, err := chain.Dial(ctx, rpcURL)
	if err != nil {
		return err
	}
	db, err := pg.Open(ctx, dbURL)
	if err != nil {
		return err
	}
	defer db.Close()
	ropt, err := redis.ParseURL(redisURL)
	if err != nil {
		return err
	}
	rdb := redis.NewClient(ropt)
	defer rdb.Close()

	reg := &registry.Registry{ETH: eth, Factory: common.HexToAddress(dep.Factory), RDB: rdb}
	if err := reg.Refresh(ctx); err != nil {
		return err
	}
	feed := &market.Feed{WSURL: wsURL, APIURL: apiURL, RDB: rdb, Markets: reg.Markets, DeviationBps: devBps, Tau: tau}
	navs := &nav.Service{ETH: eth, RDB: rdb, PG: db, Registry: reg, MaxAge: 10 * time.Second,
		AccountCore: common.HexToAddress(dep.Kuru.AccountCore), USDC: common.HexToAddress(dep.Kuru.USDC)}

	errc := make(chan error, 5)
	go func() { errc <- reg.Run(ctx, 15*time.Second) }()
	go func() { errc <- feed.Run(ctx) }()
	go func() { errc <- navs.Run(ctx, 2*time.Second) }()
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				errc <- ctx.Err()
				return
			case <-t.C:
				if err := streams.Beat(ctx, rdb); err != nil {
					slog.Warn("heartbeat", "err", err)
				}
			}
		}
	}()
	mux := httpx.NewMux(map[string]httpx.Check{
		"redis":    func(ctx context.Context) error { return rdb.Ping(ctx).Err() },
		"postgres": func(ctx context.Context) error { return db.Ping(ctx) },
		"rpc":      func(ctx context.Context) error { _, err := eth.BlockNumber(ctx); return err },
		"gateway": func(context.Context) error {
			if !feed.Connected() {
				return errors.New("disconnected")
			}
			return nil
		},
	})
	go func() { errc <- httpx.Run(ctx, addr, mux) }()
	slog.Info("publisher up", "addr", addr, "markets", len(reg.Markets()), "baskets", len(reg.Baskets()))
	err = <-errc
	stop()
	return err
}
