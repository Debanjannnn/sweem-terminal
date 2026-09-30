package planner

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
)

func fixture(t *testing.T, name string) []byte {
	b, err := os.ReadFile("../../testdata/contracts/executor/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// the api's real request must equal the executor's request fixture, and both response fixtures must decode.
func TestExecutorContract(t *testing.T) {
	var gotBody []byte
	var gotHdr http.Header
	resp := fixture(t, "user_steps.response.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotHdr = r.Header
		if r.URL.Path != "/api/v1/user/steps" {
			w.WriteHeader(404)
			return
		}
		if strings.Contains(string(gotBody), "reject") {
			w.WriteHeader(409)
			w.Write([]byte(`{"error":"delegation_inactive"}`))
			return
		}
		w.Write(resp)
	}))
	defer srv.Close()
	x := Executor{URL: srv.URL, Secret: "s3", Caller: "api", HTTP: srv.Client()}
	out, err := x.Run(context.Background(), "6f1c2a4e-2b7d-4c1e-9a0b-3f5d8e7c1a20")
	if err != nil {
		t.Fatal(err)
	}
	var want, got any
	json.Unmarshal(fixture(t, "user_steps.request.json"), &want)
	json.Unmarshal(gotBody, &got)
	if js(t, want) != js(t, got) || gotHdr.Get("X-Internal-Secret") != "s3" || gotHdr.Get("X-Caller") != "api" {
		t.Errorf("request %s headers %v", gotBody, gotHdr)
	}
	if out.Status != "confirmed" || out.StepsDone != 2 || len(out.TxHashes) != 2 || out.Error != nil {
		t.Errorf("outcome %+v", out)
	}
	var failed Outcome
	if err := json.Unmarshal(fixture(t, "user_steps_failed.response.json"), &failed); err != nil || failed.Status == "" {
		t.Errorf("failed fixture: %v %+v", err, failed)
	}
	if _, err := x.Run(context.Background(), "reject"); code(err) != "delegation_inactive" {
		t.Errorf("reject: %v", err)
	}
}

func TestMarketDataQuote(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case r.URL.Path != "/api/v1/quote/0xa6afd386135b7d41a6c40c525abc4a1019b0d132" || q.Get("side") != "buy" || q.Get("slippageBps") != "50":
			w.WriteHeader(404)
		case q.Get("amount") == "1":
			w.WriteHeader(400)
			w.Write([]byte(`{"error":"below_min_size"}`))
		case q.Get("amount") == "2":
			w.WriteHeader(503)
			w.Write([]byte(`{"error":"stale"}`))
		default:
			w.Write([]byte(`{"market":"0xa6af","side":"buy","amount":"100000000","fillable":"100000000","partial":false,"expectedOut":"25","minOut":"24","orderSize":"1000000","avgPx":"4000","bestPx":"3999","impactBps":2,"maxSafeAmount":"900000000","maxImpactBps":100}`))
		}
	}))
	defer srv.Close()
	md := MarketData{URL: srv.URL, HTTP: srv.Client()}
	ob := "0xa6afd386135b7d41a6c40c525abc4a1019b0d132"
	q, err := md.Quote(context.Background(), ob, "buy", decimal.NewFromInt(100_000_000), 50)
	if err != nil || q.MinOut != "24" || q.AvgPx != "4000" || q.MaxSafeAmount != "900000000" {
		t.Errorf("%+v %v", q, err)
	}
	if _, err := md.Quote(context.Background(), ob, "buy", decimal.NewFromInt(1), 50); code(err) != "below_min_size" {
		t.Errorf("min: %v", err)
	}
	if _, err := md.Quote(context.Background(), ob, "buy", decimal.NewFromInt(2), 50); code(err) != "quote_unavailable" {
		t.Errorf("stale: %v", err)
	}
}
