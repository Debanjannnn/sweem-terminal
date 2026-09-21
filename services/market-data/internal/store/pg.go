package store

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/config"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/candle"
)

// TickRetention is how far back ticks are kept; candles outlive them.
const TickRetention = 48 * time.Hour

type PG struct{ db *sql.DB }

func NewPG() (*PG, error) {
	dsn := config.GetEnv("DATABASE_URL", "postgres://sweem:sweem@localhost:5432/sweem?sslmode=disable")
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	db.SetMaxOpenConns(config.GetEnvInt("PG_MAX_CONNS", 10))
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(30 * time.Minute)
	return &PG{db: db}, nil
}

func (p *PG) Ping(ctx context.Context) error { return p.db.PingContext(ctx) }

func (p *PG) Close() error { return p.db.Close() }

// InsertTicks is idempotent on (market_id, ts, venue): a replayed poll is a
// no-op rather than a duplicate print.
func (p *PG) InsertTicks(ctx context.Context, ts []candle.Tick) error {
	if len(ts) == 0 {
		return nil
	}
	var sb strings.Builder
	sb.WriteString("insert into ticks (market_id, ts, venue, price, size, side) values ")
	args := make([]any, 0, len(ts)*6)
	for i, t := range ts {
		if i > 0 {
			sb.WriteByte(',')
		}
		n := i * 6
		sb.WriteString("($" + strconv.Itoa(n+1) + ",$" + strconv.Itoa(n+2) + ",$" + strconv.Itoa(n+3) +
			",$" + strconv.Itoa(n+4) + ",$" + strconv.Itoa(n+5) + ",$" + strconv.Itoa(n+6) + ")")
		args = append(args, t.MarketID, t.Ts, t.Venue, t.Price, zeroIfEmpty(t.Size), t.Side)
	}
	sb.WriteString(" on conflict (market_id, ts, venue) do nothing")
	_, err := p.db.ExecContext(ctx, sb.String(), args...)
	if err != nil {
		return fmt.Errorf("insert %d ticks: %w", len(ts), err)
	}
	return nil
}

func (p *PG) UpsertCandles(ctx context.Context, cs []candle.Candle) error {
	if len(cs) == 0 {
		return nil
	}
	var sb strings.Builder
	sb.WriteString("insert into candles (market_id, resolution, open_time, o, h, l, c, v) values ")
	args := make([]any, 0, len(cs)*8)
	for i, c := range cs {
		if i > 0 {
			sb.WriteByte(',')
		}
		n := i * 8
		sb.WriteByte('(')
		for j := range 8 {
			if j > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString("$" + strconv.Itoa(n+j+1))
		}
		sb.WriteByte(')')
		args = append(args, c.MarketID, c.Resolution, c.OpenTime,
			zeroIfEmpty(c.Open), zeroIfEmpty(c.High), zeroIfEmpty(c.Low), zeroIfEmpty(c.Close), zeroIfEmpty(c.Volume))
	}
	sb.WriteString(` on conflict (market_id, resolution, open_time) do update set
	  h = greatest(candles.h, excluded.h),
	  l = least(candles.l, excluded.l),
	  c = excluded.c,
	  v = greatest(candles.v, excluded.v)`)
	if _, err := p.db.ExecContext(ctx, sb.String(), args...); err != nil {
		return fmt.Errorf("upsert %d candles: %w", len(cs), err)
	}
	return nil
}

// Candles returns [from, to] by open time, oldest first.
func (p *PG) Candles(ctx context.Context, marketID string, resolution int, from, to int64, limit int) ([]candle.Candle, error) {
	rows, err := p.db.QueryContext(ctx,
		`select market_id, resolution, open_time, o::text, h::text, l::text, c::text, v::text
		   from candles
		  where market_id = $1 and resolution = $2 and open_time between $3 and $4
		  order by open_time asc limit $5`,
		strings.ToLower(marketID), resolution, from, to, limit)
	if err != nil {
		return nil, fmt.Errorf("query candles: %w", err)
	}
	defer rows.Close()
	out := []candle.Candle{}
	for rows.Next() {
		var c candle.Candle
		if err := rows.Scan(&c.MarketID, &c.Resolution, &c.OpenTime, &c.Open, &c.High, &c.Low, &c.Close, &c.Volume); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (p *PG) PruneTicks(ctx context.Context, retention time.Duration) (int64, error) {
	cutoff := time.Now().Add(-retention).UnixMilli()
	res, err := p.db.ExecContext(ctx, `delete from ticks where ts < $1`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("prune ticks: %w", err)
	}
	return res.RowsAffected()
}

func zeroIfEmpty(s string) string {
	if s == "" {
		return "0"
	}
	return s
}
