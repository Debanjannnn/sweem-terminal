//go:build integration

package api

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
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

// txHash is the fake executor's one hash per execution (every plan here signs a single tx).
func txHash(execID string) string { h := strings.ReplaceAll(execID, "-", ""); return "0x" + h + h }

// fake executor: accepts only the api caller, confirms every step in the DB like the real one; *failNext fails the
// next run after its first step (step_done=1), like a chain_error mid-plan.
func fakeExecutor(t *testing.T, db *pgxpool.Pool, failNext *bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ ExecutionID string }
		json.NewDecoder(r.Body).Decode(&req)
		if r.URL.Path != "/api/v1/user/steps" || r.Header.Get("X-Caller") != "api" || r.Header.Get("X-Internal-Secret") != "s" {
			w.WriteHeader(403)
			return
		}
		hash := txHash(req.ExecutionID)
		status, done := "confirmed", "jsonb_array_length(steps)"
		if *failNext {
			*failNext, status, done = false, "failed", "1"
		}
		var n int
		if err := db.QueryRow(r.Context(), `update executions set status=$3, step_done=`+done+`,
  tx_hashes=array[$2], tx_hash=$2 where id=$1 and status='pending' returning step_done`, req.ExecutionID, hash, status).Scan(&n); err != nil {
			w.WriteHeader(409)
			w.Write([]byte(`{"error":"not_pending"}`))
			return
		}
		json.NewEncoder(w).Encode(planner.Outcome{ExecutionID: req.ExecutionID, Status: status, StepsDone: n, TxHashes: []string{hash}})
	}))
}

