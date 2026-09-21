package api

import (
	"net/http"
	"strings"
	"testing"
)

const orderBody = `{"market_id":"MON-USDC","venue":"kuru","side":"buy","order_type":"market","size":"100","idempotency_key":"abc"}`

// The same order retried is the same order: one intent row, one executor call.
func TestCreateOrderIsIdempotentOnIntentKey(t *testing.T) {
	h := newHarness(t)
	h.registered(t)
	h.delegated(t, "1000", "10000")

	first := h.do(t, http.MethodPost, "/v1/orders", orderBody)
	if first.Code != http.StatusOK {
		t.Fatalf("first order: status = %d, body = %s", first.Code, first.Body)
	}
	key, _ := body(t, first)["intent_key"].(string)
	if key == "" {
		t.Fatal("first order returned no intent_key")
	}

	second := h.do(t, http.MethodPost, "/v1/orders", orderBody)
	if second.Code != http.StatusOK {
		t.Fatalf("retry: status = %d, body = %s", second.Code, second.Body)
	}
	got := body(t, second)
	if got["intent_key"] != key {
		t.Fatalf("retry derived intent_key %v, want the first one %s", got["intent_key"], key)
	}
	if got["replayed"] != true {
		t.Fatalf("retry was not reported as a replay: %v", got)
	}
	if h.exec.calls != 1 {
		t.Fatalf("executor was called %d times, want 1: a retry must not place a second order", h.exec.calls)
	}
	if len(h.store.intents) != 1 {
		t.Fatalf("%d intent rows, want 1", len(h.store.intents))
	}
	if h.store.insertAttempts != 2 {
		t.Fatalf("insert was attempted %d times, want 2: the database is the guard, not the handler", h.store.insertAttempts)
	}
}

// A different idempotency key is a different order, even with identical terms.
func TestCreateOrderDifferentIdempotencyKeyIsANewOrder(t *testing.T) {
	h := newHarness(t)
	h.registered(t)
	h.delegated(t, "1000", "10000")

	if rec := h.do(t, http.MethodPost, "/v1/orders", orderBody); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	second := `{"market_id":"MON-USDC","venue":"kuru","side":"buy","order_type":"market","size":"100","idempotency_key":"def"}`
	if rec := h.do(t, http.MethodPost, "/v1/orders", second); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if h.exec.calls != 2 || len(h.store.intents) != 2 {
		t.Fatalf("executor calls = %d, intents = %d, want 2 and 2", h.exec.calls, len(h.store.intents))
	}
}

func TestIntentKeyIsDeterministicAndPartSensitive(t *testing.T) {
	base := []string{did, "MON-USDC", "kuru", "buy", "market", "100", "abc"}
	want := intentKey(base...)
	if got := intentKey(base...); got != want {
		t.Fatalf("same parts derived %s then %s", want, got)
	}
	for i := range base {
		changed := append([]string(nil), base...)
		changed[i] += "x"
		if intentKey(changed...) == want {
			t.Fatalf("changing part %d did not change the intent key", i)
		}
	}
}

// 412, not 400: the request is well formed, the precondition is missing, and
// the body must name what is missing.
func TestPreconditionsReturn412AndNameWhatIsMissing(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*harness)
		code  string
		says  string
	}{
		{
			name:  "no user row",
			setup: func(*harness) {},
			code:  "user_not_registered",
			says:  "POST /v1/me",
		},
		{
			name:  "no delegation",
			setup: func(h *harness) { h.registered(t) },
			code:  "no_delegation",
			says:  "PUT /v1/delegation",
		},
		{
			name: "order over the per-slice cap",
			setup: func(h *harness) {
				h.registered(t)
				h.delegated(t, "10", "10000")
			},
			code: "over_slice_limit",
			says: "per slice",
		},
		{
			name: "order over the total cap",
			setup: func(h *harness) {
				h.registered(t)
				h.delegated(t, "1000", "50")
			},
			code: "over_total_limit",
			says: "total spent",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			tt.setup(h)
			rec := h.do(t, http.MethodPost, "/v1/orders", orderBody)
			if rec.Code != http.StatusPreconditionFailed {
				t.Fatalf("status = %d, want 412; body = %s", rec.Code, rec.Body)
			}
			got := body(t, rec)
			if got["code"] != tt.code {
				t.Fatalf("code = %v, want %s", got["code"], tt.code)
			}
			msg, _ := got["message"].(string)
			if msg == "" || !containsFold(msg, tt.says) {
				t.Fatalf("message %q does not name %q", msg, tt.says)
			}
			if h.exec.calls != 0 {
				t.Fatalf("executor was called %d times for a refused order", h.exec.calls)
			}
		})
	}
}

func TestCreateOrderRejectsBadRequests(t *testing.T) {
	tests := []struct{ name, body, says string }{
		{"no idempotency key", `{"market_id":"MON-USDC","venue":"kuru","side":"buy","size":"100"}`, "idempotency_key"},
		{"unknown venue", `{"market_id":"MON-USDC","venue":"bogus","side":"buy","size":"100","idempotency_key":"a"}`, "venue"},
		{"unknown side", `{"market_id":"MON-USDC","venue":"kuru","side":"long","size":"100","idempotency_key":"a"}`, "side"},
		{"fractional size", `{"market_id":"MON-USDC","venue":"kuru","side":"buy","size":"1.5","idempotency_key":"a"}`, "size"},
		{"zero size", `{"market_id":"MON-USDC","venue":"kuru","side":"buy","size":"0","idempotency_key":"a"}`, "size"},
		{"limit without price", `{"market_id":"MON-USDC","venue":"kuru","side":"buy","order_type":"limit","size":"100","idempotency_key":"a"}`, "limit_price"},
		{"unsupported chain", `{"market_id":"MON-USDC","venue":"kuru","side":"buy","size":"100","chain_id":1,"idempotency_key":"a"}`, "chain_id"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.registered(t)
			h.delegated(t, "1000", "10000")
			rec := h.do(t, http.MethodPost, "/v1/orders", tt.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body)
			}
			if msg, _ := body(t, rec)["message"].(string); !containsFold(msg, tt.says) {
				t.Fatalf("message %q does not name %q", msg, tt.says)
			}
		})
	}
}

// A failed execution is recorded as failed and reported, not swallowed.
func TestCreateOrderRecordsExecutorFailure(t *testing.T) {
	h := newHarness(t)
	h.registered(t)
	h.delegated(t, "1000", "10000")
	h.exec.status, h.exec.errMsg = "failed", "quote simulation failed"

	rec := h.do(t, http.MethodPost, "/v1/orders", orderBody)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body = %s", rec.Code, rec.Body)
	}
	for key, in := range h.store.intents {
		if in.State != "failed" {
			t.Fatalf("intent %s is %s, want failed", key, in.State)
		}
		if h.store.execs[key].Error == "" {
			t.Fatalf("execution %s recorded no reason for the failure", key)
		}
	}
}

func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}
