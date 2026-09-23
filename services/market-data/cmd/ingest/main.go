// market-data ingest: the write side. Streams Kuru and Perpl, derives ticks
// and candles, and fans everything out through redis for the api to serve.
package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/chains"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/config"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/indexerclient"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/natsbus"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/book"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/candle"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/kuru"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/perpl"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/store"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/ws"
)

// measured cadences, ported from the old stack
const (
	tickFlushEvery   = time.Second
	tickFlushAt      = 500
	candleFlushEvery = time.Second
	kuruMarketsEvery = 30 * time.Second
	pruneEvery       = time.Hour
	perplCtxEvery    = 5 * time.Minute
	backfillWindow   = 48 * time.Hour
)

type ingest struct {
	redis *store.Redis
	pg    *store.PG
	kuru  *kuru.Client
	perpl *perpl.Client
	agg   *candle.Agg

	mu      sync.Mutex
	pending []candle.Tick
	full    chan struct{}
}

func main() {
	config.LoadDotEnv(".env")
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel()})))

	chainID := config.GetEnvInt("CHAIN_ID", chains.MonadTestnet)
	if !chains.IsSupported(chainID) {
		slog.Error("CHAIN_ID is not a supported Monad chain", "chain_id", chainID)
		os.Exit(1)
	}
	rdb, err := store.NewRedis()
	if err != nil {
		slog.Error("redis", "err", err)
		os.Exit(1)
	}
	defer rdb.Close()
	pg, err := store.NewPG()
	if err != nil {
		slog.Error("postgres", "err", err)
		os.Exit(1)
	}
	defer pg.Close()

	g := &ingest{
		redis: rdb, pg: pg,
		kuru: kuru.New(chainID), perpl: perpl.New(),
		agg:  candle.New(),
		full: make(chan struct{}, 1),
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go g.kuruMarketsLoop(ctx)
	go g.kuruStreamLoop(ctx)
	go g.tickFlushLoop(ctx)
	go g.candleFlushLoop(ctx)
	go g.pruneLoop(ctx)
	go g.perplLoop(ctx)
	go g.indexLoop(ctx)
	go g.backfill(ctx) // never blocks anything live

	slog.Info("ingest running", "chain_id", chainID)
	<-ctx.Done()
	slog.Info("stopped")
}

func logLevel() slog.Level {
	var l slog.Level
	if err := l.UnmarshalText([]byte(config.GetEnv("LOG_LEVEL", "info"))); err != nil {
		return slog.LevelInfo
	}
	return l
}

func (g *ingest) publish(ctx context.Context, topic string, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		slog.Error("marshal topic payload", "topic", topic, "err", err)
		return
	}
	if err := g.redis.Publish(ctx, topic, b); err != nil {
		slog.Warn("publish", "topic", topic, "err", err)
	}
}

