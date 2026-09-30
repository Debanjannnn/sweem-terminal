package api

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/planner"
)

type Store interface {
	EnsureUser(ctx context.Context, did string) error
	User(ctx context.Context, did string) (User, error)
	PutDelegation(ctx context.Context, did, walletID, addr, quorumID string) (Delegation, error)
	RevokeDelegation(ctx context.Context, did string) error
	Wallet(ctx context.Context, did string) (string, error)      // active delegation wallet, else ErrNoDelegation
	KnownWallet(ctx context.Context, did string) (string, error) // delegation wallet, else the last client-signing wallet, else ErrNotFound

	InsertExecution(ctx context.Context, did string, e NewExec) (string, error)
	FailExecution(ctx context.Context, id, code string) error
	Executions(ctx context.Context, did string) ([]Execution, error)
	Execution(ctx context.Context, did, id string) (Execution, error)

	BasketDefs(ctx context.Context, owner string) ([]Basket, error)                 // owner "" = public
	BasketDef(ctx context.Context, did, id string) (Basket, error)                  // public or did's own
	SaveBasket(ctx context.Context, did, id string, in BasketInput) (Basket, error) // id "" creates
	DeleteBasket(ctx context.Context, did, id string) error
	BasketFills(ctx context.Context, did, wallet, basketID string) ([]planner.ExecFills, error)
	Attributions(ctx context.Context, did, wallet string) (map[string][]planner.ExecFills, map[string]string, error)

	CreateOrder(ctx context.Context, did string, o Order, e NewExec) (orderID, execID string, err error)
	Orders(ctx context.Context, did string) ([]Order, error)
	Order(ctx context.Context, did, id string) (Order, error)
	SetOrderStatus(ctx context.Context, id, status string) error

	CreateStrategy(ctx context.Context, did string, in StrategyInput) (Strategy, error)
	Strategies(ctx context.Context, creator string, own bool) ([]Strategy, error)
	Strategy(ctx context.Context, id, did string) (Strategy, error) // public or did's own
	SetBacktest(ctx context.Context, id string, summary json.RawMessage) error

	HasPerplAccount(ctx context.Context, did, wallet string) (bool, error)
	OpenPosition(ctx context.Context, did, wallet string, st Strategy, principal string, plan planner.Plan, fee, creatorOwed string) (posID, execID string, err error)
	Positions(ctx context.Context, did string) ([]Position, error)
	Position(ctx context.Context, did, id string) (Position, planner.Strategy, error)
	Peers(ctx context.Context, did, exceptID string) ([]planner.Peer, error)
	LastWithdraw(ctx context.Context, posID string) (w LastWithdraw, found bool, err error)
	StartWithdraw(ctx context.Context, did, posID string, e NewExec) (string, error) // open or withdrawing (no run in flight) → withdrawing
	SettlePosition(ctx context.Context, posID, status string) error

	CreatePrepare(ctx context.Context, did string, p Prepare) (string, error)
	Prepare(ctx context.Context, did, id string) (Prepare, error)
	RecordPrepare(ctx context.Context, did string, p Prepare, status string, stepHashes []string) (string, error)

	Schedules(ctx context.Context, did string) ([]Schedule, error)
	CreateSchedule(ctx context.Context, did string, in ScheduleInput) (Schedule, error)
	DeactivateSchedule(ctx context.Context, did, id string) error
}

type User struct {
	ID            string      `json:"id"`
	WalletAddress *string     `json:"walletAddress"`
	CreatedAt     time.Time   `json:"createdAt"`
	Delegation    *Delegation `json:"delegation"`
}

type Delegation struct {
	ID            string     `json:"id"`
	WalletID      string     `json:"walletId"`
	WalletAddress string     `json:"walletAddress"`
	KeyQuorumID   string     `json:"keyQuorumId"`
	Active        bool       `json:"active"`
	CreatedAt     time.Time  `json:"createdAt"`
	RevokedAt     *time.Time `json:"revokedAt"`
}

// NewExec is an api-created execution (user_id set, slot 0, no schedule).
type NewExec struct {
	Kind       string
	Steps      []planner.Step
	BasketID   *string
	PositionID *string
}

