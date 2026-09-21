// Package engine answers one question: is it time? the answer is a trigger,
// never an authorization. wallet re-verifies delegation, caps, policy and
// balance before the executor signs anything, so a crashed, restarted or
// accidentally duplicated scheduler cannot move funds on its own.
package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/natsbus"
	"github.com/sweem-terminal/sweem-terminal/services/scheduler/internal/client"
	"github.com/sweem-terminal/sweem-terminal/services/scheduler/internal/delegation"
	"github.com/sweem-terminal/sweem-terminal/services/scheduler/internal/store"
)

// ponytail: subject literal, move to natsbus/subjects.go when that package is next touched
const subjSchedulePaused = "schedule.paused_revoked.v1"

type Store interface {
	ActiveSchedules(context.Context) ([]store.Schedule, error)
	ClaimSlice(context.Context, store.Slice) (bool, error)
	ReleaseSlice(ctx context.Context, scheduleID string, idx int, intentKey string) error
	RearmSlices(ctx context.Context, scheduleID string, idxs []int) error
	SetScheduleState(ctx context.Context, id, state string) error
	FiredLevels(ctx context.Context, scheduleID string) (map[int]bool, error)
	Generations(ctx context.Context, scheduleID string) (map[int]int, error)
}

type Config struct {
	Tick           time.Duration
	MaxCatchup     int     // slices dispatched per schedule per tick after downtime
	GridBandBps    int     // grid hysteresis
	GasLimit       float64 // monad charges the full limit up front, so this is the real cost
	GasPriceGwei   float64
	MaxGasFraction float64
	GasMarket      string // market whose mid prices MON in the quote token
	BaseDecimals   int
	QuoteDecimals  int
}

type Engine struct {
	Store      Store
	Cfg        Config
	Log        *slog.Logger
	Delegation func(context.Context, string) error
	Dispatch   func(ctx context.Context, did, scheduleID string, index int) (client.Dispatch, error)
	Mid        func(context.Context, string) (float64, error)
	Publish    func(ctx context.Context, subject, msgID string, payload []byte) error
	Now        func() time.Time

	C Counters

	pxMu   sync.Mutex
	lastPx map[string]float64 // grid, previous poll; empty after a restart means no fire this tick
}

type Counters struct {
	Ticks, Dispatched, Claimed, ClaimLost, Replayed, GasSkipped, GasUnpriced,
	DelegationRevoked, DelegationUnknown, PriceUnavailable, DispatchFailed, Errors atomic.Int64
}

func (c *Counters) Snapshot() map[string]int64 {
	return map[string]int64{
		"ticks":              c.Ticks.Load(),
		"dispatched":         c.Dispatched.Load(),
		"claimed":            c.Claimed.Load(),
		"claim_lost":         c.ClaimLost.Load(),
		"replayed":           c.Replayed.Load(),
		"gas_skipped":        c.GasSkipped.Load(),
		"gas_unpriced":       c.GasUnpriced.Load(),
		"delegation_revoked": c.DelegationRevoked.Load(),
		"delegation_unknown": c.DelegationUnknown.Load(),
		"price_unavailable":  c.PriceUnavailable.Load(),
		"dispatch_failed":    c.DispatchFailed.Load(),
		"errors":             c.Errors.Load(),
	}
}

func (e *Engine) Run(ctx context.Context) {
	t := time.NewTicker(e.Cfg.Tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			e.Tick(ctx)
		}
	}
}

// Tick order: load active schedules, then per schedule check delegation
// health, select due slices, gate on gas, claim atomically, dispatch, emit.
func (e *Engine) Tick(ctx context.Context) {
	e.C.Ticks.Add(1)
	schedules, err := e.Store.ActiveSchedules(ctx)
	if err != nil {
		e.C.Errors.Add(1)
		e.Log.Error("load active schedules", "err", err)
		return
	}
	mids := map[string]float64{}
	for _, s := range schedules {
		e.runSchedule(ctx, s, e.now(), mids)
	}
}

// one bad schedule must not take the loop down with it
func (e *Engine) runSchedule(ctx context.Context, s store.Schedule, now time.Time, mids map[string]float64) {
	defer func() {
		if r := recover(); r != nil {
			e.C.Errors.Add(1)
			e.Log.Error("schedule panicked", "schedule", s.ID, "panic", r)
		}
	}()

	if err := e.Delegation(ctx, s.UserID); err != nil {
		if errors.Is(err, delegation.ErrRevoked) {
			e.pause(ctx, s, err)
			return
		}
		e.C.DelegationUnknown.Add(1)
		e.Log.Warn("delegation health unknown, holding schedule", "schedule", s.ID, "err", err)
		return
	}

	dues, err := e.dues(ctx, s, now, mids)
	if err != nil {
		e.C.Errors.Add(1)
		e.Log.Error("select due slices", "schedule", s.ID, "kind", s.Kind, "err", err)
		return
	}
	for _, d := range dues {
		e.fire(ctx, s, d, now, mids)
	}
}

