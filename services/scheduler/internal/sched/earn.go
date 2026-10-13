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
	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/perpl"
	"github.com/snehendu098/sweem-terminal/internal/planner"
	"github.com/snehendu098/sweem-terminal/internal/streams"
)

type position struct {
	id, user, wallet string
	strat            planner.Strategy
	legs             []planner.LegState
	peers            []planner.Peer
	last             *time.Time
	driftOverride    *int
}

// perpOf maps a basis asset to its Perpl market name (planner's hedge table).
var perpOf = map[string]string{"WETH": "ETH", "cbBTC": "BTC", "MON": "MON"}

const positionQ = `select p.id, p.user_id, lower(p.wallet), st.asset, st.sources, st.sleeves, st.rules, p.legs, p.last_rebalance_at,
	(select s.drift_bps from schedules s where s.active and s.kind = 'rebalance' and s.target_type = 'position' and s.target_id = p.id::text limit 1),
	coalesce((select jsonb_agg(jsonb_build_object('asset', st2.asset, 'legs', p2.legs)) from strategy_positions p2 join strategies st2 on st2.id = p2.strategy_id
		where p2.wallet = p.wallet and p2.id <> p.id and p2.status = 'open'), '[]')
	from strategy_positions p join strategies st on st.id = p.strategy_id join delegations d on d.user_id = p.user_id and d.active
	where p.status = 'open' `

func scanPosition(r pgx.CollectableRow) (p position, err error) {
	var sources, rulesJSON, legs, peers []byte
	if err = r.Scan(&p.id, &p.user, &p.wallet, &p.strat.Asset, &sources, &p.strat.Sleeves, &rulesJSON, &legs, &p.last, &p.driftOverride, &peers); err != nil {
		return p, err
	}
	var raw []struct {
		Asset string
		Legs  []planner.LegState
	}
	return p, errors.Join(json.Unmarshal(sources, &p.strat.Sources), json.Unmarshal(rulesJSON, &p.strat.Rules),
		json.Unmarshal(legs, &p.legs), json.Unmarshal(peers, &raw),
		func() error {
			for _, x := range raw {
				p.peers = append(p.peers, planner.Peer{Asset: x.Asset, Legs: x.Legs})
			}
			return nil
		}())
}

func (s *Scheduler) position(ctx context.Context, id string) (position, error) {
	rows, err := s.DB.Query(ctx, positionQ+`and p.id::text = $1`, id)
	if err != nil {
		return position{}, err
	}
	return pgx.CollectExactlyOneRow(rows, scanPosition)
}

// reconcile applies planned legs of finished executions: confirmed ⇒ strategy_positions.legs, failed/rejected ⇒ dropped.
// Status is never touched (api owns 'withdrawing').
// ponytail: a run that failed midway keeps the old legs; lending values self-correct via Balances, a half-done basis toggle needs a manual look.
func (s *Scheduler) reconcile(ctx context.Context) error {
	_, err := s.DB.Exec(ctx, `with done as (
			select id, position_id, legs, status from executions where legs is not null and status in ('confirmed', 'failed', 'rejected') for update),
		applied as (update strategy_positions p set legs = done.legs from done where done.position_id = p.id and done.status = 'confirmed')
		update executions set legs = null where id in (select id from done)`)
	return err
}

func (s *Scheduler) positions(ctx context.Context, now time.Time) {
	if err := s.reconcile(ctx); err != nil {
		slog.Error("reconcile legs", "err", err)
		return
	}
	rows, err := s.DB.Query(ctx, positionQ+`and not exists (select 1 from executions e where e.position_id = p.id and (e.status in ('pending', 'submitted') or e.legs is not null))`)
	if err != nil {
		slog.Error("positions", "err", err)
		return
	}
	list, err := pgx.CollectRows(rows, scanPosition)
	if err != nil {
		slog.Error("positions", "err", err)
		return
	}
	for _, p := range list {
		if err := s.evaluate(ctx, p, now); err != nil {
			slog.Warn("position skipped", "position", p.id, "err", err)
		}
	}
}

