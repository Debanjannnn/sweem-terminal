// Package routes serves the publisher's Redis state read-only (architecture §3 market-data).
package routes

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/httpx"
	"github.com/snehendu098/sweem-terminal/internal/streams"
	"github.com/snehendu098/sweem-terminal/services/market-data/internal/hub"
	"github.com/snehendu098/sweem-terminal/services/market-data/internal/quote"
)

var (
	addrRE = regexp.MustCompile(`^0x[0-9a-f]{40}$`)
	intRE  = regexp.MustCompile(`^[0-9]{1,78}$`)
)

func New(ctx context.Context, rdb *redis.Client) http.Handler {
	s := srv{rdb}
	mux := httpx.NewMux(map[string]httpx.Check{"redis": func(ctx context.Context) error { return rdb.Ping(ctx).Err() }})
	mux.HandleFunc("GET /api/v1/ws", hub.New(ctx, rdb).Serve)
	mux.HandleFunc("GET /api/v1/markets", s.markets)
	mux.HandleFunc("GET /api/v1/baskets/{address}/nav", s.nav)
	mux.HandleFunc("GET /api/v1/books/{market}", s.book)
	mux.HandleFunc("GET /api/v1/quote/{basket}", s.quote)
	return mux
}

type srv struct{ rdb *redis.Client }

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

func (s srv) nav(w http.ResponseWriter, r *http.Request) {
	a, ok := addr(r, "address")
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad_request")
		return
	}
	if n, ok := load[streams.NavPoint](s, w, r, streams.NavLatest(a), http.StatusNotFound); ok {
		n.Stale = n.Stale || s.stale(r)
		httpx.JSON(w, http.StatusOK, n)
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

func (s srv) quote(w http.ResponseWriter, r *http.Request) {
	a, ok := addr(r, "basket")
	q := r.URL.Query()
	side, amt := q.Get("side"), q.Get("amount")
	slip := int64(50)
	if v := q.Get("slippageBps"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 || n > 10_000 {
			ok = false
		}
		slip = n
	}
	if !ok || (side != "mint" && side != "redeem") || !intRE.MatchString(amt) {
		httpx.Error(w, http.StatusBadRequest, "bad_request")
		return
	}
	if s.stale(r) {
		httpx.Error(w, http.StatusServiceUnavailable, "stale")
		return
	}
	baskets, ok := load[[]streams.BasketInfo](s, w, r, streams.RegistryBaskets, http.StatusServiceUnavailable)
	if !ok {
		return
	}
	st := quote.State{Markets: map[streams.Addr]streams.MarketInfo{}, Books: map[streams.Addr]streams.BookSnapshot{}}
	found := false
	for _, b := range baskets {
		if strings.EqualFold(string(b.Address), a) {
			st.Basket, found = b, true
		}
	}
	if !found {
		httpx.Error(w, http.StatusNotFound, "not_found")
		return
	}
	if st.Nav, ok = load[streams.NavPoint](s, w, r, streams.NavLatest(a), http.StatusServiceUnavailable); !ok {
		return
	}
	markets, ok := load[[]streams.MarketInfo](s, w, r, streams.RegistryMarkets, http.StatusServiceUnavailable)
	if !ok {
		return
	}
	for _, m := range markets {
		st.Markets[streams.Addr(strings.ToLower(string(m.Market)))] = m
	}
	for _, c := range st.Basket.Components {
		m := strings.ToLower(string(c.Market))
		b, ok := load[streams.BookSnapshot](s, w, r, streams.BookLatest(m), http.StatusServiceUnavailable)
		if !ok {
			return
		}
		st.Books[streams.Addr(m)] = b
	}

	fn := quote.Mint
	if side == "redeem" {
		fn = quote.Redeem
	}
	res, err := fn(st, decimal.RequireFromString(amt), slip)
	switch {
	case errors.Is(err, quote.ErrBadInput):
		httpx.Error(w, http.StatusBadRequest, "bad_request")
	case err != nil:
		httpx.Error(w, http.StatusServiceUnavailable, "unavailable")
	default:
		res.Stale = st.Nav.Stale
		httpx.JSON(w, http.StatusOK, res)
	}
}
