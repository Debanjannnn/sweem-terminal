package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sweem-terminal/sweem-terminal/services/scheduler/internal/client"
	"github.com/sweem-terminal/sweem-terminal/services/scheduler/internal/delegation"
	"github.com/sweem-terminal/sweem-terminal/services/scheduler/internal/store"
)

// fakeStore mirrors the claim statement: stamping intent_key on a pending,
// unstamped slice succeeds exactly once.
type fakeStore struct {
	mu        sync.Mutex
	schedules []store.Schedule
	keys      map[string]string
	fired     map[int]bool
	states    map[string]string
	rearmed   []int
	gens      map[int]int
}

func newFakeStore(s ...store.Schedule) *fakeStore {
	return &fakeStore{schedules: s, keys: map[string]string{}, states: map[string]string{}}
}

func skey(id string, idx int) string { return fmt.Sprintf("%s#%d", id, idx) }

func (f *fakeStore) ActiveSchedules(context.Context) ([]store.Schedule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.schedules, nil
}

func (f *fakeStore) ClaimSlice(_ context.Context, sl store.Slice) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.keys[skey(sl.ScheduleID, sl.Index)] != "" {
		return false, nil
	}
	f.keys[skey(sl.ScheduleID, sl.Index)] = sl.IntentKey
	return true, nil
}

func (f *fakeStore) ReleaseSlice(_ context.Context, id string, idx int, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.keys[skey(id, idx)] == key {
		delete(f.keys, skey(id, idx))
	}
	return nil
}

func (f *fakeStore) RearmSlices(_ context.Context, _ string, idxs []int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rearmed = append(f.rearmed, idxs...)
	if f.gens == nil {
		f.gens = map[int]int{}
	}
	for _, i := range idxs {
		f.gens[i]++
	}
	return nil
}

func (f *fakeStore) Generations(_ context.Context, _ string) (map[int]int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[int]int{}
	for k, v := range f.gens {
		out[k] = v
	}
	return out, nil
}

func (f *fakeStore) SetScheduleState(_ context.Context, id, state string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states[id] = state
	return nil
}

func (f *fakeStore) FiredLevels(context.Context, string) (map[int]bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fired, nil
}

func (f *fakeStore) claimed(id string, idx int) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.keys[skey(id, idx)] != ""
}

type call struct {
	did        string
	scheduleID string
	index      int
}

type recorder struct {
	mu       sync.Mutex
	calls    []call
	subjects []string
	res      client.Dispatch
	err      error
}

func (r *recorder) dispatch(_ context.Context, did, id string, idx int) (client.Dispatch, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, call{did, id, idx})
	return r.res, r.err
}

func (r *recorder) publish(_ context.Context, subject, _ string, _ []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.subjects = append(r.subjects, subject)
	return nil
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func testEngine(st Store, rec *recorder, now time.Time) *Engine {
	return &Engine{
		Store:      st,
		Cfg:        Config{Tick: time.Second, MaxCatchup: 1, GridBandBps: 100, QuoteDecimals: 6},
		Log:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		Delegation: func(context.Context, string) error { return nil },
		Dispatch:   rec.dispatch,
		Mid:        func(context.Context, string) (float64, error) { return 1, nil },
		Publish:    rec.publish,
		Now:        func() time.Time { return now },
	}
}

func twapSchedule() store.Schedule {
	return store.Schedule{
		ID: "abc", UserID: "did:privy:u1", Kind: store.KindTWAP, MarketID: "MON-USDC", Venue: "kuru",
		Side: "buy", CreatedAt: start,
		Total: "1000000000", SliceCount: 10, WindowSeconds: 9000,
	}
}

// the core correctness property: two schedulers on the same row fire it once.
func TestClaimPreventsDoubleDispatch(t *testing.T) {
	st := newFakeStore(twapSchedule())
	rec := &recorder{}
	now := start.Add(time.Minute)
	a, b := testEngine(st, rec, now), testEngine(st, rec, now)

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(2)
		go func() { defer wg.Done(); a.Tick(context.Background()) }()
		go func() { defer wg.Done(); b.Tick(context.Background()) }()
	}
	wg.Wait()

	if got := rec.count(); got != 1 {
		t.Fatalf("slice dispatched %d times, want exactly 1", got)
	}
	if lost := a.C.ClaimLost.Load() + b.C.ClaimLost.Load(); lost == 0 {
		t.Fatal("losing schedulers must count a lost claim")
	}
	if got := rec.calls[0]; got.scheduleID != "abc" || got.index != 0 || got.did != "did:privy:u1" {
		t.Fatalf("dispatched %+v", got)
	}
	if st.keys[skey("abc", 0)] != IntentKey("abc", 0, 0) {
		t.Fatalf("slice carries intent key %q", st.keys[skey("abc", 0)])
	}
}