func (e *Engine) dues(ctx context.Context, s store.Schedule, now time.Time, mids map[string]float64) ([]due, error) {
	max := e.Cfg.MaxCatchup
	if max < 1 {
		max = 1
	}
	switch s.Kind {
	case store.KindTWAP:
		return twapDue(s, now, max)
	case store.KindDCA:
		return dcaDue(s, now, max)
	case store.KindGrid:
		return e.gridDues(ctx, s, now, mids)
	}
	return nil, fmt.Errorf("schedule %s has kind %q, which is not twap, dca or grid", s.ID, s.Kind)
}

func (e *Engine) gridDues(ctx context.Context, s store.Schedule, now time.Time, mids map[string]float64) ([]due, error) {
	cur, err := e.mid(ctx, s.MarketID, mids)
	if err != nil {
		e.C.PriceUnavailable.Add(1)
		return nil, err
	}
	fired, err := e.Store.FiredLevels(ctx, s.ID)
	if err != nil {
		return nil, err
	}
	fire, rearm, err := gridDue(s, e.prevPx(s.ID), cur, e.Cfg.GridBandBps, fired)
	e.setPrevPx(s.ID, cur)
	if err != nil {
		return nil, err
	}
	if err := e.Store.RearmSlices(ctx, s.ID, rearm); err != nil {
		return nil, err
	}
	// read after the rearm so a level that just re-armed carries its new
	// generation; a stale read only costs a replay, never a double spend
	gens, err := e.Store.Generations(ctx, s.ID)
	if err != nil {
		return nil, err
	}
	for i := range fire {
		fire[i].DueAt = now
		fire[i].Generation = gens[fire[i].Index]
	}
	return fire, nil
}

func (e *Engine) fire(ctx context.Context, s store.Schedule, d due, now time.Time, mids map[string]float64) {
	defer func() {
		if r := recover(); r != nil {
			e.C.Errors.Add(1)
			e.Log.Error("slice panicked", "schedule", s.ID, "slice", d.Index, "panic", r)
		}
	}()

	if skip, why := e.gasSkip(ctx, s, d, mids); skip {
		e.C.GasSkipped.Add(1)
		e.Log.Warn("slice skipped on gas", "schedule", s.ID, "slice", d.Index, "why", why)
		return
	}

	sl := store.Slice{
		ScheduleID: s.ID,
		Index:      d.Index,
		DueAt:      d.DueAt,
		Amount:     d.Amount,
		Side:       d.Side,
		IntentKey:  IntentKey(s.ID, d.Index, d.Generation),
	}
	claimed, err := e.Store.ClaimSlice(ctx, sl)
	if err != nil {
		e.C.Errors.Add(1)
		e.Log.Error("claim slice", "schedule", s.ID, "slice", d.Index, "err", err)
		return
	}
	if !claimed {
		e.C.ClaimLost.Add(1)
		return
	}
	e.C.Claimed.Add(1)

	res, err := e.Dispatch(ctx, s.UserID, s.ID, d.Index)
	switch {
	case errors.Is(err, delegation.ErrRevoked):
		// wallet already paused the schedule and marked the slice
		e.C.DelegationRevoked.Add(1)
		e.Log.Warn("wallet refused the slice, delegation is gone", "schedule", s.ID, "slice", d.Index, "err", err)
		return
	case err != nil:
		e.C.DispatchFailed.Add(1)
		e.Log.Error("dispatch slice to wallet", "schedule", s.ID, "slice", d.Index, "err", err)
		e.release(ctx, s.ID, d.Index, sl.IntentKey)
		return
	}
	if res.Replayed {
		e.C.Replayed.Add(1)
		return
	}
	e.C.Dispatched.Add(1)
	e.emit(ctx, natsbus.SubjScheduleSliceDue, sl.IntentKey, map[string]any{
		"idempotency_key": sl.IntentKey,
		"emitted_at":      now.UnixMilli(),
		"schedule_id":     s.ID,
		"slice_index":     d.Index,
		"intent_key":      sl.IntentKey,
		"tx_hash":         res.TxHash,
	})
}

