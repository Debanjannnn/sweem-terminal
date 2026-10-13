// Package sched decides when user automations fire (DCA, TP/SL, basket + strategy rebalance, Perpl guard); the executor signs.
package sched

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/config"
	"github.com/snehendu098/sweem-terminal/internal/planner"
	"github.com/snehendu098/sweem-terminal/internal/streams"
)

// Chain is the RPC reads the scheduler needs; RPC implements it, tests fake it.
type Chain interface {
	Wallet(ctx context.Context, wallet, sym string) (decimal.Decimal, error) // atoms, MON native
	Balances(ctx context.Context, wallet string, s planner.Strategy, legs []planner.LegState, peers []planner.Peer) ([]planner.Balance, error)
	Live(ctx context.Context, wallet string, s planner.Strategy, legs []planner.LegState, peers []planner.Peer) (planner.Live, error)
	Perp(ctx context.Context, wallet string, perpID int) (Perp, error)
}

type Scheduler struct {
	DB    *pgxpool.Pool
	RDB   *redis.Client
	P     *planner.Planner
	Exec  planner.Executor
	Chain Chain
	Dep   *config.Deployments143

	Tick, PositionEvery, MaxAge time.Duration   // MaxAge: older ref/rates are stale
	Slippage                    int             // strategy actions (schedules carry their own)
	GateBand, GuardBuffer       decimal.Decimal // APR hysteresis to re-enable basis; margin-fraction headroom over maintenance

	lastPositions time.Time
}

