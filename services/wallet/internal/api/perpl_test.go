package api

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/executor"
	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/marketdata"
	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/policy"
)

// verbatim GET /v1/markets/32 from the running market-data: ETH perp, price 2dp,
// size 3dp, collateral 6dp, mark 2689.5 in WAD
const liveEthPerp = `{"data":{"id":"32","venue":"perpl","kind":"perp","symbol":"ETH","base":"ETH","quote":"AUSD","baseDecimals":3,"quoteDecimals":6,"priceDecimals":2,"sizeDecimals":3,"tickSize":"1","minSize":"0","minIsQuoteNotional":true,"takerFeeMicros":345,"makerFeeMicros":45,"status":"active","address":"","mark":"2689500000000000000000","last":"2690340000000000000000"}}`

// a funded perpl account, in the executor's /perpl/account shape
func fundedPerpl() executor.PerplAccount {
	mark, liq := "268950", "215000"
	return executor.PerplAccount{
		ChainID: 10143, Registered: true, AccountID: "720", Balance: "500000000", Locked: "0",
		Collateral: executor.PerplCollateral{Symbol: "AUSD", Decimals: 6},
		Positions: []executor.PerplPosition{{
			MarketID: "32", Symbol: "ETH", Side: "long", Lot: "1000", EntryPrice: "260000",
			Deposit: "52000000", PnL: "-1500000", MarkPrice: &mark, LiquidationEst: &liq,
			PriceDecimals: 2, SizeDecimals: 3,
		}},
	}
}

