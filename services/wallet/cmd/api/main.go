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
	"github.com/sweem-terminal/sweem-terminal/internal/shared/indexerclient"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/natsbus"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/privyauth"
	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/api"
	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/auth"
	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/executor"
	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/marketdata"
	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/store"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	config.LoadDotEnv(".env")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// refuses to boot if this process can see a signing secret
	verifier, err := auth.NewVerifier(ctx)
	if err != nil {
		return err
	}

	db, err := store.New(ctx, config.GetEnv("DATABASE_URL", ""))
	if err != nil {
		return err
	}
	defer db.Close()

	bus, err := natsbus.New(config.GetEnv("NATS_URL", "nats://localhost:4222"))
	if err != nil {
		return err
	}
	defer bus.Close()

	chainID := int64(config.GetEnvInt("CHAIN_ID", chains.MonadTestnet))
	if !chains.IsSupported(int(chainID)) {
		return errors.New("CHAIN_ID is not a chain this terminal trades on")
	}

	srv := &api.Server{
		Store:              db,
		Market:             marketdata.New(config.GetEnv("MARKET_DATA_URL", "http://localhost:8081")),
		Executor:           executor.New(config.GetEnv("EXECUTOR_URL", "http://localhost:8083")),
		Indexer:            indexerclient.New(),
		Auth:               verifier,
		Internal:           privyauth.InternalAuth{Secret: config.GetEnv("INTERNAL_SECRET", "")},
		Bus:                bus,
		Log:                log,
		ChainID:            chainID,
		DefaultSlippageBps: config.GetEnvInt("MAX_SLIPPAGE_BPS", 50),
	}

	httpSrv := &http.Server{
		Addr:              config.GetEnv("WALLET_ADDR", ":8080"),
		Handler:           httpx.CORS(config.GetEnvList("CORS_ORIGINS", []string{"http://localhost:3000"}), srv.Routes()),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      150 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("wallet listening", "addr", httpSrv.Addr, "chain_id", chainID)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return httpSrv.Shutdown(shutdownCtx)
	}
}