// evaluate runs per sleeve, in order: Perpl liquidation guard, funding gate, lending rate switch, drift rebalance.
// A firing guard is submitted alone at once; sleeve rebalances are merged into one execution (within MaxSteps).
// ponytail: one sleeve's rebalance resets the min interval for all (per-position clock); per-sleeve clocks if that bites.
func (s *Scheduler) evaluate(ctx context.Context, p position, now time.Time) error {
	parts := p.strat.Parts()
	legs, err := planner.Split(parts, p.legs)
	if err != nil {
		return errors.New("legs do not match sources")
	}
	rates, err := streams.Get[streams.RatesSnapshot](ctx, s.RDB, streams.RatesLatest)
	if err != nil || time.Since(time.UnixMilli(rates.Ts)) > s.MaxAge {
		return fmt.Errorf("rates stale: %v", err) // gate + switch need fresh rates; never act without them
	}
	all, err := s.Chain.Balances(ctx, p.wallet, p.strat, p.legs, p.peers)
	if err != nil {
		return err
	}
	bals, err := planner.Split(parts, all)
	if err != nil {
		return err
	}
	claim := []any{`update strategy_positions set last_rebalance_at = $2 where id = $1 and status = 'open'
		and last_rebalance_at is not distinct from $3
		and not exists (select 1 from executions e where e.position_id = $1 and (e.status in ('pending', 'submitted') or e.legs is not null))`,
		p.id, now, p.last}
	e := exec{positionID: &p.id, user: p.user, kind: "rebalance", legs: append([]planner.LegState(nil), p.legs...)}
	var errs []error
	for k, off := 0, 0; k < len(parts); off, k = off+len(legs[k]), k+1 {
		kind, plan, err := s.evalPart(ctx, p, p.strat.Part(parts[k]), legs[k], bals[k], rates, now)
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("%s: %w", parts[k].Asset, err))
		case kind == "margin_guard":
			e.kind, e.steps, e.legs = kind, plan.Steps, append([]planner.LegState(nil), p.legs...)
			copy(e.legs[off:], plan.Legs)
			return s.submit(ctx, claim, e)
		case len(plan.Steps) > 0 && len(e.steps)+len(plan.Steps) <= planner.MaxSteps: // over the cap: next run
			e.steps = append(e.steps, plan.Steps...)
			copy(e.legs[off:], plan.Legs)
		}
	}
	if len(e.steps) > 0 {
		errs = append(errs, s.submit(ctx, claim, e))
	}
	return errors.Join(errs...)
}

// evalPart decides one sleeve (a single-asset strategy): "margin_guard" plan, rebalance plan, or nothing (empty Steps).
// Plan.Legs = the sleeve's legs after it.
func (s *Scheduler) evalPart(ctx context.Context, p position, st planner.Strategy, legs []planner.LegState, bals []planner.Balance,
	rates streams.RatesSnapshot, now time.Time) (string, planner.Plan, error) {
	var err error
	basis, perpID := -1, 0
	for i, src := range st.Sources {
		if src.Kind == "basis" {
			basis, perpID = i, s.Dep.Perpl.PerpIds[perpOf[st.Asset]]
		}
	}
	on := make([]bool, len(legs))
	var perp perpl.Perp
	if basis >= 0 {
		on[basis] = decimal.RequireFromString(orZero(legs[basis].Lot)).IsPositive()
		if perp, err = s.Chain.Perp(ctx, p.wallet, perpID); err != nil {
			return "", planner.Plan{}, err
		}
	}
	apy := make([]decimal.Decimal, len(legs))
	for i, src := range st.Sources {
		apy[i] = rateOf(rates, src, st.Asset)
	}

	// (c) liquidation guard: top up margin, else cut the short; full close (spot parked in lending) only as a last resort
	if basis >= 0 && on[basis] {
		usdc, err := s.Chain.Wallet(ctx, p.wallet, "USDC")
		if err != nil {
			return "", planner.Plan{}, err
		}
		if g := perpl.Guard(perp, s.GuardBuffer, usdc); g.Fire {
			lot := decimal.RequireFromString(legs[basis].Lot)
			var plan planner.Plan
			switch {
			case g.CloseAll || g.LotCut.GreaterThanOrEqual(lot):
				off := append([]bool(nil), on...)
				off[basis] = false
				plan, err = s.P.Rebalance(ctx, st, legs, bals, Targets(st.Sources, off, apy, nil), perp.Account > 0, s.Slippage)
			default: // isolated margin: free balance + wallet top-up + released collateral all go into the position via addMargin
				var r planner.Plan
				if r, err = s.P.ResizeBasis(ctx, st, legs[basis], g.LotCut.Neg(), g.TopUp, perp.Free.Add(g.Released), s.Slippage); err == nil {
					plan.Steps, plan.Legs = r.Steps, append([]planner.LegState(nil), legs...)
					plan.Legs[basis] = r.Legs[0]
				}
			}
			if err != nil || len(plan.Steps) == 0 {
				return "", planner.Plan{}, errors.Join(err, errors.New("guard: nothing planned"))
			}
			return "margin_guard", plan, nil
		}
	}

	// (a) funding gate
	want := append([]bool(nil), on...)
	if basis >= 0 && st.Rules.FundingGate != nil {
		apr, ok, err := s.fundingAPR(ctx, rates, perpID, st.Rules.FundingGate.LookbackDays)
		if err != nil {
			return "", planner.Plan{}, err
		}
		if !ok {
			return "", planner.Plan{}, fmt.Errorf("funding %d missing", perpID)
		}
		want[basis] = perpl.Gate(on[basis], apr, decimal.RequireFromString(orZero(st.Rules.FundingGate.APR)), s.GateBand)
	}
	var minGain *decimal.Decimal
	if r := st.Rules.RateSwitch; r != nil {
		g := decimal.RequireFromString(orZero(r.MinGain))
		minGain = &g
	}
	target := Targets(st.Sources, want, apy, minGain)
	flip := basis >= 0 && on[basis] != (target[basis] > 0)

	// (b)+(d) drift toward targets (gate flips skip the interval; hysteresis already prevents churn)
	band := st.Rules.DriftBps
	if p.driftOverride != nil {
		band = *p.driftOverride
	}
	if band == 0 {
		band = 500
	}
	minIv := time.Duration(st.Rules.MinRebalanceSeconds) * time.Second
	if st.Rules.MinRebalanceSeconds == 0 {
		minIv = 24 * time.Hour
	}
	values := make([]decimal.Decimal, len(bals))
	for i, b := range bals {
		values[i] = decimal.RequireFromString(orZero(b.Value))
	}
	due := p.last == nil || now.Sub(*p.last) >= minIv
	if !flip && (!due || Drift(values, target) <= band) {
		return "", planner.Plan{}, nil
	}
	plan, err := s.P.Rebalance(ctx, st, legs, bals, target, perp.Account > 0, s.Slippage)
	return "rebalance", plan, err
}