func (s *Scheduler) Run(ctx context.Context) {
	t := time.NewTicker(s.Tick)
	defer t.Stop()
	for {
		s.tick(ctx, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Scheduler) tick(ctx context.Context, now time.Time) {
	if ok, err := streams.Alive(ctx, s.RDB); err != nil || !ok {
		slog.Warn("publisher heartbeat missing, skipping tick", "err", err)
		return
	}
	s.schedules(ctx, now)
	if now.Sub(s.lastPositions) >= s.PositionEvery {
		s.lastPositions = now
		s.positions(ctx, now)
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

// Hit: take_profit fires at or above trigger, stop_loss at or below (never on a zero/unknown value).
func Hit(kind string, value, trigger decimal.Decimal) bool {
	if !value.IsPositive() || !trigger.IsPositive() {
		return false
	}
	if kind == "take_profit" {
		return value.GreaterThanOrEqual(trigger)
	}
	return value.LessThanOrEqual(trigger)
}

// Drift is the largest |actual − target| weight in bps; values are in one unit, targets sum to 10000.
func Drift(values []decimal.Decimal, target []int) int {
	total := decimal.Sum(decimal.Zero, values...)
	if !total.IsPositive() {
		return 0
	}
	max := 0
	for i, v := range values {
		w := int(v.Mul(decimal.NewFromInt(10_000)).Div(total).Round(0).IntPart()) - target[i]
		if w < 0 {
			w = -w
		}
		if w > max {
			max = w
		}
	}
	return max
}

type schedule struct {
	id, user, wallet, kind, targetType, targetID string
	amount, trigger                              decimal.Decimal
	interval                                     int64
	next                                         time.Time
	sellBps, driftBps, slip                      int
}

// exec is one execution row to insert after claim succeeds.
type exec struct {
	scheduleID, positionID, basketID *string
	slot                             int64
	kind, user                       string
	steps                            []planner.Step
	legs                             []planner.LegState // position legs after the run; applied on confirm (reconcile)
	settle                           bool               // full withdraw: position → closed when the run confirms
}

func (s *Scheduler) schedules(ctx context.Context, now time.Time) {
	rows, err := s.DB.Query(ctx, `select s.id, s.user_id, lower(d.wallet_address), s.kind, s.target_type, s.target_id,
		coalesce(s.amount_usdc, 0)::text, coalesce(s.trigger_price, 0)::text, coalesce(s.interval_seconds, 0),
		coalesce(s.next_run_at, 'epoch'), coalesce(s.sell_bps, 0), coalesce(s.drift_bps, 0), s.max_slippage_bps
		from schedules s join delegations d on d.user_id = s.user_id and d.active
		where s.active and s.target_type in ('market', 'basket', 'position') and s.kind in ('dca', 'take_profit', 'stop_loss', 'rebalance')`)
	if err != nil {
		slog.Error("schedules", "err", err)
		return
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (c schedule, err error) {
		var amt, trig string
		err = r.Scan(&c.id, &c.user, &c.wallet, &c.kind, &c.targetType, &c.targetID, &amt, &trig, &c.interval, &c.next, &c.sellBps, &c.driftBps, &c.slip)
		c.amount, c.trigger = decimal.RequireFromString(amt), decimal.RequireFromString(trig)
		return c, err
	})
	if err != nil {
		slog.Error("schedules", "err", err)
		return
	}
	for _, c := range list {
		if err := s.fire(ctx, c, now); err != nil {
			slog.Warn("schedule skipped", "schedule", c.id, "kind", c.kind, "err", err)
		}
	}
}

func (s *Scheduler) fire(ctx context.Context, c schedule, now time.Time) error {
	advance := func() []any {
		return []any{`update schedules set next_run_at = $2 where id = $1 and active and next_run_at = $3`, c.id, NextRun(c.next, c.interval, now), c.next}
	}
	if (c.kind == "dca" || c.kind == "rebalance") && c.interval <= 0 {
		return errors.New("interval_seconds missing")
	}
	e := exec{scheduleID: &c.id, kind: c.kind, user: c.user}
	if c.targetType == "basket" {
		e.basketID = &c.targetID
	}
	switch c.kind {
	case "dca":
		if c.next.After(now) {
			return nil
		}
		usdc := c.amount.Shift(6).Floor()
		var err error
		if c.targetType == "basket" {
			var legs []planner.Leg
			if legs, err = s.basketLegs(ctx, c.targetID); err == nil {
				e.steps, err = s.P.BasketBuy(ctx, legs, usdc, c.slip)
			}
		} else {
			e.steps, err = s.P.MarketBuy(ctx, c.targetID, usdc, c.slip)
		}
		if err != nil {
			return err
		}
		e.slot = Slot(c.next, c.interval)
		return s.submit(ctx, advance(), e)
	case "take_profit", "stop_loss":
		return s.trigger(ctx, c, e)
	case "rebalance":
		if c.targetType != "basket" || c.next.After(now) {
			return nil // position rebalance schedules only override driftBps, see positions
		}
		e.slot = Slot(c.next, c.interval)
		steps, err := s.basketRebalance(ctx, c)
		if len(steps) == 0 || err != nil {
			return errors.Join(err, s.claimOnly(ctx, advance())) // checked this slot either way
		}
		e.steps = steps
		return s.submit(ctx, advance(), e)
	}
	return nil
}

// trigger fires a one-shot TP/SL: value from Kuru ref prices, sell sellBps of the holding, deactivate.
func (s *Scheduler) trigger(ctx context.Context, c schedule, e exec) error {
	deactivate := []any{`update schedules set active = false where id = $1 and active`, c.id}
	switch c.targetType {
	case "market":
		px, err := s.price(ctx, c.targetID)
		if err != nil || !Hit(c.kind, px, c.trigger) {
			return err
		}
		bal, err := s.Chain.Wallet(ctx, c.wallet, s.Dep.Kuru.Markets[c.targetID].Base)
		if err != nil {
			return err
		}
		if s.Dep.Kuru.Markets[c.targetID].Base == "MON" {
			bal = bal.Sub(gasReserve) // native MON pays the executor's gas
		}
		amt := bpsOf(bal, c.sellBps)
		if !amt.IsPositive() {
			return errors.New("nothing to sell")
		}
		if e.steps, err = s.P.MarketSell(ctx, c.targetID, amt, c.slip); err != nil {
			return err
		}
	case "basket":
		legs, err := s.basketLegs(ctx, c.targetID)
		if err != nil {
			return err
		}
		held, px, err := s.basketHoldings(ctx, c.user, c.wallet, c.targetID, legs)
		if err != nil {
			return err
		}
		value := decimal.Sum(decimal.Zero, s.values(legs, held, px)...)
		if !Hit(c.kind, value, c.trigger) {
			return nil
		}
		var sell []planner.SellLeg
		for i, l := range legs {
			sell = append(sell, planner.SellLeg{Market: l.Market, Amount: bpsOf(held[i], c.sellBps)})
		}
		if e.steps, err = s.P.BasketSell(ctx, sell, c.slip); err != nil {
			return err
		}
	case "position":
		p, err := s.position(ctx, c.targetID)
		if err != nil {
			return err
		}
		bals, err := s.Chain.Balances(ctx, p.wallet, p.strat, p.legs, p.peers)
		if err != nil {
			return err
		}
		parts := p.strat.Parts()
		bs, err := planner.Split(parts, bals)
		if err != nil {
			return err
		}
		value := decimal.Zero // USDC: Σ sleeve value × its price
		for k, pt := range parts {
			px, v := decimal.NewFromInt(1), decimal.Zero
			if pt.Asset != "USDC" {
				if px, err = s.price(ctx, pt.Asset); err != nil {
					return err
				}
			}
			for _, b := range bs[k] {
				v = v.Add(decimal.RequireFromString(b.Value))
			}
			value = value.Add(v.Shift(-s.decimals(pt.Asset)).Mul(px))
		}
		if !Hit(c.kind, value, c.trigger) {
			return nil
		}
		e.positionID = &p.id
		inflight := `not exists (select 1 from executions e where e.position_id = $2 and (e.status in ('pending', 'submitted') or e.legs is not null))`
		if c.sellBps < 10_000 {
			plan, err := s.P.WithdrawBps(ctx, p.strat, p.legs, bals, c.sellBps, c.slip)
			if err != nil {
				return err
			}
			e.steps, e.legs = plan.Steps, plan.Legs
			deactivate = []any{`with d as (update schedules set active = false where id = $1 and active returning 1)
				update strategy_positions set status = status where id = $2 and status = 'open' and exists (select 1 from d) and ` + inflight, c.id, p.id}
			break
		}
		// full close = api's resumable withdraw: open → withdrawing → closed on confirm; a failure stays withdrawing for the user to resume
		live, err := s.Chain.Live(ctx, p.wallet, p.strat, p.legs, p.peers)
		if err != nil {
			return err
		}
		plan, err := s.P.Withdraw(ctx, p.strat, p.legs, live.Bals, c.slip)
		if err != nil {
			return err
		}
		if e.steps, err = s.P.ResumeWithdraw(ctx, p.strat, plan.Steps, 0, live, c.slip); err != nil || len(e.steps) == 0 {
			return errors.Join(err, errors.New("nothing left to withdraw"))
		}
		e.kind, e.settle = "withdraw", true
		deactivate = []any{`with d as (update schedules set active = false where id = $1 and active returning 1)
			update strategy_positions set status = 'withdrawing' where id = $2 and status = 'open' and exists (select 1 from d) and ` + inflight, c.id, p.id}
	}
	return s.submit(ctx, deactivate, e)
}

var gasReserve = decimal.New(2, 18)

func bpsOf(x decimal.Decimal, bps int) decimal.Decimal {
	return x.Mul(decimal.NewFromInt(int64(bps))).Div(decimal.NewFromInt(10_000)).Floor()
}

func (s *Scheduler) decimals(sym string) int32 { return int32(s.Dep.Tokens[sym].Decimals) }

// values converts per-leg base atoms to USDC (human).
func (s *Scheduler) values(legs []planner.Leg, held, px []decimal.Decimal) []decimal.Decimal {
	out := make([]decimal.Decimal, len(legs))
	for i, l := range legs {
		out[i] = held[i].Shift(-s.decimals(s.Dep.Kuru.Markets[l.Market].Base)).Mul(px[i])
	}
	return out
}

// basketRebalance sells over-weight legs and spends their guaranteed proceeds on under-weight legs; nil when inside the band.
func (s *Scheduler) basketRebalance(ctx context.Context, c schedule) ([]planner.Step, error) {
	legs, err := s.basketLegs(ctx, c.targetID)
	if err != nil {
		return nil, err
	}
	held, px, err := s.basketHoldings(ctx, c.user, c.wallet, c.targetID, legs)
	if err != nil {
		return nil, err
	}
	values := s.values(legs, held, px)
	weights := make([]int, len(legs))
	for i, l := range legs {
		weights[i] = l.WeightBps
	}
	if Drift(values, weights) <= c.driftBps {
		return nil, nil
	}
	total := decimal.Sum(decimal.Zero, values...)
	var sells []planner.SellLeg
	deficit := make([]decimal.Decimal, len(legs))
	for i, l := range legs {
		d := values[i].Sub(total.Mul(decimal.NewFromInt(int64(l.WeightBps))).Div(decimal.NewFromInt(10_000))) // USDC over (+) / under (−) target
		if d.IsPositive() {
			sells = append(sells, planner.SellLeg{Market: l.Market, Amount: held[i].Mul(d).Div(values[i]).Floor()})
		} else {
			deficit[i] = d.Neg()
		}
	}
	steps, err := s.P.BasketSell(ctx, sells, c.slip)
	if err != nil {
		return nil, err
	}
	proceeds := decimal.Zero
	for _, st := range steps {
		proceeds = proceeds.Add(decimal.RequireFromString(st.MinOut))
	}
	want := decimal.Sum(decimal.Zero, deficit...)
	for i, l := range legs {
		if !deficit[i].IsPositive() {
			continue
		}
		buy, err := s.P.MarketBuy(ctx, l.Market, proceeds.Mul(deficit[i]).Div(want).Floor(), c.slip)
		if err != nil {
			return nil, err
		}
		steps = append(steps, buy...)
	}
	return steps, nil
}

func (s *Scheduler) basketLegs(ctx context.Context, id string) ([]planner.Leg, error) {
	var legs []planner.Leg
	err := s.DB.QueryRow(ctx, `select legs from basket_defs where id::text = $1`, id).Scan(&legs)
	return legs, err
}

// basketHoldings: per leg, attributed base atoms (confirmed executions of this basket) capped at the wallet balance, and ref price.
func (s *Scheduler) basketHoldings(ctx context.Context, user, wallet, basketID string, legs []planner.Leg) (held, px []decimal.Decimal, err error) {
	rows, err := s.DB.Query(ctx, `select e.steps, e.tx_hashes, coalesce((select jsonb_object_agg(lower(t.tx_hash), t.sz) from (
			select tx_hash, sum(size)::text sz from user_trades where tx_hash = any(e.tx_hashes) and role = 'taker' and lower(wallet) = $3
			group by tx_hash) t), '{}')
		from executions e where e.user_id = $1 and e.basket_id::text = $2 and e.status = 'confirmed' and e.steps is not null`, user, basketID, wallet)
	if err != nil {
		return nil, nil, err
	}
	execs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (x planner.ExecFills, err error) {
		return x, r.Scan(&x.Steps, &x.TxHashes, &x.Fills)
	})
	if err != nil {
		return nil, nil, err
	}
	attr := s.P.AttributeFills(execs)
	for _, l := range legs {
		p, err := s.price(ctx, l.Market)
		if err != nil {
			return nil, nil, err
		}
		h := attr[l.Market].Amount
		if h.IsPositive() {
			bal, err := s.Chain.Wallet(ctx, wallet, s.Dep.Kuru.Markets[l.Market].Base)
			if err != nil {
				return nil, nil, err
			}
			h = decimal.Min(h, bal)
		}
		held, px = append(held, h), append(px, p)
	}
	return held, px, nil
}

// price is the Kuru reference price (USDC per unit) of market sym; missing or old ⇒ error, never act.
func (s *Scheduler) price(ctx context.Context, sym string) (decimal.Decimal, error) {
	m, ok := s.Dep.Kuru.Markets[sym]
	if !ok {
		return decimal.Zero, fmt.Errorf("unknown market %s", sym)
	}
	r, err := streams.Get[streams.RefPrice](ctx, s.RDB, streams.Ref(strings.ToLower(m.Orderbook)))
	if err != nil {
		return decimal.Zero, fmt.Errorf("ref %s: %w", sym, err)
	}
	if time.Since(time.UnixMilli(r.Ts)) > s.MaxAge || !r.Ref.IsPositive() {
		return decimal.Zero, fmt.Errorf("ref %s stale", sym)
	}
	return r.Ref, nil
}

func (s *Scheduler) claimOnly(ctx context.Context, claim []any) error {
	_, err := s.DB.Exec(ctx, claim[0].(string), claim[1:]...)
	return err
}

// submit runs claim (a CAS that must touch a row) and the execution insert in one tx, then hands the id to the executor.
// A lost claim or an existing (schedule_id, slot) row means another instance fired: no-op.
func (s *Scheduler) submit(ctx context.Context, claim []any, e exec) error {
	steps, err := json.Marshal(e.steps)
	if err != nil {
		return err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, claim[0].(string), claim[1:]...)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	var id string
	var legs []byte // nil ⇒ SQL null
	if e.legs != nil {
		legs, _ = json.Marshal(e.legs)
	}
	err = tx.QueryRow(ctx, `insert into executions (schedule_id, slot, kind, status, user_id, steps, basket_id, position_id, legs)
		values ($1, $2, $3, 'pending', $4, $5, $6, $7, $8) on conflict (schedule_id, slot) do nothing returning id`,
		e.scheduleID, e.slot, e.kind, e.user, steps, e.basketID, e.positionID, legs).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	go s.run(context.WithoutCancel(ctx), id, e)
	return nil
}

// run hands the execution to the executor; it owns status after that. Unreachable executor ⇒ failed, like api.
func (s *Scheduler) run(ctx context.Context, id string, e exec) {
	out, err := s.Exec.Run(ctx, id)
	if err != nil {
		slog.Error("executor", "execution", id, "kind", e.kind, "err", err)
		var pe *planner.Error
		if !errors.As(err, &pe) {
			s.DB.Exec(ctx, `update executions set status = 'failed', error = 'executor_unavailable', updated_at = now() where id = $1 and status = 'pending'`, id)
		}
		return
	}
	slog.Info("executor", "execution", id, "kind", e.kind, "status", out.Status, "steps", out.StepsDone)
	if e.settle && out.Status == "confirmed" {
		if _, err := s.DB.Exec(ctx, `update strategy_positions set status = 'closed', closed_at = now() where id = $1 and status = 'withdrawing'`, *e.positionID); err != nil {
			slog.Error("settle position", "position", *e.positionID, "err", err)
		}
	}
}
