//go:build integration

package market

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/snehendu098/sweem-terminal/internal/streams"
)

// DB 15 so a running dev publisher's keys are never touched.
func testRedis(t *testing.T) *redis.Client {
	url := os.Getenv("REDIS_URL")
	if url == "" {
		url = "redis://localhost:6380"
	}
	opt, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	opt.DB = 15
	return redis.NewClient(opt)
}

// Live Kuru mainnet WS → real Redis → streams.Reader, for WETH_USDC.
func TestLiveKuruToRedis(t *testing.T) {
	rdb := testRedis(t)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	ms := []streams.MarketInfo{{Market: weth.Market, PricePrecision: weth.PricePrecision, SizePrecision: weth.SizePrecision}}
	if err := Resolve(ctx, "https://exchange.kuru.io", ms); err != nil || ms[0].Symbol != "WETH_USDC" {
		t.Fatal(err, ms[0].Symbol)
	}
	m := string(weth.Market)
	t.Cleanup(func() {
		rdb.Del(context.Background(), streams.Book(m), streams.BookLatest(m), streams.Ref(m), streams.Trades(m))
	})
	f := &Feed{WSURL: "wss://exchange.kuru.io/ws", APIURL: "https://exchange.kuru.io", RDB: rdb, Markets: ms, DeviationBps: 500, Tau: time.Minute}
	go f.Run(ctx)

	got := make(chan streams.BookSnapshot, 8)
	rctx, rcancel := context.WithCancel(ctx)
	defer rcancel()
	go streams.Reader[streams.BookSnapshot]{RDB: rdb, Stream: streams.Book(m), Latest: streams.BookLatest(m),
		Seq: func(b streams.BookSnapshot) uint64 { return b.Seq }}.Run(rctx, func(b streams.BookSnapshot) {
		if b.Ts > time.Now().Add(-10*time.Second).UnixMilli() {
			select {
			case got <- b:
			default:
			}
		}
	})
	for n := 0; n < 3; n++ { // REST snapshot + at least two WS depthUpdates
		select {
		case b := <-got:
			if b.Market != weth.Market || len(b.Bids) == 0 || len(b.Asks) == 0 || len(b.Bids) > 20 || len(b.Asks) > 20 {
				t.Fatalf("%+v", b)
			}
			if !b.Bids[0][0].LessThan(b.Asks[0][0]) || b.Asks[0][0].LessThan(d("100")) || b.Asks[0][0].GreaterThan(d("100000")) {
				t.Fatalf("implausible WETH top %v %v", b.Bids[0], b.Asks[0])
			}
		case <-ctx.Done():
			t.Fatal("no fresh book snapshot")
		}
	}
	if !f.Connected() {
		t.Fatal("not connected")
	}
	for ctx.Err() == nil {
		r, err := streams.Get[streams.RefPrice](ctx, rdb, streams.Ref(m))
		if err == nil && r.Ts > time.Now().Add(-5*time.Second).UnixMilli() && r.Ref.IsPositive() {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("no fresh ref")
}

// testPG is a pool on a throwaway schema holding migration 0007.
func testPG(t *testing.T) *pgxpool.Pool {
	ctx := context.Background()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://sweem:sweem@localhost:5433/sweem?sslmode=disable"
	}
	schema := "t_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, "create schema "+schema); err != nil {
		t.Fatal(err)
	}
	db, err := pgxpool.New(ctx, url+"&search_path="+schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		admin.Exec(context.Background(), "drop schema "+schema+" cascade")
		admin.Close()
	})
	sql, err := os.ReadFile("../../../../migrations/0007_candles.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, string(sql)); err != nil {
		t.Fatal(err)
	}
	return db
}

// A reconnect replaying already-seen trade frames must not reach trades or candles; identical fills in one session still count.
func TestTradeReplayDedupe(t *testing.T) {
	ctx := context.Background()
	db, rdb := testPG(t), testRedis(t)
	mk := "0x" + strings.Repeat("d", 40)
	blk := time.Now().UnixNano() // unique per run so the 1h dedupe keys never collide across runs
	frame := []byte(fmt.Sprintf(`{"e":"trade","E":%d,"T":1,"s":%q,"t":"42","p":"2000000000000000000000","q":"10000000000","b":%d,"m":false,"M":true}`, time.Now().UnixMilli(), mk, blk))
	t.Cleanup(func() {
		keys := []string{streams.Trades(mk)}
		for _, iv := range Intervals {
			keys = append(keys, streams.Candles(mk, iv.Name), streams.CandlesLatest(mk, iv.Name))
		}
		for _, k := range rdb.Keys(context.Background(), fmt.Sprintf("trade:seen:%s:%d:*", mk, blk)).Val() {
			keys = append(keys, k)
		}
		rdb.Del(context.Background(), keys...)
	})
	info := streams.MarketInfo{Market: streams.Addr(mk), PricePrecision: weth.PricePrecision, SizePrecision: weth.SizePrecision}
	f := &Feed{RDB: rdb, Candles: &Candles{PG: db, RDB: rdb}, state: map[string]*mstate{mk: {info: info}}}
	f.handle(ctx, frame)
	f.handle(ctx, frame)      // same fill twice in one session: two real fills
	f.seen = map[string]int{} // what session() does on reconnect
	f.handle(ctx, frame)
	f.handle(ctx, frame)
	f.handle(ctx, frame) // a third occurrence is new even after the replay
	if n := rdb.XLen(ctx, streams.Trades(mk)).Val(); n != 3 {
		t.Fatal("trades", n)
	}
	var n int
	var v string
	if err := db.QueryRow(ctx, `select n, v::text from candles where market=$1 and interval='1m'`, mk).Scan(&n, &v); err != nil || n != 3 || v != "3" {
		t.Fatal(err, n, v)
	}
}

// Candles upsert into real Postgres (throwaway schema) and survive a restart mid-bucket.
func TestCandlesPostgres(t *testing.T) {
	ctx := context.Background()
	db := testPG(t)
	rdb := testRedis(t)
	mk := "0x" + strings.Repeat("c", 40)
	t.Cleanup(func() {
		for _, iv := range Intervals {
			rdb.Del(context.Background(), streams.Candles(mk, iv.Name), streams.CandlesLatest(mk, iv.Name))
		}
	})
	base := time.Now().Truncate(time.Minute).UnixMilli()
	tr := func(ts int64, px, sz string) streams.Trade {
		return streams.Trade{Market: streams.Addr(mk), Ts: ts, Px: d(px), Sz: d(sz)}
	}
	(&Candles{PG: db, RDB: rdb}).Add(ctx, tr(base+1000, "10", "1"))
	(&Candles{PG: db, RDB: rdb}).Add(ctx, tr(base+2000, "8", "2")) // fresh instance = restart
	var o, h, l, c, v string
	var n int
	if err := db.QueryRow(ctx, `select o::text, h::text, l::text, c::text, v::text, n from candles where market=$1 and interval='1m'`, mk).Scan(&o, &h, &l, &c, &v, &n); err != nil {
		t.Fatal(err)
	}
	if o != "10" || h != "10" || l != "8" || c != "8" || v != "3" || n != 2 {
		t.Fatal(o, h, l, c, v, n)
	}
	var rows int
	db.QueryRow(ctx, `select count(*) from candles where market=$1`, mk).Scan(&rows)
	if rows != len(Intervals) {
		t.Fatal("rows", rows)
	}
	latest, err := streams.Get[streams.Candle](ctx, rdb, streams.CandlesLatest(mk, "5m"))
	if err != nil || latest.N != 2 || !latest.Q.Equal(d("26")) {
		t.Fatal(err, latest)
	}
}
