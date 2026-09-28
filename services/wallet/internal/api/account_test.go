package api

import (
	"net/http"
	"testing"
)

func TestGetAccountReportsTheChainStateAndCachesIt(t *testing.T) {
	h := newHarness(t)
	h.registered(t)

	rec := h.do(t, http.MethodGet, "/v1/account", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	got := body(t, rec)
	for field, want := range map[string]any{
		"registered": true, "user_id": float64(7), "signer_authorized": true,
		"auth_nonce": "6", "chain_id": float64(10143),
		"account_core": "0x6384e9b2Bf3b65e1535403a0A543b5FDA905eE22",
	} {
		if got[field] != want {
			t.Fatalf("%s = %v, want %v", field, got[field], want)
		}
	}

	if rec := h.do(t, http.MethodGet, "/v1/account", ""); rec.Code != http.StatusOK {
		t.Fatalf("second read: status = %d", rec.Code)
	}
	if h.exec.ensures != 1 {
		t.Fatalf("the executor was asked %d times for one fresh account, want 1", h.exec.ensures)
	}
}

// The chain each order clears, in order, each naming what is missing.
func TestKuruPreconditionsReturn412AndNameWhatIsMissing(t *testing.T) {
	tests := []struct {
		name                   string
		registered, authorized bool
		code, says             string
	}{
		{"never deposited", false, false, "kuru_account_unfunded", "no deposit yet"},
		{"signer not authorized", true, false, "kuru_signer_unauthorized", "TRADE delegation"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.registered(t)
			h.delegated(t, "1000", "10000")
			h.exec.registered, h.exec.authorized = tt.registered, tt.authorized

			rec := h.do(t, http.MethodPost, "/v1/orders", orderBody)
			if rec.Code != http.StatusPreconditionFailed {
				t.Fatalf("status = %d, want 412; body = %s", rec.Code, rec.Body)
			}
			got := body(t, rec)
			if got["code"] != tt.code {
				t.Fatalf("code = %v, want %s", got["code"], tt.code)
			}
			if msg, _ := got["message"].(string); !containsFold(msg, tt.says) {
				t.Fatalf("message %q does not name %q", msg, tt.says)
			}
			if h.exec.calls != 0 {
				t.Fatalf("executor was called %d times for a refused order", h.exec.calls)
			}
		})
	}
}

