package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/chains"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/kuru"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/perpl"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/store"
)

const kuruMarkets = `{"data":[{
 "marketAddress":"0x5bdea6f9f9aba34f4ecb9b865646a792b835ef7f","symbol":"CBBTCUSDC",
 "baseToken":{"tokenAddress":"0xef2","symbol":"cbBTC","decimals":8},
 "quoteToken":{"tokenAddress":"0xee0","symbol":"USDC","decimals":6},
 "status":"active","pricePrecision":"100","sizePrecision":"100000000","tickSize":"1",
 "minQuoteNotionalX18":"10000000000000000000","takerFeePps":7000,"makerFeePps":4000}]}`

const perplContext = `{"chain":{"chain_id":10143},
 "instances":[{"id":12,"collateral_token_id":1}],
 "tokens":[{"id":1,"symbol":"AUSD","decimals":6}],
 "markets":[{"id":16,"instance_id":12,"symbol":"BTC",
  "config":{"is_open":true,"price_decimals":1,"size_decimals":5,"min_posting_amount":"0","maker_fee":45,"taker_fee":345},
  "state":{"mrk":817779,"lst":817733}}]}`

// marksAPI wires the real route table. Redis is deliberately absent, so
// allMarkets takes its cold-cache fallback and markOf is the only mark source.
func marksAPI(t *testing.T, marks map[string]string) http.Handler {
	t.Helper()
	venue := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/markets": // the kuru client pins the version prefix
			w.Write([]byte(kuruMarkets))
		case "/v1/pub/context":
			w.Write([]byte(perplContext))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(venue.Close)
	t.Setenv("KURU_API_URL_10143", venue.URL)
	t.Setenv("PERPL_API_URL", venue.URL)
	// nothing listens on port 1, so every hot-cache read misses
	t.Setenv("REDIS_URL", "redis://127.0.0.1:1?max_retries=-1&dial_timeout=5ms")

	rdb, err := store.NewRedis()
	if err != nil {
		t.Fatalf("redis client: %v", err)
	}
	a := &api{redis: rdb, kuru: kuru.New(chains.MonadTestnet), perpl: perpl.New()}
	if _, err := a.perpl.Refresh(context.Background()); err != nil {
		t.Fatalf("perpl refresh: %v", err)
	}
	a.markOf = func(_ context.Context, m store.Market) (string, int64, bool) {
		p, ok := marks[m.Venue+":"+strings.ToLower(m.Symbol)]
		return p, 1700000000000, ok
	}
	return a.routes()
}

func getMarks(t *testing.T, h http.Handler, query string) []map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/marks?markets="+query, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	var env struct {
		Data struct {
			Marks []map[string]any `json:"marks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("body %s is not the envelope wallet decodes: %v", rec.Body.String(), err)
	}
	return env.Data.Marks
}

func TestMarks(t *testing.T) {
	t.Run("a market with no mark is omitted, never zero", func(t *testing.T) {
		h := marksAPI(t, map[string]string{"kuru:cbbtcusdc": "81777.9"})
		got := getMarks(t, h, "kuru:cbbtcusdc,perpl:btc,sweem:vol:ETH/USD")
		if len(got) != 1 {
			t.Fatalf("marks = %v, want only the one market that has a mark", got)
		}
		m := got[0]
		if m["market_id"] != "kuru:cbbtcusdc" {
			t.Errorf("market_id = %v, want the id as requested", m["market_id"])
		}
		if m["price"] != "81777.9" {
			t.Errorf("price = %v (%T), want the decimal string 81777.9", m["price"], m["price"])
		}
		if m["ts"].(float64) != 1700000000000 {
			t.Errorf("ts = %v", m["ts"])
		}
	})

	t.Run("no marks at all is an empty array, not null", func(t *testing.T) {
		h := marksAPI(t, map[string]string{})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/marks?markets=kuru:cbbtcusdc", nil))
		if !strings.Contains(rec.Body.String(), `"marks":[]`) {
			t.Fatalf("body = %s, want an empty marks array", rec.Body.String())
		}
	})

	t.Run("every known market resolves by venue:symbol", func(t *testing.T) {
		h := marksAPI(t, map[string]string{"kuru:cbbtcusdc": "81777.9", "perpl:btc": "81777.9"})
		if got := getMarks(t, h, "kuru:cbbtcusdc,perpl:btc"); len(got) != 2 {
			t.Fatalf("marks = %v, want both", got)
		}
	})

	t.Run("an unknown market is omitted rather than erroring the batch", func(t *testing.T) {
		h := marksAPI(t, map[string]string{"kuru:cbbtcusdc": "81777.9"})
		if got := getMarks(t, h, "kuru:nope,kuru:cbbtcusdc"); len(got) != 1 {
			t.Fatalf("marks = %v", got)
		}
	})

	t.Run("an empty query is an empty array", func(t *testing.T) {
		h := marksAPI(t, map[string]string{"kuru:cbbtcusdc": "81777.9"})
		if got := getMarks(t, h, ""); len(got) != 0 {
			t.Fatalf("marks = %v", got)
		}
	})
}

func TestDecimalIsNeverAFloat(t *testing.T) {
	cases := []struct{ wadInt, want string }{
		{"81777900000000000000000", "81777.9"},
		{"1000000000000000000", "1"},
		{"1", "0.000000000000000001"},
		{"0", "0"},
		{"not-a-number", ""},
	}
	for _, tc := range cases {
		if got := decimal(tc.wadInt); got != tc.want {
			t.Errorf("decimal(%q) = %q, want %q", tc.wadInt, got, tc.want)
		}
	}
}
