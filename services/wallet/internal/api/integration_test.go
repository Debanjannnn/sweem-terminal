//go:build integration

// Runs the real handlers against the live stack: postgres, market-data, the
// executor and the chain behind it. Only the Privy token is minted here, with a
// throwaway key, because a real one needs a browser login.
//
//	make infra && make dev && make executor
//	go test -tags integration ./services/wallet/internal/api/ -run Live -v
package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/config"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/indexerclient"
	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/executor"
	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/marketdata"
	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/store"
)

func liveHarness(t *testing.T) (*harness, string) {
	t.Helper()
	config.LoadDotEnv("../../../../.env")
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL unset; start the stack with make infra")
	}
	db, err := store.New(t.Context(), dsn)
	if err != nil {
		t.Fatalf("postgres: %v", err)
	}
	t.Cleanup(db.Close)

	h := newHarness(t)
	s := &Server{
		Store:              db,
		Market:             marketdata.New(config.GetEnv("MARKET_DATA_URL", "http://localhost:8081")),
		Executor:           executor.New(config.GetEnv("EXECUTOR_URL", "http://localhost:8083")),
		Indexer:            indexerclient.New(),
		Auth:               h.auth,
		Log:                slog.New(slog.NewTextHandler(io.Discard, nil)),
		ChainID:            int64(config.GetEnvInt("CHAIN_ID", 10143)),
		DefaultSlippageBps: 50,
	}
	h.handler = s.Routes()

	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return h, "did:privy:it-" + hex.EncodeToString(b)
}