type Execution struct {
	ID         string          `json:"id"`
	Kind       string          `json:"kind"`
	Signer     string          `json:"signer"` // executor | client
	Status     string          `json:"status"`
	StepDone   int             `json:"stepDone"`
	StepsTotal int             `json:"stepsTotal"`
	Steps      json.RawMessage `json:"steps"`
	TxHashes   []string        `json:"txHashes"`
	TxHash     *string         `json:"txHash"`
	Error      *string         `json:"error"`
	ScheduleID *string         `json:"scheduleId"`
	PositionID *string         `json:"positionId"`
	BasketID   *string         `json:"basketId"`
	CreatedAt  time.Time       `json:"createdAt"`
	UpdatedAt  time.Time       `json:"updatedAt"`
}

type Basket struct {
	ID        string        `json:"id"`
	Owner     string        `json:"owner"`
	Name      string        `json:"name"`
	Legs      []planner.Leg `json:"legs"`
	Public    bool          `json:"public"`
	CreatedAt time.Time     `json:"createdAt"`
}

type Order struct {
	ID          string    `json:"id"`
	Market      string    `json:"market"`
	Side        string    `json:"side"`
	Price       string    `json:"price"` // raw in store, human on the wire
	Size        string    `json:"size"`
	KuruOrderID *string   `json:"kuruOrderId"`
	Status      string    `json:"status"`
	ExecutionID *string   `json:"executionId"`
	CreatedAt   time.Time `json:"createdAt"`
}

func (o *Order) human(p *planner.Planner) {
	m := p.Dep.Kuru.Markets[o.Market]
	conv := func(raw string, prec uint64) string {
		v, err := decimal.NewFromString(raw)
		if err != nil || prec == 0 {
			return raw
		}
		return v.Div(decimal.NewFromInt(int64(prec))).String()
	}
	o.Price, o.Size = conv(o.Price, m.PricePrecision), conv(o.Size, m.SizePrecision)
}

type Strategy struct {
	ID            string  `json:"id"`
	Creator       string  `json:"creator"`
	CreatorWallet *string `json:"creatorWallet"`
	Name          string  `json:"name"`
	planner.Strategy
	Public    bool            `json:"public"`
	CreatedAt time.Time       `json:"createdAt"`
	Backtest  json.RawMessage `json:"backtest"`
	APY       *APY            `json:"apy"`
}

type Position struct {
	ID              string             `json:"id"`
	StrategyID      string             `json:"strategyId"`
	StrategyName    string             `json:"strategyName"`
	Asset           string             `json:"asset"`
	Wallet          string             `json:"wallet"`
	PrincipalUSDC   string             `json:"principalUsdc"`
	Status          string             `json:"status"`
	Legs            []planner.LegState `json:"legs"`
	OpenedAt        time.Time          `json:"openedAt"`
	ClosedAt        *time.Time         `json:"closedAt"`
	LastRebalanceAt *time.Time         `json:"lastRebalanceAt"`
}

type Schedule struct {
	ID              string     `json:"id"`
	Kind            string     `json:"kind"`
	Target          Target     `json:"target"`
	AmountUSDC      *string    `json:"amountUsdc"`
	IntervalSeconds *int64     `json:"intervalSeconds"`
	NextRunAt       *time.Time `json:"nextRunAt"`
	TriggerPrice    *string    `json:"triggerPrice"`
	SellBps         *int       `json:"sellBps"`
	DriftBps        *int       `json:"driftBps"`
	MaxSlippageBps  int        `json:"maxSlippageBps"`
	MaxPerRunUSDC   string     `json:"maxPerRunUsdc"`
	Active          bool       `json:"active"`
	CreatedAt       time.Time  `json:"createdAt"`
}

// Prepare is a client-signed plan waiting for its tx hashes.
type Prepare struct {
	ID          string
	Wallet      string
	Kind        string
	BasketID    *string
	Steps       []planner.Step
	Txs         []planner.TxRequest
	ExpiresAt   time.Time
	ExecutionID *string
}

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

func (p PG) Wallet(ctx context.Context, did string) (string, error) {
	var w string
	err := p.DB.QueryRow(ctx, `select lower(wallet_address) from delegations where user_id=$1 and active`, did).Scan(&w)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNoDelegation
	}
	return w, err
}

func (p PG) KnownWallet(ctx context.Context, did string) (string, error) {
	var w *string
	err := p.DB.QueryRow(ctx, `select coalesce((select lower(wallet_address) from delegations where user_id=$1 and active), lower(wallet_address)) from users where id=$1`, did).Scan(&w)
	if err == nil && w == nil {
		return "", ErrNotFound
	}
	if err != nil {
		return "", notFound(err)
	}
	return *w, nil
}

