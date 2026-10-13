//go:build integration

package sched

import (
	"context"
	"net/http"
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
	"github.com/snehendu098/sweem-terminal/internal/perpl"
	"github.com/snehendu098/sweem-terminal/internal/pg"
	"github.com/snehendu098/sweem-terminal/internal/planner"
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

// testRedis uses a separate logical db so a running publisher's keys are never touched; flushed per test.
func testRedis(t *testing.T) *redis.Client {
	opt, err := redis.ParseURL(env("SCHED_TEST_REDIS_URL", "redis://localhost:6380/7"))
	if err != nil {
		t.Fatal(err)
	}
	c := redis.NewClient(opt)
	c.FlushDB(context.Background())
	t.Cleanup(func() { c.FlushDB(context.Background()); c.Close() })
	return c
}

type env_ struct {
	db    *pgxpool.Pool
	rdb   *redis.Client
	ex    *fakeExecutor
	chain *fakeChain
	q     *fakeQ
	a, b  *Scheduler
}

const user, wallet = "did:privy:u", "0xabc0000000000000000000000000000000000001"

func setup(t *testing.T) *env_ {
	e := &env_{db: testDB(t), rdb: testRedis(t), ex: newFakeExecutor(t), chain: &fakeChain{wallet: map[string]decimal.Decimal{}}}
	dep := testDep(t)
	e.q = &fakeQ{dep: dep, px: map[string]string{"WETH": "4000", "MON": "0.03", "cbBTC": "100000", "XAUt0": "3500"}}
	for _, q := range []string{
		`insert into users(id) values ('did:privy:u')`,
		`insert into delegations(user_id, privy_wallet_id, wallet_address, key_quorum_id) values ('did:privy:u','w','0xAbC0000000000000000000000000000000000001','kq')`,
	} {
		e.exec(t, q)
	}
	mk := func() *Scheduler {
		return &Scheduler{DB: e.db, RDB: e.rdb, Dep: dep, Chain: e.chain,
			P:    &planner.Planner{Dep: dep, Q: e.q, MaxStepUSD: 1000},
			Exec: planner.Executor{URL: e.ex.URL, Secret: "s3cret", Caller: "scheduler", HTTP: http.DefaultClient},
			Tick: time.Second, PositionEvery: time.Minute, MaxAge: time.Minute, Slippage: 50, GateBand: d("0.02"), GuardBuffer: d("0.03")}
	}
	e.a, e.b = mk(), mk()
	e.beat(t)
	for sym, px := range e.q.px {
		e.ref(t, sym, px, time.Now())
	}
	return e
}

func (e *env_) exec(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, err := e.db.Exec(context.Background(), q, args...); err != nil {
		t.Fatal(err)
	}
}

func (e *env_) beat(t *testing.T) {
	if err := e.rdb.Set(context.Background(), streams.HeartbeatKey, "1", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
}

func (e *env_) ref(t *testing.T, sym, px string, ts time.Time) {
	ob := strings.ToLower(e.a.Dep.Kuru.Markets[sym].Orderbook)
	r := streams.RefPrice{Market: streams.Addr(ob), Mid: d(px), Ref: d(px), Ts: ts.UnixMilli()}
	if err := streams.Publish(context.Background(), e.rdb, "", streams.Ref(ob), r); err != nil {
		t.Fatal(err)
	}
}

// race runs both instances' schedule pass concurrently, several times.
func (e *env_) race(now time.Time) {
	var wg sync.WaitGroup
	for range 4 {
		for _, s := range []*Scheduler{e.a, e.b} {
			wg.Add(1)
			go func() { defer wg.Done(); s.schedules(context.Background(), now) }()
		}
	}
	wg.Wait()
}

// calls waits for async executor calls to settle and returns them.
func (e *env_) calls(want int) []string {
	deadline := time.Now().Add(3 * time.Second)
	for len(e.ex.IDs()) < want && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond) // let a duplicate show up
	return e.ex.IDs()
}

func (e *env_) steps(t *testing.T, id string) (string, []planner.Step) {
	t.Helper()
	var kind string
	var steps []planner.Step
	if err := e.db.QueryRow(context.Background(), `select kind, steps from executions where id = $1`, id).Scan(&kind, &steps); err != nil {
		t.Fatal(err)
	}
	return kind, steps
}

func TestDCAFiresOnceAcrossInstances(t *testing.T) {
	e := setup(t)
	next := time.Now().Add(-90 * time.Minute).Truncate(time.Second)
	var sid string
	if err := e.db.QueryRow(context.Background(), `insert into schedules(user_id,kind,target_type,target_id,amount_usdc,interval_seconds,next_run_at,max_slippage_bps,max_per_run_usdc)
		values ($1,'dca','market','WETH',100,3600,$2,50,200) returning id`, user, next).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	// heartbeat missing ⇒ whole tick skipped
	e.rdb.Del(context.Background(), streams.HeartbeatKey)
	e.a.tick(context.Background(), time.Now())
	if n := len(e.calls(0)); n != 0 {
		t.Fatalf("fired without heartbeat: %d", n)
	}
	e.beat(t)
	now := time.Now()
	e.race(now)
	ids := e.calls(1)
	if len(ids) != 1 {
		t.Fatalf("calls = %v", ids)
	}
	kind, steps := e.steps(t, ids[0])
	if kind != "dca" || len(steps) != 1 || steps[0].Action != "kuru.marketBuy" || steps[0].Market != "WETH" || steps[0].Quote != "100000000" {
		t.Fatalf("%s %+v", kind, steps)
	}
	var slot int64
	var gotNext time.Time
	e.db.QueryRow(context.Background(), `select e.slot, s.next_run_at from executions e join schedules s on s.id = e.schedule_id`).Scan(&slot, &gotNext)
	if slot != Slot(next, 3600) || !gotNext.Equal(next.Add(2*time.Hour)) {
		t.Fatalf("slot %d next %v", slot, gotNext)
	}
}

func TestTakeProfitFiresOnce(t *testing.T) {
	e := setup(t)
	e.chain.wallet["WETH"] = d("500000000000000000") // 0.5 WETH
	e.exec(t, `insert into schedules(user_id,kind,target_type,target_id,trigger_price,sell_bps,max_slippage_bps,max_per_run_usdc)
		values ($1,'take_profit','market','WETH',4100,5000,50,1000)`, user)
	e.ref(t, "WETH", "4200", time.Now().Add(-5*time.Minute)) // above trigger but stale
	e.race(time.Now())
	e.ref(t, "WETH", "4000", time.Now()) // fresh, below trigger
	e.race(time.Now())
	if n := len(e.calls(0)); n != 0 {
		t.Fatalf("fired on stale/low price: %d", n)
	}
	e.ref(t, "WETH", "4150", time.Now())
	e.race(time.Now())
	e.race(time.Now())
	ids := e.calls(1)
	if len(ids) != 1 {
		t.Fatalf("calls = %v", ids)
	}
	kind, steps := e.steps(t, ids[0])
	if kind != "take_profit" || len(steps) != 1 || steps[0].Action != "kuru.marketSell" || steps[0].Size != "250000000000000000" {
		t.Fatalf("%s %+v", kind, steps)
	}
	var active bool
	e.db.QueryRow(context.Background(), `select active from schedules`).Scan(&active)
	if active {
		t.Fatal("still active")
	}
}

// recurring buy: a dca schedule on a strategy opens a new position per run, with its fee row, once across instances.
func TestRecurringBuy(t *testing.T) {
	e := setup(t)
	var stid string
	e.exec(t, `insert into users(id) values ('did:privy:creator')`)
	e.db.QueryRow(context.Background(), `insert into strategies(creator,name,asset,sources) values ('did:privy:creator','b','USDC','[{"kind":"lending","venue":"aave","weightBps":10000}]') returning id`).Scan(&stid)
	e.exec(t, `insert into schedules(user_id,kind,target_type,target_id,amount_usdc,interval_seconds,next_run_at,max_slippage_bps,max_per_run_usdc)
		values ($1,'dca','strategy',$2,100,3600,now() - interval '1 minute',50,100)`, user, stid)
	e.race(time.Now())
	ids := e.calls(1)
	if len(ids) != 1 {
		t.Fatalf("calls = %v", ids)
	}
	kind, steps := e.steps(t, ids[0])
	if kind != "deploy" || len(steps) != 2 || steps[0].Action != "fee.transfer" || steps[0].Amount != "400000" || steps[1].Action != "lend.supply" || steps[1].Amount != "99600000" {
		t.Fatalf("%s %+v", kind, steps)
	}
	var status, principal, fee, owed string
	deadline := time.Now().Add(3 * time.Second)
	for status != "open" && time.Now().Before(deadline) {
		e.db.QueryRow(context.Background(), `select p.status, p.principal_usdc::text, f.fee_usdc::text, f.creator_owed_usdc::text from strategy_positions p
			join executions x on x.id = p.execution_id join fee_ledger f on f.execution_id = x.id where x.id = $1 and p.user_id = $2`, ids[0], user).Scan(&status, &principal, &fee, &owed)
		time.Sleep(20 * time.Millisecond)
	}
	if status != "open" || principal != "100" || fee != "0.4" || owed != "0.2" {
		t.Fatalf("position %s %s fee %s owed %s", status, principal, fee, owed)
	}
}

// seedPosition: WETH strategy, 70% aave / 30% basis (2x), basis open; returns position id.
func seedPosition(t *testing.T, e *env_, gate string) string {
	var stid, pid string
	sources := `[{"kind":"lending","venue":"aave","weightBps":7000},{"kind":"basis","venue":"perpl","weightBps":3000,"params":{"leverage":"2"}}]`
	rules := `{"driftBps":500,"minRebalanceSeconds":86400,"fundingGate":{"apr":"` + gate + `","lookbackDays":7}}`
	e.db.QueryRow(context.Background(), `insert into strategies(creator,name,asset,sources,rules) values ($1,'s','WETH',$2,$3) returning id`, user, sources, rules).Scan(&stid)
	legs := `[{"kind":"lending","venue":"aave","asset":"140000000000000000"},{"kind":"basis","venue":"perpl","asset":"40000000000000000","collateral":"80000000","lot":"40","entryPx":"4000"}]`
	e.db.QueryRow(context.Background(), `insert into strategy_positions(strategy_id,user_id,wallet,principal_usdc,status,legs,last_rebalance_at)
		values ($1,$2,$3,800,'open',$4,now()) returning id`, stid, user, wallet, legs).Scan(&pid)
	e.chain.bals = []planner.Balance{{Value: "140000000000000000", Shares: "140000000000000000"}, {Value: "60000000000000000"}}
	e.chain.perp = healthyPerp()
	return pid
}

func healthyPerp() perpl.Perp {
	return perpl.Perp{Account: 7, Deposit: d("80000000"), Mark: d("400000"), Lot: d("40"), PriceDec: 2, LotDec: 3, MaintHdths: d("2000"), MarkValid: true}
}

func (e *env_) rates(t *testing.T, apr string) {
	s := streams.RatesSnapshot{Ts: time.Now().UnixMilli(),
		Rates:   []streams.RateInfo{{Source: "lending", Venue: "aave", Asset: "WETH", Apy: d("0.05")}},
		Funding: []streams.FundingInfo{{PerpID: 20, Name: "ETH", AprShort7d: d(apr)}}}
	if err := streams.Publish(context.Background(), e.rdb, "", streams.RatesLatest, s); err != nil {
		t.Fatal(err)
	}
}

func (e *env_) evaluate(t *testing.T) {
	var wg sync.WaitGroup
	for _, s := range []*Scheduler{e.a, e.b} {
		wg.Add(1)
		go func() { defer wg.Done(); s.positions(context.Background(), time.Now()) }()
	}
	wg.Wait()
}

func (e *env_) confirmAll(t *testing.T) { e.exec(t, `update executions set status = 'confirmed'`) }

func TestFundingGateFlips(t *testing.T) {
	e := setup(t)
	pid := seedPosition(t, e, "0.05")
	series := []struct {
		apr, action string // action: first step kind of the expected perpl order, "" = no execution
	}{
		{"0.08", ""},           // open, above threshold, in band
		{"0.04", "closeShort"}, // below threshold ⇒ unwind into aave
		{"0.06", ""},           // closed, above threshold but within hysteresis band
		{"0.08", "openShort"},  // above threshold+band ⇒ re-enable
	}
	n := 0
	for i, s := range series {
		e.rates(t, s.apr)
		e.evaluate(t)
		if s.action != "" {
			n++
		}
		ids := e.calls(n)
		if len(ids) != n {
			t.Fatalf("step %d (apr %s): calls %v", i, s.apr, ids)
		}
		if s.action == "" {
			continue
		}
		kind, steps := e.steps(t, ids[n-1])
		var got string
		for _, st := range steps {
			if st.Action == "perpl.order" {
				got = st.Kind
			}
		}
		if kind != "rebalance" || got != s.action {
			t.Fatalf("step %d: %s %+v", i, kind, steps)
		}
		legs := func() []planner.LegState {
			var l []planner.LegState
			e.db.QueryRow(context.Background(), `select legs from strategy_positions where id = $1`, pid).Scan(&l)
			return l
		}
		if !d(orZero(legs()[1].Lot)).IsPositive() != (s.action == "openShort") { // unchanged until confirmed
			t.Fatalf("step %d: legs applied before confirm %+v", i, legs())
		}
		e.confirmAll(t)
		if err := e.a.reconcile(context.Background()); err != nil {
			t.Fatal(err)
		}
		if open := d(orZero(legs()[1].Lot)).IsPositive(); open != (s.action == "openShort") {
			t.Fatalf("step %d: legs not applied on confirm %+v", i, legs())
		}
		if s.action == "closeShort" { // wallet now: everything in aave, no short
			e.chain.bals = []planner.Balance{{Value: "199000000000000000", Shares: "199000000000000000"}, {Value: "0"}}
			e.chain.perp = perpl.Perp{Account: 7}
		}
	}
}

func TestLiquidationGuard(t *testing.T) {
	e := setup(t)
	seedPosition(t, e, "0.05")
	e.rates(t, "0.10")
	e.chain.wallet["USDC"] = d("500000000")
	// ETH ran to 5800: equity 80 − 72 = 8 on notional 232 ⇒ 3.4% < 5% + 3%
	p := healthyPerp()
	p.Mark, p.PnL = d("580000"), d("-72000000")
	e.chain.perp = p
	e.evaluate(t)
	e.evaluate(t) // pending execution blocks a second guard
	ids := e.calls(1)
	if len(ids) != 1 {
		t.Fatalf("calls %v", ids)
	}
	kind, steps := e.steps(t, ids[0])
	// need 11% × 232 − 8 = 17.52 ⇒ swap 17.52 / 0.99, deposit its minOut, move it into the position
	if kind != "margin_guard" || len(steps) != 3 || steps[0].Action != "stable.swap" || steps[0].Amount != "17696970" ||
		steps[1].Action != "perpl.deposit" || steps[2].Kind != "addMargin" || steps[2].Amount != steps[1].Amount {
		t.Fatalf("%s %+v", kind, steps)
	}
	// no USDC left ⇒ cut the short: keep 40 × 8 / 25.52 = 12, re-add the released (8 − 0.232) × 28/40
	e.confirmAll(t)
	e.chain.wallet["USDC"] = decimal.Zero
	e.evaluate(t)
	ids = e.calls(2)
	if len(ids) != 2 {
		t.Fatalf("calls %v", ids)
	}
	kind, steps = e.steps(t, ids[1])
	if kind != "margin_guard" || len(steps) != 2 || steps[0].Kind != "closeShort" || steps[0].Lot != "28" ||
		steps[1].Kind != "addMargin" || steps[1].Amount != "5437600" || steps[1].Perp != "ETH" {
		t.Fatalf("%s %+v", kind, steps)
	}
	// wiped out ⇒ last resort: full close, spot parked in aave
	e.confirmAll(t)
	p.PnL = d("-80000000")
	e.chain.perp = p
	e.evaluate(t)
	ids = e.calls(3)
	if len(ids) != 3 {
		t.Fatalf("calls %v", ids)
	}
	var legs []planner.LegState
	e.db.QueryRow(context.Background(), `select legs from strategy_positions`).Scan(&legs)
	if legs[1].Lot != "12" { // cut applied on confirm, before the close was planned
		t.Fatalf("legs %+v", legs)
	}
	_, steps = e.steps(t, ids[2])
	if steps[0].Kind != "closeShort" || steps[0].Lot != "12" || steps[len(steps)-1].Action != "lend.supply" {
		t.Fatalf("%+v", steps)
	}
}

func TestPositionTakeProfitPartial(t *testing.T) {
	e := setup(t)
	pid := seedPosition(t, e, "0.05")
	e.exec(t, `insert into schedules(user_id,kind,target_type,target_id,trigger_price,sell_bps,max_slippage_bps,max_per_run_usdc)
		values ($1,'take_profit','position',$2,700,5000,50,1000)`, user, pid) // position worth 0.2 WETH × 4000 = 800
	e.race(time.Now())
	ids := e.calls(1)
	if len(ids) != 1 {
		t.Fatalf("calls %v", ids)
	}
	_, steps := e.steps(t, ids[0])
	var closeLot string
	for _, st := range steps {
		if st.Kind == "closeShort" {
			closeLot = st.Lot
		}
	}
	if closeLot != "20" || steps[len(steps)-1].Action != "kuru.marketSell" {
		t.Fatalf("%+v", steps)
	}
	var status string
	e.db.QueryRow(context.Background(), `select status from strategy_positions`).Scan(&status)
	if status != "open" {
		t.Fatal(status)
	}
	e.confirmAll(t)
	e.a.reconcile(context.Background())
	var legs []planner.LegState
	e.db.QueryRow(context.Background(), `select legs from strategy_positions`).Scan(&legs)
	if legs[0].Asset != "70000000000000000" || legs[1].Lot != "20" {
		t.Fatalf("legs %+v", legs)
	}
}

func TestPositionStopLossFullWithdraw(t *testing.T) {
	e := setup(t)
	pid := seedPosition(t, e, "0.05")
	e.exec(t, `insert into schedules(user_id,kind,target_type,target_id,trigger_price,sell_bps,max_slippage_bps,max_per_run_usdc)
		values ($1,'stop_loss','position',$2,900,10000,50,1000)`, user, pid) // worth 800 ≤ 900
	e.race(time.Now())
	ids := e.calls(1)
	if len(ids) != 1 {
		t.Fatalf("calls %v", ids)
	}
	kind, steps := e.steps(t, ids[0])
	if kind != "withdraw" || len(steps) == 0 {
		t.Fatalf("%s %+v", kind, steps)
	}
	var status string
	var legs []byte
	e.db.QueryRow(context.Background(), `select p.status, e.legs from strategy_positions p join executions e on e.position_id = p.id`).Scan(&status, &legs)
	if status != "closed" || legs != nil { // fake executor confirms; api-style withdraw rows carry no legs
		t.Fatalf("status %s legs %s", status, legs)
	}
}

func TestFundingAPRLookback(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	snap := streams.RatesSnapshot{Funding: []streams.FundingInfo{{PerpID: 20, AprShort7d: d("0.2")}}}
	if apr, ok, err := pg.GateAPR(ctx, e.db, snap, 20, 7); err != nil || !ok || !apr.Equal(d("0.2")) {
		t.Fatalf("no history ⇒ 7d fallback: %s %v %v", apr, ok, err)
	}
	day := time.Now().UTC().Truncate(24 * time.Hour)
	for _, r := range []struct {
		ago  int
		h    int
		rate string
	}{{0, 1, "1"}, {1, 1, "0.0001"}, {1, 2, "0.0001"}, {2, 1, "0.0004"}, {10, 1, "1"}} { // today + 10d ago are out of a 7d window
		e.exec(t, `insert into series_funding(perp_id, ts, rate) values (20, $1, $2)`, day.AddDate(0, 0, -r.ago).Add(time.Duration(r.h)*time.Hour), r.rate)
	}
	if apr, ok, err := pg.GateAPR(ctx, e.db, snap, 20, 7); err != nil || !ok || !apr.Equal(d("0.1095")) { // mean(0.0002, 0.0004) × 365
		t.Fatalf("%s %v %v", apr, ok, err)
	}
}

// two drifting sleeves (USDC aave/neverland, WETH aave/euler) ⇒ one rebalance execution, legs merged in flat order.
func TestMultiSleeveRebalance(t *testing.T) {
	e := setup(t)
	sleeves := `[{"asset":"USDC","weightBps":5000,"sources":[{"kind":"lending","venue":"aave","weightBps":5000},{"kind":"lending","venue":"neverland","weightBps":5000}]},
		{"asset":"WETH","weightBps":5000,"sources":[{"kind":"lending","venue":"aave","weightBps":5000},{"kind":"lending","venue":"euler-k3weth","weightBps":5000}]}]`
	var stid string
	e.db.QueryRow(context.Background(), `insert into strategies(creator,name,asset,sources,sleeves,rules) values ($1,'m','MULTI','[]',$2,'{"driftBps":500}') returning id`, user, sleeves).Scan(&stid)
	legs := `[{"token":"USDC","kind":"lending","venue":"aave","asset":"70000000"},{"token":"USDC","kind":"lending","venue":"neverland","asset":"30000000"},
		{"token":"WETH","kind":"lending","venue":"aave","asset":"70000000000000000"},{"token":"WETH","kind":"lending","venue":"euler-k3weth","asset":"30000000000000000"}]`
	e.exec(t, `insert into strategy_positions(strategy_id,user_id,wallet,principal_usdc,status,legs) values ($1,$2,$3,200,'open',$4)`, stid, user, wallet, legs)
	e.chain.bals = []planner.Balance{{Value: "70000000"}, {Value: "30000000"}, {Value: "70000000000000000"}, {Value: "30000000000000000", Shares: "30000000000000000"}}
	e.rates(t, "0.1")
	e.evaluate(t)
	ids := e.calls(1)
	if len(ids) != 1 {
		t.Fatalf("calls %v", ids)
	}
	kind, steps := e.steps(t, ids[0])
	want := []string{"lend.withdraw USDC 20000000", "lend.supply USDC 20000000", "lend.withdraw WETH 20000000000000000", "vault.deposit  20000000000000000"}
	if kind != "rebalance" || len(steps) != len(want) {
		t.Fatalf("%s %+v", kind, steps)
	}
	for i, st := range steps {
		if got := st.Action + " " + st.Asset + " " + st.Amount; got != want[i] {
			t.Errorf("step %d: %s want %s", i, got, want[i])
		}
	}
	e.confirmAll(t)
	if err := e.a.reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	var got []planner.LegState
	e.db.QueryRow(context.Background(), `select legs from strategy_positions`).Scan(&got)
	if len(got) != 4 || got[1].Token != "USDC" || got[1].Asset != "50000000" || got[3].Token != "WETH" || got[3].Asset != "50000000000000000" {
		t.Fatalf("legs %+v", got)
	}
}