func perplHarness(t *testing.T) *harness {
	t.Helper()
	md := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/markets/32":
			w.Write([]byte(liveEthPerp))
		case "/v1/markets":
			w.Write([]byte(`{"data":[{"id":"32","venue":"perpl","symbol":"ETH"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(md.Close)
	h := newHarness(t, func(s *Server) { s.Market = marketdata.New(md.URL) })
	h.registered(t)
	h.exec.perplAccount = fundedPerpl()
	if _, err := h.store.PutDelegation(context.Background(), policy.Delegation{
		UserID: did, SignerID: "signer-1", Venues: []string{"perpl"}, Markets: []string{"32"},
		MaxNotionalPerSlice: "1000000000", MaxNotionalTotal: "10000000000", ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	return h
}

// The browser names a perp by perpl's numeric id and sizes it in lots. It
// reaches the executor as a perpl order, and the cap is charged its margin.
func TestPerpMarketOrderReachesPerplWithItsMargin(t *testing.T) {
	h := perplHarness(t)
	h.exec.status = "filled"
	rec := h.do(t, http.MethodPost, "/v1/orders",
		`{"market_id":"32","venue":"perpl","side":"buy","order_type":"market","quantity":"1000","leverage":500,"idempotency_key":"p1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	h.exec.sawOnly(t, "/perpl/order")
	sent := h.exec.bodies[0]
	for k, want := range map[string]any{"market_id": "32", "kind": "market", "side": "buy", "lot": "1000", "leverage": float64(500)} {
		if sent[k] != want {
			t.Fatalf("/perpl/order %s = %v, want %v (body %v)", k, sent[k], want, sent)
		}
	}
	// 1 ETH at 2689.5 is 2,689,500,000 AUSD atoms of notional; at 5x the margin is a fifth
	for _, in := range h.store.intents {
		if in.Size != "537900000" {
			t.Fatalf("cap charged %s, want the 537900000 margin", in.Size)
		}
	}
}

func TestPerpOrderWithoutAnAccountNamesTheDeposit(t *testing.T) {
	h := perplHarness(t)
	h.exec.perplAccount = executor.PerplAccount{Registered: false}
	rec := h.do(t, http.MethodPost, "/v1/orders",
		`{"market_id":"32","venue":"perpl","side":"sell","order_type":"market","quantity":"10","leverage":200,"idempotency_key":"p2"}`)
	if rec.Code != http.StatusPreconditionFailed {
		t.Fatalf("status = %d, want 412; body = %s", rec.Code, rec.Body)
	}
	var env struct {
		Error struct{ Code string } `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if env.Error.Code != "perpl_account_unfunded" {
		t.Fatalf("code = %q, want perpl_account_unfunded", env.Error.Code)
	}
	if h.exec.calls != 0 {
		t.Fatal("an order was sent for an account that does not exist")
	}
}

// A close reduces risk and spends nothing: no lot means the whole position,
// and the delegation budget is untouched.
func TestPerpCloseSpendsNothing(t *testing.T) {
	h := perplHarness(t)
	h.exec.status = "filled"
	rec := h.do(t, http.MethodPost, "/v1/orders", `{"market_id":"32","venue":"perpl","order_type":"close","idempotency_key":"p3"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if got := h.exec.bodies[0]["kind"]; got != "close" {
		t.Fatalf("kind = %v", got)
	}
	if spent, _ := h.store.SpentTotal(t.Context(), did); spent != "0" {
		t.Fatalf("a close consumed %s of the budget", spent)
	}
}

// A resting limit order keeps perpl's id so it can be cancelled by it.
func TestRestingPerpOrderCanBeCancelledByItsId(t *testing.T) {
	h := perplHarness(t)
	h.exec.status = "resting"
	h.exec.info = json.RawMessage(`{"perp_id":32,"order_id":"127","rested_lot":"1000"}`)
	rec := h.do(t, http.MethodPost, "/v1/orders",
		`{"market_id":"32","venue":"perpl","side":"buy","order_type":"limit","price":"250000","quantity":"1000","leverage":500,"post_only":true,"idempotency_key":"p4"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if sent := h.exec.bodies[0]; sent["price"] != "250000" || sent["post_only"] != true {
		t.Fatalf("/perpl/order carried %v", sent)
	}
	var orders struct {
		Data struct {
			Orders []map[string]any `json:"orders"`
		} `json:"data"`
	}
	_ = json.Unmarshal(h.do(t, http.MethodGet, "/v1/orders", "").Body.Bytes(), &orders)
	if len(orders.Data.Orders) != 1 || orders.Data.Orders[0]["order_id"] != "127" || orders.Data.Orders[0]["state"] != "resting" {
		t.Fatalf("orders = %v, want the resting order with id 127", orders.Data.Orders)
	}

	h.exec.status, h.exec.info = "canceled", nil
	if rec := h.do(t, http.MethodDelete, "/v1/perpl/orders/32/127", ""); rec.Code != http.StatusOK {
		t.Fatalf("cancel: %d %s", rec.Code, rec.Body)
	}
	last := h.exec.bodies[len(h.exec.bodies)-1]
	if h.exec.paths[len(h.exec.paths)-1] != "/perpl/cancel" || last["order_id"] != "127" || last["market_id"] != "32" {
		t.Fatalf("cancel sent %s %v", h.exec.paths[len(h.exec.paths)-1], last)
	}
}

func TestPerplDepositAndWithdraw(t *testing.T) {
	h := perplHarness(t)
	h.exec.status = "confirmed"
	if rec := h.do(t, http.MethodPost, "/v1/perpl/deposit", `{"amount":"100000000","idempotency_key":"d1"}`); rec.Code != http.StatusOK {
		t.Fatalf("deposit: %d %s", rec.Code, rec.Body)
	}
	if rec := h.do(t, http.MethodPost, "/v1/perpl/withdraw", `{"amount":"5000000"}`); rec.Code != http.StatusOK {
		t.Fatalf("withdraw: %d %s", rec.Code, rec.Body)
	}
	if h.exec.paths[0] != "/perpl/deposit" || h.exec.bodies[0]["amount"] != "100000000" || h.exec.paths[1] != "/perpl/withdraw" {
		t.Fatalf("executor saw %v %v", h.exec.paths, h.exec.bodies)
	}
	if rec := h.do(t, http.MethodPost, "/v1/perpl/deposit", `{"amount":"1.5"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("a display amount was accepted: %d", rec.Code)
	}
	if spent, _ := h.store.SpentTotal(t.Context(), did); spent != "0" {
		t.Fatalf("funding moved the delegation budget by %s", spent)
	}
}

// The portfolio serves every price and size in WAD, perps included.
func TestPortfolioIncludesPerplPositionsInWad(t *testing.T) {
	h := perplHarness(t)
	var p struct {
		Data portfolioResponse `json:"data"`
	}
	_ = json.Unmarshal(h.do(t, http.MethodGet, "/v1/portfolio", "").Body.Bytes(), &p)
	var got *portfolioPosition
	for i := range p.Data.Positions {
		if p.Data.Positions[i].Source == "perpl" {
			got = &p.Data.Positions[i]
		}
	}
	if got == nil {
		t.Fatalf("no perpl position in %+v", p.Data)
	}
	want := portfolioPosition{MarketID: "32", Side: "long", Size: "1000000000000000000",
		EntryPrice: "2600000000000000000000", Collateral: "52000000000000000000", Mark: "2689.5",
		PnL: "-1500000000000000000", LiquidationEst: "2150000000000000000000", Source: "perpl", Reconciled: true}
	if *got != want {
		t.Fatalf("position\n got %+v\nwant %+v", *got, want)
	}
}

func TestMarginRoundsUpAndDefaultsToOneX(t *testing.T) {
	m := marketdata.Market{SizeDecimals: 3, QuoteDecimals: 6}
	lot := big.NewInt(1) // 0.001 ETH
	price, _ := new(big.Int).SetString("2689500000000000000000", 10)
	// 0.001 * 2689.5 = 2.6895 AUSD = 2,689,500 atoms; / 3 = 896,500
	if got := margin(lot, price, m, 300).String(); got != "896500" {
		t.Fatalf("margin = %s", got)
	}
	if got := margin(lot, price, m, 700).String(); got != "384215" { // 384214.28 rounds up
		t.Fatalf("margin = %s", got)
	}
	if got := margin(lot, price, m, 0).String(); got != "2689500" {
		t.Fatalf("unset leverage must count as 1x, got %s", got)
	}
}
