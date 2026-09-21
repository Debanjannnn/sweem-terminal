package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/chains"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/config"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/httpx"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/natsbus"
	"github.com/sweem-terminal/sweem-terminal/services/scheduler/internal/client"
	"github.com/sweem-terminal/sweem-terminal/services/scheduler/internal/delegation"
	"github.com/sweem-terminal/sweem-terminal/services/scheduler/internal/engine"
	"github.com/sweem-terminal/sweem-terminal/services/scheduler/internal/store"
)

func main() {
	config.LoadDotEnv(".env")
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level()}))
	slog.SetDefault(log)

	// the scheduler triggers, it never signs: seeing privy credentials means the wiring is wrong
	if os.Getenv("PRIVY_APP_SECRET") != "" || os.Getenv("PRIVY_AUTHORIZATION_PRIVATE_KEY") != "" {
		log.Error("scheduler can see privy signing credentials; scope them to the executor")
		os.Exit(1)
	}
	secret := config.GetEnv("INTERNAL_SECRET", "")
	if secret == "" {
		log.Error("INTERNAL_SECRET is empty, so wallet would reject every dispatch")
		os.Exit(1)
	}

	// wallet stamps the chain on the intent; refuse to run against one it cannot
	if _, err := chains.Normalize(config.GetEnv("CHAIN_ID", "10143")); err != nil {
		log.Error("chain id", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := store.New(ctx, config.GetEnv("DATABASE_URL", "postgres://sweem:sweem@localhost:5432/sweem?sslmode=disable"))
	if err != nil {
		log.Error("open store", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	bus, err := natsbus.New(config.GetEnv("NATS_URL", "nats://localhost:4222"))
	if err != nil {
		log.Error("open bus", "err", err)
		os.Exit(1)
	}
	defer bus.Close()

	wallet := client.NewWallet(config.GetEnv("WALLET_URL", "http://localhost:8080"), secret)
	md := client.NewMarketData(config.GetEnv("MARKET_DATA_URL", "http://localhost:8081"))
	health := &delegation.Checker{
		Fetch:          wallet.Delegation,
		ExpectedSigner: config.GetEnv("PRIVY_KEY_QUORUM_ID", ""),
		TTL:            time.Duration(config.GetEnvInt("SCHEDULER_DELEGATION_TTL_MS", 30_000)) * time.Millisecond,
	}

	eng := &engine.Engine{
		Store: db,
		Log:   log,
		Cfg: engine.Config{
			Tick:           time.Duration(config.GetEnvInt("SCHEDULER_TICK_MS", 5000)) * time.Millisecond,
			MaxCatchup:     config.GetEnvInt("SCHEDULER_MAX_CATCHUP", 1),
			GridBandBps:    config.GetEnvInt("SCHEDULER_GRID_BAND_BPS", 25),
			GasLimit:       config.GetEnvFloat("SCHEDULER_GAS_LIMIT", 500_000),
			GasPriceGwei:   config.GetEnvFloat("SCHEDULER_GAS_PRICE_GWEI", 100),
			MaxGasFraction: config.GetEnvFloat("SCHEDULER_MAX_GAS_FRACTION", 0.01),
			GasMarket:      config.GetEnv("SCHEDULER_GAS_MARKET", ""),
			BaseDecimals:   config.GetEnvInt("SCHEDULER_BASE_DECIMALS", 18),
			QuoteDecimals:  config.GetEnvInt("SCHEDULER_QUOTE_DECIMALS", 6),
		},
		Delegation: health.Check,
		Dispatch:   wallet.DispatchSlice,
		Mid:        md.Mid,
		Publish:    bus.Publish,
	}

	started := time.Now()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		httpx.OK(w, map[string]any{
			"status":     "ok",
			"uptime_sec": int(time.Since(started).Seconds()),
			"tick_ms":    eng.Cfg.Tick.Milliseconds(),
			"counters":   eng.C.Snapshot(),
		})
	})
	srv := &http.Server{
		Addr:              ":" + config.GetEnv("SCHEDULER_PORT", "8084"),
		Handler:           httpx.Logging(log, mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Info("scheduler listening", "addr", srv.Addr, "tick_ms", eng.Cfg.Tick.Milliseconds())
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server", "err", err)
			stop()
		}
	}()

	go eng.Run(ctx)

	<-ctx.Done()
	log.Info("shutting down")
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdown); err != nil {
		log.Error("shutdown", "err", err)
	}
}

func level() slog.Level {
	var l slog.Level
	if err := l.UnmarshalText([]byte(config.GetEnv("LOG_LEVEL", "info"))); err != nil {
		return slog.LevelInfo
	}
	return l
}
