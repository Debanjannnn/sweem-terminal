// Package store is the hot cache (Redis) and the durable series (Postgres).
// It also owns the venue-neutral Market shape both venues normalize into.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/redisclient"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/book"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/candle"
)

const (
	VenueKuru  = "kuru"
	VenuePerpl = "perpl"

	// ponytail: flat 5m TTL on every hot key; shorten per key kind if a stale
	// book after an ingest outage ever reads as live.
	hotTTL = 5 * time.Minute
	// fan-out channel prefix; the api mirrors these straight onto the browser hub
	chanPrefix = "md."
)

// Market is the same shape for a Kuru spot market and a Perpl perp. Prices and
// sizes on the wire are WAD strings; the decimals describe the venue's own
// scaled integers so a caller can round-trip an order size.
type Market struct {
	ID                 string `json:"id"`
	Venue              string `json:"venue"`
	Kind               string `json:"kind"`
	Symbol             string `json:"symbol"`
	Base               string `json:"base"`
	Quote              string `json:"quote"`
	BaseDecimals       int    `json:"baseDecimals"`
	QuoteDecimals      int    `json:"quoteDecimals"`
	PriceDecimals      int    `json:"priceDecimals"`
	SizeDecimals       int    `json:"sizeDecimals"`
	TickSize           string `json:"tickSize"`
	MinSize            string `json:"minSize"`
	MinIsQuoteNotional bool   `json:"minIsQuoteNotional"`
	TakerFeeMicros     int    `json:"takerFeeMicros"`
	MakerFeeMicros     int    `json:"makerFeeMicros"`
	Status             string `json:"status"`
	Address            string `json:"address"`
	Mark               string `json:"mark,omitempty"`
	Last               string `json:"last,omitempty"`
	// perps only: the venue's max leverage in hundredths, 1500 = 15x
	MaxLeverage int `json:"maxLeverage,omitempty"`
}

type Redis struct{ c *redis.Client }

func NewRedis() (*Redis, error) {
	c, err := redisclient.New()
	if err != nil {
		return nil, err
	}
	return &Redis{c: c}, nil
}

func (r *Redis) Ping(ctx context.Context) error { return r.c.Ping(ctx).Err() }

func (r *Redis) Close() error { return r.c.Close() }

func marketsKey(venue string) string { return "md:markets:" + venue }
func bookKey(venue, id string) string {
	return "md:book:" + venue + ":" + strings.ToLower(id)
}
func tickKey(venue, id string) string {
	return "md:tick:" + venue + ":" + strings.ToLower(id)
}

func (r *Redis) PutMarkets(ctx context.Context, venue string, ms []Market) error {
	return r.put(ctx, marketsKey(venue), ms)
}

func (r *Redis) Markets(ctx context.Context, venue string) ([]Market, error) {
	var out []Market
	err := r.get(ctx, marketsKey(venue), &out)
	return out, err
}

func (r *Redis) PutBook(ctx context.Context, b book.Book) error {
	return r.put(ctx, bookKey(b.Venue, b.MarketID), b)
}

func (r *Redis) Book(ctx context.Context, venue, id string) (book.Book, error) {
	var b book.Book
	err := r.get(ctx, bookKey(venue, id), &b)
	return b, err
}

func (r *Redis) PutTick(ctx context.Context, t candle.Tick) error {
	return r.put(ctx, tickKey(t.Venue, t.MarketID), t)
}

func (r *Redis) Tick(ctx context.Context, venue, id string) (candle.Tick, error) {
	var t candle.Tick
	err := r.get(ctx, tickKey(venue, id), &t)
	return t, err
}

// ErrMiss separates "nothing cached yet" from a broken Redis so a handler can
// answer 404 instead of 500.
var ErrMiss = errors.New("nothing cached under that key yet")

func (r *Redis) put(ctx context.Context, key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return r.c.Set(ctx, key, b, hotTTL).Err()
}

func (r *Redis) get(ctx context.Context, key string, out any) error {
	b, err := r.c.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return ErrMiss
	}
	if err != nil {
		return fmt.Errorf("read %s from redis: %w", key, err)
	}
	return json.Unmarshal(b, out)
}

// Publish pushes a WS topic payload to every api replica.
func (r *Redis) Publish(ctx context.Context, topic string, payload []byte) error {
	return r.c.Publish(ctx, chanPrefix+topic, payload).Err()
}

// Fanout delivers every published topic until ctx is cancelled.
func (r *Redis) Fanout(ctx context.Context, fn func(topic string, payload []byte)) error {
	sub := r.c.PSubscribe(ctx, chanPrefix+"*")
	go func() {
		<-ctx.Done()
		sub.Close()
	}()
	ch := sub.Channel()
	for msg := range ch {
		fn(strings.TrimPrefix(msg.Channel, chanPrefix), []byte(msg.Payload))
	}
	return ctx.Err()
}
