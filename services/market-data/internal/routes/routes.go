// Package routes serves the publisher's Redis state and candles read-only (architecture §3 market-data).
package routes

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/httpx"
	"github.com/snehendu098/sweem-terminal/internal/streams"
	"github.com/snehendu098/sweem-terminal/services/market-data/internal/hub"
	"github.com/snehendu098/sweem-terminal/services/market-data/internal/quote"
)

var (
	addrRE = regexp.MustCompile(`^0x[0-9a-f]{40}$`)
	intRE  = regexp.MustCompile(`^[0-9]{1,30}$`)
)

const (
	bookMaxAge    = 30 * time.Second
	maxCandles    = 1000
	defaultSlip   = 50
	defaultImpact = 100
)

var intervals = map[string]time.Duration{"1m": time.Minute, "5m": 5 * time.Minute, "1h": time.Hour, "1d": 24 * time.Hour}

// New serves the routes; origins are CORS origins (scheme://host[:port] or "*"), also allowed on the WebSocket.
func New(ctx context.Context, rdb *redis.Client, db *pgxpool.Pool, origins []string) http.Handler {
	s := srv{rdb, db}
	mux := httpx.NewMux(map[string]httpx.Check{
		"redis":    func(ctx context.Context) error { return rdb.Ping(ctx).Err() },
		"postgres": func(ctx context.Context) error { return db.Ping(ctx) },
	})
	h := hub.New(ctx, rdb)
	for _, o := range origins {
		if _, host, ok := strings.Cut(o, "://"); ok {
			o = host
		}
		h.OriginPatterns = append(h.OriginPatterns, o)
	}
	mux.HandleFunc("GET /api/v1/ws", h.Serve)
	mux.HandleFunc("GET /api/v1/markets", s.markets)
	mux.HandleFunc("GET /api/v1/books/{market}", s.book)
	mux.HandleFunc("GET /api/v1/quote/{market}", s.quote)
	mux.HandleFunc("POST /api/v1/quote/basket", s.basket)
	mux.HandleFunc("GET /api/v1/candles/{market}", s.candles)
	mux.HandleFunc("GET /api/v1/rates", s.rates)
	return httpx.CORS(origins, mux)
}

type srv struct {
	rdb *redis.Client
	db  *pgxpool.Pool
}

// addr lowercases a path address and reports whether it is well formed.
func addr(r *http.Request, name string) (string, bool) {
	a := strings.ToLower(r.PathValue(name))
	return a, addrRE.MatchString(a)
}

// load GETs a JSON key; false means a response was already written.
func load[T any](s srv, w http.ResponseWriter, r *http.Request, key string, missing int) (T, bool) {
	v, err := streams.Get[T](r.Context(), s.rdb, key)
	switch {
	case errors.Is(err, redis.Nil):
		code := "unavailable"
		if missing == http.StatusNotFound {
			code = "not_found"
		}
		httpx.Error(w, missing, code)
		return v, false
	case err != nil:
		slog.Error("redis get", "key", key, "err", err)
		httpx.Error(w, http.StatusServiceUnavailable, "unavailable")
		return v, false
	}
	return v, true
}

func (s srv) stale(r *http.Request) bool {
	alive, err := streams.Alive(r.Context(), s.rdb)
	return err != nil || !alive
}

func (s srv) markets(w http.ResponseWriter, r *http.Request) {
	if m, ok := load[json.RawMessage](s, w, r, streams.RegistryMarkets, http.StatusServiceUnavailable); ok {
		httpx.JSON(w, http.StatusOK, m)
	}
}

func (s srv) book(w http.ResponseWriter, r *http.Request) {
	a, ok := addr(r, "market")
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad_request")
		return
	}
	if b, ok := load[streams.BookSnapshot](s, w, r, streams.BookLatest(a), http.StatusNotFound); ok {
		httpx.JSON(w, http.StatusOK, struct {
			streams.BookSnapshot
			Stale bool `json:"stale"`
		}{b, s.stale(r)})
	}
}