// A deposit is a transaction: the retry of one must replay, not double-spend.
func TestDepositIsIdempotentAndNeedsNoKuruAccount(t *testing.T) {
	h := newHarness(t)
	h.registered(t)
	h.exec.registered, h.exec.authorized = false, false
	const dep = `{"market_id":"MON-USDC","token":"quote","amount":"25000000"}`

	first := h.do(t, http.MethodPost, "/v1/account/deposit", dep)
	if first.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", first.Code, first.Body)
	}
	h.exec.sawOnly(t, "/account/deposit")
	sent := h.exec.bodies[0]
	if sent["token"] != "quote" || sent["amount"] != "25000000" {
		t.Fatalf("/account/deposit carried %v", sent)
	}

	second := h.do(t, http.MethodPost, "/v1/account/deposit", dep)
	if second.Code != http.StatusOK {
		t.Fatalf("retry: status = %d, body = %s", second.Code, second.Body)
	}
	if body(t, second)["replayed"] != true {
		t.Fatalf("retry was not a replay: %v", body(t, second))
	}
	if h.exec.calls != 1 {
		t.Fatalf("the executor was called %d times, want 1: a retried deposit must not deposit twice", h.exec.calls)
	}
	// a deposit funds the account, it does not spend the delegation's budget
	if spent, _ := h.store.SpentTotal(t.Context(), did); spent != "0" {
		t.Fatalf("a deposit consumed %s of the delegation budget", spent)
	}

	t.Run("a deliberate second deposit names itself", func(t *testing.T) {
		rec := h.do(t, http.MethodPost, "/v1/account/deposit",
			`{"market_id":"MON-USDC","token":"quote","amount":"25000000","idempotency_key":"again"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
		}
		if h.exec.calls != 2 {
			t.Fatalf("executor calls = %d, want 2", h.exec.calls)
		}
	})
}

func TestDepositRejectsBadRequests(t *testing.T) {
	tests := []struct{ name, body, says string }{
		{"token is an address", `{"market_id":"MON-USDC","token":"0xee0722ead54f1b4fe97be399be43bc0226a6f97e","amount":"1"}`, "token"},
		{"no token", `{"market_id":"MON-USDC","amount":"1"}`, "token"},
		{"fractional amount", `{"market_id":"MON-USDC","token":"base","amount":"1.5"}`, "amount"},
		{"zero amount", `{"market_id":"MON-USDC","token":"base","amount":"0"}`, "amount"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.registered(t)
			rec := h.do(t, http.MethodPost, "/v1/account/deposit", tt.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body)
			}
			if msg, _ := body(t, rec)["message"].(string); !containsFold(msg, tt.says) {
				t.Fatalf("message %q does not name %q", msg, tt.says)
			}
		})
	}
}

// The authorization epoch keys the intent: a retry inside one epoch replays,
// and the next deliberate grant, which the epoch bump marks, does not.
func TestAuthorizeReplaysWithinAnEpochAndSignsAgainAfterIt(t *testing.T) {
	h := newHarness(t)
	h.registered(t)
	h.exec.authorized = false

	rec := h.do(t, http.MethodPost, "/v1/account/authorize", `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	h.exec.sawOnly(t, "/account/authorize")
	got := body(t, rec)
	if got["auth_nonce"] != "6" || got["tx_hash"] == "" {
		t.Fatalf("authorize answered %v", got)
	}

	if rec := h.do(t, http.MethodPost, "/v1/account/authorize", `{}`); rec.Code != http.StatusOK {
		t.Fatalf("retry: status = %d, body = %s", rec.Code, rec.Body)
	}
	if h.exec.calls != 1 {
		t.Fatalf("the executor was called %d times, want 1: a retried grant must not send a second tx", h.exec.calls)
	}

	// the grant landed, so the epoch advanced; the next change is a new intent
	h.exec.authNonce = "7"
	if rec := h.do(t, http.MethodDelete, "/v1/account/authorize", ""); rec.Code != http.StatusOK {
		t.Fatalf("revoke: status = %d, body = %s", rec.Code, rec.Body)
	}
	if h.exec.calls != 2 || h.exec.paths[1] != "/account/revoke" {
		t.Fatalf("executor saw %v after a revoke", h.exec.paths)
	}
}

// registered is the client's "funded": it is false until a deposit mints the
// Kuru account id, and true on the read after one lands.
func TestRegisteredFlipsOnlyWhenADepositLands(t *testing.T) {
	h := newHarness(t)
	h.registered(t)
	h.exec.registered = false

	if got := body(t, h.do(t, http.MethodGet, "/v1/account", "")); got["registered"] != false {
		t.Fatalf("registered = %v before any deposit, want false", got["registered"])
	}
	rec := h.do(t, http.MethodPost, "/v1/account/deposit",
		`{"market_id":"MON-USDC","token":"quote","amount":"25000000"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("deposit: status = %d, body = %s", rec.Code, rec.Body)
	}
	got := body(t, h.do(t, http.MethodGet, "/v1/account", ""))
	if got["registered"] != true || got["user_id"] != float64(7) {
		t.Fatalf("after a deposit the account reads %v", got)
	}
}

// The expiry emits no event, so a reload must still show it. auth_nonce is what
// says the stored value can no longer be trusted.
func TestSignerExpirySurvivesARefreshAndDiesWithThePermission(t *testing.T) {
	h := newHarness(t)
	h.registered(t)
	h.exec.authorized = false
	const expiry = 2000000000

	rec := h.do(t, http.MethodPost, "/v1/account/authorize", `{"expiry":2000000000}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if got := body(t, rec)["signer_expiry"]; got != float64(expiry) {
		t.Fatalf("authorize answered signer_expiry = %v, want %d", got, expiry)
	}

	// a reload: the row is stale, so this read refreshes from the executor
	h.store.kuru[did] = stale(h.store.kuru[did])
	got := body(t, h.do(t, http.MethodGet, "/v1/account", ""))
	if got["signer_expiry"] != float64(expiry) {
		t.Fatalf("a refresh lost the expiry: %v", got)
	}

	h.exec.authorized = false
	h.store.kuru[did] = stale(h.store.kuru[did])
	got = body(t, h.do(t, http.MethodGet, "/v1/account", ""))
	if got["signer_authorized"] != false || got["signer_expiry"] != float64(0) {
		t.Fatalf("a revoked permission still reports a deadline: %v", got)
	}
}

func TestAuthorizeRejectsALapsedExpiry(t *testing.T) {
	h := newHarness(t)
	h.registered(t)
	rec := h.do(t, http.MethodPost, "/v1/account/authorize", `{"expiry":1000000000}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body)
	}
	if msg, _ := body(t, rec)["message"].(string); !containsFold(msg, "expiry") {
		t.Fatalf("message %q does not name expiry", msg)
	}
	if h.exec.calls != 0 {
		t.Fatalf("executor was called %d times for a lapsed grant", h.exec.calls)
	}
}

// With no backend signer the owner's own wallet trades, so the account reports
// no signer and the client offers no grant; with one, its address is shown.
func TestAccountNamesTheSignerOnlyWhenThereIsOne(t *testing.T) {
	for _, signer := range []string{"", "0x2222222222222222222222222222222222222222"} {
		h := newHarness(t)
		h.registered(t)
		h.exec.signer = signer
		got := body(t, h.do(t, http.MethodGet, "/v1/account", ""))
		v, present := got["signer"]
		if signer == "" && present {
			t.Fatalf("owner-signed account reported a signer: %v", got)
		}
		if signer != "" && v != signer {
			t.Fatalf("signer = %v, want %s", v, signer)
		}
	}
}
