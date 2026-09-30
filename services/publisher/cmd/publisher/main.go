package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/chain"
	"github.com/snehendu098/sweem-terminal/internal/config"
	"github.com/snehendu098/sweem-terminal/internal/httpx"
	"github.com/snehendu098/sweem-terminal/internal/logx"
	"github.com/snehendu098/sweem-terminal/internal/pg"
	"github.com/snehendu098/sweem-terminal/internal/streams"
	"github.com/snehendu098/sweem-terminal/services/publisher/internal/market"
	"github.com/snehendu098/sweem-terminal/services/publisher/internal/rates"
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
	rpcURL, dbURL, redisURL, depPath := e.Str("RPC_URL"), e.Str("DATABASE_URL"), e.Str("REDIS_URL"), e.Str("DEPLOYMENTS_PATH")
	wsURL := e.Opt("KURU_WS_URL", "wss://exchange.kuru.io/ws")
	apiURL := e.Opt("KURU_API_URL", "https://exchange.kuru.io")
	perplURL := e.Opt("PERPL_API_URL", "https://app.perpl.xyz")
	devBps := e.Int("REF_DEVIATION_BPS", 500)
	tau := e.Duration("REF_TAU", time.Minute)
	rateEvery := e.Duration("RATES_EVERY", time.Minute)
	addr := e.Opt("ADDR", ":8081")
	if err := e.Err(); err != nil {
		return err
	}
	dep, err := config.LoadDeployments143(depPath)
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

	markets := Markets(dep)
	if err := market.Resolve(ctx, apiURL, markets); err != nil {
		return err
	}
	if err := streams.Publish(ctx, rdb, "", streams.RegistryMarkets, markets); err != nil {
		return err
	}
	feed := &market.Feed{WSURL: wsURL, APIURL: apiURL, RDB: rdb, Markets: markets, DeviationBps: devBps, Tau: tau,
		Candles: &market.Candles{PG: db, RDB: rdb}}
	poller := &rates.Poller{ETH: eth, RDB: rdb, Dep: dep, PerplURL: perplURL, HTTP: &http.Client{Timeout: 20 * time.Second}}

	errc := make(chan error, 4)
	go func() { errc <- feed.Run(ctx) }()
	go func() { errc <- poller.Run(ctx, rateEvery) }()
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
		"kuru": func(context.Context) error {
			if !feed.Connected() {
				return errors.New("disconnected")
			}
			return nil
		},
	})
	go func() { errc <- httpx.Run(ctx, addr, mux) }()
	slog.Info("publisher up", "addr", addr, "markets", len(markets))
	err = <-errc
	stop()
	return err
}

// Markets is the registry view of the Kuru V1 markets in deployments (raw precisions, lowercase addresses).
func Markets(dep *config.Deployments143) []streams.MarketInfo {
	var out []streams.MarketInfo
	for _, m := range dep.Kuru.Markets {
		base, quote := dep.Tokens[m.Base], dep.Tokens[m.Quote]
		out = append(out, streams.MarketInfo{
			Market: streams.Addr(strings.ToLower(m.Orderbook)), Base: streams.Addr(base.Address), Quote: streams.Addr(quote.Address),
			BaseDecimals: int(base.Decimals), QuoteDecimals: int(quote.Decimals),
			PricePrecision: dec(m.PricePrecision), SizePrecision: dec(m.SizePrecision), TickSize: dec(m.TickSize), MinSize: dec(m.MinSize),
			TakerFeePps: decimal.Zero,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Market < out[j].Market })
	return out
}

func dec(v uint64) decimal.Decimal { return decimal.NewFromUint64(v) }