func (s srv) rates(w http.ResponseWriter, r *http.Request) {
	if v, ok := load[streams.RatesSnapshot](s, w, r, streams.RatesLatest, http.StatusServiceUnavailable); ok {
		httpx.JSON(w, http.StatusOK, struct {
			streams.RatesSnapshot
			Stale bool `json:"stale"`
		}{v, s.stale(r)})
	}
}

// bpsParam parses an optional 0..10000 query param.
func bpsParam(r *http.Request, name string, def int64) (int64, bool) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return def, true
	}
	n, err := strconv.ParseInt(v, 10, 64)
	return n, err == nil && n >= 0 && n <= 10_000
}

// legState loads the market params and a fresh book for one market; false means a response was written.
func (s srv) legState(w http.ResponseWriter, r *http.Request, markets []streams.MarketInfo, market string) (streams.MarketInfo, streams.BookSnapshot, bool) {
	i := slices.IndexFunc(markets, func(m streams.MarketInfo) bool { return strings.EqualFold(string(m.Market), market) })
	if i < 0 {
		httpx.Error(w, http.StatusNotFound, "not_found")
		return streams.MarketInfo{}, streams.BookSnapshot{}, false
	}
	b, ok := load[streams.BookSnapshot](s, w, r, streams.BookLatest(market), http.StatusServiceUnavailable)
	return markets[i], b, ok
}

// stateStale: publisher heartbeat gone or a book older than bookMaxAge; quotes then refuse with 503 stale.
func (s srv) stateStale(r *http.Request, books ...streams.BookSnapshot) bool {
	for _, b := range books {
		if time.Since(time.UnixMilli(b.Ts)) > bookMaxAge {
			return true
		}
	}
	return s.stale(r)
}

func quoteErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, quote.ErrBadInput), errors.Is(err, quote.ErrMinSize):
		httpx.Error(w, http.StatusBadRequest, err.Error())
	default:
		httpx.Error(w, http.StatusServiceUnavailable, "unavailable")
	}
}