// wallet derives the same key from ("schedule", id, index, generation); drift breaks idempotency
func TestIntentKeyMirrorsWallet(t *testing.T) {
	// literally wallet's intentKey("schedule", id, index, generation)
	sum := sha256.Sum256([]byte(strings.Join([]string{"schedule", "abc", "3", "0"}, "\x00")))
	if want := hex.EncodeToString(sum[:16]); IntentKey("abc", 3, 0) != want {
		t.Fatalf("intent key %q, wallet computes %q", IntentKey("abc", 3, 0), want)
	}
	if IntentKey("abc", 3, 0) == IntentKey("abc", 4, 0) || IntentKey("abc", 3, 0) == IntentKey("abd", 3, 0) {
		t.Fatal("different slices must not share a key")
	}
	// the whole point: a re-armed grid level must not replay its previous fire
	if IntentKey("abc", 3, 0) == IntentKey("abc", 3, 1) {
		t.Fatal("a re-armed level must derive a fresh key")
	}
}

func TestDelegationRevokedPausesSchedule(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantState string
		wantCalls int
	}{
		{"revoked pauses and stops dispatching", delegation.ErrRevoked, store.StatePausedRevoked, 0},
		{"revoked wrapped in context still pauses", fmt.Errorf("checking user: %w", delegation.ErrRevoked), store.StatePausedRevoked, 0},
		{"a transient wallet error holds, it does not pause", errors.New("connection refused"), "", 0},
		{"healthy dispatches", nil, "", 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := newFakeStore(twapSchedule())
			rec := &recorder{}
			e := testEngine(st, rec, start.Add(time.Minute))
			e.Delegation = func(context.Context, string) error { return tc.err }
			e.Tick(context.Background())

			if st.states["abc"] != tc.wantState {
				t.Fatalf("schedule state %q, want %q", st.states["abc"], tc.wantState)
			}
			if rec.count() != tc.wantCalls {
				t.Fatalf("dispatched %d slices, want %d", rec.count(), tc.wantCalls)
			}
			if tc.wantState == store.StatePausedRevoked {
				if e.C.DelegationRevoked.Load() != 1 {
					t.Fatal("revocation must be counted for /health")
				}
				if len(rec.subjects) != 1 || rec.subjects[0] != subjSchedulePaused {
					t.Fatalf("emitted %v, want the paused event", rec.subjects)
				}
				if st.claimed("abc", 0) {
					t.Fatal("a paused schedule must not leave a claimed slice behind")
				}
			}
		})
	}
}

