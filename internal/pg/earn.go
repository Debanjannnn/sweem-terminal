package pg

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/streams"
)

// Querier is a pool or a tx.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// GateAPR is the funding gate's input: mean daily funding over the last `days` full UTC days × 365 (backtest's gate
// formula), else (no history, or days == 0) the publisher's 7d trailing APR. ok false = no data at all.
// Shared by the scheduler (acts on it) and the api (shows it).
func GateAPR(ctx context.Context, q Querier, r streams.RatesSnapshot, perpID, days int) (apr decimal.Decimal, ok bool, err error) {
	if days > 0 {
		var v *string
		if err := q.QueryRow(ctx, `select (avg(rate) * 365)::text from series_funding_daily
			where perp_id = $1 and day >= (now() at time zone 'utc')::date - $2::int and day < (now() at time zone 'utc')::date`, perpID, days).Scan(&v); err != nil {
			return decimal.Zero, false, err
		}
		if v != nil {
			apr, err := decimal.NewFromString(*v)
			return apr, err == nil, err
		}
	}
	for _, f := range r.Funding {
		if f.PerpID == perpID {
			return f.AprShort7d, true, nil
		}
	}
	return decimal.Zero, false, nil
}

// InsertPosition adds an opening strategy position (status 'opening') with its planned legs.
func InsertPosition(ctx context.Context, q Querier, strategyID, user, wallet, principal string, legs any) (id string, err error) {
	b, err := json.Marshal(legs)
	if err != nil {
		return "", err
	}
	err = q.QueryRow(ctx, `insert into strategy_positions(strategy_id, user_id, wallet, principal_usdc, legs) values ($1, $2, $3, $4::numeric, $5) returning id::text`,
		strategyID, user, wallet, principal, b).Scan(&id)
	return id, err
}

// LinkDeploy ties a position to its deploy execution and books the open fee (creator nil = own strategy).
func LinkDeploy(ctx context.Context, q Querier, posID, execID, user, strategyID string, creator *string, fee, owed string) error {
	if _, err := q.Exec(ctx, `update strategy_positions set execution_id = $2 where id = $1`, posID, execID); err != nil {
		return err
	}
	_, err := q.Exec(ctx, `insert into fee_ledger(execution_id, user_id, strategy_id, creator, fee_usdc, creator_owed_usdc) values ($1, $2, $3, $4, $5::numeric, $6::numeric)`,
		execID, user, strategyID, creator, fee, owed)
	return err
}
