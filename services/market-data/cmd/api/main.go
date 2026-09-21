// market-data api: read-only HTTP on :8081 and an unauthenticated browser
// websocket on :8082. It never signs and never writes to chain.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/chains"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/config"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/httpx"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/wad"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/kuru"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/perpl"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/quote"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/store"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/ws"
)

// ponytail: 200-id cap per marks query, page it if a portfolio ever exceeds that
const maxMarkQuery = 200

type api struct {
	redis *store.Redis
	pg    *store.PG
	kuru  *kuru.Client
	perpl *perpl.Client
	// hot-cache mark reader; a field so a test can drive the route without redis
	markOf func(context.Context, store.Market) (price string, ts int64, ok bool)
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

	a := &api{redis: rdb, pg: pg, kuru: kuru.New(chainID), perpl: perpl.New()}
	a.markOf = a.cachedMark
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// perpl market ids and decimals are needed to serve their candles
	go a.refreshLoop(ctx)

	hub := ws.NewHub()
	go a.fanout(ctx, hub)

	origins := config.GetEnvList("CORS_ORIGINS", []string{"http://localhost:3000"})
	httpSrv := &http.Server{
		Addr:              ":" + config.GetEnv("MARKET_DATA_PORT", "8081"),
		Handler:           httpx.Logging(slog.Default(), httpx.CORS(origins, a.routes())),
		ReadHeaderTimeout: 10 * time.Second,
	}
	wsMux := http.NewServeMux()
	wsMux.Handle("/", hub)
	wsMux.Handle("/ws", hub)
	wsSrv := &http.Server{
		Addr:              ":" + config.GetEnv("MARKET_DATA_WS_PORT", "8082"),
		Handler:           httpx.CORS(origins, wsMux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	for _, s := range []*http.Server{httpSrv, wsSrv} {
		go func(s *http.Server) {
			slog.Info("listening", "addr", s.Addr)
			if err := s.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.Error("server stopped", "addr", s.Addr, "err", err)
				stop()
			}
		}(s)
	}

	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	httpSrv.Shutdown(shutdown)
	wsSrv.Shutdown(shutdown)
	slog.Info("stopped")
}

func logLevel() slog.Level {
	var l slog.Level
	if err := l.UnmarshalText([]byte(config.GetEnv("LOG_LEVEL", "info"))); err != nil {
		return slog.LevelInfo
	}
	return l
}

// fanout mirrors ingest's redis pub/sub onto every connected browser.
func (a *api) fanout(ctx context.Context, hub *ws.Hub) {
	for ctx.Err() == nil {
		if err := a.redis.Fanout(ctx, func(topic string, payload []byte) {
			hub.Publish(topic, payload)
		}); err != nil && ctx.Err() == nil {
			slog.Warn("redis fanout dropped, retrying", "err", err)
			time.Sleep(time.Second)
		}
	}
}

func (a *api) refreshLoop(ctx context.Context) {
	for {
		if _, err := a.perpl.Refresh(ctx); err != nil {
			slog.Warn("perpl context refresh", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Minute):
		}
	}
}

func (a *api) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", a.health)
	mux.HandleFunc("GET /v1/markets", a.markets)
	mux.HandleFunc("GET /v1/markets/{id}", a.market)
	mux.HandleFunc("GET /v1/marks", a.marks)
	mux.HandleFunc("GET /v1/book/{market_id}", a.book)
	mux.HandleFunc("GET /v1/candles/{market_id}", a.candles)
	mux.HandleFunc("POST /v1/quote", a.quote)
	return mux
}

func (a *api) health(w http.ResponseWriter, r *http.Request) {
	out := map[string]string{"status": "ok", "redis": "ok", "postgres": "ok"}
	if err := a.redis.Ping(r.Context()); err != nil {
		out["redis"], out["status"] = err.Error(), "degraded"
	}
	if err := a.pg.Ping(r.Context()); err != nil {
		out["postgres"], out["status"] = err.Error(), "degraded"
	}
	httpx.OK(w, out)
}

// allMarkets reads the hot cache and falls back to the venue when it is cold.
func (a *api) allMarkets(ctx context.Context) []store.Market {
	out := []store.Market{}
	if ms, err := a.redis.Markets(ctx, store.VenueKuru); err == nil {
		out = append(out, ms...)
	} else if ms, err := a.kuru.Markets(ctx); err == nil {
		out = append(out, ms...)
	}
	if ms, err := a.redis.Markets(ctx, store.VenuePerpl); err == nil {
		out = append(out, ms...)
	} else if pc := a.perpl.Cached(); pc != nil {
		out = append(out, pc.Markets...)
	}
	return out
}

func (a *api) lookup(ctx context.Context, id string) (store.Market, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, m := range a.allMarkets(ctx) {
		if strings.ToLower(m.ID) == id {
			return m, true
		}
	}
	return store.Market{}, false
}

func (a *api) markets(w http.ResponseWriter, r *http.Request) {
	httpx.OK(w, a.allMarkets(r.Context()))
}

func (a *api) market(w http.ResponseWriter, r *http.Request) {
	m, ok := a.lookup(r.Context(), r.PathValue("id"))
	if !ok {
		httpx.Fail(w, http.StatusNotFound, "market_not_found", "No market with that id is listed on Kuru or Perpl for this chain.")
		return
	}
	httpx.OK(w, m)
}

type mark struct {
	MarketID string `json:"market_id"`
	Price    string `json:"price"`
	Ts       int64  `json:"ts"`
}

