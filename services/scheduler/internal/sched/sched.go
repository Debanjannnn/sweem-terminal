package sched

import (
	"context"
	"errors"
	"log/slog"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/streams"
)

type Scheduler struct {
	DB        *pgxpool.Pool
	RDB       *redis.Client
	Exec      Executor
	Quotes    Quotes
	BalanceOf func(ctx context.Context, basket, owner string) (*big.Int, error)
	Tick      time.Duration

	mu       sync.Mutex
	watching map[string]bool
}

// Run polls recurring schedules and keeps a NAV reader per stop-loss basket until ctx ends.
func (s *Scheduler) Run(ctx context.Context) {
	t := time.NewTicker(s.Tick)
	defer t.Stop()
	for {
		s.recurring(ctx, time.Now())
		s.watch(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Slot identifies a run; stable across instances so ON CONFLICT dedupes.
func Slot(next time.Time, interval int64) int64 { return next.Unix() / interval }

// NextRun is the first next+k*interval strictly after now (k≥1); missed slots are skipped, not backfilled.
func NextRun(next time.Time, interval int64, now time.Time) time.Time {
	iv := time.Duration(interval) * time.Second
	k := int64(1)
	if now.After(next) {
		k = int64(now.Sub(next)/iv) + 1
	}
	return next.Add(time.Duration(k) * iv)
}

// Triggered: stale or heartbeat-less NAV never fires.
func Triggered(p streams.NavPoint, alive bool, stop streams.Dec) bool {
	return alive && !p.Stale && p.NavPerShare.Sign() > 0 && p.NavPerShare.LessThanOrEqual(stop)
}

// due.amount is raw USDC base units (6dp); schedules store human USDC.
type due struct {
	id, basket, amount string
	interval           int64
	next               time.Time
	slippage           int
}

func (s *Scheduler) recurring(ctx context.Context, now time.Time) {
	rows, err := s.DB.Query(ctx, `select s.id, s.basket, trunc(s.amount_usdc * 1000000)::text, s.interval_seconds, s.next_run_at, s.max_slippage_bps
		from schedules s
		where s.kind = 'recurring' and s.active and s.next_run_at <= $1
		  and exists (select 1 from delegations d where d.user_id = s.user_id and d.active)`, now)
	if err != nil {
		slog.Error("due schedules", "err", err)
		return
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (d due, err error) {
		return d, r.Scan(&d.id, &d.basket, &d.amount, &d.interval, &d.next, &d.slippage)
	})
	if err != nil {
		slog.Error("due schedules", "err", err)
		return
	}
	for _, d := range list {
		s.fireRecurring(ctx, d, now)
	}
}

func (s *Scheduler) fireRecurring(ctx context.Context, d due, now time.Time) {
	log := slog.With("schedule", d.id)
	minOut, err := s.Quotes.MinOut(ctx, d.basket, "mint", d.amount, d.slippage)
	if err != nil {
		log.Warn("skip tick", "err", err)
		return
	}
	id, err := s.insert(ctx, func(tx pgx.Tx) (int64, error) {
		tag, err := tx.Exec(ctx, `update schedules set next_run_at = $2 where id = $1 and active and next_run_at = $3`,
			d.id, NextRun(d.next, d.interval, now), d.next)
		return tag.RowsAffected(), err
	}, d.id, Slot(d.next, d.interval), "mint", d.amount, minOut)
	if err != nil || id == "" {
		if err != nil {
			log.Error("insert execution", "err", err)
		}
		return
	}
	s.call(ctx, "mint", id)
}

// insert runs claim (a CAS on the schedule row) then the idempotent execution insert in one tx.
// Returns the new execution id, or "" when another instance got there first.
func (s *Scheduler) insert(ctx context.Context, claim func(pgx.Tx) (int64, error), scheduleID string, slot int64, kind, amount, minOut string) (string, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if n, err := claim(tx); err != nil || n == 0 {
		return "", err
	}
	var id string
	err = tx.QueryRow(ctx, `insert into executions (schedule_id, slot, kind, status, amount, min_out)
		values ($1, $2, $3, 'pending', $4::numeric, $5::numeric)
		on conflict (schedule_id, slot) do nothing returning id`, scheduleID, slot, kind, amount, minOut).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		id, err = "", nil
	}
	if err != nil {
		return "", err
	}
	return id, tx.Commit(ctx)
}

// call logs executor errors only; executor owns status, retries come from the next slot.
func (s *Scheduler) call(ctx context.Context, op, id string) {
	res, err := s.Exec.Call(ctx, op, id)
	if err != nil {
		slog.Error("executor", "op", op, "execution", id, "err", err)
		return
	}
	slog.Info("executor", "op", op, "execution", id, "status", res.Status, "tx", res.TxHash)
}

func (s *Scheduler) watch(ctx context.Context) {
	rows, err := s.DB.Query(ctx, `select distinct lower(basket) from schedules where kind = 'stop_loss' and active`)
	if err != nil {
		slog.Error("stop-loss baskets", "err", err)
		return
	}
	baskets, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		slog.Error("stop-loss baskets", "err", err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.watching == nil {
		s.watching = map[string]bool{}
	}
	// ponytail: readers for baskets whose stop-losses all ended keep running; cheap until basket count grows.
	for _, b := range baskets {
		if s.watching[b] {
			continue
		}
		s.watching[b] = true
		go func() {
			r := streams.Reader[streams.NavPoint]{RDB: s.RDB, Stream: streams.Nav(b), Latest: streams.NavLatest(b)}
			err := r.Run(ctx, func(p streams.NavPoint) { s.onNav(ctx, b, p) })
			if ctx.Err() == nil {
				slog.Error("nav reader", "basket", b, "err", err)
			}
			s.mu.Lock()
			delete(s.watching, b) // restarted on next tick
			s.mu.Unlock()
		}()
	}
}

func (s *Scheduler) onNav(ctx context.Context, basket string, p streams.NavPoint) {
	if p.Stale {
		return
	}
	alive, err := streams.Alive(ctx, s.RDB)
	if err != nil || !alive {
		return
	}
	rows, err := s.DB.Query(ctx, `select s.id, s.redeem_bps, s.max_slippage_bps, d.wallet_address, s.stop_nav_per_share::text
		from schedules s join delegations d on d.user_id = s.user_id and d.active
		where s.kind = 'stop_loss' and s.active and lower(s.basket) = $1`, basket)
	if err != nil {
		slog.Error("stop-loss schedules", "err", err)
		return
	}
	type hit struct {
		id, wallet    string
		bps, slippage int64
		stop          streams.Dec
	}
	hits, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (h hit, err error) {
		var stop string
		if err := r.Scan(&h.id, &h.bps, &h.slippage, &h.wallet, &stop); err != nil {
			return h, err
		}
		h.stop, err = decimal.NewFromString(stop)
		return h, err
	})
	if err != nil {
		slog.Error("stop-loss schedules", "err", err)
		return
	}
	for _, h := range hits {
		if !Triggered(p, alive, h.stop) {
			continue
		}
		s.fireStopLoss(ctx, basket, h.id, strings.ToLower(h.wallet), h.bps, int(h.slippage))
	}
}

func (s *Scheduler) fireStopLoss(ctx context.Context, basket, scheduleID, wallet string, bps int64, slippage int) {
	log := slog.With("schedule", scheduleID)
	bal, err := s.BalanceOf(ctx, basket, wallet)
	if err != nil {
		log.Error("balanceOf", "err", err)
		return
	}
	shares := new(big.Int).Div(new(big.Int).Mul(bal, big.NewInt(bps)), big.NewInt(10000))
	if shares.Sign() == 0 {
		log.Warn("stop-loss: no shares to redeem")
		return
	}
	minOut, err := s.Quotes.MinOut(ctx, basket, "redeem", shares.String(), slippage)
	if err != nil {
		log.Warn("skip stop-loss", "err", err)
		return
	}
	id, err := s.insert(ctx, func(tx pgx.Tx) (int64, error) {
		tag, err := tx.Exec(ctx, `update schedules set active = false where id = $1 and active`, scheduleID)
		return tag.RowsAffected(), err
	}, scheduleID, 0, "redeem", shares.String(), minOut)
	if err != nil || id == "" {
		if err != nil {
			log.Error("insert execution", "err", err)
		}
		return
	}
	s.call(ctx, "redeem", id)
}
