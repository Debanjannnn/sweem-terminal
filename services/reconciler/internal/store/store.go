package store

import (
	"context"
	"math/big"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sweem-terminal/sweem-terminal/services/reconciler/internal/diff"
	"github.com/sweem-terminal/sweem-terminal/services/reconciler/internal/risk"
)

type Store struct{ db *pgxpool.Pool }

func New(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	return &Store{db: pool}, pool.Ping(ctx)
}

func (s *Store) Close() { s.db.Close() }

func (s *Store) Ping(ctx context.Context) error { return s.db.Ping(ctx) }

type Execution struct {
	IntentKey    string
	TxHash       string
	Confirmation string
}

// executions is wallet's table; reconciler only walks it and moves the tier.
// ponytail: reads the three columns wallet guarantees, widen if it adds a block column
func (s *Store) PendingExecutions(ctx context.Context, limit int) ([]Execution, error) {
	rows, err := s.db.Query(ctx,
		`select intent_key, tx_hash, confirmation from executions
		 where tx_hash <> '' and confirmation <> 'final'
		 order by submitted_at asc limit $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Execution
	for rows.Next() {
		var e Execution
		if err := rows.Scan(&e.IntentKey, &e.TxHash, &e.Confirmation); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) SetConfirmation(ctx context.Context, intentKey, tier string) error {
	_, err := s.db.Exec(ctx,
		`update executions set confirmation = $2,
		   settled_at = case when $2 = 'final' then now() else settled_at end
		 where intent_key = $1`, intentKey, tier)
	return err
}

type Snapshot struct {
	MarketID    string
	Owner       string
	Block       uint64
	Size        *big.Int
	Collateral  *big.Int
	EntryIndex  *big.Int
	Notional    *big.Int
	MarginRatio *big.Int
	Tier        string
}

func (s *Store) SaveSnapshot(ctx context.Context, sn Snapshot) error {
	_, err := s.db.Exec(ctx,
		`insert into position_snapshots
		   (market_id, owner, block_number, size, collateral, entry_index, notional, margin_ratio, tier)
		 values ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		 on conflict (market_id, owner, block_number) do nothing`,
		sn.MarketID, sn.Owner, sn.Block, num(sn.Size), num(sn.Collateral), num(sn.EntryIndex),
		num(sn.Notional), num(sn.MarginRatio), sn.Tier)
	return err
}

// latest snapshot is what our database believes about a position
func (s *Store) LatestSnapshot(ctx context.Context, marketID, owner string) (diff.Position, uint64, error) {
	var size, collateral string
	var block uint64
	err := s.db.QueryRow(ctx,
		`select size::text, collateral::text, block_number from position_snapshots
		 where market_id = $1 and owner = $2 order by block_number desc limit 1`, marketID, owner).
		Scan(&size, &collateral, &block)
	if err != nil {
		return diff.Position{}, 0, err
	}
	return diff.Position{Size: diff.Amount(size), Collateral: diff.Amount(collateral)}, block, nil
}

func (s *Store) Owners(ctx context.Context, marketID string) ([]string, error) {
	rows, err := s.db.Query(ctx, `select distinct owner from position_snapshots where market_id = $1`, marketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var o string
		if err := rows.Scan(&o); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *Store) SaveAlert(ctx context.Context, a risk.Alert) error {
	_, err := s.db.Exec(ctx,
		`insert into risk_alerts (idempotency_key, kind, market_id, owner, block_number, detail)
		 values ($1,$2,$3,$4,$5,$6) on conflict (idempotency_key) do nothing`,
		a.Key, a.Kind, a.MarketID, a.Owner, a.Block, a.Detail)
	return err
}

func (s *Store) SaveDivergence(ctx context.Context, d diff.Divergence) error {
	_, err := s.db.Exec(ctx,
		`insert into divergences (kind, market_id, owner, expected, actual, reason, observed_at_block)
		 values ($1,$2,$3,$4,$5,$6,$7)`,
		d.Kind, d.MarketID, d.Owner, d.Expected, d.Actual, d.Reason, d.ObservedAtBlock)
	return err
}

func (s *Store) SaveFunding(ctx context.Context, marketID string, observedAt int64, rate string) error {
	_, err := s.db.Exec(ctx,
		`insert into funding_snapshots (market_id, observed_at, funding_rate) values ($1,$2,$3)
		 on conflict (market_id, observed_at) do nothing`, marketID, observedAt, rate)
	return err
}

func (s *Store) SaveOI(ctx context.Context, marketID string, block uint64, longOI, shortOI, maxPerSide *big.Int) error {
	_, err := s.db.Exec(ctx,
		`insert into oi_snapshots (market_id, block_number, long_oi, short_oi, max_oi_per_side)
		 values ($1,$2,$3,$4,$5) on conflict (market_id, block_number) do nothing`,
		marketID, block, num(longOI), num(shortOI), num(maxPerSide))
	return err
}

func num(v *big.Int) string {
	if v == nil {
		return "0"
	}
	return v.String()
}
