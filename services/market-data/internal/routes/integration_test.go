//go:build integration

package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/streams"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func randAddr() string {
	return "0x" + strings.Repeat("0", 8) + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// seed writes registry, books, rates into Redis DB 15 (never a dev publisher's DB 0) and candles into a throwaway PG schema.
func seed(t *testing.T, ctx context.Context) (*redis.Client, *pgxpool.Pool, []string) {
	url := os.Getenv("REDIS_URL")
	if url == "" {
		url = "redis://localhost:6380"
	}
	opt, err := redis.ParseURL(url)
	must(t, err)
	opt.DB = 15
	rdb := redis.NewClient(opt)

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://sweem:sweem@localhost:5433/sweem?sslmode=disable"
	}
	schema := "t_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	admin, err := pgxpool.New(ctx, dbURL)
	must(t, err)
	_, err = admin.Exec(ctx, "create schema "+schema)
	must(t, err)
	db, err := pgxpool.New(ctx, dbURL+"&search_path="+schema)
	must(t, err)
	sql, err := os.ReadFile("../../../../migrations/0007_candles.sql")
	must(t, err)
	_, err = db.Exec(ctx, string(sql))
	must(t, err)

	// WETH-like and MON-like markets with the real V1 precisions
	mk := []string{randAddr(), randAddr()}
	infos := []streams.MarketInfo{
		{Market: streams.Addr(mk[0]), Symbol: "WETH_USDC", BaseDecimals: 18, QuoteDecimals: 6, PricePrecision: d("10000"), SizePrecision: d("10000000000"), MinSize: d("20000000")},
		{Market: streams.Addr(mk[1]), Symbol: "MON_USDC", BaseDecimals: 18, QuoteDecimals: 6, PricePrecision: d("100000000"), SizePrecision: d("10000000000"), MinSize: d("2000000000000")},
	}
	now := time.Now().UnixMilli()
	books := []streams.BookSnapshot{
		{Market: streams.Addr(mk[0]), Seq: 1, Ts: now, Bids: []streams.Level{{d("2000"), d("1")}}, Asks: []streams.Level{{d("2000"), d("1")}, {d("2010"), d("1")}}},
		{Market: streams.Addr(mk[1]), Seq: 1, Ts: now, Bids: []streams.Level{{d("0.027"), d("100000")}}, Asks: []streams.Level{{d("0.02705"), d("100000")}}},
	}
	for i, m := range mk {
		must(t, streams.Publish(ctx, rdb, streams.Book(m), streams.BookLatest(m), books[i]))
	}
	must(t, streams.Publish(ctx, rdb, "", streams.RegistryMarkets, infos))
	must(t, streams.Publish(ctx, rdb, streams.Rates, streams.RatesLatest, streams.RatesSnapshot{Ts: now,
		Rates:   []streams.RateInfo{{Source: "lending", Venue: "aave", Asset: "USDC", Apy: d("0.039946"), Kind: "supply", Ts: now}},
		Funding: []streams.FundingInfo{{PerpID: 10, Name: "MON", Rate: d("0.00001"), IntervalSec: 2580, AprShort: d("0.122233"), AprShort7d: d("0.2")}}}))
	for i := range 3 {
		_, err := db.Exec(ctx, `insert into candles values ($1,'1m',$2,1,2,0.5,1.5,10,15,3)`, mk[0], time.UnixMilli(now-now%60000-int64(i)*60000))
		must(t, err)
	}
	must(t, streams.Beat(ctx, rdb))
	t.Cleanup(func() {
		keys := []string{streams.RegistryMarkets, streams.Rates, streams.RatesLatest, streams.HeartbeatKey}
		for _, m := range mk {
			keys = append(keys, streams.Book(m), streams.BookLatest(m), streams.Candles(m, "1m"), streams.CandlesLatest(m, "1m"))
		}
		rdb.Del(context.Background(), keys...)
		db.Close()
		admin.Exec(context.Background(), "drop schema "+schema+" cascade")
		admin.Close()
	})
	return rdb, db, mk
}

func call(t *testing.T, method, url, body string) (int, map[string]any, []byte) {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	res, err := http.DefaultClient.Do(req)
	must(t, err)
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	var v map[string]any
	json.Unmarshal(b, &v)
	return res.StatusCode, v, b
}

