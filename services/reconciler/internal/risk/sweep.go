// Package risk watches position health and shouts; it never signs or liquidates.
package risk

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/finality"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/natsbus"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/wad"
	"github.com/sweem-terminal/sweem-terminal/services/reconciler/internal/chain"
)

const (
	KindLiquidation    = "liquidation_candidate"
	KindMarginWarning  = "margin_warning"
	KindOICap          = "oi_cap"
	KindUtilizationCap = "utilization_cap"
	KindOracleStale    = "oracle_stale"
)

const SubjRiskAlert = "risk.alert.v1"

// an index feed quieter than this is stale, and stale is an alert, not a number we use
const OracleStaleness = 60 * time.Second

// warn one quarter above maintenance
var warnNum, warnDen = big.NewInt(125), big.NewInt(100)

// bps -> wad ratio
var bpsToWad = big.NewInt(1e14)

type Alert struct {
	Key      string `json:"idempotencyKey"`
	Kind     string `json:"kind"`
	MarketID string `json:"marketId"`
	Owner    string `json:"owner,omitempty"`
	Detail   string `json:"detail"`
	Block    uint64 `json:"blockNumber"`
}

type PositionInput struct {
	MarketID             string
	Owner                string
	Block                uint64
	Size                 *big.Int // signed, wad
	Collateral           *big.Int // wad
	IndexPrice           *big.Int // wad
	MaintenanceMarginBps *big.Int
}

func EvaluatePosition(in PositionInput) []Alert {
	if in.Size == nil || in.Size.Sign() == 0 || in.Collateral == nil || in.IndexPrice == nil {
		return nil
	}
	notional := wad.MulWad(new(big.Int).Abs(in.Size), in.IndexPrice)
	if notional.Sign() == 0 {
		return nil
	}
	ratio := wad.DivWad(in.Collateral, notional)
	maint := new(big.Int).Mul(in.MaintenanceMarginBps, bpsToWad)
	if ratio.Cmp(maint) <= 0 {
		return []Alert{{
			Key: fmt.Sprintf("liq:%s:%s:%d", in.MarketID, in.Owner, in.Block), Kind: KindLiquidation,
			MarketID: in.MarketID, Owner: in.Owner, Block: in.Block,
			Detail: fmt.Sprintf("margin ratio %s is at or below the %s maintenance requirement on notional %s at speculative block %d",
				ratio, maint, notional, in.Block),
		}}
	}
	warn := new(big.Int).Quo(new(big.Int).Mul(maint, warnNum), warnDen)
	if ratio.Cmp(warn) <= 0 {
		return []Alert{{
			Key: key(KindMarginWarning, in.MarketID, in.Owner, in.Block), Kind: KindMarginWarning,
			MarketID: in.MarketID, Owner: in.Owner, Block: in.Block,
			Detail: fmt.Sprintf("margin ratio %s is within 25%% of the %s maintenance requirement at block %d", ratio, maint, in.Block),
		}}
	}
	return nil
}

type MarketInput struct {
	MarketID        string
	Block           uint64
	LongOI          *big.Int
	ShortOI         *big.Int
	MaxOiPerSide    *big.Int
	Utilization     *big.Int
	MaxUtilization  *big.Int
	OracleUpdatedAt int64 // unix seconds
	Now             time.Time
}

func EvaluateMarket(in MarketInput) []Alert {
	var out []Alert
	hasCap := in.MaxOiPerSide != nil && in.MaxOiPerSide.Sign() > 0
	for _, side := range []struct {
		name string
		oi   *big.Int
	}{{"long", in.LongOI}, {"short", in.ShortOI}} {
		if hasCap && side.oi != nil && side.oi.Cmp(in.MaxOiPerSide) >= 0 {
			out = append(out, Alert{
				Key: key(KindOICap+":"+side.name, in.MarketID, "", in.Block), Kind: KindOICap,
				MarketID: in.MarketID, Block: in.Block,
				Detail: fmt.Sprintf("%s open interest %s has reached the per-side cap of %s at block %d", side.name, side.oi, in.MaxOiPerSide, in.Block),
			})
		}
	}
	if in.Utilization != nil && in.MaxUtilization != nil && in.MaxUtilization.Sign() > 0 && in.Utilization.Cmp(in.MaxUtilization) >= 0 {
		out = append(out, Alert{
			Key: key(KindUtilizationCap, in.MarketID, "", in.Block), Kind: KindUtilizationCap,
			MarketID: in.MarketID, Block: in.Block,
			Detail: fmt.Sprintf("vault utilization %s has reached the cap of %s at block %d", in.Utilization, in.MaxUtilization, in.Block),
		})
	}
	if age := in.Now.Sub(time.Unix(in.OracleUpdatedAt, 0)); in.OracleUpdatedAt == 0 || age > OracleStaleness {
		out = append(out, Alert{
			Key: key(KindOracleStale, in.MarketID, "", in.Block), Kind: KindOracleStale,
			MarketID: in.MarketID, Block: in.Block,
			Detail: fmt.Sprintf("index feed last published %.0fs ago, past the %.0fs staleness limit, so its value must not be traded on", age.Seconds(), OracleStaleness.Seconds()),
		})
	}
	return out
}