type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func insertExec(ctx context.Context, q querier, did string, e NewExec) (string, error) {
	steps, err := json.Marshal(e.Steps)
	if err != nil {
		return "", err
	}
	var id string
	err = q.QueryRow(ctx, `insert into executions(user_id, slot, kind, steps, basket_id, position_id)
values ($1, 0, $2, $3, $4, $5) returning id::text`, did, e.Kind, steps, e.BasketID, e.PositionID).Scan(&id)
	return id, err
}

func (p PG) InsertExecution(ctx context.Context, did string, e NewExec) (string, error) {
	return insertExec(ctx, p.DB, did, e)
}

// FailExecution marks a still-pending execution failed (the executor never saw it).
func (p PG) FailExecution(ctx context.Context, id, code string) error {
	_, err := p.DB.Exec(ctx, `update executions set status='failed', error=$2, updated_at=now() where id=$1 and status='pending'`, id, code)
	return err
}

const execSQL = `select e.id::text, e.kind, e.signer, e.status, e.step_done, coalesce(jsonb_array_length(e.steps),0), coalesce(e.steps,'[]'),
  e.tx_hashes, e.tx_hash, e.error, e.schedule_id::text, e.position_id::text, e.basket_id::text, e.created_at, e.updated_at
from executions e left join schedules s on s.id=e.schedule_id where coalesce(e.user_id, s.user_id)=$1`

func scanExec(r pgx.CollectableRow) (Execution, error) {
	var e Execution
	return e, r.Scan(&e.ID, &e.Kind, &e.Signer, &e.Status, &e.StepDone, &e.StepsTotal, &e.Steps, &e.TxHashes, &e.TxHash, &e.Error,
		&e.ScheduleID, &e.PositionID, &e.BasketID, &e.CreatedAt, &e.UpdatedAt)
}

func (p PG) Executions(ctx context.Context, did string) ([]Execution, error) {
	// ponytail: last 200 only; add cursor pagination when needed
	rows, _ := p.DB.Query(ctx, execSQL+` order by e.created_at desc limit 200`, did)
	return collect(rows, scanExec)
}

func (p PG) Execution(ctx context.Context, did, id string) (Execution, error) {
	rows, _ := p.DB.Query(ctx, execSQL+` and e.id=$2`, did, id)
	e, err := pgx.CollectExactlyOneRow(rows, scanExec)
	return e, notFound(err)
}

const basketCols = `id::text, owner, name, legs, public, created_at`

func scanBasket(r pgx.CollectableRow) (Basket, error) {
	var b Basket
	return b, r.Scan(&b.ID, &b.Owner, &b.Name, &b.Legs, &b.Public, &b.CreatedAt)
}

func (p PG) BasketDefs(ctx context.Context, owner string) ([]Basket, error) {
	rows, _ := p.DB.Query(ctx, `select `+basketCols+` from basket_defs where ($1='' and public) or owner=$1 order by created_at desc limit 500`, owner)
	return collect(rows, scanBasket)
}

func (p PG) BasketDef(ctx context.Context, did, id string) (Basket, error) {
	rows, _ := p.DB.Query(ctx, `select `+basketCols+` from basket_defs where id=$2 and (public or owner=$1)`, did, id)
	b, err := pgx.CollectExactlyOneRow(rows, scanBasket)
	return b, notFound(err)
}

func (p PG) SaveBasket(ctx context.Context, did, id string, in BasketInput) (Basket, error) {
	legs, _ := json.Marshal(in.Legs)
	var rows pgx.Rows
	if id == "" {
		rows, _ = p.DB.Query(ctx, `insert into basket_defs(owner, name, legs, public) values ($1,$2,$3,$4) returning `+basketCols, did, in.Name, legs, in.Public)
	} else {
		rows, _ = p.DB.Query(ctx, `update basket_defs set name=$3, legs=$4, public=$5 where id=$2 and owner=$1 returning `+basketCols, did, id, in.Name, legs, in.Public)
	}
	b, err := pgx.CollectExactlyOneRow(rows, scanBasket)
	return b, notFound(err)
}

