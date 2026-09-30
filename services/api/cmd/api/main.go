package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/snehendu098/sweem-terminal/internal/config"
	"github.com/snehendu098/sweem-terminal/internal/httpx"
	"github.com/snehendu098/sweem-terminal/internal/logx"
	"github.com/snehendu098/sweem-terminal/internal/pg"
	"github.com/snehendu098/sweem-terminal/internal/planner"
	"github.com/snehendu098/sweem-terminal/services/api/internal/api"
	"github.com/snehendu098/sweem-terminal/services/api/internal/auth"
	"github.com/snehendu098/sweem-terminal/services/api/internal/privy"
)

func main() {
	logx.Setup("api")
	if err := run(); err != nil {
		slog.Error("api exited", "err", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var e config.Env
	dbURL, appID, secret, quorum := e.Str("DATABASE_URL"), e.Str("PRIVY_APP_ID"), e.Str("PRIVY_APP_SECRET"), e.Str("PRIVY_KEY_QUORUM_ID")
	depPath, mdURL, histURL, execURL, rpcURL := e.Str("DEPLOYMENTS_PATH"), e.Str("MARKET_DATA_URL"), e.Str("HISTORY_URL"), e.Str("EXECUTOR_URL"), e.Str("RPC_URL")
	secret2, maxStep := e.Str("INTERNAL_SECRET"), e.Int("MAX_STEP_USD", 1000)
	addr := ":" + e.Opt("PORT", "8080")
	origins := strings.Split(e.Opt("CORS_ORIGINS", "http://localhost:3000"), ",")
	if err := e.Err(); err != nil {
		return err
	}
	db, err := pg.Open(ctx, dbURL)
	if err != nil {
		return err
	}
	defer db.Close()
	dep, err := config.LoadDeployments143(depPath)
	if err != nil {
		return err
	}
	eth, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return err
	}
	defer eth.Close()
	hc := &http.Client{Timeout: 20 * time.Second}
	md := planner.MarketData{URL: mdURL, HTTP: hc}
	verifier, err := auth.New(ctx, appID, auth.JWKSURL(appID))
	if err != nil {
		return err
	}
	mux := httpx.NewMux(map[string]httpx.Check{"postgres": db.Ping})
	(&api.Server{
		Store: api.PG{DB: db}, Auth: verifier, Privy: privy.New(appID, secret, quorum), QuorumID: quorum,
		Plan:    &planner.Planner{Dep: dep, Q: md, MaxStepUSD: int64(maxStep)},
		Exec:    planner.Executor{URL: execURL, Secret: secret2, Caller: "api", HTTP: &http.Client{Timeout: 15 * time.Minute}},
		Chain:   eth,
		Txs:     eth,
		Rates:   md,
		History: api.History{URL: histURL, HTTP: &http.Client{Timeout: 60 * time.Second}},
	}).Routes(mux)
	slog.Info("listening", "addr", addr)
	return httpx.Run(ctx, addr, httpx.CORS(origins, mux))
}
