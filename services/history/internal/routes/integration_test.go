//go:build integration

package routes

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/snehendu098/sweem-terminal/services/history/internal/ingest"
)

// envToken reads ENVIO_API_TOKEN from the env or main/.env.
func envToken(t *testing.T) string {
	if v := os.Getenv("ENVIO_API_TOKEN"); v != "" {
		return v
	}
	f, err := os.Open("../../../../.env")
	if err != nil {
		t.Skip("no ENVIO_API_TOKEN")
	}
	defer f.Close()
	for sc := bufio.NewScanner(f); sc.Scan(); {
		if v, ok := strings.CutPrefix(sc.Text(), "ENVIO_API_TOKEN="); ok {
			return strings.Trim(v, `"'`)
		}
	}
	t.Skip("no ENVIO_API_TOKEN")
	return ""
}

// Live APIs -> throwaway schema -> HTTP series + backtest through the real router.
func TestIngestAndBacktest(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://sweem:sweem@localhost:5433/sweem?sslmode=disable"
	}
	schema := "t_hist_" + uuid.NewString()[:8]
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(ctx, "create schema "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(context.Background(), "drop schema "+schema+" cascade") })
	for _, f := range []string{"0006_history.sql", "0007_candles.sql"} {
		sql, err := os.ReadFile("../../../../migrations/" + f)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(ctx, string(sql)); err != nil {
			t.Fatal(f, err)
		}
	}

	g := &ingest.Ingester{DB: db, HTTP: &http.Client{Timeout: time.Minute}, EnvioToken: envToken(t), Now: time.Now}
	for name, job := range map[string]func(context.Context) error{"llama": g.Llama, "kintsu": g.Kintsu, "funding": g.Funding} {
		if err := job(ctx); err != nil {
			t.Fatal(name, err)
		}
	}
	// 50k blocks around 2026-09-30 00:00 UTC (~block 109.18M): 09-29 closes and is written, 09-30 stays pending
	const from, to = 109_150_000, 109_200_000
	if _, err := db.Exec(ctx, `insert into series_cursor(name,block) values('kuru_trades',$1)`, from); err != nil {
		t.Fatal(err)
	}
	n, err := g.Prices(ctx, to)
	if err != nil || n == 0 {
		t.Fatal(n, err)
	}
	var mark int64
	var days []string
	db.QueryRow(ctx, "select block from series_cursor").Scan(&mark)
	rs, _ := db.Query(ctx, "select distinct to_char(t at time zone 'UTC','YYYY-MM-DD') from candles where interval='1d'")
	for rs.Next() {
		var d string
		rs.Scan(&d)
		days = append(days, d)
	}
	if len(days) != 1 || days[0] != "2026-09-29" || mark <= from || mark >= to {
		t.Fatal("closed days", days, "cursor", mark)
	}
	var hours int
	db.QueryRow(ctx, "select count(*) from candles where interval='1h' and market=$1", ingest.Markets[0].Address).Scan(&hours)
	if hours == 0 || hours > 24 {
		t.Fatal("hour candles", hours)
	}
	// rerun resumes at the open day's first block and still writes nothing for it
	if n2, err := g.Prices(ctx, to); err != nil || n2 != 0 {
		t.Fatal("rerun", n2, err)
	}
	// funding rerun is incremental: no duplicates
	var before, after int
	db.QueryRow(ctx, "select count(*) from series_funding").Scan(&before)
	if err := g.Funding(ctx); err != nil {
		t.Fatal(err)
	}
	db.QueryRow(ctx, "select count(*) from series_funding").Scan(&after)
	if before < 3000 || after-before > 10 {
		t.Fatal("funding rows", before, after)
	}

	mux := http.NewServeMux()
	(&Server{DB: db, Now: time.Now}).Routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	get := func(path string, v any) int {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		json.NewDecoder(resp.Body).Decode(v)
		return resp.StatusCode
	}
	var rates, fund, prices struct{ Data []map[string]any }
	if c := get("/api/v1/series/rates?venue=aave&asset=USDC", &rates); c != 200 || len(rates.Data) < 60 {
		t.Fatal("rates", c, len(rates.Data))
	}
	if c := get("/api/v1/series/funding?perpId=20&from=2026-04-01&to=2026-04-30", &fund); c != 200 || len(fund.Data) != 30 {
		t.Fatal("funding", c, len(fund.Data))
	}
	if c := get("/api/v1/series/prices?market=WETH&from=2026-01-01", &prices); c != 200 || len(prices.Data) == 0 {
		t.Fatal("prices", c, len(prices.Data))
	}
	if c := get("/api/v1/series/funding?perpId=7", &fund); c != 400 {
		t.Fatal("bad perp", c)
	}

	post := func(body string) (int, map[string]any) {
		resp, err := http.Post(srv.URL+"/api/v1/backtest", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var m map[string]any
		json.NewDecoder(resp.Body).Decode(&m)
		return resp.StatusCode, m
	}
	// USDC: aave + ETH basis with funding gate over the Perpl era
	c, m := post(`{"asset":"USDC","amount":"1000","from":"2026-02-16","to":"2026-09-28",
		"legs":[{"kind":"lending","venue":"aave","weight":"0.5"},{"kind":"basis","perpId":20,"weight":"0.5"}],
		"rules":{"driftBps":"500","minIntervalDays":1,"fundingGate":{"apr":"0.05","lookbackDays":7},"basisLeverage":"2"}}`)
	if c != 200 || m["from"] != "2026-07-02" || m["label"] != "historical, mainnet data" {
		t.Fatal(c, m["from"], m["error"], m["detail"])
	}
	// WETH: price coverage is only the ingested few hours, so the window clamps and says so
	c, m = post(`{"asset":"WETH","amount":"1","legs":[{"kind":"lending","venue":"aave","weight":"0.5"},{"kind":"basis","perpId":20,"weight":"0.5"}]}`)
	if c != 200 || !strings.Contains(toJSON(m["coverage"]), "clamped") {
		t.Fatal(c, m["error"], m["detail"], m["coverage"])
	}
	if c, m = post(`{"asset":"USDC","amount":"1","legs":[{"kind":"lending","venue":"nope","weight":"1"}]}`); c != 422 || m["error"] != "no_data" {
		t.Fatal(c, m)
	}
	if c, m = post(`{"asset":"USDC","amount":"1","legs":[{"kind":"lending","venue":"aave","weight":"0.7"}]}`); c != 400 || m["error"] != "bad_request" {
		t.Fatal(c, m)
	}
	if c, _ = post(`{"asset":"USDC","bogus":1}`); c != 400 {
		t.Fatal("unknown field accepted")
	}
}

func toJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