func (s srv) quote(w http.ResponseWriter, r *http.Request) {
	a, ok := addr(r, "market")
	q := r.URL.Query()
	side, amt := q.Get("side"), q.Get("amount")
	slip, ok1 := bpsParam(r, "slippageBps", defaultSlip)
	impact, ok2 := bpsParam(r, "maxImpactBps", defaultImpact)
	if !ok || !ok1 || !ok2 || (side != "buy" && side != "sell") || !intRE.MatchString(amt) {
		httpx.Error(w, http.StatusBadRequest, "bad_request")
		return
	}
	markets, ok := load[[]streams.MarketInfo](s, w, r, streams.RegistryMarkets, http.StatusServiceUnavailable)
	if !ok {
		return
	}
	m, b, ok := s.legState(w, r, markets, a)
	if !ok {
		return
	}
	if s.stateStale(r, b) {
		httpx.Error(w, http.StatusServiceUnavailable, "stale")
		return
	}
	res, err := quote.Market(m, b, side, decimal.RequireFromString(amt), slip, impact)
	if err != nil {
		quoteErr(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, res)
}

type basketReq struct {
	Side         string `json:"side"` // buy (default) | sell
	AmountUsdc   string `json:"amountUsdc"`
	SlippageBps  *int64 `json:"slippageBps"`
	MaxImpactBps *int64 `json:"maxImpactBps"`
	Legs         []struct {
		Market    string `json:"market"`
		WeightBps int64  `json:"weightBps"` // buy
		Amount    string `json:"amount"`    // sell: base atoms
	} `json:"legs"`
}

// basket: buy splits amountUsdc by weightBps (sum 10000); sell takes per-leg base amounts.
func (s srv) basket(w http.ResponseWriter, r *http.Request) {
	var req basketReq
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req)
	if req.Side == "" {
		req.Side = "buy"
	}
	slip, impact := int64(defaultSlip), int64(defaultImpact)
	if req.SlippageBps != nil {
		slip = *req.SlippageBps
	}
	if req.MaxImpactBps != nil {
		impact = *req.MaxImpactBps
	}
	buy := req.Side == "buy"
	valid := err == nil && (buy || req.Side == "sell") && (!buy || intRE.MatchString(req.AmountUsdc)) &&
		len(req.Legs) > 0 && len(req.Legs) <= 10 && slip >= 0 && slip <= 10_000 && impact >= 0 && impact <= 10_000
	var sum int64
	seen := map[string]bool{}
	for i, l := range req.Legs {
		m := strings.ToLower(l.Market)
		if !addrRE.MatchString(m) || seen[m] || (buy && l.WeightBps <= 0) || (!buy && !intRE.MatchString(l.Amount)) {
			valid = false
		}
		seen[m], req.Legs[i].Market, sum = true, m, sum+l.WeightBps
	}
	if !valid || (buy && sum != 10_000) {
		httpx.Error(w, http.StatusBadRequest, "bad_request")
		return
	}
	markets, ok := load[[]streams.MarketInfo](s, w, r, streams.RegistryMarkets, http.StatusServiceUnavailable)
	if !ok {
		return
	}
	var ms []streams.MarketInfo
	var bs []streams.BookSnapshot
	var ws []int64
	var amts []decimal.Decimal
	for _, l := range req.Legs {
		m, b, ok := s.legState(w, r, markets, l.Market)
		if !ok {
			return
		}
		ms, bs, ws = append(ms, m), append(bs, b), append(ws, l.WeightBps)
		if !buy {
			amts = append(amts, decimal.RequireFromString(l.Amount))
		}
	}
	if s.stateStale(r, bs...) {
		httpx.Error(w, http.StatusServiceUnavailable, "stale")
		return
	}
	if buy {
		httpx.JSON(w, http.StatusOK, quote.BuyBasket(decimal.RequireFromString(req.AmountUsdc), ws, ms, bs, slip, impact))
	} else {
		httpx.JSON(w, http.StatusOK, quote.SellBasket(amts, ms, bs, slip, impact))
	}
}

// candles returns up to 1000 candles in [from, to) (unix ms), newest last; default is the latest 1000.
func (s srv) candles(w http.ResponseWriter, r *http.Request) {
	a, ok := addr(r, "market")
	q := r.URL.Query()
	iv := q.Get("interval")
	d, ok2 := intervals[iv]
	now := time.Now().UnixMilli()
	to, err1 := msParam(q.Get("to"), now+1)
	from, err2 := msParam(q.Get("from"), to-maxCandles*d.Milliseconds())
	if !ok || !ok2 || err1 != nil || err2 != nil || from >= to {
		httpx.Error(w, http.StatusBadRequest, "bad_request")
		return
	}
	rows, err := s.db.Query(r.Context(), `select * from (
		select extract(epoch from t)::bigint * 1000, o, h, l, c, v, q, n from candles
		where market = $1 and interval = $2 and t >= $3 and t < $4 order by t desc limit $5) x order by 1`,
		a, iv, time.UnixMilli(from), time.UnixMilli(to), maxCandles)
	if err != nil {
		slog.Error("candles", "err", err)
		httpx.Error(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	defer rows.Close()
	out := []streams.Candle{}
	for rows.Next() {
		c := streams.Candle{Market: streams.Addr(a), Interval: iv}
		if err := rows.Scan(&c.T, &c.O, &c.H, &c.L, &c.C, &c.V, &c.Q, &c.N); err != nil {
			slog.Error("candles scan", "err", err)
			httpx.Error(w, http.StatusServiceUnavailable, "unavailable")
			return
		}
		out = append(out, c)
	}
	if rows.Err() != nil {
		httpx.Error(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func msParam(v string, def int64) (int64, error) {
	if v == "" {
		return def, nil
	}
	return strconv.ParseInt(v, 10, 64)
}
