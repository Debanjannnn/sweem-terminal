//go:build integration

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
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

func TestFlow(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	const (
		b1     = "0x00000000000000000000000000000000000000a1"
		b2     = "0x00000000000000000000000000000000000000a2"
		wallet = "0x00000000000000000000000000000000000000bb"
	)
	seed := `
insert into baskets values ('` + b1 + `','Blue','BLU','[{"market":"0xm","token":"0xt","weightBps":10000}]',10,'0xc1','` + wallet + `'),
                           ('` + b2 + `','Red','RED','[]',11,'0xc2',null);
insert into mints values ('0x1',0,'` + b1 + `','` + wallet + `',10000000,10000000000000000000,100,now()-interval '3 min'),
                         ('0x2',0,'` + b1 + `','` + wallet + `',5000000,5000000000000000000,101,now()-interval '2 min'),
                         ('0x3',0,'` + b1 + `','0xother',1000000,1000000000000000000,101,now()-interval '2 min'),
                         ('0x4',0,'` + b2 + `','` + wallet + `',1000000,1000000000000000000,102,now()-interval '2 min');
insert into redeems values ('0x5',0,'` + b1 + `','` + wallet + `',3000000000000000000,3100000,103,now()-interval '1 min'),
                           ('0x6',0,'` + b2 + `','` + wallet + `',1000000000000000000,990000,104,now()-interval '1 min');
insert into nav_points values ('` + b1 + `',now()-interval '2 min',1.0,16,16),('` + b1 + `',now()-interval '1 min',1.25,15,12);`
	if _, err := db.Exec(ctx, seed); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	(&Server{Store: PG{DB: db}, Auth: fakeAuth{}, Privy: fakePrivy{}, QuorumID: "q1"}).Routes(mux)
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
			if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
				t.Fatal(err)
			}
		}
	}

	var baskets []Basket
	call("GET", "/api/v1/baskets", "", 200, &baskets)
	if len(baskets) != 2 || baskets[0].Nav == nil || baskets[0].Nav.NavPerShare != "1.25" || baskets[1].Nav != nil {
		t.Fatalf("baskets: %+v", baskets)
	}
	if baskets[0].Creator == nil || *baskets[0].Creator != wallet || baskets[1].Creator != nil {
		t.Fatalf("creators: %+v", baskets)
	}
	call("GET", "/api/v1/baskets?creator=0x00000000000000000000000000000000000000BB", "", 200, &baskets)
	if len(baskets) != 1 || baskets[0].Address != b1 {
		t.Fatalf("by creator: %+v", baskets)
	}
	call("GET", "/api/v1/baskets?creator=nope", "", 400, nil)
	var bd Basket
	call("GET", "/api/v1/baskets/0x00000000000000000000000000000000000000A1", "", 200, &bd)
	if s := bd.Stats; s == nil || s.Mints != 3 || s.Redeems != 1 || s.UsdcInRaw != "16000000" || s.SharesRaw != "13000000000000000000" || s.Holders != 2 {
		t.Fatalf("stats: %+v", bd.Stats)
	}
	if bd.Creator == nil || *bd.Creator != wallet {
		t.Fatalf("detail creator: %+v", bd.Creator)
	}
	call("GET", "/api/v1/baskets/0xnope", "", 404, nil)
	var nav []NavPoint
	call("GET", "/api/v1/baskets/"+b1+"/nav/history", "", 200, &nav)
	if len(nav) != 2 || nav[1].NavPerShare != "1.25" {
		t.Fatalf("nav: %+v", nav)
	}
	call("GET", "/api/v1/baskets/"+b1+"/nav/history?from=bad", "", 400, nil)

	var u User
	call("GET", "/api/v1/me", "", 200, &u)
	if u.ID != "did:privy:u1" || u.Delegation != nil || u.WalletAddress != nil {
		t.Fatalf("me: %+v", u)
	}
	var pos []Position
	call("GET", "/api/v1/me/positions", "", 200, &pos)
	if len(pos) != 0 {
		t.Fatalf("positions before wallet known: %+v", pos)
	}

	sched := `{"basket":"` + b1 + `","kind":"recurring","amountUsdc":"10","intervalSeconds":3600,"maxSlippageBps":50,"maxPerRunUsdc":"12.5"}`
	call("POST", "/api/v1/me/schedules", sched, 409, nil)

	del := `{"walletId":"w1","walletAddress":"0x00000000000000000000000000000000000000BB"}`
	call("POST", "/api/v1/me/delegation", del, 200, nil)
	var d2 Delegation
	call("POST", "/api/v1/me/delegation", strings.Replace(del, "w1", "w2", 1), 200, &d2)
	var active int
	db.QueryRow(ctx, `select count(*) from delegations where user_id='did:privy:u1' and active`).Scan(&active)
	call("GET", "/api/v1/me", "", 200, &u)
	if active != 1 || u.Delegation == nil || u.Delegation.ID != d2.ID || u.Delegation.WalletID != "w2" || *u.WalletAddress != wallet {
		t.Fatalf("delegation: active=%d %+v", active, u)
	}

	call("GET", "/api/v1/me/positions", "", 200, &pos)
	if len(pos) != 1 || pos[0].Basket != b1 || pos[0].SharesRaw != "12000000000000000000" || *pos[0].ValueUSDC != "15.000000" {
		t.Fatalf("positions: %+v", pos)
	}
	var hist []Transfer
	call("GET", "/api/v1/me/history", "", 200, &hist)
	if len(hist) != 5 || hist[0].Kind != "redeem" || hist[0].Block != 104 {
		t.Fatalf("history: %+v", hist)
	}

	var rs, ss Schedule
	call("POST", "/api/v1/me/schedules", sched, 201, &rs)
	call("POST", "/api/v1/me/schedules", `{"basket":"`+b1+`","kind":"stop_loss","stopNavPerShare":"0.9","redeemBps":5000,"maxSlippageBps":100,"maxPerRunUsdc":"1000"}`, 201, &ss)
	call("POST", "/api/v1/me/schedules", strings.Replace(sched, b1, "0x00000000000000000000000000000000000000ff", 1), 404, nil)
	if rs.NextRunAt == nil || *rs.AmountUSDC != "10" || rs.MaxPerRunUSDC != "12.5" || ss.NextRunAt != nil || *ss.StopNavPerShare != "0.9" || *ss.RedeemBps != 5000 {
		t.Fatalf("created: %+v %+v", rs, ss)
	}
	var list []Schedule
	call("GET", "/api/v1/me/schedules", "", 200, &list)
	if len(list) != 2 {
		t.Fatalf("schedules: %+v", list)
	}

	if _, err := db.Exec(ctx, `insert into executions(schedule_id,slot,kind,amount,min_out) values ($1,1,'mint',10000000,9900000000000000000)`, rs.ID); err != nil {
		t.Fatal(err)
	}
	var ex []Execution
	call("GET", "/api/v1/me/executions", "", 200, &ex)
	if len(ex) != 1 || ex[0].Status != "pending" || *ex[0].AmountRaw != "10000000" || *ex[0].MinOutRaw != "9900000000000000000" || ex[0].Basket != b1 {
		t.Fatalf("executions: %+v", ex)
	}

	call("DELETE", "/api/v1/me/schedules/"+rs.ID, "", 200, nil)
	call("DELETE", "/api/v1/me/schedules/"+uuid.NewString(), "", 404, nil)
	call("DELETE", "/api/v1/me/delegation", "", 200, nil)
	call("DELETE", "/api/v1/me/delegation", "", 404, nil)
	var stillActive int
	db.QueryRow(ctx, `select count(*) from schedules where active`).Scan(&stillActive)
	call("GET", "/api/v1/me", "", 200, &u)
	if stillActive != 0 || u.Delegation != nil {
		t.Fatalf("after revoke: active schedules=%d delegation=%+v", stillActive, u.Delegation)
	}
	call("POST", "/api/v1/me/schedules", sched, 409, nil)
}
