//go:build integration

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/planner"
)

// testDB applies every migration into a throwaway schema and returns a pool pinned to it.
func testDB(t *testing.T) *pgxpool.Pool {
	ctx := context.Background()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://sweem:sweem@localhost:5433/sweem?sslmode=disable"
	}
	schema := "t_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
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
	if len(files) == 0 {
		t.Fatal("no migrations")
	}
	for _, f := range files {
		b, _ := os.ReadFile(f)
		if _, err := pool.Exec(ctx, string(b)); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
	}
	return pool
}

// fake market-data: fakeQ prices over the real quote route shape, plus rates.
func fakeMarketData(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/rates" {
			w.Write([]byte(`{"ts":1,"rates":[{"source":"lending","venue":"aave","asset":"USDC","address":"0x69a5f9ad4f96ebf0a0c792dd42a01cc5c0102fef","apy":"0.039","kind":"supply","ts":1}],"funding":[],"stale":false}`))
			return
		}
		ob := strings.TrimPrefix(r.URL.Path, "/api/v1/quote/")
		q := r.URL.Query()
		var slip int
		fmt.Sscan(q.Get("slippageBps"), &slip)
		res, _ := fakeQ{}.Quote(r.Context(), ob, q.Get("side"), mustDec(q.Get("amount")), slip)
		json.NewEncoder(w).Encode(res)
	}))
}

// fake executor: accepts only the api caller, confirms every step in the DB like the real one.
func fakeExecutor(t *testing.T, db *pgxpool.Pool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ ExecutionID string }
		json.NewDecoder(r.Body).Decode(&req)
		if r.URL.Path != "/api/v1/user/steps" || r.Header.Get("X-Caller") != "api" || r.Header.Get("X-Internal-Secret") != "s" {
			w.WriteHeader(403)
			return
		}
		hash := "0x" + strings.Repeat("ab", 32)
		var n int
		if err := db.QueryRow(r.Context(), `update executions set status='confirmed', step_done=jsonb_array_length(steps),
  tx_hashes=array[$2], tx_hash=$2 where id=$1 and status='pending' returning step_done`, req.ExecutionID, hash).Scan(&n); err != nil {
			w.WriteHeader(409)
			w.Write([]byte(`{"error":"not_pending"}`))
			return
		}
		json.NewEncoder(w).Encode(planner.Outcome{ExecutionID: req.ExecutionID, Status: "confirmed", StepsDone: n, TxHashes: []string{hash}})
	}))
}

