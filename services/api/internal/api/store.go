package api

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PG is the Postgres Store. Numerics leave the DB as ::text so no float ever touches them.
type PG struct{ DB *pgxpool.Pool }

func (p PG) EnsureUser(ctx context.Context, did string) error {
	_, err := p.DB.Exec(ctx, `insert into users(id) values ($1) on conflict do nothing`, did)
	return err
}

func (p PG) User(ctx context.Context, did string) (User, error) {
	var u User
	if err := p.DB.QueryRow(ctx, `select id, wallet_address, created_at from users where id=$1`, did).
		Scan(&u.ID, &u.WalletAddress, &u.CreatedAt); err != nil {
		return u, notFound(err)
	}
	rows, _ := p.DB.Query(ctx, `select `+delegationCols+` from delegations where user_id=$1 and active`, did)
	ds, err := pgx.CollectRows(rows, scanDelegation)
	if len(ds) > 0 {
		u.Delegation = &ds[0]
	}
	return u, err
}

const delegationCols = `id::text, privy_wallet_id, wallet_address, key_quorum_id, active, created_at, revoked_at`

func scanDelegation(row pgx.CollectableRow) (Delegation, error) {
	var d Delegation
	err := row.Scan(&d.ID, &d.WalletID, &d.WalletAddress, &d.KeyQuorumID, &d.Active, &d.CreatedAt, &d.RevokedAt)
	return d, err
}

const basketSQL = `select b.address, b.name, b.symbol, b.components, b.created_block, b.created_tx, b.creator,
  n.nav_per_share::text, n.total_nav::text, n.supply::text, n.ts
from baskets b left join lateral (
  select * from nav_points where basket=b.address order by ts desc limit 1) n on true`

func scanBasket(row pgx.CollectableRow) (Basket, error) {
	var b Basket
	var nps, tn, sup *string
	var ts *time.Time
	err := row.Scan(&b.Address, &b.Name, &b.Symbol, &b.Components, &b.CreatedBlock, &b.CreatedTx, &b.Creator, &nps, &tn, &sup, &ts)
	if nps != nil {
		b.Nav = &Nav{NavPerShare: *nps, TotalNav: *tn, Supply: *sup, Ts: *ts}
	}
	return b, err
}

// Baskets lists all baskets, or only creator's when creator != "".
func (p PG) Baskets(ctx context.Context, creator string) ([]Basket, error) {
	rows, _ := p.DB.Query(ctx, basketSQL+` where $1 = '' or b.creator = $1 order by b.created_block, b.address`, creator)
	return collect(rows, scanBasket)
}

func (p PG) Basket(ctx context.Context, addr string) (Basket, error) {
	rows, _ := p.DB.Query(ctx, basketSQL+` where b.address=$1`, addr)
	b, err := pgx.CollectExactlyOneRow(rows, scanBasket)
	if err != nil {
		return b, notFound(err)
	}
	var s BasketStats
	err = p.DB.QueryRow(ctx, `
with m as (select account, usdc, shares from mints where basket=$1),
     r as (select account, usdc, shares from redeems where basket=$1),
     net as (select account, sum(s) s from (select account, shares s from m union all select account, -shares from r) x group by account)
select (select count(*) from m), (select count(*) from r),
  (select coalesce(sum(usdc),0)::text from m), (select coalesce(sum(usdc),0)::text from r),
  (select coalesce(sum(s),0)::text from net), (select count(*) from net where s > 0)`, addr).
		Scan(&s.Mints, &s.Redeems, &s.UsdcInRaw, &s.UsdcOutRaw, &s.SharesRaw, &s.Holders)
	b.Stats = &s
	return b, err
}

func (p PG) NavHistory(ctx context.Context, addr string, from, to time.Time) ([]NavPoint, error) {
	// ponytail: 5000-row cap (~3.5 days at 1/min); add downsampling when charts want longer ranges
	rows, _ := p.DB.Query(ctx, `select nav_per_share::text, total_nav::text, supply::text, ts from nav_points
where basket=$1 and ts >= $2 and ts <= $3 order by ts limit 5000`, addr, from, to)
	return collect(rows, func(r pgx.CollectableRow) (NavPoint, error) {
		var n NavPoint
		return n, r.Scan(&n.NavPerShare, &n.TotalNav, &n.Supply, &n.Ts)
	})
}

// Positions: value = raw 18dp shares × latest NAV (USDC per whole share).
func (p PG) Positions(ctx context.Context, did string) ([]Position, error) {
	rows, _ := p.DB.Query(ctx, `
with w as (select lower(wallet_address) a from users where id=$1),
s as (select basket, sum(sh) sh from (
  select basket, shares sh from mints where account=(select a from w)
  union all select basket, -shares from redeems where account=(select a from w)) x
  group by basket having sum(sh) > 0)
select s.basket, s.sh::text, n.nav_per_share::text, round(s.sh * n.nav_per_share / 1e18, 6)::text, n.ts
from s left join lateral (select nav_per_share, ts from nav_points where basket=s.basket order by ts desc limit 1) n on true
order by s.basket`, did)
	return collect(rows, func(r pgx.CollectableRow) (Position, error) {
		var x Position
		return x, r.Scan(&x.Basket, &x.SharesRaw, &x.NavPerShare, &x.ValueUSDC, &x.NavTs)
	})
}

