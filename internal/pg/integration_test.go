//go:build integration

package pg

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/google/uuid"
)

// Applies every migration into a throwaway schema, then checks the idempotency constraint.
func TestMigrations(t *testing.T) {
	ctx := context.Background()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://sweem:sweem@localhost:5433/sweem?sslmode=disable"
	}
	pool, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	schema := "t_" + uuid.NewString()[:8]
	must := func(sql string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%v\n%s", err, sql)
		}
	}
	must("create schema " + schema)
	t.Cleanup(func() { pool.Exec(context.Background(), "drop schema "+schema+" cascade") })
	must("set search_path to " + schema)

	files, _ := filepath.Glob("../../migrations/*.sql")
	sort.Strings(files)
	if len(files) == 0 {
		t.Fatal("no migrations found")
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		must(string(b))
	}

	must("insert into users(id) values ('did:privy:x')")
	var sid string
	if err := conn.QueryRow(ctx, `insert into schedules(user_id,kind,target_type,target_id,amount_usdc,interval_seconds,next_run_at,max_slippage_bps,max_per_run_usdc)
		values ('did:privy:x','dca','market','WETH',10,86400,now(),50,10) returning id`).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	ins := `insert into executions(schedule_id,slot,kind) values ($1,1,'dca') on conflict (schedule_id,slot) do nothing`
	for i, want := range []int64{1, 0} {
		tag, err := conn.Exec(ctx, ins, sid)
		if err != nil || tag.RowsAffected() != want {
			t.Fatalf("insert %d: rows=%d err=%v", i, tag.RowsAffected(), err)
		}
	}
	if _, err := conn.Exec(ctx, "update executions set status='bogus'"); err == nil {
		t.Fatal("status check missing")
	}
	if _, err := conn.Exec(ctx, `insert into schedules(user_id,kind,target_type,target_id,max_slippage_bps,max_per_run_usdc) values ('did:privy:x','dca','basket','x',50,10)`); err == nil {
		t.Fatal("basket target accepted") // baskets are strategies now (0014)
	}
}