func orZero(s string) string {
	if s == "" {
		return "0"
	}
	return s
}

// Targets is the per-source target bps: gated-off basis parks its weight in the best lending source (stays on if there is none);
// with minGain set, each lending source the best beats by more than minGain APY moves there too. apy zero = unknown.
func Targets(srcs []planner.Source, basisOn []bool, apy []decimal.Decimal, minGain *decimal.Decimal) []int {
	t := make([]int, len(srcs))
	best := -1
	for i, src := range srcs {
		t[i] = src.WeightBps
		if src.Kind == "lending" && (best < 0 || apy[i].GreaterThan(apy[best])) {
			best = i
		}
	}
	if best < 0 {
		return t
	}
	for i, src := range srcs {
		switch {
		case src.Kind == "basis" && !basisOn[i]:
		case src.Kind == "lending" && i != best && minGain != nil && apy[i].IsPositive() && apy[best].Sub(apy[i]).GreaterThan(*minGain):
		default:
			continue
		}
		t[best], t[i] = t[best]+t[i], 0
	}
	return t
}

func rateOf(r streams.RatesSnapshot, src planner.Source, asset string) decimal.Decimal {
	for _, x := range r.Rates {
		if x.Venue == src.Venue && (strings.EqualFold(x.Asset, asset) || strings.EqualFold(x.Asset, "W"+asset)) {
			return x.Apy
		}
	}
	return decimal.Zero
}

// fundingAPR is the mean daily funding over the last `days` full UTC days × 365 (backtest's gate formula; rates are fractions
// like the publisher's AprShort7d). No history (or days == 0) falls back to the publisher's 7d trailing APR.
func (s *Scheduler) fundingAPR(ctx context.Context, r streams.RatesSnapshot, perpID, days int) (decimal.Decimal, bool, error) {
	if days > 0 {
		var apr *string
		err := s.DB.QueryRow(ctx, `select (avg(rate) * 365)::text from series_funding_daily
			where perp_id = $1 and day >= (now() at time zone 'utc')::date - $2::int and day < (now() at time zone 'utc')::date`, perpID, days).Scan(&apr)
		if err != nil {
			return decimal.Zero, false, err
		}
		if apr != nil {
			v, err := decimal.NewFromString(*apr)
			return v, err == nil, err
		}
	}
	for _, f := range r.Funding {
		if f.PerpID == perpID {
			return f.AprShort7d, true, nil
		}
	}
	return decimal.Zero, false, nil
}
