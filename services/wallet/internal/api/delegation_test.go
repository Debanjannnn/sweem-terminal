package api

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func future() string { return time.Now().Add(time.Hour).UTC().Format(time.RFC3339) }

func TestPutDelegationValidation(t *testing.T) {
	tests := []struct{ name, body, says string }{
		{"no signer", `{"venues":["kuru"],"markets":["MON-USDC"],"max_notional_per_slice":"1","max_notional_total":"2","expires_at":"EXP"}`, "signer_id"},
		{"empty allowlist", `{"signer_id":"s","venues":[],"markets":[],"max_notional_per_slice":"1","max_notional_total":"2","expires_at":"EXP"}`, "at least one"},
		{"unknown venue", `{"signer_id":"s","venues":["perpl"],"markets":["MON-USDC"],"max_notional_per_slice":"1","max_notional_total":"2","expires_at":"EXP"}`, "venue"},
		{"fractional cap", `{"signer_id":"s","venues":["kuru"],"markets":["MON-USDC"],"max_notional_per_slice":"1.5","max_notional_total":"2","expires_at":"EXP"}`, "base units"},
		{"slice cap above total", `{"signer_id":"s","venues":["kuru"],"markets":["MON-USDC"],"max_notional_per_slice":"3","max_notional_total":"2","expires_at":"EXP"}`, "larger than"},
		{"already expired", `{"signer_id":"s","venues":["kuru"],"markets":["MON-USDC"],"max_notional_per_slice":"1","max_notional_total":"2","expires_at":"2020-01-01T00:00:00Z"}`, "future"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.registered(t)
			rec := h.do(t, http.MethodPut, "/v1/delegation", replaceExp(tt.body))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body)
			}
			if msg, _ := body(t, rec)["message"].(string); !containsFold(msg, tt.says) {
				t.Fatalf("message %q does not name %q", msg, tt.says)
			}
		})
	}
}

func TestDelegationLifecycle(t *testing.T) {
	h := newHarness(t)
	h.registered(t)

	if rec := h.do(t, http.MethodGet, "/v1/delegation", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 before any delegation", rec.Code)
	}

	put := replaceExp(`{"signer_id":"signer-1","venues":["kuru"],"markets":["MON-USDC"],"max_notional_per_slice":"100","max_notional_total":"1000","expires_at":"EXP"}`)
	if rec := h.do(t, http.MethodPut, "/v1/delegation", put); rec.Code != http.StatusOK {
		t.Fatalf("put: status = %d, body = %s", rec.Code, rec.Body)
	}
	got := body(t, h.do(t, http.MethodGet, "/v1/delegation", ""))
	if got["live"] != true {
		t.Fatalf("delegation is not live: %v", got)
	}

	if rec := h.do(t, http.MethodDelete, "/v1/delegation", ""); rec.Code != http.StatusOK {
		t.Fatalf("delete: status = %d, body = %s", rec.Code, rec.Body)
	}
	if got := body(t, h.do(t, http.MethodGet, "/v1/delegation", "")); got["live"] != false {
		t.Fatalf("delegation is still live after revocation: %v", got)
	}
	if rec := h.do(t, http.MethodDelete, "/v1/delegation", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("second delete: status = %d, want 404", rec.Code)
	}
}

func replaceExp(s string) string { return strings.ReplaceAll(s, `"EXP"`, `"`+future()+`"`) }
