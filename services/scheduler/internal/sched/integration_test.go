//go:build integration

package sched

import (
	"context"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/pg"
	"github.com/snehendu098/sweem-terminal/internal/streams"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// testDB applies every migration into a throwaway schema and returns a pool pinned to it.
func testDB(t *testing.T) *pgxpool.Pool {
	ctx := context.Background()
	url := env("DATABASE_URL", "postgres://sweem:sweem@localhost:5433/sweem?sslmode=disable")
	admin, err := pg.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	schema := "t_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	if _, err := admin.Exec(ctx, "create schema "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Exec(context.Background(), "drop schema "+schema+" cascade") })
	pool, err := pg.Open(ctx, url+"&search_path="+schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	files, _ := filepath.Glob("../../../../migrations/*.sql")
	sort.Strings(files)
	for _, f := range files {
		b, _ := os.ReadFile(f)
		if _, err := pool.Exec(ctx, string(b)); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
	}
	return pool
}

func testRedis(t *testing.T) *redis.Client {
	opt, err := redis.ParseURL(env("REDIS_URL", "redis://localhost:6380"))
	if err != nil {
		t.Fatal(err)
	}
	c := redis.NewClient(opt)
	t.Cleanup(func() { c.Close() })
	return c
}

func quoteServer(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"minOut":"123","stale":false}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func seedUser(t *testing.T, db *pgxpool.Pool) {
	ctx := context.Background()
	for _, q := range []string{
		`insert into users(id) values ('did:privy:u')`,
		`insert into delegations(user_id, privy_wallet_id, wallet_address, key_quorum_id) values ('did:privy:u','w','0xAbC0000000000000000000000000000000000001','kq')`,
	} {
		if _, err := db.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
}

func newScheduler(db *pgxpool.Pool, rdb *redis.Client, ex *fakeExecutor, q *httptest.Server) *Scheduler {
	return &Scheduler{
		DB: db, RDB: rdb, Tick: time.Second,
		Exec:   Executor{URL: ex.URL, Secret: "s3cret", HTTP: http.DefaultClient},
		Quotes: Quotes{URL: q.URL, HTTP: http.DefaultClient},
		BalanceOf: func(ctx context.Context, basket, owner string) (*big.Int, error) {
			if owner != "0xabc0000000000000000000000000000000000001" {
				panic("owner " + owner)
			}
			return new(big.Int).Mul(big.NewInt(2), big.NewInt(1e18)), nil
		},
	}
}

func TestRecurringConcurrentInstancesCallOnce(t *testing.T) {
	ctx := context.Background()
	db, rdb, ex, q := testDB(t), testRedis(t), newFakeExecutor(t), quoteServer(t)
	seedUser(t, db)
	next := time.Now().Add(-90 * time.Minute).Truncate(time.Second)
	var sid string
	if err := db.QueryRow(ctx, `insert into schedules(user_id,basket,kind,amount_usdc,interval_seconds,next_run_at,max_slippage_bps,max_per_run_usdc)
		values ('did:privy:u','0xb','recurring',10.5,3600,$1,50,20) returning id`, next).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	a, b := newScheduler(db, rdb, ex, q), newScheduler(db, rdb, ex, q)
	now := time.Now()
	var wg sync.WaitGroup
	for range 5 {
		for _, s := range []*Scheduler{a, b} {
			wg.Add(1)
			go func() { defer wg.Done(); s.recurring(ctx, now) }()
		}
	}
	wg.Wait()
	calls := ex.Calls()
	if len(calls) != 1 || !strings.HasPrefix(calls[0], "mint:") {
		t.Fatalf("calls = %v", calls)
	}
	var slot int64
	var kind, amount, minOut string
	var gotNext time.Time
	if err := db.QueryRow(ctx, `select e.slot, e.kind, e.amount::text, e.min_out::text, s.next_run_at
		from executions e join schedules s on s.id = e.schedule_id where e.id = $1`, strings.TrimPrefix(calls[0], "mint:")).
		Scan(&slot, &kind, &amount, &minOut, &gotNext); err != nil {
		t.Fatal(err)
	}
	if slot != Slot(next, 3600) || kind != "mint" || amount != "10500000" || minOut != "123" {
		t.Fatalf("row slot=%d kind=%s amount=%s minOut=%s", slot, kind, amount, minOut)
	}
	if !gotNext.Equal(next.Add(2*time.Hour)) || !gotNext.After(now) {
		t.Fatalf("next_run_at %v, want %v", gotNext, next.Add(2*time.Hour))
	}
	// revoked delegation: nothing due
	db.Exec(ctx, `update delegations set active=false`)
	db.Exec(ctx, `update schedules set next_run_at = now() - interval '1 hour'`)
	a.recurring(ctx, time.Now())
	if n := len(ex.Calls()); n != 1 {
		t.Fatalf("fired without delegation: %d calls", n)
	}
}

func TestStopLossFiresOnceAndNeverOnStale(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, rdb, ex, q := testDB(t), testRedis(t), newFakeExecutor(t), quoteServer(t)
	seedUser(t, db)
	basket := "0xTEST" + uuid.NewString()[:8]
	lb := strings.ToLower(basket)
	t.Cleanup(func() { rdb.Del(context.Background(), streams.Nav(lb), streams.NavLatest(lb)) })
	if _, err := db.Exec(ctx, `insert into schedules(user_id,basket,kind,stop_nav_per_share,redeem_bps,max_slippage_bps,max_per_run_usdc)
		values ('did:privy:u',$1,'stop_loss',0.95,5000,50,20)`, basket); err != nil {
		t.Fatal(err)
	}
	go func() {
		for ctx.Err() == nil {
			streams.Beat(ctx, rdb)
			time.Sleep(time.Second)
		}
	}()
	a, b := newScheduler(db, rdb, ex, q), newScheduler(db, rdb, ex, q)
	a.watch(ctx)
	b.watch(ctx)
	time.Sleep(300 * time.Millisecond) // readers attach
	pub := func(nav string, stale bool) {
		p := streams.NavPoint{Basket: streams.Addr(lb), NavPerShare: decimal.RequireFromString(nav), Stale: stale}
		if err := streams.Publish(ctx, rdb, streams.Nav(lb), streams.NavLatest(lb), p); err != nil {
			t.Fatal(err)
		}
	}
	pub("1.00", false)
	pub("0.50", true)
	time.Sleep(time.Second)
	if c := ex.Calls(); len(c) != 0 {
		t.Fatalf("fired on stale/high NAV: %v", c)
	}
	pub("0.90", false)
	pub("0.80", false)
	deadline := time.Now().Add(5 * time.Second)
	for len(ex.Calls()) == 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	time.Sleep(time.Second) // let any duplicate arrive
	calls := ex.Calls()
	if len(calls) != 1 || !strings.HasPrefix(calls[0], "redeem:") {
		t.Fatalf("calls = %v", calls)
	}
	var slot int64
	var kind, amount string
	var active bool
	if err := db.QueryRow(ctx, `select e.slot, e.kind, e.amount::text, s.active from executions e join schedules s on s.id = e.schedule_id`).
		Scan(&slot, &kind, &amount, &active); err != nil {
		t.Fatal(err)
	}
	if slot != 0 || kind != "redeem" || amount != "1000000000000000000" || active {
		t.Fatalf("slot=%d kind=%s amount=%s active=%v", slot, kind, amount, active)
	}
}
