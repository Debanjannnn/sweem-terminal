package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/policy"
)

var ErrNotFound = errors.New("store: not found")

// ErrWalletTaken: another identity already holds this wallet address.
var ErrWalletTaken = errors.New("store: wallet address belongs to another user")

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
	if pe := (*pgconn.PgError)(nil); errors.As(err, &pe) && pe.Code == "23505" {
		return User{}, ErrWalletTaken
	}
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
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(size), 0)::text FROM intents
		WHERE user_id = $1 AND state <> 'failed' AND order_type IN ('market', 'limit')`,
		userID).Scan(&total)
	return total, err
}

// InsertIntent reports false when the key already existed: that is the
// double-spend guard, and a retry of the same intent is not a second order.
func (s *Store) InsertIntent(ctx context.Context, in Intent) (bool, error) {
	const q = `
		INSERT INTO intents (intent_key, user_id, market_id, venue, side, order_type, size,
		                     limit_price, quantity, tif, post_only, token, slot_idx,
		                     expected_order_id, max_slippage_bps, chain_id, state, leverage)
		VALUES ($1,$2,$3,$4,$5,$6,$7::numeric, NULLIF($8,'')::numeric, NULLIF($9,'')::numeric,
		        $10, $11, $12, $13, NULLIF($14,'')::numeric, $15, $16, $17, $18)
		ON CONFLICT (intent_key) DO NOTHING`
	tag, err := s.pool.Exec(ctx, q, in.Key, in.UserID, in.MarketID, in.Venue, in.Side, in.OrderType,
		in.Size, in.LimitPrice, in.Quantity, in.Tif, in.PostOnly, in.Token, in.SlotIdx,
		in.ExpectedOrderID, in.MaxSlippageBps, in.ChainID, orDefault(in.State, "pending"), in.Leverage)
	if err != nil {
		return false, fmt.Errorf("store: insert intent: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

const intentCols = `intent_key, user_id, market_id, venue, side, order_type, size::text,
                    COALESCE(limit_price::text, ''), COALESCE(quantity::text, ''), tif, post_only,
                    token, slot_idx, COALESCE(expected_order_id::text, ''),
                    max_slippage_bps, chain_id, state, created_at, leverage, order_id`

func scanIntent(row pgx.Row) (Intent, error) {
	var in Intent
	err := row.Scan(&in.Key, &in.UserID, &in.MarketID, &in.Venue, &in.Side, &in.OrderType, &in.Size,
		&in.LimitPrice, &in.Quantity, &in.Tif, &in.PostOnly, &in.Token, &in.SlotIdx,
		&in.ExpectedOrderID, &in.MaxSlippageBps, &in.ChainID, &in.State, &in.CreatedAt,
		&in.Leverage, &in.OrderID)
	return in, err
}

// SetOrderRef records where a resting order sits, as the executor reported it,
// so a later cancel can name it. slot is kuru's; perpl has none.
func (s *Store) SetOrderRef(ctx context.Context, key, orderID string, slot *int) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE intents SET order_id = $2, slot_idx = COALESCE($3, slot_idx) WHERE intent_key = $1`,
		key, orderID, slot)
	return err
}

// PutKuruAccount replaces the cached AccountCore state; the chain is the truth
// and this row is only a copy of the last read.
func (s *Store) PutKuruAccount(ctx context.Context, ka KuruAccount) error {
	const q = `
		INSERT INTO kuru_accounts (user_id, chain_id, kuru_user_id, signer_authorized,
		                           auth_nonce, account_core, signer_expiry, signer, updated_at)
		VALUES ($1,$2,$3,$4,$5::numeric,$6,$7,$8, now())
		ON CONFLICT (user_id) DO UPDATE SET
			chain_id = EXCLUDED.chain_id, kuru_user_id = EXCLUDED.kuru_user_id,
			signer_authorized = EXCLUDED.signer_authorized, auth_nonce = EXCLUDED.auth_nonce,
			account_core = EXCLUDED.account_core, signer = EXCLUDED.signer, updated_at = now()`
	_, err := s.pool.Exec(ctx, q, ka.UserID, ka.ChainID, ka.KuruUserID, ka.SignerAuthorized,
		orDefault(ka.AuthNonce, "0"), ka.AccountCore, ka.SignerExpiry, ka.Signer)
	return err
}

// SetSignerExpiry records the expiry this service asked for. /account/ensure
// reads the permission bit back but not its expiry, so a refresh leaves this
// column alone rather than overwriting it with a zero it cannot know.
func (s *Store) SetSignerExpiry(ctx context.Context, userID string, expiry int64) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE kuru_accounts SET signer_expiry = $2 WHERE user_id = $1`, userID, expiry)
	return err
}

func (s *Store) KuruAccount(ctx context.Context, userID string) (KuruAccount, error) {
	var ka KuruAccount
	err := s.pool.QueryRow(ctx, `
		SELECT user_id, chain_id, kuru_user_id, signer_authorized, auth_nonce::text,
		       account_core, signer_expiry, signer, updated_at
		FROM kuru_accounts WHERE user_id = $1`, userID).
		Scan(&ka.UserID, &ka.ChainID, &ka.KuruUserID, &ka.SignerAuthorized, &ka.AuthNonce,
			&ka.AccountCore, &ka.SignerExpiry, &ka.Signer, &ka.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return KuruAccount{}, ErrNotFound
	}
	return ka, err
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
