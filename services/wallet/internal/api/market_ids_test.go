package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/policy"
)

// the id the browser actually holds: market-data lists a kuru market by its address
const monAddr = "0xfdbe356828c8f5a5d5ed4f69dde0816f4058ef61"

func (h *harness) delegatedOn(t *testing.T, markets ...string) {
	t.Helper()
	if _, err := h.store.PutDelegation(context.Background(), policy.Delegation{
		UserID: did, SignerID: "signer-1", Venues: []string{"kuru"}, Markets: markets,
		MaxNotionalPerSlice: "1000000000000000000000", MaxNotionalTotal: "1000000000000000000000",
		ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
}

// The executor takes a name and never an address. Every write the browser
// makes names the market by address, so each must arrive as the allowlist name.
func TestClientMarketAddressReachesTheExecutorAsItsName(t *testing.T) {
	for _, tc := range []struct {
		name, path, body, execPath string
	}{
		{"market order", "/v1/orders",
			`{"market_id":"` + monAddr + `","venue":"kuru","side":"buy","order_type":"market","size":"25000000","idempotency_key":"a"}`, "/route"},
		{"limit order", "/v1/orders",
			`{"market_id":"` + monAddr + `","venue":"kuru","side":"buy","order_type":"limit","price":"20000","quantity":"100000000","idempotency_key":"b"}`, "/order"},
		{"deposit", "/v1/account/deposit",
			`{"market_id":"` + monAddr + `","token":"quote","amount":"25000000"}`, "/account/deposit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.registered(t)
			h.delegatedOn(t, monAddr)

			if rec := h.do(t, http.MethodPost, tc.path, tc.body); rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
			}
			h.exec.sawOnly(t, tc.execPath)
			if got := h.exec.bodies[0]["market_id"]; got != "MON-USDC" {
				t.Fatalf("executor got market_id %v, want the allowlist name MON-USDC", got)
			}
			// the intent keeps the id the rest of the system joins on
			for _, in := range h.store.intents {
				if in.MarketID != monAddr {
					t.Fatalf("intent recorded market %q, want %q", in.MarketID, monAddr)
				}
			}
		})
	}
}

func TestCancelResolvesTheMarketAddress(t *testing.T) {
	h := newHarness(t)
	h.registered(t)
	h.delegatedOn(t, monAddr)
	h.exec.status = "canceled"

	rec := h.do(t, http.MethodDelete, "/v1/orders/"+monAddr+"/3?expected_order_id=42", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	h.exec.sawOnly(t, "/order/cancel")
	if got := h.exec.bodies[0]["market_id"]; got != "MON-USDC" {
		t.Fatalf("executor got market_id %v, want MON-USDC", got)
	}
}

// An address the allowlist does not hold is refused before anything is signed.
func TestUnlistedAddressNeverReachesTheExecutor(t *testing.T) {
	h := newHarness(t)
	h.registered(t)
	const evil = "0x00000000000000000000000000000000000000ee"
	h.delegatedOn(t, evil)

	rec := h.do(t, http.MethodPost, "/v1/orders",
		`{"market_id":"`+evil+`","venue":"kuru","side":"buy","size":"25000000","idempotency_key":"c"}`)
	if rec.Code == http.StatusOK {
		t.Fatalf("an unlisted market was accepted: %s", rec.Body)
	}
	if h.exec.calls != 0 {
		t.Fatalf("executor was called %d times for an unlisted market", h.exec.calls)
	}
}
