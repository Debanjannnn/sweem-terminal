package sched

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/httpx"
	"github.com/snehendu098/sweem-terminal/internal/streams"
)

const fixtures = "../../../../testdata/contracts/executor/"

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(fixtures + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func sameJSON(t *testing.T, got, want []byte) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("%v: %s", err, got)
	}
	json.Unmarshal(want, &w)
	if !reflect.DeepEqual(keys(g), keys(w)) {
		t.Fatalf("shape mismatch\n got  %s\n want %s", got, want)
	}
}

func keys(v any) []string {
	m, _ := v.(map[string]any)
	var k []string
	for s := range m {
		k = append(k, s)
	}
	slices.Sort(k)
	return k
}

// fakeExecutor behaves like the executor's internal router: checks auth headers and request shape against fixtures.
type fakeExecutor struct {
	*httptest.Server
	mu    sync.Mutex
	calls []string // "op:executionId"
	fail  bool
}

func newFakeExecutor(t *testing.T) *fakeExecutor {
	f := &fakeExecutor{}
	h := func(op string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			sameJSON(t, body, fixture(t, "user_"+op+".request.json"))
			var req ExecRequest
			json.Unmarshal(body, &req)
			f.mu.Lock()
			f.calls = append(f.calls, op+":"+req.ExecutionID)
			fail := f.fail
			f.mu.Unlock()
			if fail {
				w.WriteHeader(http.StatusConflict)
				w.Write(fixture(t, "error.response.json"))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write(fixture(t, "user_tx.response.json"))
		}
	}
	mux := http.NewServeMux()
	auth := httpx.RequireInternal("s3cret", "scheduler")
	mux.Handle("POST /api/v1/user/mint", auth(h("mint")))
	mux.Handle("POST /api/v1/user/redeem", auth(h("redeem")))
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func (f *fakeExecutor) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func TestExecutorContract(t *testing.T) {
	f := newFakeExecutor(t)
	x := Executor{URL: f.URL, Secret: "s3cret", HTTP: http.DefaultClient}
	var fx ExecResponse
	json.Unmarshal(fixture(t, "user_tx.response.json"), &fx)
	for _, op := range []string{"mint", "redeem"} {
		res, err := x.Call(context.Background(), op, "6f1c2a4e-2b7d-4c1e-9a0b-3f5d8e7c1a20")
		if err != nil {
			t.Fatal(err)
		}
		if res != fx || res.Status != "submitted" || res.TxHash == "" {
			t.Fatalf("decoded %+v", res)
		}
	}
	f.mu.Lock()
	f.fail = true
	f.mu.Unlock()
	_, err := x.Call(context.Background(), "mint", "x")
	var ee *ExecError
	if !errors.As(err, &ee) || ee.Code != "delegation_inactive" || ee.Status != http.StatusConflict {
		t.Fatalf("err = %v", err)
	}
	if _, err := (Executor{URL: f.URL, Secret: "wrong", HTTP: http.DefaultClient}).Call(context.Background(), "mint", "x"); !errors.As(err, &ee) || ee.Status != 401 {
		t.Fatalf("bad secret: %v", err)
	}
}

func TestRequestEncodingMatchesFixture(t *testing.T) {
	b, _ := json.Marshal(ExecRequest{"6f1c2a4e-2b7d-4c1e-9a0b-3f5d8e7c1a20"})
	if string(b) != strings.TrimSpace(string(fixture(t, "user_mint.request.json"))) {
		t.Fatalf("%s", b)
	}
}

func TestSlotAndNextRun(t *testing.T) {
	iv := int64(3600)
	next := time.Unix(1_000_000_800, 0) // mid-slot anchor
	if s := Slot(next, iv); s != 1_000_000_800/3600 {
		t.Fatal(s)
	}
	// consecutive runs land in consecutive slots
	n := NextRun(next, iv, next)
	if Slot(n, iv) != Slot(next, iv)+1 || !n.Equal(next.Add(time.Hour)) {
		t.Fatal(n)
	}
	// 5.5 intervals late: skip to the first future slot, no backfill
	now := next.Add(5*time.Hour + 30*time.Minute)
	n = NextRun(next, iv, now)
	if !n.Equal(next.Add(6*time.Hour)) || !n.After(now) {
		t.Fatal(n)
	}
	// exactly on a boundary: strictly after now
	now = next.Add(3 * time.Hour)
	if n = NextRun(next, iv, now); !n.Equal(next.Add(4 * time.Hour)) {
		t.Fatal(n)
	}
}

func TestTriggered(t *testing.T) {
	d := decimal.RequireFromString
	p := func(nav string, stale bool) streams.NavPoint {
		return streams.NavPoint{NavPerShare: d(nav), Stale: stale}
	}
	stop := d("0.95")
	cases := []struct {
		p     streams.NavPoint
		alive bool
		want  bool
	}{
		{p("0.95", false), true, true},
		{p("0.90", false), true, true},
		{p("0.96", false), true, false},
		{p("0.90", true), true, false},
		{p("0.90", false), false, false},
		{p("0", false), true, false},
	}
	for i, c := range cases {
		if got := Triggered(c.p, c.alive, stop); got != c.want {
			t.Errorf("case %d: got %v", i, got)
		}
	}
}

func TestQuotes(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/api/v1/quote/0xb" || q.Get("side") != "mint" || q.Get("amount") != "1000000" || q.Get("slippageBps") != "50" {
			t.Errorf("bad query %s", r.URL)
		}
		io.WriteString(w, body)
	}))
	defer srv.Close()
	q := Quotes{URL: srv.URL, HTTP: http.DefaultClient}
	body = `{"minOut":"990000","stale":false}`
	if m, err := q.MinOut(context.Background(), "0xb", "mint", "1000000", 50); err != nil || m != "990000" {
		t.Fatal(m, err)
	}
	body = `{"minOut":"990000","stale":true}`
	if _, err := q.MinOut(context.Background(), "0xb", "mint", "1000000", 50); err == nil {
		t.Fatal("stale accepted")
	}
}