func (p PG) DeleteBasket(ctx context.Context, did, id string) error {
	tag, err := p.DB.Exec(ctx, `delete from basket_defs where id=$2 and owner=$1`, did, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// fillsSQL: confirmed basket executions with the indexer's taker fills (Kuru Trade size) per tx of ours.
const fillsSQL = `select e.basket_id::text, coalesce(b.name,''), e.steps, e.tx_hashes,
  coalesce((select jsonb_object_agg(lower(t.tx_hash), t.sz) from (select tx_hash, sum(size)::text sz from user_trades
    where tx_hash = any(e.tx_hashes) and role='taker' and lower(wallet)=$2 group by tx_hash) t), '{}')
from executions e left join basket_defs b on b.id=e.basket_id
where e.user_id=$1 and e.basket_id is not null and e.status='confirmed' and ($3='' or e.basket_id::text=$3)
order by e.created_at`

func (p PG) fills(ctx context.Context, did, wallet, basketID string) (map[string][]planner.ExecFills, map[string]string, error) {
	rows, _ := p.DB.Query(ctx, fillsSQL, did, wallet, basketID)
	out, names := map[string][]planner.ExecFills{}, map[string]string{}
	var id, name string
	var e planner.ExecFills
	_, err := pgx.ForEachRow(rows, []any{&id, &name, &e.Steps, &e.TxHashes, &e.Fills}, func() error {
		out[id], names[id] = append(out[id], e), name
		e = planner.ExecFills{}
		return nil
	})
	return out, names, err
}

func (p PG) BasketFills(ctx context.Context, did, wallet, basketID string) ([]planner.ExecFills, error) {
	m, _, err := p.fills(ctx, did, wallet, basketID)
	return m[basketID], err
}

func (p PG) Attributions(ctx context.Context, did, wallet string) (map[string][]planner.ExecFills, map[string]string, error) {
	return p.fills(ctx, did, wallet, "")
}

func (p PG) CreateOrder(ctx context.Context, did string, o Order, e NewExec) (orderID, execID string, err error) {
	err = pgx.BeginFunc(ctx, p.DB, func(tx pgx.Tx) error {
		if execID, err = insertExec(ctx, tx, did, e); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `insert into orders(user_id, market, side, price, size, execution_id) values ($1,$2,$3,$4::numeric,$5::numeric,$6) returning id::text`,
			did, o.Market, o.Side, o.Price, o.Size, execID).Scan(&orderID)
	})
	return
}

// orderSQL resolves the Kuru order id from the indexer's OrderCreated row for the placing tx, and a later cancel.
const orderSQL = `select o.id::text, o.market, o.side, o.price::text, o.size::text, c.order_id::text,
  case when x.order_id is not null then 'cancelled' else o.status end, o.execution_id::text, o.created_at
from orders o left join executions e on e.id=o.execution_id
left join lateral (select order_id from user_orders_events where kind='created' and tx_hash = any(e.tx_hashes) order by log_index desc limit 1) c on true
left join lateral (select order_id from user_orders_events where kind='canceled' and order_id=c.order_id limit 1) x on true
where o.user_id=$1`

func scanOrder(r pgx.CollectableRow) (Order, error) {
	var o Order
	return o, r.Scan(&o.ID, &o.Market, &o.Side, &o.Price, &o.Size, &o.KuruOrderID, &o.Status, &o.ExecutionID, &o.CreatedAt)
}

func (p PG) Orders(ctx context.Context, did string) ([]Order, error) {
	rows, _ := p.DB.Query(ctx, orderSQL+` order by o.created_at desc limit 200`, did)
	return collect(rows, scanOrder)
}

func (p PG) Order(ctx context.Context, did, id string) (Order, error) {
	rows, _ := p.DB.Query(ctx, orderSQL+` and o.id=$2`, did, id)
	o, err := pgx.CollectExactlyOneRow(rows, scanOrder)
	return o, notFound(err)
}

func (p PG) SetOrderStatus(ctx context.Context, id, status string) error {
	_, err := p.DB.Exec(ctx, `update orders set status=$2 where id=$1`, id, status)
	return err
}

const strategyCols = `s.id::text, s.creator, u.wallet_address, s.name, s.asset, s.sources, s.rules, s.public, s.created_at, s.backtest
from strategies s join users u on u.id=s.creator`

func scanStrategy(r pgx.CollectableRow) (Strategy, error) {
	var s Strategy
	return s, r.Scan(&s.ID, &s.Creator, &s.CreatorWallet, &s.Name, &s.Asset, &s.Sources, &s.Rules, &s.Public, &s.CreatedAt, &s.Backtest)
}

func (p PG) CreateStrategy(ctx context.Context, did string, in StrategyInput) (Strategy, error) {
	pub := in.Public == nil || *in.Public
	src, _ := json.Marshal(in.Sources)
	rules, _ := json.Marshal(in.Rules)
	var id string
	if err := p.DB.QueryRow(ctx, `insert into strategies(creator, name, asset, sources, rules, public) values ($1,$2,$3,$4,$5,$6) returning id::text`,
		did, in.Name, in.Asset, src, rules, pub).Scan(&id); err != nil {
		return Strategy{}, err
	}
	return p.Strategy(ctx, id, did)
}

// Strategies: public ones (creator filter optional), or every one of creator's when own.
func (p PG) Strategies(ctx context.Context, creator string, own bool) ([]Strategy, error) {
	rows, _ := p.DB.Query(ctx, `select `+strategyCols+` where ($2 or s.public) and ($1='' or s.creator=$1) order by s.created_at desc limit 500`, creator, own)
	return collect(rows, scanStrategy)
}

func (p PG) Strategy(ctx context.Context, id, did string) (Strategy, error) {
	rows, _ := p.DB.Query(ctx, `select `+strategyCols+` where s.id=$1 and (s.public or s.creator=$2)`, id, did)
	s, err := pgx.CollectExactlyOneRow(rows, scanStrategy)
	return s, notFound(err)
}

func (p PG) SetBacktest(ctx context.Context, id string, summary json.RawMessage) error {
	_, err := p.DB.Exec(ctx, `update strategies set backtest=$2 where id=$1`, id, summary)
	return err
}

// HasPerplAccount: the indexer saw AccountCreated, or a confirmed plan of ours created it (indexer lag).
func (p PG) HasPerplAccount(ctx context.Context, did, wallet string) (bool, error) {
	var ok bool
	err := p.DB.QueryRow(ctx, `select exists(select 1 from perpl_accounts where lower(wallet)=$2)
  or exists(select 1 from executions where user_id=$1 and status in ('confirmed','failed') and step_done > 0
    and steps @> '[{"action":"perpl.createAccount"}]'
    and step_done > (select min(i) - 1 from jsonb_array_elements(steps) with ordinality a(s, i) where s->>'action'='perpl.createAccount'))`, did, wallet).Scan(&ok)
	return ok, err
}

func (p PG) OpenPosition(ctx context.Context, did, wallet string, st Strategy, principal string, plan planner.Plan, fee, creatorOwed string) (posID, execID string, err error) {
	legs, _ := json.Marshal(plan.Legs)
	err = pgx.BeginFunc(ctx, p.DB, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `insert into strategy_positions(strategy_id, user_id, wallet, principal_usdc, legs) values ($1,$2,$3,$4::numeric,$5) returning id::text`,
			st.ID, did, wallet, principal, legs).Scan(&posID); err != nil {
			return err
		}
		if execID, err = insertExec(ctx, tx, did, NewExec{Kind: "deploy", Steps: plan.Steps, PositionID: &posID}); err != nil {
			return err
		}
		var creator *string
		if st.Creator != did {
			creator = &st.Creator
		}
		if _, err := tx.Exec(ctx, `update strategy_positions set execution_id=$2 where id=$1`, posID, execID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `insert into fee_ledger(execution_id, user_id, strategy_id, creator, fee_usdc, creator_owed_usdc) values ($1,$2,$3,$4,$5::numeric,$6::numeric)`,
			execID, did, st.ID, creator, fee, creatorOwed)
		return err
	})
	return
}

