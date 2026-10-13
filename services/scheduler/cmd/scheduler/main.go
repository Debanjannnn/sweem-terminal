package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/chain"
	"github.com/snehendu098/sweem-terminal/internal/config"
	"github.com/snehendu098/sweem-terminal/internal/httpx"
	"github.com/snehendu098/sweem-terminal/internal/logx"
	"github.com/snehendu098/sweem-terminal/internal/perpl"
	"github.com/snehendu098/sweem-terminal/internal/pg"
	"github.com/snehendu098/sweem-terminal/internal/planner"
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
	dbURL, redisURL, rpcURL, depPath := e.Str("DATABASE_URL"), e.Str("REDIS_URL"), e.Str("RPC_URL"), e.Str("DEPLOYMENTS_PATH")
	secret, execURL, mdURL := e.Str("INTERNAL_SECRET"), e.Str("EXECUTOR_URL"), e.Str("MARKET_DATA_URL")
	addr := ":" + e.Opt("PORT", "8084")
	tick := e.Duration("TICK", 5*time.Second)
	posEvery := e.Duration("POSITION_EVERY", time.Minute)
	maxAge := e.Duration("MAX_DATA_AGE", 3*time.Minute)
	maxStep := e.Int("MAX_STEP_USD", 1000)
	slip := e.Int("STRATEGY_SLIPPAGE_BPS", 50)
	band := decimal.RequireFromString(e.Opt("FUNDING_GATE_BAND", perpl.DefaultGateBand))
	buffer := decimal.RequireFromString(e.Opt("GUARD_BUFFER", perpl.DefaultGuardBuffer))
	if err := e.Err(); err != nil {
		return err
	}
	dep, err := config.LoadDeployments143(depPath)
	if err != nil {
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

	s := &sched.Scheduler{
		DB: db, RDB: rdb, Dep: dep,
		P:     &planner.Planner{Dep: dep, Q: planner.MarketData{URL: mdURL, HTTP: &http.Client{Timeout: 15 * time.Second}}, MaxStepUSD: int64(maxStep)},
		Exec:  planner.Executor{URL: execURL, Secret: secret, Caller: "scheduler", HTTP: &http.Client{Timeout: 10 * time.Minute}}, // runs every step before replying
		Chain: sched.RPC{C: eth, Dep: dep},
		Tick:  tick, PositionEvery: posEvery, MaxAge: maxAge, Slippage: slip, GateBand: band, GuardBuffer: buffer,
	}
	go s.Run(ctx)
	return httpx.Run(ctx, addr, httpx.NewMux(map[string]httpx.Check{
		"postgres": func(ctx context.Context) error { return db.Ping(ctx) },
		"redis":    func(ctx context.Context) error { return rdb.Ping(ctx).Err() },
		"rpc":      func(ctx context.Context) error { _, err := eth.BlockNumber(ctx); return err },
	}))
}
