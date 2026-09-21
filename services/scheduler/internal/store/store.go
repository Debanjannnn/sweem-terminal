// Package store reads the schedules and slices tables defined by
// services/wallet/migrations/0004_schedules_slices.sql. wallet owns those
// migrations; scheduler claims slices and flips their state, nothing else.
package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Kind = string

const (
	KindTWAP Kind = "twap"
	KindDCA  Kind = "dca"
	KindGrid Kind = "grid"
)

const (
	StateActive        = "active"
	StatePausedRevoked = "paused_revoked"
)

type Schedule struct {
	ID             string
	UserID         string // privy DID
	Kind           Kind
	MarketID       string
	Venue          string
	Side           string
	Total          string // base-unit integer
	SliceCount     int    // 0 on a dca means indefinite
	WindowSeconds  int64
	CadenceSeconds int64
	Levels         []string // grid prices
	CreatedAt      time.Time
	NextIndex      int // lowest index still pending; twap and dca only
}

type Slice struct {
	ScheduleID string
	Index      int
	DueAt      time.Time
	Amount     string
	Side       string
	IntentKey  string
}

type Store struct{ pool *pgxpool.Pool }

func New(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	cfg.MaxConns = 8
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }

const activeSQL = `
SELECT sc.id::text, sc.user_id, sc.kind, sc.market_id, sc.venue, sc.side, sc.total::text,
       sc.slice_count, sc.window_seconds, sc.cadence_seconds,
       COALESCE(sc.levels, 'null'::jsonb), sc.created_at,
       COALESCE(MAX(sl.slice_index) FILTER (WHERE sl.state <> 'pending' OR sl.intent_key IS NOT NULL), -1) + 1
FROM schedules sc
LEFT JOIN slices sl ON sl.schedule_id = sc.id
WHERE sc.state = 'active'
GROUP BY sc.id
ORDER BY sc.created_at`

func (s *Store) ActiveSchedules(ctx context.Context) ([]Schedule, error) {
	rows, err := s.pool.Query(ctx, activeSQL)
	if err != nil {
		return nil, fmt.Errorf("query active schedules: %w", err)
	}
	defer rows.Close()
	var out []Schedule
	for rows.Next() {
		var sc Schedule
		var levels []byte
		if err := rows.Scan(&sc.ID, &sc.UserID, &sc.Kind, &sc.MarketID, &sc.Venue, &sc.Side, &sc.Total,
			&sc.SliceCount, &sc.WindowSeconds, &sc.CadenceSeconds, &levels, &sc.CreatedAt, &sc.NextIndex); err != nil {
			return nil, fmt.Errorf("scan schedule: %w", err)
		}
		if sc.Levels, err = decodeLevels(levels); err != nil {
			return nil, fmt.Errorf("levels of schedule %s: %w", sc.ID, err)
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

// levels arrive as json, quoted or not depending on who wrote the row
func decodeLevels(raw []byte) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var vals []json.RawMessage
	if err := json.Unmarshal(raw, &vals); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		s := string(v)
		if unquoted, err := strconv.Unquote(s); err == nil {
			s = unquoted
		}
		out = append(out, s)
	}
	return out, nil
}

// the claim is the whole correctness story: stamping intent_key is what one
// instance wins in a single statement. the row stays 'pending' because wallet
// requires that when it dispatches, and wallet owns the state from there.
// the insert only matters for a schedule whose slices were never materialised.
// ponytail: a claim stamped by an instance that then crashed stays stamped;
// clear intent_key by hand, wallet's dispatch is idempotent on it anyway
const claimSQL = `
INSERT INTO slices (schedule_id, slice_index, due_at, amount, intent_key, state)
VALUES ($1::uuid, $2, $3, $4::numeric, $5, 'pending')
ON CONFLICT (schedule_id, slice_index) DO UPDATE
  SET intent_key = EXCLUDED.intent_key
  WHERE slices.state = 'pending' AND slices.intent_key IS NULL`

func (s *Store) ClaimSlice(ctx context.Context, sl Slice) (bool, error) {
	tag, err := s.pool.Exec(ctx, claimSQL, sl.ScheduleID, sl.Index, sl.DueAt, sl.Amount, sl.IntentKey)
	if err != nil {
		return false, fmt.Errorf("claim slice %s#%d: %w", sl.ScheduleID, sl.Index, err)
	}
	return tag.RowsAffected() == 1, nil
}

// ReleaseSlice hands a claim back when the dispatch never reached wallet, so
// the next tick retries it. a slice wallet did answer is wallet's to mark.
func (s *Store) ReleaseSlice(ctx context.Context, scheduleID string, idx int, intentKey string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE slices SET intent_key = NULL
		 WHERE schedule_id = $1::uuid AND slice_index = $2 AND state = 'pending' AND intent_key = $3`,
		scheduleID, idx, intentKey)
	if err != nil {
		return fmt.Errorf("release slice %s#%d: %w", scheduleID, idx, err)
	}
	return nil
}

// ponytail: rearms every level outside the band each tick, narrow it if a grid ever carries hundreds of levels
func (s *Store) RearmSlices(ctx context.Context, scheduleID string, idxs []int) error {
	if len(idxs) == 0 {
		return nil
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE slices SET state = 'pending', intent_key = NULL, generation = generation + 1
		 WHERE schedule_id = $1::uuid AND slice_index = ANY($2::int[])
		   AND state IN ('dispatched', 'filled')`, scheduleID, idxs)
	if err != nil {
		return fmt.Errorf("rearm slices of %s: %w", scheduleID, err)
	}
	return nil
}

func (s *Store) SetScheduleState(ctx context.Context, id, state string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE schedules SET state = $2 WHERE id = $1::uuid AND state = 'active'`, id, state)
	if err != nil {
		return fmt.Errorf("set schedule %s to %s: %w", id, state, err)
	}
	return nil
}

// Generations reports each level's fire count, which disambiguates a re-armed
// level's intent key from its previous fire.
func (s *Store) Generations(ctx context.Context, scheduleID string) (map[int]int, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT slice_index, generation FROM slices WHERE schedule_id = $1::uuid`, scheduleID)
	if err != nil {
		return nil, fmt.Errorf("query generations of %s: %w", scheduleID, err)
	}
	defer rows.Close()
	gens := map[int]int{}
	for rows.Next() {
		var idx, gen int
		if err := rows.Scan(&idx, &gen); err != nil {
			return nil, err
		}
		gens[idx] = gen
	}
	return gens, rows.Err()
}

// FiredLevels reports the grid levels that are currently not armed.
func (s *Store) FiredLevels(ctx context.Context, scheduleID string) (map[int]bool, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT slice_index FROM slices
		 WHERE schedule_id = $1::uuid AND (state <> 'pending' OR intent_key IS NOT NULL)`, scheduleID)
	if err != nil {
		return nil, fmt.Errorf("query fired levels of %s: %w", scheduleID, err)
	}
	defer rows.Close()
	fired := map[int]bool{}
	for rows.Next() {
		var idx int
		if err := rows.Scan(&idx); err != nil {
			return nil, err
		}
		fired[idx] = true
	}
	return fired, rows.Err()
}