func TestDispatchOutcomes(t *testing.T) {
	tests := []struct {
		name        string
		res         client.Dispatch
		err         error
		wantClaimed bool
		wantEvents  int
	}{
		{name: "dispatched emits", res: client.Dispatch{State: "dispatched", TxHash: "0xabc"}, wantClaimed: true, wantEvents: 1},
		{name: "a replay emits nothing", res: client.Dispatch{Replayed: true}, wantClaimed: true},
		{name: "a transient failure releases the claim", err: errors.New("connection refused")},
		{name: "a revoked delegation leaves the slice to wallet", err: delegation.ErrRevoked, wantClaimed: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := newFakeStore(twapSchedule())
			rec := &recorder{res: tc.res, err: tc.err}
			e := testEngine(st, rec, start.Add(time.Minute))
			e.Tick(context.Background())

			if st.claimed("abc", 0) != tc.wantClaimed {
				t.Fatalf("slice claimed=%v, want %v", st.claimed("abc", 0), tc.wantClaimed)
			}
			if len(rec.subjects) != tc.wantEvents {
				t.Fatalf("emitted %v, want %d events", rec.subjects, tc.wantEvents)
			}
		})
	}
}

func TestGasGateSkipsSlicesThatCostMoreThanTheyTrade(t *testing.T) {
	tests := []struct {
		name     string
		total    string // 6dp quote over 10 slices
		monPrice float64
		wantSkip bool
	}{
		{"a slice worth 100 quote carries the gas", "1000000000", 20, false},
		{"a slice worth 1 quote does not", "10000000", 20, true},
		{"a cheap gas token lets the small slice through", "10000000", 0.0002, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := twapSchedule()
			s.Total = tc.total
			st := newFakeStore(s)
			rec := &recorder{}
			e := testEngine(st, rec, start.Add(time.Minute))
			e.Cfg.GasMarket = "MON-USD"
			e.Cfg.GasLimit = 500_000
			e.Cfg.GasPriceGwei = 100
			e.Cfg.MaxGasFraction = 0.01
			e.Mid = func(_ context.Context, id string) (float64, error) {
				if id == "MON-USD" {
					return tc.monPrice, nil
				}
				return 1, nil
			}
			e.Tick(context.Background())

			if tc.wantSkip {
				if rec.count() != 0 || e.C.GasSkipped.Load() != 1 {
					t.Fatalf("expected the slice to be skipped, dispatched %d", rec.count())
				}
				if st.claimed("abc", 0) {
					t.Fatal("a gas-skipped slice must stay unclaimed, it is retried next tick")
				}
				return
			}
			if rec.count() != 1 {
				t.Fatalf("expected a dispatch, got %d", rec.count())
			}
		})
	}
}

func TestOneBadScheduleDoesNotStopTheRest(t *testing.T) {
	bad := twapSchedule()
	bad.ID, bad.SliceCount = "bad", 0
	st := newFakeStore(bad, twapSchedule())
	rec := &recorder{}
	e := testEngine(st, rec, start.Add(time.Minute))
	e.Tick(context.Background())
	if rec.count() != 1 {
		t.Fatalf("healthy schedule dispatched %d times, want 1", rec.count())
	}
	if e.C.Errors.Load() != 1 {
		t.Fatalf("broken schedule should count one error, got %d", e.C.Errors.Load())
	}
}

func TestGridTickRearmsAndFires(t *testing.T) {
	s := store.Schedule{
		ID: "g", UserID: "did:privy:u1", Kind: store.KindGrid, MarketID: "MON-USDC",
		Levels: []string{"1.00", "1.10"}, Total: "10000000",
	}
	st := newFakeStore(s)
	st.fired = map[int]bool{0: true}
	rec := &recorder{}
	e := testEngine(st, rec, start)
	px := 1.05
	e.Mid = func(context.Context, string) (float64, error) { return px, nil }

	e.Tick(context.Background()) // first poll only records the price
	if rec.count() != 0 {
		t.Fatal("the first poll after a restart must not fire")
	}
	if len(st.rearmed) != 1 || st.rearmed[0] != 0 {
		t.Fatalf("level 0 is outside its band and should rearm, got %v", st.rearmed)
	}
	px = 1.12
	e.Tick(context.Background())
	if rec.count() != 1 {
		t.Fatalf("crossing level 1 should fire once, got %d", rec.count())
	}
	if rec.calls[0].index != 1 {
		t.Fatalf("fired level %d, want 1", rec.calls[0].index)
	}
}
