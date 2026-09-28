package main

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/chain"
	"github.com/snehendu098/sweem-terminal/internal/config"
	"github.com/snehendu098/sweem-terminal/internal/httpx"
	"github.com/snehendu098/sweem-terminal/internal/logx"
	"github.com/snehendu098/sweem-terminal/services/seeder/internal/executor"
	"github.com/snehendu098/sweem-terminal/services/seeder/internal/kuru"
	"github.com/snehendu098/sweem-terminal/services/seeder/internal/mm"
)

// raw atoms: 5000 USDC, 0.5 WETH, 0.05 cbBTC, 0.5 XAUt0, 3 MON
const defaultTargets = "0xee0722ead54f1b4fe97be399be43bc0226a6f97e=5000000000," +
	"0x8b6c5fafef85b030bb1e71ae7ac085cc2380aaf8=500000000000000000," +
	"0xef2a20a161ac9ed1117d721336226b6399f15b4d=5000000," +
	"0xee1dce135a9ab598bca8cf3a28bdef6892100740=500000," +
	"0x0000000000000000000000000000000000000000=3000000000000000000"

func main() {
	log := logx.Setup("seeder")
	if err := run(); err != nil {
		log.Error("exit", "err", err)
		os.Exit(1)
	}
}

func run() error {
	var e config.Env
	var decErr []string
	dec := func(key, def string) decimal.Decimal {
		v, err := decimal.NewFromString(e.Opt(key, def))
		if err != nil {
			decErr = append(decErr, key)
		}
		return v
	}
	redisURL, execURL, secret := e.Str("REDIS_URL"), e.Str("EXECUTOR_URL"), e.Str("INTERNAL_SECRET")
	cfg := mm.Config{
		Params: mm.Params{
			Levels:         e.Int("SEEDER_LEVELS", 3),
			SpreadBps:      int64(e.Int("SEEDER_SPREAD_BPS", 30)),
			StepBps:        int64(e.Int("SEEDER_STEP_BPS", 20)),
			LevelUSD:       dec("SEEDER_LEVEL_USD", "100"),
			MinNotionalUSD: dec("SEEDER_MIN_NOTIONAL_USD", "10"),
		},
		Enabled:        e.Bool("SEEDER_ENABLED", true),
		MaxNotionalUSD: dec("SEEDER_MAX_NOTIONAL_USD", "4000"),
		RequoteBps:     int64(e.Int("SEEDER_REQUOTE_BPS", 20)),
		RequoteEvery:   e.Duration("SEEDER_REQUOTE_EVERY", 60*time.Second),
		MinGap:         e.Duration("SEEDER_MIN_GAP", 5*time.Second),
		DepositEvery:   e.Duration("SEEDER_DEPOSIT_EVERY", 5*time.Minute),
		GasReserveWei:  dec("SEEDER_GAS_RESERVE_WEI", "2000000000000000000"),
		DepositTargets: map[string]decimal.Decimal{},
	}
	for _, kv := range strings.Split(e.Opt("SEEDER_DEPOSIT_TARGETS", defaultTargets), ",") {
		tok, amt, _ := strings.Cut(strings.TrimSpace(kv), "=")
		v, err := decimal.NewFromString(amt)
		if err != nil || !common.IsHexAddress(tok) {
			decErr = append(decErr, "SEEDER_DEPOSIT_TARGETS")
			continue
		}
		cfg.DepositTargets[strings.ToLower(tok)] = v
	}
	poll := e.Duration("SEEDER_POLL", 2*time.Second)
	gateway := e.Opt("KURU_GATEWAY_URL", "https://gateway.testnet.kuru.io")
	rpcURL := e.Opt("RPC_URL", "")
	bot := common.HexToAddress(e.Opt("SEEDER_BOT_ADDRESS", "0xdf8f5E4d6e27ccfA85fD74Fc9b3c1c8632059970"))
	addr := ":" + e.Opt("PORT", "8086")
	if err := e.Err(); err != nil {
		return err
	}
	if len(decErr) > 0 {
		return fmt.Errorf("invalid decimal env: %v", decErr)
	}
	if cfg.Levels < 1 || cfg.SpreadBps < 1 {
		return fmt.Errorf("SEEDER_LEVELS and SEEDER_SPREAD_BPS must be >= 1")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return err
	}
	rdb := redis.NewClient(opt)
	defer rdb.Close()

	s := &mm.Seeder{Cfg: cfg, RDB: rdb, Exec: executor.New(execURL, secret), Kuru: kuru.New(gateway)}
	if rpcURL != "" {
		eth, err := chain.Dial(ctx, rpcURL)
		if err != nil {
			return err
		}
		defer eth.Close()
		s.WalletBalance = func(ctx context.Context) (*big.Int, error) { return eth.BalanceAt(ctx, bot, nil) }
	}

	mux := httpx.NewMux(map[string]httpx.Check{"redis": func(ctx context.Context) error { return rdb.Ping(ctx).Err() }})
	mux.HandleFunc("GET /api/v1/status", func(w http.ResponseWriter, _ *http.Request) { httpx.JSON(w, http.StatusOK, s.Status()) })
	go s.Run(ctx, poll)
	slog.Info("listening", "addr", addr, "enabled", cfg.Enabled)
	return httpx.Run(ctx, addr, mux)
}
