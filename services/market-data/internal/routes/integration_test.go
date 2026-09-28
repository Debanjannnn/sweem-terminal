//go:build integration

package routes

import (
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
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/streams"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func randAddr() string {
	return "0x" + strings.Repeat("0", 8) + strings.ReplaceAll(uuid.NewString(), "-", "")
}

// Seeds DB 15 so registry keys never clobber a dev publisher's DB 0.
func seed(t *testing.T, ctx context.Context) (*redis.Client, string, []string) {
	url := os.Getenv("REDIS_URL")
	if url == "" {
		url = "redis://localhost:6380"
	}
	opt, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	opt.DB = 15
	rdb := redis.NewClient(opt)
	basket, mk := randAddr(), []string{randAddr(), randAddr(), randAddr()}
	pp, sp, bd := []string{"1000000", "100", "100"}, []string{"100000000", "10000000000", "100000000"}, []int{18, 18, 8}
	bids, asks, szs := []string{"0.024975", "2747.25", "84065.85"}, []string{"0.025025", "2752.75", "84234.15"}, []string{"1000000", "100", "5"}
	var infos []streams.MarketInfo
	var comps []streams.Component
	for i, m := range mk {
		infos = append(infos, streams.MarketInfo{Market: streams.Addr(m), BaseDecimals: bd[i], QuoteDecimals: 6, PricePrecision: d(pp[i]), SizePrecision: d(sp[i]), TakerFeePps: d("7000")})
		comps = append(comps, streams.Component{Market: streams.Addr(m), WeightBps: []int{4000, 3000, 3000}[i]})
		must(t, streams.Publish(ctx, rdb, streams.Book(m), streams.BookLatest(m), streams.BookSnapshot{Market: streams.Addr(m), Seq: 1,
			Bids: []streams.Level{{d(bids[i]), d(szs[i])}}, Asks: []streams.Level{{d(asks[i]), d(szs[i])}}}))
	}
	must(t, streams.Publish(ctx, rdb, "", streams.RegistryMarkets, infos))
	must(t, streams.Publish(ctx, rdb, "", streams.RegistryBaskets, []streams.BasketInfo{{Address: streams.Addr(basket), Components: comps}}))
	must(t, streams.Publish(ctx, rdb, streams.Nav(basket), streams.NavLatest(basket), streams.NavPoint{Basket: streams.Addr(basket), Supply: decimal.Zero}))
	must(t, streams.Beat(ctx, rdb))
	t.Cleanup(func() {
		keys := []string{streams.Nav(basket), streams.NavLatest(basket)}
		for _, m := range mk {
			keys = append(keys, streams.Book(m), streams.BookLatest(m))
		}
		rdb.Del(context.Background(), keys...)
	})
	return rdb, basket, mk
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func get(t *testing.T, url string) (int, map[string]any) {
	t.Helper()
	res, err := http.Get(url)
	must(t, err)
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	var v map[string]any
	json.Unmarshal(b, &v)
	return res.StatusCode, v
}

func TestHTTPRoutes(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	rdb, basket, mk := seed(t, ctx)
	srv := httptest.NewServer(New(ctx, rdb))
	defer srv.Close()
	u := srv.URL + "/api/v1"

	res, err := http.Get(u + "/markets")
	must(t, err)
	var markets []streams.MarketInfo
	must(t, json.NewDecoder(res.Body).Decode(&markets))
	if res.StatusCode != 200 || len(markets) != 3 {
		t.Fatalf("markets %d %v", res.StatusCode, markets)
	}

	if c, v := get(t, u+"/baskets/0x"+strings.ToUpper(basket[2:])+"/nav"); c != 200 || v["stale"] != false || v["basket"] != basket {
		t.Fatalf("nav %d %v", c, v)
	}
	if c, v := get(t, u+"/books/"+mk[1]); c != 200 || v["seq"] != 1.0 || v["stale"] != false {
		t.Fatalf("book %d %v", c, v)
	}
	if c, _ := get(t, u+"/books/"+randAddr()); c != 404 {
		t.Fatalf("missing book %d", c)
	}
	if c, _ := get(t, u+"/books/0xnothex"); c != 400 {
		t.Fatalf("bad addr %d", c)
	}

	// scheduler's request shape: raw 6dp USDC in, raw 18dp shares out
	c, v := get(t, u+"/quote/"+basket+"?side=mint&amount=1000000000&slippageBps=50")
	if c != 200 || v["expectedOut"] != "999979999000000000000" || v["minOut"] != "994980099005000000000" || v["stale"] != false || len(v["legs"].([]any)) != 3 {
		t.Fatalf("quote %d %v", c, v)
	}
	for q, want := range map[string]int{
		"?side=buy&amount=1":                       400,
		"?side=mint&amount=1.5":                    400,
		"?side=mint&amount=-1":                     400,
		"?side=mint&amount=1000&slippageBps=10001": 400,
		"?side=redeem&amount=1":                    400, // supply 0
	} {
		if c, v := get(t, u+"/quote/"+basket+q); c != want {
			t.Errorf("%s: %d %v", q, c, v)
		}
	}
	if c, _ := get(t, u+"/quote/"+randAddr()+"?side=mint&amount=1000000"); c != 404 {
		t.Fatalf("unknown basket %d", c)
	}

	must(t, rdb.Del(ctx, streams.HeartbeatKey).Err())
	if c, v := get(t, u+"/quote/"+basket+"?side=mint&amount=1000000000"); c != 503 || v["error"] != "stale" {
		t.Fatalf("stale quote %d %v", c, v)
	}
	if _, v := get(t, u+"/baskets/"+basket+"/nav"); v["stale"] != true {
		t.Fatalf("nav not stale %v", v)
	}
}

func TestWebSocket(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	rdb, basket, mk := seed(t, ctx)
	srv := httptest.NewServer(New(ctx, rdb))
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

	bookT, navT := "book."+mk[0], "nav."+basket
	send(fmt.Sprintf(`{"op":"sub","topics":[%q,%q,"book.0xNOPE"]}`, bookT, navT))
	got := map[string]bool{}
	for range 3 {
		f := read()
		got[f.Topic+f.Error] = true
	}
	if !got[bookT] || !got[navT] || !got["book.0xNOPEbad_topic"] {
		t.Fatalf("snapshots %v", got)
	}

	must(t, streams.Publish(ctx, rdb, streams.Book(mk[0]), streams.BookLatest(mk[0]), streams.BookSnapshot{Market: streams.Addr(mk[0]), Seq: 2}))
	f := read()
	var b streams.BookSnapshot
	must(t, json.Unmarshal(f.Data, &b))
	if f.Topic != bookT || b.Seq != 2 {
		t.Fatalf("update %s %+v", f.Topic, b)
	}

	send(fmt.Sprintf(`{"op":"unsub","topics":[%q]}`, bookT))
	must(t, streams.Publish(ctx, rdb, streams.Book(mk[0]), streams.BookLatest(mk[0]), streams.BookSnapshot{Market: streams.Addr(mk[0]), Seq: 3}))
	must(t, streams.Publish(ctx, rdb, streams.Nav(basket), streams.NavLatest(basket), streams.NavPoint{Basket: streams.Addr(basket), Block: 7}))
	if f := read(); f.Topic != navT { // book seq 3 must not arrive after unsub
		t.Fatalf("got %s after unsub", f.Topic)
	}
}