func (e *Engine) pause(ctx context.Context, s store.Schedule, cause error) {
	e.C.DelegationRevoked.Add(1)
	e.Log.Warn("pausing schedule, delegation is gone", "schedule", s.ID, "user", s.UserID, "err", cause)
	if err := e.Store.SetScheduleState(ctx, s.ID, store.StatePausedRevoked); err != nil {
		e.C.Errors.Add(1)
		e.Log.Error("pause schedule", "schedule", s.ID, "err", err)
		return
	}
	e.emit(ctx, subjSchedulePaused, s.ID+"|revoked", map[string]any{
		"idempotency_key": s.ID + "|revoked",
		"emitted_at":      e.now().UnixMilli(),
		"schedule_id":     s.ID,
		"reason":          "delegation_revoked",
	})
}

// gasSkip gates a slice whose gas costs more than a fraction of what it trades.
// float is precise enough for a ratio, never for the amount itself.
func (e *Engine) gasSkip(ctx context.Context, s store.Schedule, d due, mids map[string]float64) (bool, string) {
	if e.Cfg.MaxGasFraction <= 0 || e.Cfg.GasMarket == "" {
		return false, ""
	}
	gasPx, err := e.mid(ctx, e.Cfg.GasMarket, mids)
	if err != nil {
		e.C.GasUnpriced.Add(1)
		return false, ""
	}
	notional, err := e.notional(ctx, s, d, mids)
	if err != nil || notional <= 0 {
		e.C.GasUnpriced.Add(1)
		return false, ""
	}
	cost := e.Cfg.GasPriceGwei * 1e-9 * e.Cfg.GasLimit * gasPx
	if cost > notional*e.Cfg.MaxGasFraction {
		return true, fmt.Sprintf("gas %.4f exceeds %.2f%% of notional %.4f", cost, e.Cfg.MaxGasFraction*100, notional)
	}
	return false, ""
}

// notional values a slice in the quote token: a buy spends quote, a sell sends base.
func (e *Engine) notional(ctx context.Context, s store.Schedule, d due, mids map[string]float64) (float64, error) {
	raw, err := strconv.ParseFloat(d.Amount, 64)
	if err != nil {
		return 0, err
	}
	if d.Side == "sell" {
		px, err := e.mid(ctx, s.MarketID, mids)
		if err != nil {
			return 0, err
		}
		return raw / math.Pow10(decimals(e.Cfg.BaseDecimals, 18)) * px, nil
	}
	return raw / math.Pow10(decimals(e.Cfg.QuoteDecimals, 6)), nil
}

func decimals(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

func (e *Engine) mid(ctx context.Context, marketID string, mids map[string]float64) (float64, error) {
	if px, ok := mids[marketID]; ok {
		return px, nil
	}
	px, err := e.Mid(ctx, marketID)
	if err != nil {
		return 0, err
	}
	mids[marketID] = px
	return px, nil
}

func (e *Engine) emit(ctx context.Context, subject, msgID string, payload map[string]any) {
	if e.Publish == nil {
		return
	}
	body, err := json.Marshal(payload)
	if err != nil {
		e.C.Errors.Add(1)
		return
	}
	if err := e.Publish(ctx, subject, msgID, body); err != nil {
		e.C.Errors.Add(1)
		e.Log.Error("publish event", "subject", subject, "err", err)
	}
}

func (e *Engine) release(ctx context.Context, scheduleID string, idx int, intentKey string) {
	if err := e.Store.ReleaseSlice(ctx, scheduleID, idx, intentKey); err != nil {
		e.C.Errors.Add(1)
		e.Log.Error("release slice", "schedule", scheduleID, "slice", idx, "err", err)
	}
}

func (e *Engine) prevPx(id string) float64 {
	e.pxMu.Lock()
	defer e.pxMu.Unlock()
	return e.lastPx[id]
}

func (e *Engine) setPrevPx(id string, px float64) {
	e.pxMu.Lock()
	defer e.pxMu.Unlock()
	if e.lastPx == nil {
		e.lastPx = map[string]float64{}
	}
	e.lastPx[id] = px
}

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now().UTC()
}

// IntentKey is the idempotency key for the whole downstream path. it mirrors
// wallet's intentKey("schedule", id, index) exactly: the same slice must hash
// to the same key on both sides or the claim and the intent drift apart.
// mirrors wallet's intentKey("schedule", id, index, generation). generation
// disambiguates a re-armed grid level from its first fire; without it the
// second fire replays the first execution.
func IntentKey(scheduleID string, idx, generation int) string {
	sum := sha256.Sum256([]byte(strings.Join(
		[]string{"schedule", scheduleID, strconv.Itoa(idx), strconv.Itoa(generation)}, "\x00")))
	return hex.EncodeToString(sum[:16])
}