const positionCols = `p.id::text, p.strategy_id::text, s.name, s.asset, p.wallet, p.principal_usdc::text, p.status, p.legs,
  p.opened_at, p.closed_at, p.last_rebalance_at from strategy_positions p join strategies s on s.id=p.strategy_id`

func scanPosition(r pgx.CollectableRow) (Position, error) {
	var x Position
	return x, r.Scan(&x.ID, &x.StrategyID, &x.StrategyName, &x.Asset, &x.Wallet, &x.PrincipalUSDC, &x.Status, &x.Legs, &x.OpenedAt, &x.ClosedAt, &x.LastRebalanceAt)
}

func (p PG) Positions(ctx context.Context, did string) ([]Position, error) {
	rows, _ := p.DB.Query(ctx, `select `+positionCols+` where p.user_id=$1 order by p.opened_at desc limit 200`, did)
	return collect(rows, scanPosition)
}

func (p PG) Position(ctx context.Context, did, id string) (Position, planner.Strategy, error) {
	var st planner.Strategy
	rows, _ := p.DB.Query(ctx, `select `+positionCols+` where p.user_id=$1 and p.id=$2`, did, id)
	pos, err := pgx.CollectExactlyOneRow(rows, scanPosition)
	if err != nil {
		return pos, st, notFound(err)
	}
	err = p.DB.QueryRow(ctx, `select asset, sources, rules from strategies where id=$1`, pos.StrategyID).Scan(&st.Asset, &st.Sources, &st.Rules)
	return pos, st, err
}

