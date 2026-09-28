//go:build integration

package market

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/streams"
)

// Live gateway → real Redis → streams.Reader, for cbBTC/USDC (two-sided on testnet).
func TestLiveGatewayToRedis(t *testing.T) {
	url := os.Getenv("REDIS_URL")
	if url == "" {
		url = "redis://localhost:6380"
	}
	opt, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(opt)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	// same shape registry.market produces: checksummed address
	m := streams.MarketInfo{Market: "0x5BDEA6F9F9abA34F4EcB9B865646A792b835ef7f", PricePrecision: decimal.NewFromInt(100), SizePrecision: decimal.NewFromInt(100000000)}
	f := &Feed{WSURL: "wss://gateway.testnet.kuru.io/ws", APIURL: "https://api.testnet.kuru.io", RDB: rdb,
		Markets: func() []streams.MarketInfo { return []streams.MarketInfo{m} }, DeviationBps: 500, Tau: time.Minute}
	go f.Run(ctx)

	got := make(chan streams.BookSnapshot, 1)
	rctx, rcancel := context.WithCancel(ctx)
	defer rcancel()
	go streams.Reader[streams.BookSnapshot]{RDB: rdb, Stream: streams.Book(string(m.Market)), Latest: streams.BookLatest(string(m.Market)),
		Seq: func(b streams.BookSnapshot) uint64 { return b.Seq }}.Run(rctx, func(b streams.BookSnapshot) {
		if b.Ts > time.Now().Add(-10*time.Second).UnixMilli() {
			select {
			case got <- b:
			default:
			}
		}
	})
	select {
	case b := <-got:
		if b.Market != "0x5bdea6f9f9aba34f4ecb9b865646a792b835ef7f" || len(b.Bids)+len(b.Asks) == 0 || len(b.Bids) > 20 || len(b.Asks) > 20 {
			t.Fatalf("%+v", b)
		}
		if len(b.Bids) > 0 && len(b.Asks) > 0 && !b.Bids[0][0].LessThan(b.Asks[0][0]) {
			t.Fatalf("crossed book %v %v", b.Bids[0], b.Asks[0])
		}
		if !f.Connected() {
			t.Fatal("not connected")
		}
	case <-ctx.Done():
		t.Fatal("no fresh book snapshot")
	}
	for ctx.Err() == nil {
		r, err := streams.Get[streams.RefPrice](ctx, rdb, streams.Ref(string(m.Market)))
		if err == nil && r.Ts > time.Now().Add(-5*time.Second).UnixMilli() {
			if !r.Ref.IsPositive() {
				t.Fatalf("%+v", r)
			}
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("no fresh ref")
}