func (p PG) History(ctx context.Context, did string) ([]Transfer, error) {
	// ponytail: last 200 only; add cursor pagination when a user outgrows it
	rows, _ := p.DB.Query(ctx, `
with w as (select lower(wallet_address) a from users where id=$1)
select * from (
  select 'mint' kind, tx_hash, log_index, basket, usdc::text, shares::text, block, ts from mints where account=(select a from w)
  union all
  select 'redeem', tx_hash, log_index, basket, usdc::text, shares::text, block, ts from redeems where account=(select a from w)) x
order by block desc, log_index desc limit 200`, did)
	return collect(rows, func(r pgx.CollectableRow) (Transfer, error) {
		var t Transfer
		return t, r.Scan(&t.Kind, &t.TxHash, &t.LogIndex, &t.Basket, &t.UsdcRaw, &t.SharesRaw, &t.Block, &t.Ts)
	})
}

func (p PG) PutDelegation(ctx context.Context, did, walletID, addr, quorumID string) (Delegation, error) {
	var d Delegation
	err := pgx.BeginFunc(ctx, p.DB, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `update delegations set active=false, revoked_at=now() where user_id=$1 and active`, did); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `update users set wallet_address=$2 where id=$1`, did, addr); err != nil {
			return err
		}
		rows, _ := tx.Query(ctx, `insert into delegations(user_id, privy_wallet_id, wallet_address, key_quorum_id)
values ($1,$2,$3,$4) returning `+delegationCols, did, walletID, addr, quorumID)
		var err error
		d, err = pgx.CollectExactlyOneRow(rows, scanDelegation)
		return err
	})
	return d, err
}

func (p PG) RevokeDelegation(ctx context.Context, did string) error {
	return pgx.BeginFunc(ctx, p.DB, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `update delegations set active=false, revoked_at=now() where user_id=$1 and active`, did)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		_, err = tx.Exec(ctx, `update schedules set active=false where user_id=$1 and active`, did)
		return err
	})
}

const scheduleCols = `id::text, basket, kind, amount_usdc::text, interval_seconds, next_run_at, stop_nav_per_share::text,
  redeem_bps, max_slippage_bps, max_per_run_usdc::text, active, created_at`

func scanSchedule(r pgx.CollectableRow) (Schedule, error) {
	var s Schedule
	return s, r.Scan(&s.ID, &s.Basket, &s.Kind, &s.AmountUSDC, &s.IntervalSeconds, &s.NextRunAt, &s.StopNavPerShare,
		&s.RedeemBps, &s.MaxSlippageBps, &s.MaxPerRunUSDC, &s.Active, &s.CreatedAt)
}

func (p PG) Schedules(ctx context.Context, did string) ([]Schedule, error) {
	rows, _ := p.DB.Query(ctx, `select `+scheduleCols+` from schedules where user_id=$1 order by created_at desc`, did)
	return collect(rows, scanSchedule)
}

func (p PG) CreateSchedule(ctx context.Context, did string, in ScheduleInput) (Schedule, error) {
	var basket, deleg bool
	if err := p.DB.QueryRow(ctx, `select exists(select 1 from baskets where address=$1),
  exists(select 1 from delegations where user_id=$2 and active)`, in.Basket, did).Scan(&basket, &deleg); err != nil {
		return Schedule{}, err
	}
	if !basket {
		return Schedule{}, ErrNoBasket
	}
	if !deleg {
		return Schedule{}, ErrNoDelegation
	}
	rows, _ := p.DB.Query(ctx, `insert into schedules(user_id, basket, kind, amount_usdc, interval_seconds, next_run_at,
  stop_nav_per_share, redeem_bps, max_slippage_bps, max_per_run_usdc)
values ($1,$2,$3,$4::numeric,$5,case when $3='recurring' then now() end,$6::numeric,$7,$8,$9::numeric)
returning `+scheduleCols,
		did, in.Basket, in.Kind, in.AmountUSDC, in.IntervalSeconds, in.StopNavPerShare, in.RedeemBps, in.MaxSlippageBps, in.MaxPerRunUSDC)
	return pgx.CollectExactlyOneRow(rows, scanSchedule)
}

func (p PG) DeactivateSchedule(ctx context.Context, did, id string) error {
	tag, err := p.DB.Exec(ctx, `update schedules set active=false where id=$2 and user_id=$1`, did, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func (p PG) Executions(ctx context.Context, did string) ([]Execution, error) {
	// ponytail: last 200 only; add cursor pagination when needed
	rows, _ := p.DB.Query(ctx, `select e.id::text, e.schedule_id::text, s.basket, e.slot, e.kind, e.status, e.tx_hash, e.error,
  e.amount::text, e.min_out::text, e.created_at, e.updated_at
from executions e join schedules s on s.id=e.schedule_id where s.user_id=$1 order by e.created_at desc limit 200`, did)
	return collect(rows, func(r pgx.CollectableRow) (Execution, error) {
		var e Execution
		return e, r.Scan(&e.ID, &e.ScheduleID, &e.Basket, &e.Slot, &e.Kind, &e.Status, &e.TxHash, &e.Error,
			&e.AmountRaw, &e.MinOutRaw, &e.CreatedAt, &e.UpdatedAt)
	})
}

// collect never returns a nil slice, so JSON lists are [] not null.
func collect[T any](rows pgx.Rows, fn pgx.RowToFunc[T]) ([]T, error) {
	xs, err := pgx.CollectRows(rows, fn)
	if xs == nil {
		xs = []T{}
	}
	return xs, err
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
