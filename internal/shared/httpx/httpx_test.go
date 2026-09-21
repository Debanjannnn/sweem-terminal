package httpx

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEnvelopes(t *testing.T) {
	w := httptest.NewRecorder()
	OK(w, map[string]string{"id": "1"})
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("code %d ct %q", w.Code, w.Header().Get("Content-Type"))
	}
	var env Envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error != nil {
		t.Fatal("ok envelope carried an error")
	}

	w = httptest.NewRecorder()
	Fail(w, http.StatusBadRequest, "bad_market", "Market 0xabc is not listed on this chain, so the order was rejected.")
	if w.Code != 400 {
		t.Fatalf("code %d", w.Code)
	}
	env = Envelope{}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Data != nil || env.Error == nil || env.Error.Code != "bad_market" {
		t.Fatalf("bad error envelope %+v", env)
	}
}

func TestCORS(t *testing.T) {
	tests := []struct {
		name    string
		origins []string
		origin  string
		method  string
		wantACO string
		wantSt  int
	}{
		{"allowed", []string{"https://a.dev"}, "https://a.dev", "GET", "https://a.dev", 200},
		{"denied", []string{"https://a.dev"}, "https://evil.dev", "GET", "", 200},
		{"wildcard echoes origin", []string{"*"}, "https://any.dev", "GET", "https://any.dev", 200},
		{"no origin header", []string{"*"}, "", "GET", "", 200},
		{"preflight short circuits", []string{"*"}, "https://a.dev", "OPTIONS", "https://a.dev", 204},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			h := CORS(tt.origins, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
			r := httptest.NewRequest(tt.method, "/", nil)
			if tt.origin != "" {
				r.Header.Set("Origin", tt.origin)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if got := w.Header().Get("Access-Control-Allow-Origin"); got != tt.wantACO {
				t.Fatalf("allow-origin %q want %q", got, tt.wantACO)
			}
			if w.Code != tt.wantSt {
				t.Fatalf("status %d want %d", w.Code, tt.wantSt)
			}
			if called == (tt.method == "OPTIONS") {
				t.Fatalf("next called = %v for %s", called, tt.method)
			}
		})
	}
}

func TestLoggingRecordsStatus(t *testing.T) {
	var sb strings.Builder
	log := slog.New(slog.NewTextHandler(&sb, nil))
	h := Logging(log, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		io.WriteString(w, "nope")
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/orders", nil))
	out := sb.String()
	if !strings.Contains(out, "status=418") || !strings.Contains(out, "path=/orders") || !strings.Contains(out, "bytes=4") {
		t.Fatalf("log line missing fields: %s", out)
	}
}
