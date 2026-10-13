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
	"github.com/snehendu098/sweem-terminal/internal/perpl"
	"github.com/snehendu098/sweem-terminal/internal/pg"
	"github.com/snehendu098/sweem-terminal/internal/planner"
	"github.com/snehendu098/sweem-terminal/internal/streams"
)

// Chain is the RPC reads the scheduler needs; RPC implements it, tests fake it.
type Chain interface {
	Wallet(ctx context.Context, wallet, sym string) (decimal.Decimal, error) // atoms, MON native
	Balances(ctx context.Context, wallet string, s planner.Strategy, legs []planner.LegState, peers []planner.Peer) ([]planner.Balance, error)
	Live(ctx context.Context, wallet string, s planner.Strategy, legs []planner.LegState, peers []planner.Peer) (planner.Live, error)
	Perp(ctx context.Context, wallet string, perpID int) (perpl.Perp, error)
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
	m := 0
	for i, v := range values {
		w := int(v.Mul(decimal.NewFromInt(10_000)).Div(total).Round(0).IntPart()) - target[i]
		m = max(m, w, -w)
	}
	return m
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
	scheduleID, positionID *string
	slot                   int64
	kind, user             string
	steps                  []planner.Step
	legs                   []planner.LegState // position legs after the run; applied on confirm (reconcile)
	settle                 bool               // full withdraw: position → closed when the run confirms
	open                   *opening           // recurring buy: the position this deploy opens
}

// opening is a new strategy position, written with its deploy execution and fee row (pg.InsertPosition + pg.LinkDeploy, like api).
type opening struct {
	strategyID, wallet, principal, fee, owed string
	creator                                  *string // nil = own strategy, nothing owed
	legs                                     []planner.LegState
}

func (s *Scheduler) schedules(ctx context.Context, now time.Time) {
	rows, err := s.DB.Query(ctx, `select s.id, s.user_id, lower(d.wallet_address), s.kind, s.target_type, s.target_id,
		coalesce(s.amount_usdc, 0)::text, coalesce(s.trigger_price, 0)::text, coalesce(s.interval_seconds, 0),
		coalesce(s.next_run_at, 'epoch'), coalesce(s.sell_bps, 0), coalesce(s.drift_bps, 0), s.max_slippage_bps
		from schedules s join delegations d on d.user_id = s.user_id and d.active
		where s.active and s.target_type in ('market', 'position', 'strategy') and s.kind in ('dca', 'take_profit', 'stop_loss', 'rebalance')`)
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
	switch c.kind {
	case "dca":
		if c.next.After(now) {
			return nil
		}
		usdc := c.amount.Shift(6).Floor()
		e.slot = Slot(c.next, c.interval)
		if c.targetType == "strategy" {
			return s.recurringBuy(ctx, c, e, usdc, advance())
		}
		var err error
		if e.steps, err = s.P.MarketBuy(ctx, c.targetID, usdc, c.slip); err != nil {
			return err
		}
		return s.submit(ctx, advance(), e)
	case "take_profit", "stop_loss":
		return s.trigger(ctx, c, e)
	case "rebalance":
		return nil // position rebalance schedules only override driftBps, see positions
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

// recurringBuy deploys usdc into the strategy as a new position, like api's deploy: fee → swaps → venues.
func (s *Scheduler) recurringBuy(ctx context.Context, c schedule, e exec, usdc decimal.Decimal, claim []any) error {
	var st planner.Strategy
	var creator string
	var sources, rules []byte
	if err := s.DB.QueryRow(ctx, `select asset, sources, sleeves, rules, creator from strategies where id::text = $1`, c.targetID).
		Scan(&st.Asset, &sources, &st.Sleeves, &rules, &creator); err != nil {
		return err
	}
	if err := errors.Join(json.Unmarshal(sources, &st.Sources), json.Unmarshal(rules, &st.Rules)); err != nil {
		return err
	}
	perpl := false // a basis leg needs to know whether the wallet already has a Perpl account
	for _, pt := range st.Parts() {
		for _, src := range pt.Sources {
			if src.Kind == "basis" && !perpl {
				p, err := s.Chain.Perp(ctx, c.wallet, s.P.PerpID(pt.Asset))
				if err != nil {
					return err
				}
				perpl = p.Account > 0
			}
		}
	}
	plan, err := s.P.Deploy(ctx, st, usdc, perpl, c.slip)
	if err != nil {
		return err
	}
	fee := decimal.RequireFromString(plan.FeeUSDC).Shift(-6)
	o := &opening{strategyID: c.targetID, wallet: c.wallet, principal: c.amount.String(), fee: fee.String(), owed: "0", legs: plan.Legs}
	if creator != c.user {
		o.creator = &creator
		o.owed = fee.Mul(decimal.NewFromInt(planner.CreatorBps)).Div(decimal.NewFromInt(planner.FeeBps)).String()
	}
	e.kind, e.steps, e.open = "deploy", plan.Steps, o
	return s.submit(ctx, claim, e)
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
	if o := e.open; o != nil {
		id, err := pg.InsertPosition(ctx, tx, o.strategyID, e.user, o.wallet, o.principal, o.legs)
		if err != nil {
			return err
		}
		e.positionID = &id
	}
	err = tx.QueryRow(ctx, `insert into executions (schedule_id, slot, kind, status, user_id, steps, position_id, legs)
		values ($1, $2, $3, 'pending', $4, $5, $6, $7) on conflict (schedule_id, slot) do nothing returning id`,
		e.scheduleID, e.slot, e.kind, e.user, steps, e.positionID, legs).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // rolled back, incl. a recurring buy's position
	}
	if err != nil {
		return err
	}
	if o := e.open; o != nil {
		if err := pg.LinkDeploy(ctx, tx, *e.positionID, id, e.user, o.strategyID, o.creator, o.fee, o.owed); err != nil {
			return err
		}
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
	if e.open != nil { // recurring buy: opening → open, or failed (like api's settle)
		st := "failed"
		if err == nil && out.Status == "confirmed" {
			st = "open"
		}
		if _, err := s.DB.Exec(ctx, `update strategy_positions set status = $2 where id = $1 and status = 'opening'`, *e.positionID, st); err != nil {
			slog.Error("settle position", "position", *e.positionID, "err", err)
		}
	}
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