func TestHTTPRoutes(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	rdb, db, mk := seed(t, ctx)
	srv := httptest.NewServer(New(ctx, rdb, db, []string{"http://allowed.test"}))
	defer srv.Close()
	u := srv.URL + "/api/v1"

	if c, _, b := call(t, "GET", u+"/markets", ""); c != 200 || !bytes.Contains(b, []byte(`"symbol":"WETH_USDC"`)) {
		t.Fatalf("markets %d %s", c, b)
	}
	if c, v, _ := call(t, "GET", u+"/books/"+mk[1], ""); c != 200 || v["seq"] != 1.0 || v["stale"] != false {
		t.Fatalf("book %d %v", c, v)
	}

	// client/executor shape: market address (any case), raw atoms
	c, v, _ := call(t, "GET", u+"/quote/0x"+strings.ToUpper(mk[0][2:])+"?side=buy&amount=3000000000&slippageBps=100&maxImpactBps=50", "")
	if c != 200 || v["expectedOut"] != "1497512437800000000" || v["minOut"] != "1482537313422000000" || v["orderSize"] != "30000000" || v["maxSafeAmount"] != "4010000000" {
		t.Fatalf("quote %d %v", c, v)
	}
	if c, v, _ := call(t, "GET", u+"/quote/"+mk[1]+"?side=sell&amount=300000000000000000000", ""); c != 200 || v["expectedOut"] != "8100000" || v["orderSize"] != "3000000000000" {
		t.Fatalf("mon sell %d %v", c, v)
	}
	for q, want := range map[string]int{
		"?side=mint&amount=1":                      400,
		"?side=buy&amount=1.5":                     400,
		"?side=buy&amount=-1":                      400,
		"?side=buy&amount=1000&slippageBps=10001":  400,
		"?side=buy&amount=1000&maxImpactBps=x":     400,
		"?side=buy&amount=1000000":                 400, // $1 < minSize
		"?side=sell&amount=100000000000000000000":  400, // 100 MON < 200
		"?side=sell&amount=1000000000000000000000": 200,
	} {
		m := mk[0]
		if strings.Contains(q, "sell") {
			m = mk[1]
		}
		if c, v, _ := call(t, "GET", u+"/quote/"+m+q, ""); c != want {
			t.Errorf("%s: %d %v", q, c, v)
		}
	}
	if c, _, _ := call(t, "GET", u+"/quote/"+randAddr()+"?side=buy&amount=1000000", ""); c != 404 {
		t.Fatalf("unknown market %d", c)
	}

	body := fmt.Sprintf(`{"amountUsdc":"6000000000","slippageBps":50,"legs":[{"market":%q,"weightBps":5000},{"market":%q,"weightBps":5000}]}`, mk[0], strings.ToUpper(mk[1]))
	c, v, b := call(t, "POST", u+"/quote/basket", body)
	if c != 200 || v["ok"] != true || v["partial"] != true || v["fillableUsdc"] != "5705000000" || len(v["legs"].([]any)) != 2 { // MON leg exhausts its book
		t.Fatalf("basket %d %s", c, b)
	}
	// sell: per-leg holdings attributed to the basket
	sell := fmt.Sprintf(`{"side":"sell","slippageBps":0,"legs":[{"market":%q,"amount":"1000000000000000000"},{"market":%q,"amount":"300000000000000000000"}]}`, mk[0], mk[1])
	if c, v, b := call(t, "POST", u+"/quote/basket", sell); c != 200 || v["side"] != "sell" || v["ok"] != true || v["expectedOutUsdc"] != "2008100000" || v["minOutUsdc"] != "2008100000" {
		t.Fatalf("sell basket %d %s", c, b)
	}
	for _, bad := range []string{
		fmt.Sprintf(`{"side":"sell","legs":[{"market":%q}]}`, mk[0]),
		fmt.Sprintf(`{"side":"hold","amountUsdc":"1","legs":[{"market":%q,"weightBps":10000}]}`, mk[0]),
		`{"amountUsdc":"1","legs":[]}`,
		fmt.Sprintf(`{"amountUsdc":"1","legs":[{"market":%q,"weightBps":9000}]}`, mk[0]),
		fmt.Sprintf(`{"amountUsdc":"1","legs":[{"market":%q,"weightBps":5000},{"market":%q,"weightBps":5000}]}`, mk[0], mk[0]),
		`not json`,
	} {
		if c, _, _ := call(t, "POST", u+"/quote/basket", bad); c != 400 {
			t.Errorf("%s: %d", bad, c)
		}
	}

	c, _, b = call(t, "GET", u+"/candles/"+mk[0]+"?interval=1m", "")
	var cs []streams.Candle
	json.Unmarshal(b, &cs)
	if c != 200 || len(cs) != 3 || cs[0].T >= cs[2].T || !cs[2].C.Equal(d("1.5")) || cs[2].N != 3 {
		t.Fatalf("candles %d %s", c, b)
	}
	if c, _, b := call(t, "GET", u+"/candles/"+mk[0]+fmt.Sprintf("?interval=1m&from=%d", cs[2].T), ""); c != 200 || bytes.Count(b, []byte(`"t":`)) != 1 {
		t.Fatalf("candles from %d %s", c, b)
	}
	for _, q := range []string{"?interval=2m", "", "?interval=1m&from=x", "?interval=1m&from=10&to=5"} {
		if c, _, _ := call(t, "GET", u+"/candles/"+mk[0]+q, ""); c != 400 {
			t.Errorf("candles %s: %d", q, c)
		}
	}
	if c, _, b := call(t, "GET", u+"/rates", ""); c != 200 || !bytes.Contains(b, []byte(`"venue":"aave"`)) || !bytes.Contains(b, []byte(`"aprShort7d":"0.2"`)) {
		t.Fatalf("rates %d %s", c, b)
	}

	must(t, rdb.Del(ctx, streams.HeartbeatKey).Err())
	if c, v, _ := call(t, "GET", u+"/quote/"+mk[0]+"?side=buy&amount=3000000000", ""); c != 503 || v["error"] != "stale" {
		t.Fatalf("stale quote %d %v", c, v)
	}
	if c, v, _ := call(t, "POST", u+"/quote/basket", body); c != 503 || v["error"] != "stale" {
		t.Fatalf("stale basket %d %v", c, v)
	}
}

