package market

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/snehendu098/sweem-terminal/internal/streams"
)

// Intervals are the candle sizes built from live trades; buckets are aligned to the unix epoch (1d = UTC day).
var Intervals = []struct {
	Name string
	D    time.Duration
}{{"1m", time.Minute}, {"5m", 5 * time.Minute}, {"1h", time.Hour}, {"1d", 24 * time.Hour}}

// Candles aggregates trades into OHLCV, upserts Postgres `candles` and publishes candles:{market}:{interval}.
type Candles struct {
	PG  *pgxpool.Pool
	RDB redis.Cmdable
	cur map[string]*streams.Candle // market|interval → open candle
}

// apply folds t into c, starting a new candle when t falls in a later bucket. Older trades are ignored.
func apply(c *streams.Candle, t streams.Trade, interval string, d time.Duration) *streams.Candle {
	b := t.Ts - t.Ts%d.Milliseconds()
	if c == nil || b > c.T {
		return &streams.Candle{Market: t.Market, Interval: interval, T: b, O: t.Px, H: t.Px, L: t.Px, C: t.Px, V: t.Sz, Q: t.Px.Mul(t.Sz), N: 1}
	}
	if b < c.T {
		return c
	}
	if t.Px.GreaterThan(c.H) {
		c.H = t.Px
	}
	if t.Px.LessThan(c.L) {
		c.L = t.Px
	}
	c.C, c.V, c.Q, c.N = t.Px, c.V.Add(t.Sz), c.Q.Add(t.Px.Mul(t.Sz)), c.N+1
	return c
}

// Add folds one live trade into every interval. A candle opened before a restart is reloaded from Postgres first.
// ponytail: a trade replayed by the socket after a reconnect is counted twice; dedupe by tx+index if Kuru ever replays.
func (cs *Candles) Add(ctx context.Context, t streams.Trade) {
	if cs.cur == nil {
		cs.cur = map[string]*streams.Candle{}
	}
	m := strings.ToLower(string(t.Market))
	for _, iv := range Intervals {
		k := m + "|" + iv.Name
		c := cs.cur[k]
		if b := t.Ts - t.Ts%iv.D.Milliseconds(); c == nil || c.T < b {
			if prev, err := cs.load(ctx, m, iv.Name, b); err == nil {
				c = prev
			} else if !errors.Is(err, pgx.ErrNoRows) {
				slog.Warn("candle load", "market", m, "interval", iv.Name, "err", err)
			}
		}
		c = apply(c, t, iv.Name, iv.D)
		cs.cur[k] = c
		if err := cs.save(ctx, c); err != nil {
			slog.Error("candle save", "market", m, "interval", iv.Name, "err", err)
		}
		if err := streams.Publish(ctx, cs.RDB, streams.Candles(m, iv.Name), streams.CandlesLatest(m, iv.Name), c); err != nil {
			slog.Error("candle publish", "market", m, "err", err)
		}
	}
}

func (cs *Candles) load(ctx context.Context, market, interval string, t int64) (*streams.Candle, error) {
	c := &streams.Candle{Market: streams.Addr(market), Interval: interval, T: t}
	err := cs.PG.QueryRow(ctx, `select o, h, l, c, v, q, n from candles where market = $1 and interval = $2 and t = $3`,
		market, interval, time.UnixMilli(t)).Scan(&c.O, &c.H, &c.L, &c.C, &c.V, &c.Q, &c.N)
	return c, err
}

func (cs *Candles) save(ctx context.Context, c *streams.Candle) error {
	_, err := cs.PG.Exec(ctx, `insert into candles (market, interval, t, o, h, l, c, v, q, n) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		on conflict (market, interval, t) do update set o = excluded.o, h = excluded.h, l = excluded.l, c = excluded.c, v = excluded.v, q = excluded.q, n = excluded.n`,
		strings.ToLower(string(c.Market)), c.Interval, time.UnixMilli(c.T), c.O, c.H, c.L, c.C, c.V, c.Q, c.N)
	return err
}