func TestFlow(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	md, ex := fakeMarketData(t), fakeExecutor(t, db)
	defer md.Close()
	defer ex.Close()
	var histReq []byte
	hist := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		histReq = make([]byte, r.ContentLength)
		r.Body.Read(histReq)
		w.Write([]byte(`{"from":"2025-10-01","to":"2026-09-29","days":364,"apyUsd":"0.041000","apyAsset":"0.041000","maxDrawdownUsd":"0.000000","finalUsd":"1041.000000","hodl":{"apyUsd":"0.000000"},"curve":[]}`))
	}))
	defer hist.Close()

	srv := &Server{Store: PG{DB: db}, Auth: fakeAuth{}, Privy: fakePrivy{}, QuorumID: "q1",
		Plan:  &planner.Planner{Dep: testDep(t), Q: planner.MarketData{URL: md.URL, HTTP: md.Client()}, MaxStepUSD: 1000},
		Exec:  planner.Executor{URL: ex.URL, Secret: "s", Caller: "api", HTTP: ex.Client()},
		Chain: fakeChain{7_000_000_000_000_000}, Rates: planner.MarketData{URL: md.URL, HTTP: md.Client()},
		History: History{URL: hist.URL, HTTP: hist.Client()}}
	mux := http.NewServeMux()
	srv.Routes(mux)
	call := func(method, path, body string, wantCode int, out any) {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer good")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		srv.bg.Wait()
		if w.Code != wantCode {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body)
		}
		if out != nil {
			if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
				t.Fatal(err)
			}
		}
	}
	const wallet = "0x00000000000000000000000000000000000000bb"

	var b Basket
	call("POST", "/api/v1/me/baskets", `{"name":"Blue","legs":[{"market":"WETH","weightBps":6000},{"market":"cbBTC","weightBps":4000}],"public":true}`, 201, &b)
	call("POST", "/api/v1/me/baskets/"+b.ID, `{"name":"Blue2","legs":[{"market":"WETH","weightBps":10000}],"public":true}`, 200, &b)
	var bs []Basket
	call("GET", "/api/v1/baskets", "", 200, &bs)
	if len(bs) != 1 || bs[0].Name != "Blue2" || len(bs[0].Legs) != 1 {
		t.Fatalf("public baskets: %+v", bs)
	}
	call("POST", "/api/v1/me/baskets/"+b.ID+"/buy", `{"amountUsdc":"40"}`, 409, nil) // no delegation yet
	call("POST", "/api/v1/me/delegation", `{"walletId":"w1","walletAddress":"`+wallet+`"}`, 200, nil)

	var acc struct {
		ExecutionID string         `json:"executionId"`
		Steps       []planner.Step `json:"steps"`
		PositionID  string         `json:"positionId"`
		OrderID     string         `json:"orderId"`
	}
	call("POST", "/api/v1/me/baskets/"+b.ID+"/buy", `{"amountUsdc":"40","slippageBps":0}`, 202, &acc)
	var e Execution
	call("GET", "/api/v1/me/executions/"+acc.ExecutionID, "", 200, &e)
	if e.Status != "confirmed" || e.StepsTotal != 1 || e.StepDone != 1 || e.BasketID == nil || *e.BasketID != b.ID || e.Kind != "basket_buy" {
		t.Fatalf("basket buy execution: %+v", e)
	}
	var h struct {
		Tokens  []TokenBalance  `json:"tokens"`
		Baskets []BasketHolding `json:"baskets"`
	}
	call("GET", "/api/v1/me/holdings", "", 200, &h)
	if len(h.Tokens) != 9 || len(h.Baskets) != 1 || h.Baskets[0].Legs[0].Raw != "10000000000000000" || h.Baskets[0].Legs[0].Amount != "0.01" {
		t.Fatalf("holdings: %+v", h)
	}
	call("POST", "/api/v1/me/baskets/"+b.ID+"/sell", `{"bps":5000,"slippageBps":0}`, 202, &acc)
	if len(acc.Steps) != 1 || acc.Steps[0].Size != "5000000000000000" {
		t.Fatalf("basket sell: %+v", acc.Steps)
	}

	// strategy: create (backtest stored), explore with apy, deploy, withdraw
	var st Strategy
	call("POST", "/api/v1/strategies", `{"name":"Safe USDC","asset":"USDC","sources":[{"kind":"lending","venue":"aave","weightBps":10000}],"rules":{"driftBps":300}}`, 201, &st)
	if !strings.Contains(string(st.Backtest), `"apyUsd":"0.041000"`) || !strings.Contains(string(histReq), `"venue":"aave-v3"`) {
		t.Fatalf("backtest: %s / req %s", st.Backtest, histReq)
	}
	var list []Strategy
	call("GET", "/api/v1/strategies", "", 200, &list)
	if len(list) != 1 || list[0].APY == nil || list[0].APY.Estimate != "0.039000" || list[0].Rules.DriftBps != 300 || list[0].CreatorWallet == nil {
		t.Fatalf("strategies: %+v", list)
	}
	srv.Plan.Dep.Treasury = ""
	call("POST", "/api/v1/me/strategies/"+st.ID+"/deploy", `{"amountUsdc":"100"}`, 422, nil)
	srv.Plan.Dep.Treasury = "0x7777777777777777777777777777777777777777"
	call("POST", "/api/v1/me/strategies/"+st.ID+"/deploy", `{"amountUsdc":"100"}`, 202, &acc)
	var pos []Position
	call("GET", "/api/v1/me/positions", "", 200, &pos)
	if len(pos) != 1 || pos[0].Status != "open" || pos[0].PrincipalUSDC != "100" || pos[0].Legs[0].Asset != "99600000" {
		t.Fatalf("positions: %+v", pos)
	}
	var fee, owed string
	db.QueryRow(ctx, `select fee_usdc::text, creator_owed_usdc::text from fee_ledger where execution_id=$1`, acc.ExecutionID).Scan(&fee, &owed)
	if fee != "0.4" || owed != "0" {
		t.Fatalf("fee ledger: %s %s", fee, owed)
	}
	call("POST", "/api/v1/me/positions/"+pos[0].ID+"/withdraw", ``, 202, &acc)
	if acc.Steps[0].Action != "lend.withdraw" || acc.Steps[0].Amount != "7000000000000000" {
		t.Fatalf("withdraw steps: %+v", acc.Steps)
	}
	call("GET", "/api/v1/me/positions", "", 200, &pos)
	if pos[0].Status != "closed" || pos[0].ClosedAt == nil {
		t.Fatalf("after withdraw: %+v", pos[0])
	}
	call("POST", "/api/v1/me/positions/"+pos[0].ID+"/withdraw", ``, 409, nil)

	// limit order: id resolved from the indexer's OrderCreated row for the placing tx
	call("POST", "/api/v1/me/orders", `{"market":"WETH","side":"buy","price":"3600","size":"0.01","fund":true}`, 202, &acc)
	if _, err := db.Exec(ctx, `insert into user_orders_events values ($1,3,777,'weth',$2,'created','buy',36000000,100000000,1,now())`, "0x"+strings.Repeat("ab", 32), wallet); err != nil {
		t.Fatal(err)
	}
	var orders []Order
	call("GET", "/api/v1/me/orders", "", 200, &orders)
	if len(orders) != 1 || orders[0].Status != "open" || orders[0].KuruOrderID == nil || *orders[0].KuruOrderID != "777" || orders[0].Price != "3600" || orders[0].Size != "0.01" {
		t.Fatalf("orders: %+v", orders)
	}
	call("DELETE", "/api/v1/me/orders/"+acc.OrderID, "", 202, &acc)
	call("GET", "/api/v1/me/orders", "", 200, &orders)
	if orders[0].Status != "cancelled" {
		t.Fatalf("after cancel: %+v", orders[0])
	}

	var sc Schedule
	call("POST", "/api/v1/me/schedules", `{"kind":"rebalance","target":{"type":"basket","id":"`+b.ID+`"},"intervalSeconds":3600,"driftBps":500,"maxSlippageBps":50,"maxPerRunUsdc":"100"}`, 201, &sc)
	call("POST", "/api/v1/me/schedules", `{"kind":"stop_loss","target":{"type":"market","id":"WETH"},"triggerPrice":"3000","sellBps":10000,"maxSlippageBps":50,"maxPerRunUsdc":"100"}`, 201, nil)
	call("POST", "/api/v1/me/schedules", `{"kind":"rebalance","target":{"type":"position","id":"`+uuid.NewString()+`"},"intervalSeconds":3600,"maxSlippageBps":50,"maxPerRunUsdc":"100"}`, 404, nil)
	var scs []Schedule
	call("GET", "/api/v1/me/schedules", "", 200, &scs)
	if len(scs) != 2 || sc.NextRunAt == nil || *sc.DriftBps != 500 || sc.Target.ID != b.ID {
		t.Fatalf("schedules: %+v %+v", sc, scs)
	}
	var exs []Execution
	call("GET", "/api/v1/me/executions", "", 200, &exs)
	if len(exs) != 6 {
		t.Fatalf("executions: %d", len(exs))
	}

	p := PG{DB: db}
	has, err := p.HasPerplAccount(ctx, "did:privy:u1", wallet)
	if err != nil || has {
		t.Fatalf("perpl before: %v %v", has, err)
	}
	db.Exec(ctx, `insert into executions(user_id,slot,kind,status,step_done,steps) values ('did:privy:u1',0,'deploy','failed',2,
  '[{"action":"fee.transfer","amount":"1"},{"action":"perpl.createAccount","amount":"1"},{"action":"perpl.order"}]')`)
	if has, _ = p.HasPerplAccount(ctx, "did:privy:u1", wallet); !has {
		t.Fatal("createAccount step confirmed but not seen")
	}
	call("DELETE", "/api/v1/me/delegation", "", 200, nil)
	call("POST", "/api/v1/me/trade", `{"market":"WETH","side":"buy","amount":"10"}`, 409, nil)
}

func mustDec(s string) decimal.Decimal { return decimal.RequireFromString(s) }