func key(kind, marketID, owner string, block uint64) string {
	return fmt.Sprintf("%s:%s:%s:%d", kind, marketID, owner, block)
}

type Market struct {
	ID  [32]byte
	Hex string
}

type Sweeper struct {
	Chain            *chain.Reader
	Engine, Registry string
	Vault            string
	Markets          []Market
	Owners           func(marketID string) []string
	Claim            func(ctx context.Context, key string) (bool, error)
	Publish          func(ctx context.Context, subject, key string, payload []byte) error
	Save             func(ctx context.Context, a Alert) error
	Log              *slog.Logger
}

// Sweep reads speculative `latest` on purpose: liquidation must see a position
// the block it goes bad, and the per-block idempotency key is what makes that safe.
func (s *Sweeper) Sweep(ctx context.Context) {
	block, err := s.Chain.BlockNumber(ctx, finality.Seen)
	if err != nil {
		s.Log.Warn("risk sweep could not read head", "err", err)
		return
	}
	util, maxUtil, err := s.Chain.Utilization(ctx, s.Vault, finality.Seen)
	if err != nil {
		s.Log.Warn("vault utilization read failed", "err", err)
	}
	for _, m := range s.Markets {
		if err := s.sweepMarket(ctx, m, block, util, maxUtil); err != nil {
			s.Log.Warn("market sweep failed", "market", m.Hex, "err", err) // one bad market must not kill the sweep
		}
	}
}

func (s *Sweeper) sweepMarket(ctx context.Context, m Market, block uint64, util, maxUtil *big.Int) error {
	p, err := s.Chain.MarketParams(ctx, s.Registry, m.ID, finality.Seen)
	if err != nil {
		return err
	}
	st, err := s.Chain.MarketState(ctx, s.Engine, m.ID, finality.Seen)
	if err != nil {
		return err
	}
	oracle, err := s.Chain.Oracle(ctx, p.IndexOracle, finality.Seen)
	if err != nil {
		return err
	}
	for _, a := range EvaluateMarket(MarketInput{
		MarketID: m.Hex, Block: block, LongOI: st.LongOI, ShortOI: st.ShortOI, MaxOiPerSide: p.MaxOiPerSide,
		Utilization: util, MaxUtilization: maxUtil, OracleUpdatedAt: oracle.UpdatedAt, Now: time.Now(),
	}) {
		s.emitLogged(ctx, a)
	}
	for _, owner := range s.Owners(m.Hex) {
		pos, err := s.Chain.Position(ctx, s.Engine, m.ID, owner, finality.Seen)
		if err != nil {
			s.Log.Warn("position read failed", "market", m.Hex, "owner", owner, "err", err)
			continue
		}
		for _, a := range EvaluatePosition(PositionInput{
			MarketID: m.Hex, Owner: owner, Block: block, Size: pos.Size, Collateral: pos.Collateral,
			IndexPrice: oracle.Value, MaintenanceMarginBps: p.MaintenanceMarginBps,
		}) {
			s.emitLogged(ctx, a)
		}
	}
	return nil
}

func (s *Sweeper) emitLogged(ctx context.Context, a Alert) {
	if err := s.Emit(ctx, a); err != nil {
		s.Log.Warn("alert emit failed", "kind", a.Kind, "key", a.Key, "err", err)
	}
}

// Emit claims the key first, so a 1s loop cannot re-fire the same candidate
// at the same block. Postgres is written before NATS: an alert that only ever
// existed in a message is not auditable.
func (s *Sweeper) Emit(ctx context.Context, a Alert) error {
	ok, err := s.Claim(ctx, a.Key)
	if err != nil || !ok {
		return err
	}
	if err := s.Save(ctx, a); err != nil {
		return err
	}
	payload, err := json.Marshal(a)
	if err != nil {
		return err
	}
	subject := SubjRiskAlert
	if a.Kind == KindLiquidation {
		subject = natsbus.SubjRiskLiquidationCandidate
	}
	return s.Publish(ctx, subject, a.Key, payload)
}
