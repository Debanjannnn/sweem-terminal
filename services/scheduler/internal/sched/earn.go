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

const positionQ = `select p.id, p.user_id, lower(p.wallet), st.asset, st.sources, st.rules, p.legs, p.last_rebalance_at,
	(select s.drift_bps from schedules s where s.active and s.kind = 'rebalance' and s.target_type = 'position' and s.target_id = p.id::text limit 1),
	coalesce((select jsonb_agg(jsonb_build_object('asset', st2.asset, 'legs', p2.legs)) from strategy_positions p2 join strategies st2 on st2.id = p2.strategy_id
		where p2.wallet = p.wallet and p2.id <> p.id and p2.status = 'open'), '[]')
	from strategy_positions p join strategies st on st.id = p.strategy_id join delegations d on d.user_id = p.user_id and d.active
	where p.status = 'open' `

func scanPosition(r pgx.CollectableRow) (p position, err error) {
	var sources, rulesJSON, legs, peers []byte
	if err = r.Scan(&p.id, &p.user, &p.wallet, &p.strat.Asset, &sources, &rulesJSON, &legs, &p.last, &p.driftOverride, &peers); err != nil {
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

// reconcile applies planned legs of finished executions: confirmed ⇒ strategy_positions.legs (closing ⇒ closed),
// failed/rejected ⇒ legs dropped (closing ⇒ open again).
// ponytail: a run that failed midway keeps the old legs; lending values self-correct via Balances, a half-done basis toggle needs a manual look.
func (s *Scheduler) reconcile(ctx context.Context) error {
	_, err := s.DB.Exec(ctx, `with done as (
			select id, position_id, legs, status from executions where legs is not null and status in ('confirmed', 'failed', 'rejected') for update),
		applied as (update strategy_positions p set
			legs = case when done.status = 'confirmed' then done.legs else p.legs end,
			status = case when p.status <> 'closing' then p.status when done.status = 'confirmed' then 'closed' else 'open' end,
			closed_at = case when p.status = 'closing' and done.status = 'confirmed' then now() else p.closed_at end
			from done where done.position_id = p.id)
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

// evaluate runs, in order: Perpl liquidation guard, funding gate, lending rate switch, drift rebalance. At most one execution.
func (s *Scheduler) evaluate(ctx context.Context, p position, now time.Time) error {
	if len(p.legs) != len(p.strat.Sources) {
		return errors.New("legs do not match sources")
	}
	rates, err := streams.Get[streams.RatesSnapshot](ctx, s.RDB, streams.RatesLatest)
	if err != nil || time.Since(time.UnixMilli(rates.Ts)) > s.MaxAge {
		return fmt.Errorf("rates stale: %v", err) // gate + switch need fresh rates; never act without them
	}
	bals, err := s.Chain.Balances(ctx, p.wallet, p.strat, p.legs, p.peers)
	if err != nil {
		return err
	}
	basis, perpID := -1, 0
	for i, src := range p.strat.Sources {
		if src.Kind == "basis" {
			basis, perpID = i, s.Dep.Perpl.PerpIds[perpOf[p.strat.Asset]]
		}
	}
	on := make([]bool, len(p.legs))
	var perp Perp
	if basis >= 0 {
		on[basis] = decimal.RequireFromString(orZero(p.legs[basis].Lot)).IsPositive()
		if perp, err = s.Chain.Perp(ctx, p.wallet, perpID); err != nil {
			return err
		}
	}
	apy := make([]decimal.Decimal, len(p.legs))
	for i, src := range p.strat.Sources {
		apy[i] = rateOf(rates, src, p.strat.Asset)
	}
	claim := []any{`update strategy_positions set last_rebalance_at = $2 where id = $1 and status = 'open'
		and last_rebalance_at is not distinct from $3
		and not exists (select 1 from executions e where e.position_id = $1 and (e.status in ('pending', 'submitted') or e.legs is not null))`,
		p.id, now, p.last}
	e := exec{positionID: &p.id, user: p.user}

	// (c) liquidation guard: top up margin, else cut the short; full close (spot parked in lending) only as a last resort
	if basis >= 0 && on[basis] {
		usdc, err := s.Chain.Wallet(ctx, p.wallet, "USDC")
		if err != nil {
			return err
		}
		if g := Guard(perp, s.GuardBuffer, usdc); g.Fire {
			e.kind = "margin_guard"
			lot := decimal.RequireFromString(p.legs[basis].Lot)
			var plan planner.Plan
			if g.CloseAll || g.LotCut.GreaterThanOrEqual(lot) {
				off := append([]bool(nil), on...)
				off[basis] = false
				plan, err = s.P.Rebalance(ctx, p.strat, p.legs, bals, Targets(p.strat.Sources, off, apy, nil), perp.Account > 0, s.Slippage)
			} else if plan, err = s.P.ResizeBasis(ctx, p.strat, p.legs[basis], g.LotCut.Neg(), g.TopUp, s.Slippage); err == nil {
				leg := plan.Legs[0]
				plan.Legs = append([]planner.LegState(nil), p.legs...)
				plan.Legs[basis] = leg
			}
			if err != nil || len(plan.Steps) == 0 {
				return errors.Join(err, errors.New("guard: nothing planned"))
			}
			e.steps, e.legs = plan.Steps, plan.Legs
			return s.submit(ctx, claim, e)
		}
	}

	// (a) funding gate
	want := append([]bool(nil), on...)
	if basis >= 0 && p.strat.Rules.FundingGate != nil {
		apr, ok := fundingAPR(rates, perpID)
		if !ok {
			return fmt.Errorf("funding %d missing", perpID)
		}
		want[basis] = Gate(on[basis], apr, decimal.RequireFromString(orZero(p.strat.Rules.FundingGate.APR)), s.GateBand)
	}
	var minGain *decimal.Decimal
	if r := p.strat.Rules.RateSwitch; r != nil {
		g := decimal.RequireFromString(orZero(r.MinGain))
		minGain = &g
	}
	target := Targets(p.strat.Sources, want, apy, minGain)
	flip := basis >= 0 && on[basis] != (target[basis] > 0)

	// (b)+(d) drift toward targets (gate flips skip the interval; hysteresis already prevents churn)
	band := p.strat.Rules.DriftBps
	if p.driftOverride != nil {
		band = *p.driftOverride
	}
	if band == 0 {
		band = 500
	}
	minIv := time.Duration(p.strat.Rules.MinRebalanceSeconds) * time.Second
	if p.strat.Rules.MinRebalanceSeconds == 0 {
		minIv = 24 * time.Hour
	}
	values := make([]decimal.Decimal, len(bals))
	for i, b := range bals {
		values[i] = decimal.RequireFromString(orZero(b.Value))
	}
	due := p.last == nil || now.Sub(*p.last) >= minIv
	if !flip && (!due || Drift(values, target) <= band) {
		return nil
	}
	plan, err := s.P.Rebalance(ctx, p.strat, p.legs, bals, target, perp.Account > 0, s.Slippage)
	if err != nil || len(plan.Steps) == 0 {
		return err
	}
	e.kind, e.steps, e.legs = "rebalance", plan.Steps, plan.Legs
	return s.submit(ctx, claim, e)
}

func orZero(s string) string {
	if s == "" {
		return "0"
	}
	return s
}

// Gate is the funding-gate hysteresis: an open basis leg stays while trailing APR ≥ threshold; a closed one reopens only above threshold+band.
func Gate(open bool, apr, threshold, band decimal.Decimal) bool {
	if open {
		return apr.GreaterThanOrEqual(threshold)
	}
	return apr.GreaterThan(threshold.Add(band))
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

// ponytail: gate uses the publisher's 7d trailing APR regardless of fundingGate.lookbackDays; other windows need a history read.
func fundingAPR(r streams.RatesSnapshot, perpID int) (decimal.Decimal, bool) {
	for _, f := range r.Funding {
		if f.PerpID == perpID {
			return f.AprShort7d, true
		}
	}
	return decimal.Zero, false
}
