package main

import (
	"context"
	"log/slog"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/redis/go-redis/v9"
	"github.com/snehendu098/sweem-terminal/internal/chain"
	"github.com/snehendu098/sweem-terminal/internal/chain/bindings/basket"
	"github.com/snehendu098/sweem-terminal/internal/config"
	"github.com/snehendu098/sweem-terminal/internal/httpx"
	"github.com/snehendu098/sweem-terminal/internal/logx"
	"github.com/snehendu098/sweem-terminal/internal/pg"
	"github.com/snehendu098/sweem-terminal/services/scheduler/internal/sched"
)

func main() {
	logx.Setup("scheduler")
	if err := run(); err != nil {
		slog.Error("exit", "err", err)
		os.Exit(1)
	}
}

func run() error {
	var e config.Env
	dbURL, redisURL, rpcURL := e.Str("DATABASE_URL"), e.Str("REDIS_URL"), e.Str("RPC_URL")
	secret, execURL, mdURL := e.Str("INTERNAL_SECRET"), e.Str("EXECUTOR_URL"), e.Str("MARKET_DATA_URL")
	addr := ":" + e.Opt("PORT", "8084")
	tick := e.Duration("TICK", 5*time.Second)
	if err := e.Err(); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := pg.Open(ctx, dbURL)
	if err != nil {
		return err
	}
	defer db.Close()
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return err
	}
	rdb := redis.NewClient(opt)
	defer rdb.Close()
	eth, err := chain.Dial(ctx, rpcURL)
	if err != nil {
		return err
	}
	defer eth.Close()

	hc := &http.Client{Timeout: 30 * time.Second}
	s := &sched.Scheduler{
		DB: db, RDB: rdb, Tick: tick,
		Exec:   sched.Executor{URL: execURL, Secret: secret, HTTP: hc},
		Quotes: sched.Quotes{URL: mdURL, HTTP: hc},
		BalanceOf: func(ctx context.Context, b, owner string) (*big.Int, error) {
			c, err := basket.NewSweemBasketCaller(common.HexToAddress(b), eth)
			if err != nil {
				return nil, err
			}
			return c.BalanceOf(&bind.CallOpts{Context: ctx}, common.HexToAddress(owner))
		},
	}
	go s.Run(ctx)
	return httpx.Run(ctx, addr, httpx.NewMux(map[string]httpx.Check{
		"postgres": func(ctx context.Context) error { return db.Ping(ctx) },
		"redis":    func(ctx context.Context) error { return rdb.Ping(ctx).Err() },
	}))
}