// cachedMark reads the hot cache only: the last tick, else the book mid. A
// synchronous venue call here would put a venue outage on wallet's critical path.
func (a *api) cachedMark(ctx context.Context, m store.Market) (string, int64, bool) {
	if t, err := a.redis.Tick(ctx, m.Venue, m.ID); err == nil && t.Price != "" {
		return decimal(t.Price), t.Ts, true
	}
	if b, err := a.redis.Book(ctx, m.Venue, m.ID); err == nil {
		if mid := b.Mid(); mid != "" {
			return decimal(mid), b.Ts, true
		}
	}
	return "", 0, false
}

// marks answers wallet's portfolio valuation. A market we have no mark for is
// omitted: a zero would silently value a position at nothing.
func (a *api) marks(w http.ResponseWriter, r *http.Request) {
	requested := splitIDs(r.URL.Query().Get("markets"))
	if len(requested) > maxMarkQuery {
		httpx.Fail(w, http.StatusBadRequest, "too_many_markets", "markets accepts at most "+strconv.Itoa(maxMarkQuery)+" comma-separated ids per request.")
		return
	}
	out := []mark{}
	if len(requested) > 0 {
		byKey := map[string]store.Market{}
		for _, m := range a.allMarkets(r.Context()) {
			byKey[strings.ToLower(m.Venue+":"+m.Symbol)] = m
			byKey[strings.ToLower(m.ID)] = m
		}
		for _, id := range requested {
			m, ok := byKey[strings.ToLower(id)]
			if !ok {
				continue
			}
			price, ts, ok := a.markOf(r.Context(), m)
			if !ok {
				continue
			}
			out = append(out, mark{MarketID: id, Price: price, Ts: ts})
		}
	}
	httpx.OK(w, map[string]any{"marks": out})
}

// decimal renders a WAD integer as a human decimal string, never a float.
func decimal(wadInt string) string {
	n, ok := new(big.Int).SetString(wadInt, 10)
	if !ok {
		return ""
	}
	return wad.ToDecimalString(n, wad.Decimals)
}

func splitIDs(raw string) []string {
	out := []string{}
	for _, p := range strings.Split(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (a *api) book(w http.ResponseWriter, r *http.Request) {
	m, ok := a.lookup(r.Context(), r.PathValue("market_id"))
	if !ok {
		httpx.Fail(w, http.StatusNotFound, "market_not_found", "No market with that id is listed on Kuru or Perpl for this chain.")
		return
	}
	b, err := a.redis.Book(r.Context(), m.Venue, m.ID)
	if errors.Is(err, store.ErrMiss) {
		httpx.Fail(w, http.StatusServiceUnavailable, "book_cold", "No book has been ingested for this market yet; the ingest loop fills it within a second of starting.")
		return
	}
	if err != nil {
		httpx.Fail(w, http.StatusInternalServerError, "book_unavailable", err.Error())
		return
	}
	httpx.OK(w, b)
}

func (a *api) candles(w http.ResponseWriter, r *http.Request) {
	m, ok := a.lookup(r.Context(), r.PathValue("market_id"))
	if !ok {
		httpx.Fail(w, http.StatusNotFound, "market_not_found", "No market with that id is listed on Kuru or Perpl for this chain.")
		return
	}
	q := r.URL.Query()
	resolution, err := strconv.Atoi(q.Get("resolution"))
	if err != nil || resolution <= 0 {
		httpx.Fail(w, http.StatusBadRequest, "bad_resolution", "resolution must be a positive number of seconds, for example 60 or 3600.")
		return
	}
	now := time.Now().Unix()
	from := atoi64(q.Get("from"), now-int64(resolution)*500)
	to := atoi64(q.Get("to"), now)
	if from > to {
		httpx.Fail(w, http.StatusBadRequest, "bad_range", "from must not be later than to.")
		return
	}
	// Perpl's own candles are authoritative for Perpl markets; we only build
	// our own for Kuru, which has no candle endpoint at all.
	if m.Venue == store.VenuePerpl {
		cs, err := a.perpl.Candles(r.Context(), m.ID, resolution, from*1000, to*1000)
		if err != nil {
			httpx.Fail(w, http.StatusBadGateway, "perpl_candles_unavailable", err.Error())
			return
		}
		httpx.OK(w, cs)
		return
	}
	cs, err := a.pg.Candles(r.Context(), m.ID, resolution, from, to, perpl.MaxCandles)
	if err != nil {
		httpx.Fail(w, http.StatusInternalServerError, "candles_unavailable", err.Error())
		return
	}
	httpx.OK(w, cs)
}

type quoteRequest struct {
	MarketID string `json:"marketId"`
	Side     string `json:"side"`
	Size     string `json:"size"`
}

func (a *api) quote(w http.ResponseWriter, r *http.Request) {
	var req quoteRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		httpx.Fail(w, http.StatusBadRequest, "bad_body", "Body must be JSON with marketId, side and size.")
		return
	}
	m, ok := a.lookup(r.Context(), req.MarketID)
	if !ok {
		httpx.Fail(w, http.StatusNotFound, "market_not_found", "No market with that id is listed on Kuru or Perpl for this chain.")
		return
	}
	b, err := a.redis.Book(r.Context(), m.Venue, m.ID)
	if err != nil {
		httpx.Fail(w, http.StatusServiceUnavailable, "book_cold", "No book has been ingested for this market yet, so it cannot be quoted.")
		return
	}
	q, err := quote.FromBook(b, req.Side, req.Size)
	if err != nil {
		httpx.Fail(w, http.StatusBadRequest, "bad_quote_request", err.Error())
		return
	}
	httpx.OK(w, q)
}

func atoi64(s string, def int64) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return def
	}
	return n
}