func (h *harness) as(t *testing.T, subject, method, path, body string) (int, map[string]any) {
	t.Helper()
	rec := h.send(t, method, path, body, map[string]string{"Authorization": "Bearer " + h.token(t, subject)})
	var env struct {
		Data  json.RawMessage `json:"data"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	out := map[string]any{}
	_ = json.Unmarshal(env.Data, &out)
	if env.Error != nil {
		out["_code"], out["_message"] = env.Error.Code, env.Error.Message
	}
	t.Logf("%s %s -> %d %s", method, path, rec.Code, strings.TrimSpace(rec.Body.String()))
	return rec.Code, out
}

func kuruMarkets(t *testing.T) []string {
	t.Helper()
	ids, err := marketdata.New(config.GetEnv("MARKET_DATA_URL", "http://localhost:8081")).Markets(t.Context())
	if err != nil {
		t.Fatalf("market-data: %v", err)
	}
	var out []string
	for _, id := range ids {
		if strings.HasPrefix(id, "0x") {
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		t.Fatal("market-data lists no kuru market")
	}
	return out
}

// A user who has just logged in: registered, no kuru account yet. Every step
// names markets exactly as the browser does, by market-data's id.
func TestLiveSpotJourney(t *testing.T) {
	h, user := liveHarness(t)
	markets := kuruMarkets(t)
	market := markets[0]
	wallet := "0x" + user[len(user)-16:] + strings.Repeat("ab", 12)

	if code, _ := h.as(t, user, "POST", "/v1/me", `{"wallet_address":"`+wallet+`","privy_wallet_id":"it-wallet"}`); code != http.StatusOK {
		t.Fatalf("register: %d", code)
	}
	if code, me := h.as(t, user, "GET", "/v1/me", ""); code != http.StatusOK || !strings.EqualFold(me["wallet_address"].(string), wallet) {
		t.Fatalf("me: %d %v", code, me)
	}

	// the executor proves a wallet id belongs to the address before it reads
	// anything, and this one is fake, so the account is unknown rather than empty
	if code, acct := h.as(t, user, "GET", "/v1/account", ""); code != http.StatusPreconditionFailed || acct["_code"] != "kuru_account_unknown" {
		t.Fatalf("account for an unprovable wallet: %d %v", code, acct)
	}

	if code, _ := h.as(t, user, "GET", "/v1/delegation", ""); code != http.StatusNotFound {
		t.Fatalf("delegation before any grant: %d, want 404", code)
	}
	mjson, _ := json.Marshal(markets)
	del := `{"signer_id":"it-signer","venues":["kuru"],"markets":` + string(mjson) +
		`,"max_notional_per_slice":"1000000000","max_notional_total":"10000000000","expires_at":"` +
		time.Now().Add(time.Hour).UTC().Format(time.RFC3339) + `"}`
	if code, d := h.as(t, user, "PUT", "/v1/delegation", del); code != http.StatusOK || d["live"] != true {
		t.Fatalf("delegate: %d %v", code, d)
	}

	// the account cannot be proven: the order is refused before anything is signed
	code, rej := h.as(t, user, "POST", "/v1/orders",
		`{"market_id":"`+market+`","venue":"kuru","side":"buy","size":"25000000","idempotency_key":"it-1"}`)
	if code != http.StatusPreconditionFailed {
		t.Fatalf("order without a kuru account: %d %v, want 412", code, rej)
	}

	// the deposit reaches the executor under the allowlist name. The fake privy
	// wallet cannot sign, so it fails there, and never on the market lookup.
	code, dep := h.as(t, user, "POST", "/v1/account/deposit",
		`{"market_id":"`+market+`","token":"quote","amount":"25000000","idempotency_key":"it-dep"}`)
	msg, _ := dep["_message"].(string)
	if code == http.StatusOK {
		t.Fatalf("a deposit signed by a wallet that does not exist succeeded: %v", dep)
	}
	if strings.Contains(msg, "allowlist") || strings.Contains(msg, "scales") {
		t.Fatalf("deposit failed on the market id, not on signing: %s", msg)
	}

	code, orders := h.as(t, user, "GET", "/v1/orders", "")
	if code != http.StatusOK {
		t.Fatalf("orders: %d", code)
	}
	if rows, _ := orders["orders"].([]any); len(rows) != 1 {
		t.Fatalf("want the one failed deposit intent, got %v", orders)
	}
	if code, _ := h.as(t, user, "GET", "/v1/executions", ""); code != http.StatusOK {
		t.Fatalf("executions: %d", code)
	}
	if code, p := h.as(t, user, "GET", "/v1/portfolio", ""); code != http.StatusOK {
		t.Fatalf("portfolio: %d %v", code, p)
	}

	// a schedule is refused for the same reason the order was
	if code, _ := h.as(t, user, "POST", "/v1/schedules",
		`{"kind":"twap","market_id":"`+market+`","venue":"kuru","side":"buy","total":"100000000","slice_count":4,"window_seconds":3600,"idempotency_key":"it-s"}`); code != http.StatusPreconditionFailed {
		t.Fatalf("schedule without a kuru account: %d, want 412", code)
	}

	// a perp order for a wallet the executor cannot prove is refused before signing
	if code, rej := h.as(t, user, "POST", "/v1/orders",
		`{"market_id":"32","venue":"perpl","side":"buy","order_type":"market","quantity":"10","leverage":500,"idempotency_key":"it-p"}`); code != http.StatusPreconditionFailed || rej["_code"] != "perpl_account_unknown" {
		t.Fatalf("perp order for an unprovable wallet: %d %v", code, rej)
	}

	if code, _ := h.as(t, user, "DELETE", "/v1/delegation", ""); code != http.StatusOK {
		t.Fatalf("revoke: %d", code)
	}
}

// A real logged-in user, strictly read only: it writes nothing but the /v1/me
// upsert that user's own login already makes. With no backend
// signer configured the owner's own wallet trades, so an account must read as
// needing no grant; before the fix every order was refused for want of one.
//
//	IT_DID=<did:privy:…> IT_PRIVY_WALLET_ID=<its wallet id> IT_WALLET=<its address> go test -tags integration ...
func TestLiveAccountOfARealWallet(t *testing.T) {
	walletID, wallet, user := os.Getenv("IT_PRIVY_WALLET_ID"), os.Getenv("IT_WALLET"), os.Getenv("IT_DID")
	if walletID == "" || wallet == "" || user == "" {
		t.Skip("IT_DID, IT_PRIVY_WALLET_ID and IT_WALLET name a real privy user to read")
	}
	h, _ := liveHarness(t)
	if code, _ := h.as(t, user, "POST", "/v1/me", `{"wallet_address":"`+wallet+`","privy_wallet_id":"`+walletID+`"}`); code != http.StatusOK {
		t.Fatalf("register: %d", code)
	}
	code, acct := h.as(t, user, "GET", "/v1/account", "")
	if code != http.StatusOK {
		t.Fatalf("account: %d %v", code, acct)
	}
	if _, backend := acct["signer"]; !backend && acct["signer_authorized"] != true {
		t.Fatalf("owner-signed account reads as needing a grant: %v", acct)
	}

	// perpl, read straight off the Exchange: minimums come from perpl's own context
	code, p := h.as(t, user, "GET", "/v1/perpl/account", "")
	if code != http.StatusOK {
		t.Fatalf("perpl account: %d %v", code, p)
	}
	if p["min_open"] != "100000000" || p["registered"] == nil {
		t.Fatalf("perpl account = %v", p)
	}
}
