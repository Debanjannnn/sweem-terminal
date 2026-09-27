package api

import (
	"net/http"
	"testing"
)

// 400 MON @ 0.025 on MON-USDC: price_precision 1e6, size_precision 1e8.
const limitBody = `{"market_id":"MON-USDC","venue":"kuru","side":"buy","order_type":"limit",` +
	`"price":"25000","quantity":"40000000000","tif":"gtc","post_only":true,"idempotency_key":"lim"}`

// The whole point of this change: a limit order used to be validated and then
// routed as a market swap. It must reach the maker endpoint and nothing else.
func TestLimitOrderNeverRoutesAsAMarketSwap(t *testing.T) {
	h := newHarness(t)
	h.registered(t)
	h.delegated(t, "10000000000", "100000000000")

	rec := h.do(t, http.MethodPost, "/v1/orders", limitBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	h.exec.sawOnly(t, "/order")

	sent := h.exec.bodies[0]
	for field, want := range map[string]any{
		"market_id": "MON-USDC", "side": "buy",
		"price": "25000", "quantity": "40000000000", "tif": "gtc", "post_only": true,
	} {
		if sent[field] != want {
			t.Fatalf("/order carried %s = %v, want %v", field, sent[field], want)
		}
	}
	// a maker order has no taker amount and no slippage bound to clamp
	if _, ok := sent["amount"]; ok {
		t.Fatalf("/order carried a taker amount: %v", sent)
	}
}

// A market order still takes the taker endpoint, and its size crosses the
// boundary as amount, unscaled.
func TestMarketOrderRoutesAsASwapWithSizeAsAmount(t *testing.T) {
	h := newHarness(t)
	h.registered(t)
	h.delegated(t, "1000", "10000")

	if rec := h.do(t, http.MethodPost, "/v1/orders", orderBody); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	h.exec.sawOnly(t, "/route")
	if got := h.exec.bodies[0]["amount"]; got != "100" {
		t.Fatalf("/route carried amount = %v, want the request's own size 100", got)
	}
}

// The cap is measured in what the order can spend, so a bid is capped on its
// quote notional and not on its book quantity.
func TestLimitOrderIsCappedOnWhatItCanSpend(t *testing.T) {
	h := newHarness(t)
	h.registered(t)
	// 400 MON @ 0.025 reserves 10 USDC = 10_000_000 quote atoms
	h.delegated(t, "9999999", "100000000000")

	rec := h.do(t, http.MethodPost, "/v1/orders", limitBody)
	if rec.Code != http.StatusPreconditionFailed {
		t.Fatalf("status = %d, want 412; body = %s", rec.Code, rec.Body)
	}
	got := body(t, rec)
	if got["code"] != "over_slice_limit" {
		t.Fatalf("code = %v, want over_slice_limit", got["code"])
	}
	if h.exec.calls != 0 {
		t.Fatalf("executor was called %d times for a refused order", h.exec.calls)
	}

	h.delegated(t, "10000000", "100000000000")
	if rec := h.do(t, http.MethodPost, "/v1/orders", limitBody); rec.Code != http.StatusOK {
		t.Fatalf("at exactly the cap: status = %d, body = %s", rec.Code, rec.Body)
	}
}

// A maker order that does not rest is not a failure: the executor is truthful
// and the state it reports is the state recorded.
func TestExecutorStatusIsRecordedAsGiven(t *testing.T) {
	for _, status := range []string{"resting", "not_resting", "filled_or_expired"} {
		t.Run(status, func(t *testing.T) {
			h := newHarness(t)
			h.registered(t)
			h.delegated(t, "10000000", "100000000000")
			h.exec.status = status

			rec := h.do(t, http.MethodPost, "/v1/orders", limitBody)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body)
			}
			if got := body(t, rec)["state"]; got != status {
				t.Fatalf("state = %v, want %s", got, status)
			}
		})
	}
}

func TestCancelOrderNeedsASlotAndTheOrderInIt(t *testing.T) {
	tests := []struct{ name, path, says string }{
		{"no expected order id", "/v1/orders/MON-USDC/3", "expected_order_id"},
		{"zero expected order id", "/v1/orders/MON-USDC/3?expected_order_id=0", "expected_order_id"},
		{"slot past the last maker slot", "/v1/orders/MON-USDC/62?expected_order_id=99", "slot_idx"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.registered(t)
			h.delegated(t, "1000", "10000")
			rec := h.do(t, http.MethodDelete, tt.path, "")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body)
			}
			if msg, _ := body(t, rec)["message"].(string); !containsFold(msg, tt.says) {
				t.Fatalf("message %q does not name %q", msg, tt.says)
			}
		})
	}

	t.Run("a bound cancel reaches the cancel endpoint and spends nothing", func(t *testing.T) {
		h := newHarness(t)
		h.registered(t)
		h.delegated(t, "1000", "10000")
		rec := h.do(t, http.MethodDelete, "/v1/orders/MON-USDC/3?expected_order_id=4242", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
		}
		h.exec.sawOnly(t, "/order/cancel")
		sent := h.exec.bodies[0]
		if sent["slot_idx"] != float64(3) || sent["expected_order_id"] != "4242" {
			t.Fatalf("/order/cancel carried %v", sent)
		}
		spent, _ := h.store.SpentTotal(t.Context(), did)
		if spent != "0" {
			t.Fatalf("a cancel consumed %s of the delegation budget", spent)
		}
	})
}