func (p PG) Peers(ctx context.Context, did, exceptID string) ([]planner.Peer, error) {
	rows, _ := p.DB.Query(ctx, `select s.asset, p.legs from strategy_positions p join strategies s on s.id=p.strategy_id
where p.user_id=$1 and p.id<>$2 and p.status in ('opening','open','closing')`, did, exceptID)
	return collect(rows, func(r pgx.CollectableRow) (planner.Peer, error) {
		var x planner.Peer
		return x, r.Scan(&x.Asset, &x.Legs)
	})
}

// LastWithdraw is a position's latest withdraw execution.
type LastWithdraw struct {
	Status   string
	Steps    []planner.Step
	StepDone int
}

func (p PG) LastWithdraw(ctx context.Context, posID string) (LastWithdraw, bool, error) {
	var w LastWithdraw
	err := p.DB.QueryRow(ctx, `select status, steps, step_done from executions where position_id=$1 and kind='withdraw'
order by created_at desc limit 1`, posID).Scan(&w.Status, &w.Steps, &w.StepDone)
	if errors.Is(err, pgx.ErrNoRows) {
		return w, false, nil
	}
	return w, err == nil, err
}

// StartWithdraw claims the position (row lock): open, or withdrawing with no withdraw run in flight.
func (p PG) StartWithdraw(ctx context.Context, did, posID string, e NewExec) (execID string, err error) {
	err = pgx.BeginFunc(ctx, p.DB, func(tx pgx.Tx) error {
		var status string
		var busy bool
		if err := tx.QueryRow(ctx, `select status, exists(select 1 from executions where position_id=$1 and kind='withdraw' and status in ('pending','submitted'))
from strategy_positions where id=$1 and user_id=$2 for update`, posID, did).Scan(&status, &busy); err != nil {
			return notFound(err)
		}
		if status != "open" && status != "withdrawing" {
			return ErrNotOpen
		}
		if busy {
			return ErrWithdrawBusy
		}
		if _, err := tx.Exec(ctx, `update strategy_positions set status='withdrawing' where id=$1`, posID); err != nil {
			return err
		}
		execID, err = insertExec(ctx, tx, did, e)
		return err
	})
	return
}

func (p PG) SettlePosition(ctx context.Context, posID, status string) error {
	_, err := p.DB.Exec(ctx, `update strategy_positions set status=$2, closed_at=case when $2='closed' then now() end where id=$1`, posID, status)
	return err
}

const scheduleCols = `id::text, kind, coalesce(target_type,'basket'), coalesce(target_id, basket, ''), amount_usdc::text, interval_seconds, next_run_at,
  trigger_price::text, sell_bps, drift_bps, max_slippage_bps, max_per_run_usdc::text, active, created_at`

func scanSchedule(r pgx.CollectableRow) (Schedule, error) {
	var s Schedule
	return s, r.Scan(&s.ID, &s.Kind, &s.Target.Type, &s.Target.ID, &s.AmountUSDC, &s.IntervalSeconds, &s.NextRunAt,
		&s.TriggerPrice, &s.SellBps, &s.DriftBps, &s.MaxSlippageBps, &s.MaxPerRunUSDC, &s.Active, &s.CreatedAt)
}

func (p PG) Schedules(ctx context.Context, did string) ([]Schedule, error) {
	rows, _ := p.DB.Query(ctx, `select `+scheduleCols+` from schedules where user_id=$1 order by created_at desc`, did)
	return collect(rows, scanSchedule)
}