func TestFlow(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	failNext := false
	md, ex := fakeMarketData(t), fakeExecutor(t, db, &failNext)
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

	call("POST", "/api/v1/me/trade", `{"market":"WETH","side":"buy","amount":"40"}`, 409, nil) // no delegation yet
	call("POST", "/api/v1/me/delegation", `{"walletId":"w1","walletAddress":"`+wallet+`"}`, 200, nil)

	var acc struct {
		ExecutionID string         `json:"executionId"`
		Steps       []planner.Step `json:"steps"`
		PositionID  string         `json:"positionId"`
		OrderID     string         `json:"orderId"`
	}
	call("POST", "/api/v1/me/trade", `{"market":"WETH","side":"buy","amount":"40","slippageBps":0}`, 202, &acc)
	var e Execution
	call("GET", "/api/v1/me/executions/"+acc.ExecutionID, "", 200, &e)
	if e.Status != "confirmed" || e.StepsTotal != 1 || e.StepDone != 1 || e.Kind != "trade" {
		t.Fatalf("trade execution: %+v", e)
	}
	var h struct {
		Tokens []TokenBalance `json:"tokens"`
	}
	call("GET", "/api/v1/me/holdings", "", 200, &h)
	if len(h.Tokens) != 9 {
		t.Fatalf("holdings: %+v", h)
	}

	// strategy: create (backtest stored), explore with apy, deploy, withdraw
	var st Strategy
	call("POST", "/api/v1/strategies", `{"name":"Safe USDC","asset":"USDC","sources":[{"kind":"lending","venue":"aave","weightBps":10000}],"rules":{"driftBps":300}}`, 201, &st)
	if !strings.Contains(string(st.Backtest), `"apyUsd":"0.041000"`) || !strings.Contains(string(histReq), `"venue":"aave"`) {
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
	failNext = true
	call("POST", "/api/v1/me/positions/"+pos[0].ID+"/withdraw", ``, 202, &acc)
	if acc.Steps[0].Action != "lend.withdraw" || acc.Steps[0].Amount != "7000000000000000" {
		t.Fatalf("withdraw steps: %+v", acc.Steps)
	}
	call("GET", "/api/v1/me/positions", "", 200, &pos)
	if pos[0].Status != "withdrawing" {
		t.Fatalf("failed withdraw must stay withdrawing: %+v", pos[0])
	}
	// a run in flight blocks a second claim
	var busy string
	db.QueryRow(ctx, `insert into executions(user_id,slot,kind,position_id,steps) values ('did:privy:u1',0,'withdraw',$1,'[]') returning id::text`, pos[0].ID).Scan(&busy)
	call("POST", "/api/v1/me/positions/"+pos[0].ID+"/withdraw", ``, 409, nil)
	db.Exec(ctx, `delete from executions where id=$1`, busy)
	// resume: the one-step plan finished step 0 → nothing left, closed without a new execution
	var closed map[string]any
	call("POST", "/api/v1/me/positions/"+pos[0].ID+"/withdraw", ``, 200, &closed)
	if closed["status"] != "closed" {
		t.Fatalf("resume: %+v", closed)
	}
	call("GET", "/api/v1/me/positions", "", 200, &pos)
	if pos[0].Status != "closed" || pos[0].ClosedAt == nil {
		t.Fatalf("after withdraw: %+v", pos[0])
	}
	call("POST", "/api/v1/me/positions/"+pos[0].ID+"/withdraw", ``, 409, nil)

	// limit order: id resolved from the indexer's OrderCreated row for the placing tx
	call("POST", "/api/v1/me/orders", `{"market":"WETH","side":"buy","price":"3600","size":"0.01","fund":true}`, 202, &acc)
	if _, err := db.Exec(ctx, `insert into user_orders_events values ($1,3,777,'weth',$2,'created','buy',36000000,100000000,1,now())`, txHash(acc.ExecutionID), wallet); err != nil {
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
	call("POST", "/api/v1/me/schedules", `{"kind":"dca","target":{"type":"strategy","id":"`+st.ID+`"},"amountUsdc":"10","intervalSeconds":3600,"maxSlippageBps":50,"maxPerRunUsdc":"100"}`, 201, &sc)
	call("POST", "/api/v1/me/schedules", `{"kind":"stop_loss","target":{"type":"market","id":"WETH"},"triggerPrice":"3000","sellBps":10000,"maxSlippageBps":50,"maxPerRunUsdc":"100"}`, 201, nil)
	call("POST", "/api/v1/me/schedules", `{"kind":"rebalance","target":{"type":"position","id":"`+uuid.NewString()+`"},"intervalSeconds":3600,"maxSlippageBps":50,"maxPerRunUsdc":"100"}`, 404, nil)
	var scs []Schedule
	call("GET", "/api/v1/me/schedules", "", 200, &scs)
	if len(scs) != 2 || sc.NextRunAt == nil || sc.Target.Type != "strategy" || sc.Target.ID != st.ID {
		t.Fatalf("schedules: %+v %+v", sc, scs)
	}
	var exs []Execution
	call("GET", "/api/v1/me/executions", "", 200, &exs)
	if len(exs) != 5 {
		t.Fatalf("executions: %d", len(exs))
	}
	var older []Execution
	call("GET", "/api/v1/me/executions?before="+url.QueryEscape(exs[1].CreatedAt.Format(time.RFC3339Nano)), "", 200, &older)
	if len(older) != 3 || older[0].ID != exs[2].ID {
		t.Fatalf("executions before: %+v", older)
	}
	var earn Earnings
	call("GET", "/api/v1/me/earnings", "", 200, &earn) // own strategy: fee paid, nothing owed to self
	if earn.FeesPaidUSDC != "0.4" || earn.OwedUSDC != "0" || earn.PaidUSDC != "0" || len(earn.Rows) != 0 {
		t.Fatalf("earnings: %+v", earn)
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

type fakeTxs struct {
	txs      map[common.Hash]*types.Transaction
	reverted map[common.Hash]bool
}

func (f *fakeTxs) TransactionByHash(_ context.Context, h common.Hash) (*types.Transaction, bool, error) {
	tx, ok := f.txs[h]
	if !ok {
		return nil, false, ethereum.NotFound
	}
	return tx, false, nil
}

func (f *fakeTxs) TransactionReceipt(_ context.Context, h common.Hash) (*types.Receipt, error) {
	if _, ok := f.txs[h]; !ok {
		return nil, ethereum.NotFound
	}
	st := types.ReceiptStatusSuccessful
	if f.reverted[h] {
		st = types.ReceiptStatusFailed
	}
	return &types.Receipt{Status: st}, nil
}

// client-signed trades: prepare renders txs for the user's own wallet (no delegation), record checks them on chain.
func TestClientSigned(t *testing.T) {
	db := testDB(t)
	md := fakeMarketData(t)
	defer md.Close()
	key, _ := crypto.GenerateKey()
	wallet := crypto.PubkeyToAddress(key.PublicKey)
	other, _ := crypto.GenerateKey()
	ftx := &fakeTxs{txs: map[common.Hash]*types.Transaction{}, reverted: map[common.Hash]bool{}}
	// allowance (and every balance) reads 0 → every buy needs its approve
	srv := &Server{Store: PG{DB: db}, Auth: fakeAuth{}, Privy: fakePrivy{},
		Plan:  &planner.Planner{Dep: testDep(t), Q: planner.MarketData{URL: md.URL, HTTP: md.Client()}, MaxStepUSD: 1000},
		Chain: fakeChain{0}, Txs: ftx}
	mux := http.NewServeMux()
	srv.Routes(mux)
	call := func(method, path, body string, wantCode int, out any) {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer good")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != wantCode {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body)
		}
		if out != nil {
			json.Unmarshal(w.Body.Bytes(), out)
		}
	}
	signer := types.LatestSignerForChainID(big.NewInt(143))
	send := func(k *ecdsa.PrivateKey, tx planner.TxRequest, nonce uint64) string {
		to := common.HexToAddress(tx.To)
		v, _ := hexutil.DecodeBig(tx.Value)
		st, err := types.SignNewTx(k, signer, &types.DynamicFeeTx{ChainID: big.NewInt(143), Nonce: nonce, To: &to, Value: v, Data: hexutil.MustDecode(tx.Data), Gas: 1e6})
		if err != nil {
			t.Fatal(err)
		}
		ftx.txs[st.Hash()] = st
		return st.Hash().Hex()
	}
	var prep struct {
		PrepareID string              `json:"prepareId"`
		Steps     []planner.Step      `json:"steps"`
		Txs       []planner.TxRequest `json:"txs"`
		ExpiresAt time.Time           `json:"expiresAt"`
	}
	call("POST", "/api/v1/me/trade/prepare", `{"kind":"trade","market":"WETH","side":"buy","amount":"50","slippageBps":0}`, 400, nil) // no wallet
	call("POST", "/api/v1/me/trade/prepare", `{"kind":"trade","market":"WETH","side":"buy","amount":"50","slippageBps":0,"wallet":"`+wallet.Hex()+`"}`, 200, &prep)
	if len(prep.Steps) != 1 || len(prep.Txs) != 2 || prep.Txs[0].Description != "approve USDC 50000000" || prep.Txs[1].Step != 0 ||
		prep.Txs[1].ChainID != 143 || time.Until(prep.ExpiresAt) <= 0 {
		t.Fatalf("prepare: %+v", prep)
	}
	var hashes []string
	for i, tx := range prep.Txs {
		hashes = append(hashes, send(key, tx, uint64(i)))
	}
	rec := func(hs []string) string {
		b, _ := json.Marshal(map[string]any{"prepareId": prep.PrepareID, "txHashes": hs})
		return string(b)
	}
	call("POST", "/api/v1/me/trade/record", rec([]string{"0x" + strings.Repeat("11", 32)}), 409, nil) // not mined yet
	bad := prep.Txs[0]
	bad.Data = bad.Data[:len(bad.Data)-2] + "00"
	call("POST", "/api/v1/me/trade/record", rec([]string{send(key, bad, 9)}), 400, nil)           // other calldata
	call("POST", "/api/v1/me/trade/record", rec([]string{send(other, prep.Txs[0], 0)}), 400, nil) // other signer

	var e Execution
	call("POST", "/api/v1/me/trade/record", rec(hashes), 200, &e)
	if e.Status != "confirmed" || e.Signer != "client" || e.Kind != "trade" || e.StepDone != 1 ||
		len(e.TxHashes) != 1 || e.TxHashes[0] != strings.ToLower(hashes[1]) {
		t.Fatalf("record: %+v", e)
	}
	var again Execution
	call("POST", "/api/v1/me/trade/record", rec(hashes), 200, &again)
	if again.ID != e.ID {
		t.Fatalf("record not idempotent: %s vs %s", again.ID, e.ID)
	}
	// no delegation: holdings use the wallet the client signed with
	var h struct {
		Wallet string `json:"wallet"`
	}
	call("GET", "/api/v1/me/holdings", "", 200, &h)
	if h.Wallet != strings.ToLower(wallet.Hex()) {
		t.Fatalf("holdings: %+v", h)
	}

	// a trade whose buy reverts after its approve: recorded as failed with no step done
	call("POST", "/api/v1/me/trade/prepare", `{"kind":"trade","market":"WETH","side":"buy","amount":"10","wallet":"`+wallet.Hex()+`"}`, 200, &prep)
	h1, h2 := send(key, prep.Txs[0], 10), send(key, prep.Txs[1], 11)
	ftx.reverted[common.HexToHash(h2)] = true
	call("POST", "/api/v1/me/trade/record", rec([]string{h1, h2}), 200, &e)
	if e.Status != "failed" || e.StepDone != 0 || e.Kind != "trade" {
		t.Fatalf("reverted: %+v", e)
	}
}

// multi-token strategy row: asset MULTI, sources [], sleeves jsonb; reads back valid for the planner.
func TestStrategySleevesStore(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	p := PG{DB: db}
	if _, err := db.Exec(ctx, `insert into users(id) values ('did:privy:m')`); err != nil {
		t.Fatal(err)
	}
	var in StrategyInput
	json.Unmarshal([]byte(`{"name":"mix","sleeves":[{"asset":"USDC","weightBps":4000,"sources":[{"kind":"lending","venue":"aave","weightBps":10000}]},
		{"asset":"WETH","weightBps":6000,"sources":[{"kind":"lending","venue":"aave","weightBps":10000}]}],"rules":{"driftBps":300}}`), &in)
	st, err := p.CreateStrategy(ctx, "did:privy:m", in)
	if err != nil {
		t.Fatal(err)
	}
	pl := &planner.Planner{Dep: testDep(t)}
	if st.Asset != planner.Multi || len(st.Sources) != 0 || len(st.Sleeves) != 2 || st.Sleeves[1].Asset != "WETH" || pl.Validate(st.Strategy) != nil {
		t.Fatalf("%+v %v", st, pl.Validate(st.Strategy))
	}
	b, _ := json.Marshal(st.Strategy)
	if !strings.HasPrefix(string(b), `{"asset":"MULTI","sleeves":[{"asset":"USDC","weightBps":4000,`) {
		t.Fatalf("json %s", b)
	}
	in = StrategyInput{Name: "one", Strategy: planner.Strategy{Asset: "USDC", Sources: in.Sleeves[0].Sources}}
	if st, err = p.CreateStrategy(ctx, "did:privy:m", in); err != nil || st.Sleeves != nil || st.Asset != "USDC" {
		t.Fatalf("single: %+v %v", st, err)
	}
}
