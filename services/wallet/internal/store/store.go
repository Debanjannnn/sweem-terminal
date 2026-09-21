package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/policy"
)

var ErrNotFound = errors.New("store: not found")

type Store struct{ pool *pgxpool.Pool }

func New(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("store: connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: ping: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }

func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

func (s *Store) UpsertUser(ctx context.Context, did, wallet, privyWalletID string) (User, error) {
	const q = `
		INSERT INTO users (id, wallet_address, privy_wallet_id)
		VALUES ($1, NULLIF($2, ''), NULLIF($3, ''))
		ON CONFLICT (id) DO UPDATE SET
			wallet_address  = COALESCE(EXCLUDED.wallet_address, users.wallet_address),
			privy_wallet_id = COALESCE(EXCLUDED.privy_wallet_id, users.privy_wallet_id)
		RETURNING id, COALESCE(wallet_address, ''), COALESCE(privy_wallet_id, ''), created_at, first_funded_at`
	var u User
	err := s.pool.QueryRow(ctx, q, did, strings.ToLower(strings.TrimSpace(wallet)), privyWalletID).
		Scan(&u.ID, &u.WalletAddress, &u.PrivyWalletID, &u.CreatedAt, &u.FirstFundedAt)
	return u, err
}

func (s *Store) User(ctx context.Context, did string) (User, error) {
	const q = `SELECT id, COALESCE(wallet_address, ''), COALESCE(privy_wallet_id, ''), created_at, first_funded_at
	           FROM users WHERE id = $1`
	var u User
	err := s.pool.QueryRow(ctx, q, did).
		Scan(&u.ID, &u.WalletAddress, &u.PrivyWalletID, &u.CreatedAt, &u.FirstFundedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

// PutDelegation writes the delegation and its audit row in one transaction; a
// fresh delegation clears any earlier revocation.
func (s *Store) PutDelegation(ctx context.Context, d policy.Delegation) (policy.Delegation, error) {
	d.Venues, d.Markets = normalizeList(d.Venues), normalizeList(d.Markets)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return d, err
	}
	defer tx.Rollback(ctx)

	const q = `
		INSERT INTO delegations (user_id, signer_id, policy_id, venues, markets,
		                         max_notional_per_slice, max_notional_total, expires_at, revoked_at)
		VALUES ($1, $2, NULLIF($3, ''), $4, $5, $6::numeric, $7::numeric, $8, NULL)
		ON CONFLICT (user_id) DO UPDATE SET
			signer_id = EXCLUDED.signer_id, policy_id = EXCLUDED.policy_id,
			venues = EXCLUDED.venues, markets = EXCLUDED.markets,
			max_notional_per_slice = EXCLUDED.max_notional_per_slice,
			max_notional_total = EXCLUDED.max_notional_total,
			expires_at = EXCLUDED.expires_at, revoked_at = NULL`
	if _, err := tx.Exec(ctx, q, d.UserID, d.SignerID, d.PolicyID, d.Venues, d.Markets,
		d.MaxNotionalPerSlice, d.MaxNotionalTotal, d.ExpiresAt); err != nil {
		return d, fmt.Errorf("store: put delegation: %w", err)
	}
	if err := audit(ctx, tx, d.UserID, "granted", d); err != nil {
		return d, err
	}
	return d, tx.Commit(ctx)
}

func (s *Store) Delegation(ctx context.Context, userID string) (policy.Delegation, error) {
	const q = `SELECT user_id, signer_id, COALESCE(policy_id, ''), venues, markets,
	                  max_notional_per_slice::text, max_notional_total::text, expires_at, revoked_at
	           FROM delegations WHERE user_id = $1`
	var d policy.Delegation
	err := s.pool.QueryRow(ctx, q, userID).Scan(&d.UserID, &d.SignerID, &d.PolicyID, &d.Venues, &d.Markets,
		&d.MaxNotionalPerSlice, &d.MaxNotionalTotal, &d.ExpiresAt, &d.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return policy.Delegation{}, ErrNotFound
	}
	return d, err
}

func (s *Store) RevokeDelegation(ctx context.Context, userID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `UPDATE delegations SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err := audit(ctx, tx, userID, "revoked", nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func audit(ctx context.Context, tx pgx.Tx, userID, action string, detail any) error {
	body := []byte("{}")
	if detail != nil {
		b, err := json.Marshal(detail)
		if err != nil {
			return err
		}
		body = b
	}
	_, err := tx.Exec(ctx, `INSERT INTO delegation_audit (user_id, action, detail) VALUES ($1, $2, $3)`,
		userID, action, body)
	return err
}

// SpentTotal is the notional already consumed under the delegation. A failed
// execution moved nothing, so it does not count.
func (s *Store) SpentTotal(ctx context.Context, userID string) (string, error) {
	var total string
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(size), 0)::text FROM intents WHERE user_id = $1 AND state <> 'failed'`,
		userID).Scan(&total)
	return total, err
}

// InsertIntent reports false when the key already existed: that is the
// double-spend guard, and a retry of the same intent is not a second order.
func (s *Store) InsertIntent(ctx context.Context, in Intent) (bool, error) {
	const q = `
		INSERT INTO intents (intent_key, user_id, market_id, venue, side, order_type, size,
		                     limit_price, max_slippage_bps, chain_id, state)
		VALUES ($1,$2,$3,$4,$5,$6,$7::numeric, NULLIF($8,'')::numeric, $9, $10, $11)
		ON CONFLICT (intent_key) DO NOTHING`
	tag, err := s.pool.Exec(ctx, q, in.Key, in.UserID, in.MarketID, in.Venue, in.Side, in.OrderType,
		in.Size, in.LimitPrice, in.MaxSlippageBps, in.ChainID, orDefault(in.State, "pending"))
	if err != nil {
		return false, fmt.Errorf("store: insert intent: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

const intentCols = `intent_key, user_id, market_id, venue, side, order_type, size::text,
                    COALESCE(limit_price::text, ''), max_slippage_bps, chain_id, state, created_at`

func scanIntent(row pgx.Row) (Intent, error) {
	var in Intent
	err := row.Scan(&in.Key, &in.UserID, &in.MarketID, &in.Venue, &in.Side, &in.OrderType, &in.Size,
		&in.LimitPrice, &in.MaxSlippageBps, &in.ChainID, &in.State, &in.CreatedAt)
	return in, err
}

func (s *Store) Intent(ctx context.Context, key string) (Intent, error) {
	in, err := scanIntent(s.pool.QueryRow(ctx, `SELECT `+intentCols+` FROM intents WHERE intent_key = $1`, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return Intent{}, ErrNotFound
	}
	return in, err
}

func (s *Store) Intents(ctx context.Context, userID string, limit int) ([]Intent, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+intentCols+` FROM intents WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2`,
		userID, clampLimit(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Intent{}
	for rows.Next() {
		in, err := scanIntent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

func (s *Store) SetIntentState(ctx context.Context, key, state string) error {
	_, err := s.pool.Exec(ctx, `UPDATE intents SET state = $2 WHERE intent_key = $1`, key, state)
	return err
}

func (s *Store) RecordExecution(ctx context.Context, e Execution) error {
	const q = `
		INSERT INTO executions (intent_key, tx_hash, confirmation, error, submitted_at, settled_at)
		VALUES ($1,$2,$3,$4, now(), $5)
		ON CONFLICT (intent_key) DO UPDATE SET
			tx_hash = EXCLUDED.tx_hash, confirmation = EXCLUDED.confirmation,
			error = EXCLUDED.error, settled_at = EXCLUDED.settled_at`
	_, err := s.pool.Exec(ctx, q, e.IntentKey, e.TxHash, orDefault(e.Confirmation, "pending"), e.Error, e.SettledAt)
	return err
}

func (s *Store) Execution(ctx context.Context, key string) (Execution, error) {
	var e Execution
	err := s.pool.QueryRow(ctx,
		`SELECT intent_key, tx_hash, confirmation, error, submitted_at, settled_at FROM executions WHERE intent_key = $1`,
		key).Scan(&e.IntentKey, &e.TxHash, &e.Confirmation, &e.Error, &e.SubmittedAt, &e.SettledAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Execution{}, ErrNotFound
	}
	return e, err
}

func (s *Store) Executions(ctx context.Context, userID string, limit int) ([]Execution, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT e.intent_key, e.tx_hash, e.confirmation, e.error, e.submitted_at, e.settled_at
		FROM executions e JOIN intents i ON i.intent_key = e.intent_key
		WHERE i.user_id = $1 ORDER BY e.submitted_at DESC LIMIT $2`, userID, clampLimit(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Execution{}
	for rows.Next() {
		var e Execution
		if err := rows.Scan(&e.IntentKey, &e.TxHash, &e.Confirmation, &e.Error, &e.SubmittedAt, &e.SettledAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// CreateSchedule is idempotent on (user_id, idempotency_key): a retry returns
// the schedule that already exists rather than a second one.
func (s *Store) CreateSchedule(ctx context.Context, sc Schedule, slices []Slice) (Schedule, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Schedule{}, err
	}
	defer tx.Rollback(ctx)

	const q = `
		INSERT INTO schedules (user_id, kind, market_id, venue, side, total, slice_count,
		                       window_seconds, cadence_seconds, levels, idempotency_key)
		VALUES ($1,$2,$3,$4,$5,$6::numeric,$7,$8,$9,$10,$11)
		ON CONFLICT (user_id, idempotency_key) DO NOTHING
		RETURNING id, state, created_at`
	err = tx.QueryRow(ctx, q, sc.UserID, sc.Kind, sc.MarketID, sc.Venue, sc.Side, sc.Total, sc.SliceCount,
		sc.WindowSeconds, sc.CadenceSeconds, nullableJSON(sc.Levels), sc.IdempotencyKey).
		Scan(&sc.ID, &sc.State, &sc.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return s.scheduleBy(ctx, `user_id = $1 AND idempotency_key = $2`, sc.UserID, sc.IdempotencyKey)
	}
	if err != nil {
		return Schedule{}, fmt.Errorf("store: insert schedule: %w", err)
	}

	for _, sl := range slices {
		if _, err := tx.Exec(ctx,
			`INSERT INTO slices (schedule_id, slice_index, due_at, amount) VALUES ($1,$2,$3,$4::numeric)`,
			sc.ID, sl.Index, sl.DueAt, sl.Amount); err != nil {
			return Schedule{}, fmt.Errorf("store: insert slice %d: %w", sl.Index, err)
		}
		sl.ScheduleID, sl.State = sc.ID, "pending"
		sc.Slices = append(sc.Slices, sl)
	}
	return sc, tx.Commit(ctx)
}

const scheduleCols = `id::text, user_id, kind, market_id, venue, side, total::text, slice_count,
                      window_seconds, cadence_seconds, COALESCE(levels, 'null'::jsonb), state,
                      idempotency_key, created_at`

func scanSchedule(row pgx.Row) (Schedule, error) {
	var sc Schedule
	err := row.Scan(&sc.ID, &sc.UserID, &sc.Kind, &sc.MarketID, &sc.Venue, &sc.Side, &sc.Total,
		&sc.SliceCount, &sc.WindowSeconds, &sc.CadenceSeconds, &sc.Levels, &sc.State,
		&sc.IdempotencyKey, &sc.CreatedAt)
	return sc, err
}

func (s *Store) scheduleBy(ctx context.Context, where string, args ...any) (Schedule, error) {
	sc, err := scanSchedule(s.pool.QueryRow(ctx, `SELECT `+scheduleCols+` FROM schedules WHERE `+where, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return Schedule{}, ErrNotFound
	}
	if err != nil {
		return Schedule{}, err
	}
	sc.Slices, err = s.slices(ctx, sc.ID)
	return sc, err
}

func (s *Store) Schedule(ctx context.Context, id, userID string) (Schedule, error) {
	return s.scheduleBy(ctx, `id = $1 AND user_id = $2`, id, userID)
}

func (s *Store) Schedules(ctx context.Context, userID string) ([]Schedule, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+scheduleCols+` FROM schedules WHERE user_id = $1 ORDER BY created_at DESC LIMIT 200`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Schedule{}
	for rows.Next() {
		sc, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

func (s *Store) slices(ctx context.Context, scheduleID string) ([]Slice, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT schedule_id::text, slice_index, due_at, amount::text, state, COALESCE(intent_key, ''), skip_reason
		FROM slices WHERE schedule_id = $1 ORDER BY slice_index`, scheduleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Slice{}
	for rows.Next() {
		var sl Slice
		if err := rows.Scan(&sl.ScheduleID, &sl.Index, &sl.DueAt, &sl.Amount, &sl.State, &sl.IntentKey, &sl.SkipReason); err != nil {
			return nil, err
		}
		out = append(out, sl)
	}
	return out, rows.Err()
}

func (s *Store) CancelSchedule(ctx context.Context, id, userID string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE schedules SET state = 'cancelled' WHERE id = $1 AND user_id = $2 AND state IN ('active','paused_revoked')`,
		id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	_, err = s.pool.Exec(ctx,
		`UPDATE slices SET state = 'skipped', skip_reason = 'The schedule was cancelled before this slice came due.'
		 WHERE schedule_id = $1 AND state = 'pending'`, id)
	return err
}

func (s *Store) PauseScheduleRevoked(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `UPDATE schedules SET state = 'paused_revoked' WHERE id = $1 AND state = 'active'`, id)
	return err
}

// DispatchTarget returns the slice and its schedule, scoped to the owner.
func (s *Store) DispatchTarget(ctx context.Context, scheduleID string, index int, userID string) (Schedule, Slice, error) {
	var sc Schedule
	var sl Slice
	err := s.pool.QueryRow(ctx, `
		SELECT sc.id::text, sc.user_id, sc.kind, sc.market_id, sc.venue, sc.side, sc.total::text, sc.slice_count,
		       sc.window_seconds, sc.cadence_seconds, COALESCE(sc.levels, 'null'::jsonb), sc.state,
		       sc.idempotency_key, sc.created_at,
		       sl.slice_index, sl.due_at, sl.amount::text, sl.state, COALESCE(sl.intent_key, ''), sl.skip_reason,
		       sl.generation
		FROM slices sl JOIN schedules sc ON sc.id = sl.schedule_id
		WHERE sl.schedule_id = $1 AND sl.slice_index = $2 AND sc.user_id = $3`,
		scheduleID, index, userID).
		Scan(&sc.ID, &sc.UserID, &sc.Kind, &sc.MarketID, &sc.Venue, &sc.Side, &sc.Total, &sc.SliceCount,
			&sc.WindowSeconds, &sc.CadenceSeconds, &sc.Levels, &sc.State, &sc.IdempotencyKey, &sc.CreatedAt,
			&sl.Index, &sl.DueAt, &sl.Amount, &sl.State, &sl.IntentKey, &sl.SkipReason, &sl.Generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return Schedule{}, Slice{}, ErrNotFound
	}
	sl.ScheduleID = scheduleID
	return sc, sl, err
}

// MarkSlice records the outcome and completes the schedule once nothing is left.
func (s *Store) MarkSlice(ctx context.Context, scheduleID string, index int, state, intentKey, skipReason string) error {
	if _, err := s.pool.Exec(ctx, `
		UPDATE slices SET state = $3, intent_key = NULLIF($4, ''), skip_reason = $5
		WHERE schedule_id = $1 AND slice_index = $2`, scheduleID, index, state, intentKey, skipReason); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE schedules SET state = 'completed'
		WHERE id = $1 AND state = 'active'
		  AND NOT EXISTS (SELECT 1 FROM slices WHERE schedule_id = $1 AND state = 'pending')`, scheduleID)
	return err
}

func nullableJSON(b json.RawMessage) any {
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	return []byte(b)
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

const maxLimit = 200

func clampLimit(n int) int {
	switch {
	case n <= 0:
		return 50
	case n > maxLimit:
		return maxLimit
	}
	return n
}

// normalizeList lowercases, trims, dedupes and sorts an allowlist so two
// delegations that permit the same thing are stored the same way.
func normalizeList(vs []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, v := range vs {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