func TestWebSocket(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	rdb, db, mk := seed(t, ctx)
	srv := httptest.NewServer(New(ctx, rdb, db, []string{"http://allowed.test"}))
	defer srv.Close()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/v1/ws", nil)
	must(t, err)
	defer conn.CloseNow()
	type frame struct {
		Topic string          `json:"topic"`
		Data  json.RawMessage `json:"data"`
		Error string          `json:"error"`
	}
	read := func() frame {
		_, b, err := conn.Read(ctx)
		must(t, err)
		var f frame
		must(t, json.Unmarshal(b, &f))
		return f
	}
	send := func(s string) { must(t, conn.Write(ctx, websocket.MessageText, []byte(s))) }

	bookT, candT := "book."+mk[0], "candles."+mk[0]+".1m"
	must(t, streams.Publish(ctx, rdb, streams.Candles(mk[0], "1m"), streams.CandlesLatest(mk[0], "1m"), streams.Candle{Market: streams.Addr(mk[0]), Interval: "1m", T: 60000, N: 1}))
	send(fmt.Sprintf(`{"op":"sub","topics":[%q,%q,"rates","nav.%s"]}`, bookT, candT, mk[0]))
	got := map[string]bool{}
	for range 4 {
		f := read()
		got[f.Topic+f.Error] = true
	}
	if !got[bookT] || !got[candT] || !got["rates"] || !got["nav."+mk[0]+"bad_topic"] {
		t.Fatalf("snapshots %v", got)
	}

	must(t, streams.Publish(ctx, rdb, streams.Candles(mk[0], "1m"), streams.CandlesLatest(mk[0], "1m"), streams.Candle{Market: streams.Addr(mk[0]), Interval: "1m", T: 60000, N: 2}))
	f := read()
	var c streams.Candle
	must(t, json.Unmarshal(f.Data, &c))
	if f.Topic != candT || c.N != 2 {
		t.Fatalf("candle update %s %+v", f.Topic, c)
	}

	send(fmt.Sprintf(`{"op":"unsub","topics":[%q]}`, bookT))
	must(t, streams.Publish(ctx, rdb, streams.Book(mk[0]), streams.BookLatest(mk[0]), streams.BookSnapshot{Market: streams.Addr(mk[0]), Seq: 3}))
	must(t, streams.Publish(ctx, rdb, streams.Rates, streams.RatesLatest, streams.RatesSnapshot{Ts: 9}))
	if f := read(); f.Topic != "rates" { // book seq 3 must not arrive after unsub
		t.Fatalf("got %s after unsub", f.Topic)
	}
}