func every(ctx context.Context, d time.Duration, fn func(context.Context)) {
	t := time.NewTicker(d)
	defer t.Stop()
	for {
		fn(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (g *ingest) kuruMarketsLoop(ctx context.Context) {
	every(ctx, kuruMarketsEvery, func(ctx context.Context) {
		ms, err := g.kuru.Markets(ctx)
		if err != nil {
			slog.Warn("kuru markets", "err", err)
			return
		}
		if err := g.redis.PutMarkets(ctx, store.VenueKuru, ms); err != nil {
			slog.Warn("cache kuru markets", "err", err)
		}
	})
}

// The Kuru book arrives on the gateway websocket at Monad's proposed tier and
// the trades at finalized, so ticks are real prints and never a book mid.
func (g *ingest) kuruStreamLoop(ctx context.Context) {
	err := g.kuru.Stream(ctx, kuru.Handlers{
		Book: func(b book.Book) { g.onBook(ctx, b) },
		Tick: func(t candle.Tick) { g.onTick(ctx, t, true) },
	})
	if err != nil && ctx.Err() == nil {
		slog.Error("kuru gateway stream stopped", "err", err)
	}
}

func (g *ingest) onBook(ctx context.Context, b book.Book) {
	if err := g.redis.PutBook(ctx, b); err != nil {
		slog.Warn("cache book", "market", b.MarketID, "err", err)
	}
	g.publish(ctx, ws.BookTopic(b.Venue, b.MarketID), b)
}

// aggregate is false for venues that publish their own candles.
func (g *ingest) onTick(ctx context.Context, t candle.Tick, aggregate bool) {
	if err := g.redis.PutTick(ctx, t); err != nil {
		slog.Warn("cache tick", "market", t.MarketID, "err", err)
	}
	g.publish(ctx, ws.TickTopic(t.Venue, t.MarketID), t)
	if aggregate {
		g.agg.Add(t)
	}
	g.mu.Lock()
	g.pending = append(g.pending, t)
	overflow := len(g.pending) >= tickFlushAt
	g.mu.Unlock()
	if overflow {
		select {
		case g.full <- struct{}{}:
		default:
		}
	}
}

// flush at 1000ms or 500 ticks, whichever comes first
func (g *ingest) tickFlushLoop(ctx context.Context) {
	t := time.NewTicker(tickFlushEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-g.full:
		}
		g.mu.Lock()
		batch := g.pending
		g.pending = nil
		g.mu.Unlock()
		if err := g.pg.InsertTicks(ctx, batch); err != nil {
			slog.Warn("persist ticks", "count", len(batch), "err", err)
		}
	}
}

func (g *ingest) candleFlushLoop(ctx context.Context) {
	every(ctx, candleFlushEvery, func(ctx context.Context) {
		cs := g.agg.Flush()
		if len(cs) == 0 {
			return
		}
		if err := g.pg.UpsertCandles(ctx, cs); err != nil {
			slog.Warn("persist candles", "count", len(cs), "err", err)
		}
		for _, c := range cs {
			g.publish(ctx, ws.CandleTopic(c.MarketID, c.Resolution), c)
		}
	})
}

func (g *ingest) pruneLoop(ctx context.Context) {
	every(ctx, pruneEvery, func(ctx context.Context) {
		n, err := g.pg.PruneTicks(ctx, store.TickRetention)
		if err != nil {
			slog.Warn("prune ticks", "err", err)
			return
		}
		slog.Info("pruned ticks", "rows", n, "retention", store.TickRetention)
	})
}

func (g *ingest) perplLoop(ctx context.Context) {
	go every(ctx, perplCtxEvery, func(ctx context.Context) {
		pc, err := g.perpl.Refresh(ctx)
		if err != nil {
			slog.Warn("perpl context", "err", err)
			return
		}
		if err := g.redis.PutMarkets(ctx, store.VenuePerpl, pc.Markets); err != nil {
			slog.Warn("cache perpl markets", "err", err)
		}
	})
	err := g.perpl.Stream(ctx, perpl.Handlers{
		Book: func(b book.Book) { g.onBook(ctx, b) },
		// perpl serves its own candles, so their ticks are never aggregated
		Tick:   func(t candle.Tick) { g.onTick(ctx, t, false) },
		Candle: func(c candle.Candle) { g.publish(ctx, ws.CandleTopic(c.MarketID, c.Resolution), c) },
	})
	if err != nil && ctx.Err() == nil {
		slog.Error("perpl stream stopped", "err", err)
	}
}

// indexLoop republishes the oracle's index values onto the browser's
// index.<market_id> topic, which is what the Velocity and Power charts read.
func (g *ingest) indexLoop(ctx context.Context) {
	bus, err := natsbus.New(config.GetEnv("NATS_URL", "nats://localhost:4222"))
	if err != nil {
		slog.Warn("nats unavailable, index topics will stay silent", "err", err)
		return
	}
	defer bus.Close()
	err = bus.Subscribe(ctx, natsbus.SubjOracleIndexPublished, "market-data-index", func(payload []byte) error {
		var msg map[string]any
		if err := json.Unmarshal(payload, &msg); err != nil {
			return err
		}
		id, _ := firstString(msg, "marketId", "market_id", "indexId", "index_id")
		if id == "" {
			slog.Warn("index event without a market id", "payload", string(payload))
			return nil
		}
		return g.redis.Publish(ctx, ws.IndexTopic(id), payload)
	})
	if err != nil {
		slog.Warn("subscribe index events", "err", err)
		return
	}
	<-ctx.Done()
}

func firstString(m map[string]any, keys ...string) (string, bool) {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			return v, true
		}
	}
	return "", false
}

// the indexer stores block timestamps in seconds; candles bucket in millis
func millis(ts int64) int64 {
	if ts < 1e12 {
		return ts * 1000
	}
	return ts
}

// backfill replays finalized Kuru Trade events into candles so a cold start
// shows a chart. Nothing live waits on the indexer.
func (g *ingest) backfill(ctx context.Context) {
	ms, err := g.kuru.Markets(ctx)
	if err != nil {
		slog.Warn("backfill skipped, kuru market list unavailable", "err", err)
		return
	}
	idx := indexerclient.New()
	to := time.Now().Unix()
	from := to - int64(backfillWindow.Seconds())
	agg := candle.New()
	for _, m := range ms {
		trades, err := idx.KuruTrades(ctx, m.ID, from, to)
		if err != nil {
			slog.Warn("backfill", "market", m.ID, "err", err)
			continue
		}
		for _, t := range trades {
			side := "sell"
			if t.IsBuy {
				side = "buy"
			}
			agg.Add(candle.Tick{MarketID: m.ID, Venue: store.VenueKuru, Price: t.Price, Size: t.Size, Side: side, Ts: millis(t.Ts)})
		}
		cs := agg.Flush()
		if err := g.pg.UpsertCandles(ctx, cs); err != nil {
			slog.Warn("backfill persist", "market", m.ID, "err", err)
			continue
		}
		slog.Info("backfilled", "market", m.ID, "trades", len(trades), "candles", len(cs))
	}
}
