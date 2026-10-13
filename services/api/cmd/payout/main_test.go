//go:build integration

package main

import (
	"context"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/snehendu098/sweem-terminal/services/api/internal/api"
)

// testDB applies every migration into a throwaway schema and returns a pool pinned to it.
func testDB(t *testing.T) *pgxpool.Pool {
	ctx := context.Background()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://sweem:sweem@localhost:5433/sweem?sslmode=disable"
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	schema := "t_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "create schema "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), "drop schema "+schema+" cascade"); pool.Close() })
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

func TestPayout(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	exec := func(q string, args ...any) string {
		t.Helper()
		var id string
		if err := db.QueryRow(ctx, q, args...).Scan(&id); err != nil {
			t.Fatalf("%v\n%s", err, q)
		}
		return id
	}
	const c, f, wallet = "did:privy:c", "did:privy:f", "0x00000000000000000000000000000000000000cc"
	exec(`insert into users(id) values ($1), ($2) returning id`, c, f)
	exec(`insert into delegations(user_id, privy_wallet_id, wallet_address, key_quorum_id) values ($1,'w','0x00000000000000000000000000000000000000CC','kq') returning user_id`, c)
	st := exec(`insert into strategies(creator,name,asset,sources,rules) values ($1,'s','USDC','[]','{}') returning id::text`, c)
	row := func(status string, done int, owed string, paid bool) string {
		id := exec(`insert into executions(user_id,slot,kind,status,step_done,steps) values ($1,0,'deploy',$2,$3,'[{"action":"fee.transfer"},{"action":"lend.supply"}]') returning id::text`, f, status, done)
		exec(`insert into fee_ledger(execution_id,user_id,strategy_id,creator,fee_usdc,creator_owed_usdc,paid) values ($1,$2,$3,$4,$5::numeric * 2,$5::numeric,$6) returning execution_id::text`, id, f, st, c, owed, paid)
		return id
	}
	a := row("confirmed", 2, "0.2", false)
	row("failed", 0, "0.3", false) // fee never transferred: not owed
	row("confirmed", 2, "0.5", true)
	b := row("failed", 1, "0.1000005", false) // fee step went through before the failure

	ds, err := dues(ctx, db, "")
	if err != nil || len(ds) != 1 || ds[0].Creator != c || ds[0].Wallet != wallet || ds[0].Total.String() != "0.3000005" || strings.Join(ds[0].IDs, ",") != a+","+b {
		t.Fatalf("dues: %+v %v", ds, err)
	}
	if _, _, err := pay(ctx, db, c, 6, func(context.Context, common.Address, *big.Int) (string, error) {
		return "0xdead", errors.New("reverted")
	}); err == nil || !strings.Contains(err.Error(), "0xdead") {
		t.Fatalf("failed send: %v", err)
	}
	if ds, _ = dues(ctx, db, c); len(ds) != 1 {
		t.Fatal("failed send must leave rows unpaid")
	}
	var to common.Address
	var atoms *big.Int
	if _, _, err := pay(ctx, db, c, 6, func(_ context.Context, a common.Address, n *big.Int) (string, error) {
		to, atoms = a, n
		return "0xok", nil
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(to.Hex(), wallet) || atoms.String() != "300000" {
		t.Fatalf("sent %s to %s", atoms, to)
	}
	if ds, _ = dues(ctx, db, ""); len(ds) != 0 {
		t.Fatalf("still due: %+v", ds)
	}
	e, err := api.PG{DB: db}.Earnings(ctx, c)
	if err != nil || e.OwedUSDC != "0" || e.PaidUSDC != "0.8000005" || e.FeesPaidUSDC != "0" || len(e.Rows) != 3 {
		t.Fatalf("creator earnings: %+v %v", e, err)
	}
	if e, _ = (api.PG{DB: db}).Earnings(ctx, f); e.FeesPaidUSDC != "1.6000010" || len(e.Rows) != 0 {
		t.Fatalf("follower earnings: %+v", e)
	}
}
