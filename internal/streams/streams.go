// Package streams is the Redis wire contract from architecture §4. Publisher is the sole writer.
package streams

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
)

const (
	RegistryMarkets = "registry:markets"
	RegistryBaskets = "registry:baskets"
	HeartbeatKey    = "publisher:heartbeat"
	MaxLen          = 1000
	field           = "data"
)

func Book(market string) string       { return "book:" + strings.ToLower(market) }
func BookLatest(market string) string { return Book(market) + ":latest" }
func Trades(market string) string     { return "trades:" + strings.ToLower(market) }
func Ref(market string) string        { return "ref:" + strings.ToLower(market) }
func Nav(basket string) string        { return "nav:" + strings.ToLower(basket) }
func NavLatest(basket string) string  { return Nav(basket) + ":latest" }

// Addr is an address that is always lowercase on the wire.
type Addr string

func (a Addr) MarshalJSON() ([]byte, error) { return json.Marshal(strings.ToLower(string(a))) }

func (a *Addr) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	*a = Addr(strings.ToLower(s))
	return nil
}

// decimal.Decimal marshals as a quoted string, which is the wire rule.
type Dec = decimal.Decimal

type MarketInfo struct {
	Market         Addr `json:"market"`
	Base           Addr `json:"base"`
	Quote          Addr `json:"quote"`
	BaseDecimals   int  `json:"baseDecimals"`
	QuoteDecimals  int  `json:"quoteDecimals"`
	PricePrecision Dec  `json:"pricePrecision"`
	SizePrecision  Dec  `json:"sizePrecision"`
	TickSize       Dec  `json:"tickSize"`
	MinSize        Dec  `json:"minSize"`
	TakerFeePps    Dec  `json:"takerFeePps"`
}

type Component struct {
	Market    Addr `json:"market"`
	Token     Addr `json:"token"`
	WeightBps int  `json:"weightBps"`
}

type BasketInfo struct {
	Address    Addr        `json:"address"`
	Name       string      `json:"name"`
	Symbol     string      `json:"symbol"`
	Components []Component `json:"components"`
}

// Level is [price, size].
type Level [2]Dec

type BookSnapshot struct {
	Market Addr    `json:"market"`
	Seq    uint64  `json:"seq"`
	Ts     int64   `json:"ts"` // unix ms
	Bids   []Level `json:"bids"`
	Asks   []Level `json:"asks"`
}

type Trade struct {
	Market Addr   `json:"market"`
	Ts     int64  `json:"ts"`
	Px     Dec    `json:"px"`
	Sz     Dec    `json:"sz"`
	Side   string `json:"side"` // buy|sell (taker side)
}

type RefPrice struct {
	Market   Addr  `json:"market"`
	Mid      Dec   `json:"mid"`
	Ref      Dec   `json:"ref"`
	Vol      Dec   `json:"vol"`
	Ts       int64 `json:"ts"`
	Deviated bool  `json:"deviated"`
}

type NavComponent struct {
	Market  Addr `json:"market"`
	Token   Addr `json:"token"`
	Balance Dec  `json:"balance"`
	Mid     Dec  `json:"mid"`
	Value   Dec  `json:"value"`
}

type NavPoint struct {
	Basket      Addr           `json:"basket"`
	Block       uint64         `json:"block"`
	Ts          int64          `json:"ts"`
	Supply      Dec            `json:"supply"`
	TotalNav    Dec            `json:"totalNav"`
	NavPerShare Dec            `json:"navPerShare"`
	Stale       bool           `json:"stale"`
	Components  []NavComponent `json:"components"`
}

// Publish XADDs msg to stream (if set) and SETs latestKey (if set), atomically.
func Publish(ctx context.Context, rdb redis.Cmdable, stream, latestKey string, msg any) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = rdb.TxPipelined(ctx, func(p redis.Pipeliner) error {
		if stream != "" {
			p.XAdd(ctx, &redis.XAddArgs{Stream: stream, MaxLen: MaxLen, Approx: true, Values: map[string]any{field: b}})
		}
		if latestKey != "" {
			p.Set(ctx, latestKey, b, 0)
		}
		return nil
	})
	return err
}

// Beat marks the publisher alive for 5s.
func Beat(ctx context.Context, rdb redis.Cmdable) error {
	return rdb.Set(ctx, HeartbeatKey, strconv.FormatInt(time.Now().UnixMilli(), 10), 5*time.Second).Err()
}

// Alive reports whether the publisher heartbeat is present; false means data is stale.
func Alive(ctx context.Context, rdb redis.Cmdable) (bool, error) {
	n, err := rdb.Exists(ctx, HeartbeatKey).Result()
	return n == 1, err
}

// Get reads a JSON string key (latest snapshots, registries, ref). redis.Nil when absent.
func Get[T any](ctx context.Context, rdb redis.Cmdable, key string) (T, error) {
	var v T
	b, err := rdb.Get(ctx, key).Bytes()
	if err != nil {
		return v, err
	}
	return v, json.Unmarshal(b, &v)
}

// Reader follows the §4 protocol: latest snapshot, then stream tail, re-reading latest on a seq gap.
type Reader[T any] struct {
	RDB    *redis.Client
	Stream string
	Latest string         // empty: no snapshot (trades)
	Seq    func(T) uint64 // nil: no dedupe/gap check
	Block  time.Duration  // default 5s
}

// Run calls fn for every message until ctx ends. fn is never given a seq <= one already delivered.
func (r Reader[T]) Run(ctx context.Context, fn func(T)) error {
	block := r.Block
	if block == 0 {
		block = 5 * time.Second
	}
	// Start from the current tail id, not "$", so nothing published between GET and XREAD is lost.
	id := "0-0"
	tail, err := r.RDB.XRevRangeN(ctx, r.Stream, "+", "-", 1).Result()
	if err != nil {
		return err
	}
	if len(tail) == 1 {
		id = tail[0].ID
	}
	var last uint64
	have := false
	deliver := func(v T) {
		if r.Seq != nil {
			s := r.Seq(v)
			if have && s <= last {
				return
			}
			last, have = s, true
		}
		fn(v)
	}
	snapshot := func() error {
		if r.Latest == "" {
			return nil
		}
		v, err := Get[T](ctx, r.RDB, r.Latest)
		if errors.Is(err, redis.Nil) {
			return nil
		}
		if err != nil {
			return err
		}
		deliver(v)
		return nil
	}
	if err := snapshot(); err != nil {
		return err
	}
	for {
		res, err := r.RDB.XRead(ctx, &redis.XReadArgs{Streams: []string{r.Stream, id}, Block: block}).Result()
		if errors.Is(err, redis.Nil) {
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		for _, m := range res[0].Messages {
			id = m.ID
			s, _ := m.Values[field].(string)
			var v T
			if err := json.Unmarshal([]byte(s), &v); err != nil {
				return err
			}
			if r.Seq != nil && have && r.Seq(v) > last+1 {
				if err := snapshot(); err != nil {
					return err
				}
			}
			deliver(v)
		}
	}
}