func (p PG) CreateSchedule(ctx context.Context, did string, in ScheduleInput) (Schedule, error) {
	var target, deleg bool
	if err := p.DB.QueryRow(ctx, `select case $2
    when 'basket' then exists(select 1 from basket_defs where id::text=$3 and (public or owner=$1))
    when 'position' then exists(select 1 from strategy_positions where id::text=$3 and user_id=$1)
    else true end,
  exists(select 1 from delegations where user_id=$1 and active)`, did, in.Target.Type, in.Target.ID).Scan(&target, &deleg); err != nil {
		return Schedule{}, err
	}
	if !target {
		return Schedule{}, ErrNotFound
	}
	if !deleg {
		return Schedule{}, ErrNoDelegation
	}
	rows, _ := p.DB.Query(ctx, `insert into schedules(user_id, kind, target_type, target_id, amount_usdc, interval_seconds, next_run_at,
  trigger_price, sell_bps, drift_bps, max_slippage_bps, max_per_run_usdc)
values ($1,$2,$3,$4,$5::numeric,$6,case when $6::bigint is not null then now() end,$7::numeric,$8,$9,$10,$11::numeric)
returning `+scheduleCols,
		did, in.Kind, in.Target.Type, in.Target.ID, in.AmountUSDC, in.IntervalSeconds, in.TriggerPrice, in.SellBps, in.DriftBps, in.MaxSlippageBps, in.MaxPerRunUSDC)
	return pgx.CollectExactlyOneRow(rows, scanSchedule)
}

func (p PG) DeactivateSchedule(ctx context.Context, did, id string) error {
	tag, err := p.DB.Exec(ctx, `update schedules set active=false where id=$2 and user_id=$1`, did, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
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

func (p PG) CreatePrepare(ctx context.Context, did string, x Prepare) (string, error) {
	steps, _ := json.Marshal(x.Steps)
	txs, _ := json.Marshal(x.Txs)
	if _, err := p.DB.Exec(ctx, `update users set wallet_address=$2 where id=$1 and wallet_address is null`, did, x.Wallet); err != nil {
		return "", err
	}
	var id string
	err := p.DB.QueryRow(ctx, `insert into trade_prepares(user_id, wallet, kind, basket_id, steps, txs, expires_at) values ($1,$2,$3,$4,$5,$6,$7) returning id::text`,
		did, x.Wallet, x.Kind, x.BasketID, steps, txs, x.ExpiresAt).Scan(&id)
	return id, err
}

func (p PG) Prepare(ctx context.Context, did, id string) (Prepare, error) {
	x := Prepare{ID: id}
	err := p.DB.QueryRow(ctx, `select wallet, kind, basket_id::text, steps, txs, expires_at, execution_id::text from trade_prepares where id=$1 and user_id=$2`, id, did).
		Scan(&x.Wallet, &x.Kind, &x.BasketID, &x.Steps, &x.Txs, &x.ExpiresAt, &x.ExecutionID)
	return x, notFound(err)
}

// RecordPrepare inserts the client-signed run as an execution (tx_hashes[i] = plan step i) once per prepare.
func (p PG) RecordPrepare(ctx context.Context, did string, x Prepare, status string, stepHashes []string) (execID string, err error) {
	steps, _ := json.Marshal(x.Steps)
	err = pgx.BeginFunc(ctx, p.DB, func(tx pgx.Tx) error {
		var prev *string
		if err := tx.QueryRow(ctx, `select execution_id::text from trade_prepares where id=$1 for update`, x.ID).Scan(&prev); err != nil {
			return err
		}
		if prev != nil {
			execID = *prev
			return nil
		}
		var last *string
		if len(stepHashes) > 0 {
			last = &stepHashes[len(stepHashes)-1]
		}
		if err := tx.QueryRow(ctx, `insert into executions(user_id, slot, kind, steps, basket_id, status, step_done, tx_hashes, tx_hash, signer)
values ($1, 0, $2, $3, $4, $5, $6, $7, $8, 'client') returning id::text`, did, x.Kind, steps, x.BasketID, status, len(stepHashes), stepHashes, last).Scan(&execID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `update trade_prepares set execution_id=$2 where id=$1`, x.ID, execID)
		return err
	})
	return
}
