//go:build e2e

// Package e2e drives a running `make up` stack on Monad mainnet (143) with REAL money through the v2 api.
// Run via `make e2e` (E2E_REAL_MONEY=1). See tasks/real-money-test.md.
package e2e

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/chain"
	"github.com/snehendu098/sweem-terminal/internal/config"
)

const (
	api = "http://localhost:8080/api/v1"
	md  = "http://localhost:8082/api/v1"
	mkt = "WETH" // ERC20 base (clean balance deltas); Kuru min 0.002 WETH ≈ $5
)

var erc20, _ = abi.JSON(strings.NewReader(`[
{"name":"balanceOf","type":"function","stateMutability":"view","inputs":[{"type":"address"}],"outputs":[{"type":"uint256"}]}]`))

func env(t *testing.T, k string) string {
	v := os.Getenv(k)
	if v == "" {
		t.Fatalf("%s not set (make e2e loads it)", k)
	}
	return v
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// do sends a JSON request, decodes a 2xx body into out, and returns the status and raw body.
func do(t *testing.T, method, url, token string, body, out any) (int, string) {
	t.Helper()
	var rd io.Reader = http.NoBody
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, url, rd)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if out != nil && res.StatusCode < 300 {
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("%s %s: decode %v: %s", method, url, err, raw)
		}
	}
	return res.StatusCode, string(raw)
}

// must is do + status check.
func must(t *testing.T, method, url, token string, body any, want int, out any) {
	t.Helper()
	if c, raw := do(t, method, url, token, body, out); c != want {
		t.Fatalf("%s %s: %d (want %d): %s", method, url, c, want, raw)
	}
}

func get(t *testing.T, url string, out any) {
	t.Helper()
	must(t, http.MethodGet, url, "", nil, http.StatusOK, out)
}

// eventually polls f every 3s until it returns "" or timeout, failing with the last reason.
func eventually(t *testing.T, timeout time.Duration, f func() string) {
	t.Helper()
	var why string
	for end := time.Now().Add(timeout); time.Now().Before(end); time.Sleep(3 * time.Second) {
		if why = f(); why == "" {
			return
		}
	}
	t.Fatalf("timed out after %s: %s", timeout, why)
}

type execution struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	Status     string    `json:"status"`
	TxHashes   []string  `json:"txHashes"`
	Error      *string   `json:"error"`
	ScheduleID *string   `json:"scheduleId"`
	CreatedAt  time.Time `json:"createdAt"`
}

// accepted is the 202 body of every execution-creating route.
type accepted struct {
	ExecutionID string `json:"executionId"`
	Kind        string `json:"kind"`
	OrderID     string `json:"orderId"`
	PositionID  string `json:"positionId"`
	Status      string `json:"status"` // withdraw: "closed" on 200 (nothing on chain)
}

func balanceOf(t *testing.T, ctx context.Context, eth *ethclient.Client, token, owner common.Address) *big.Int {
	var out []any
	if err := bind.NewBoundContract(token, erc20, eth, eth, eth).Call(&bind.CallOpts{Context: ctx}, &out, "balanceOf", owner); err != nil {
		t.Fatal(err)
	}
	return out[0].(*big.Int)
}

// siweLogin gets a real Privy access token for the e2e user (wallet login; no test-account toggle needed).
func siweLogin(t *testing.T, appID string, key *ecdsa.PrivateKey) string {
	addr := crypto.PubkeyToAddress(key.PublicKey).Hex()
	post := func(path string, body, out any) {
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, "https://auth.privy.io/api/v1/siwe/"+path, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("privy-app-id", appID)
		req.Header.Set("Origin", "http://localhost:3000")
		res, err := http.DefaultClient.Do(req)
		if err != nil || res.StatusCode != http.StatusOK {
			t.Fatalf("privy siwe/%s: %v %v", path, err, res)
		}
		defer res.Body.Close()
		_ = json.NewDecoder(res.Body).Decode(out)
	}
	var init struct{ Nonce string }
	post("init", map[string]string{"address": addr}, &init)
	msg := fmt.Sprintf("localhost:3000 wants you to sign in with your Ethereum account:\n%s\n\nsweem e2e login\n\nURI: http://localhost:3000\nVersion: 1\nChain ID: %d\nNonce: %s\nIssued At: %s",
		addr, config.MainnetChainID, init.Nonce, time.Now().UTC().Format("2006-01-02T15:04:05.000Z"))
	sig, err := crypto.Sign(accounts.TextHash([]byte(msg)), key)
	if err != nil {
		t.Fatal(err)
	}
	sig[64] += 27
	var auth struct{ Token string }
	post("authenticate", map[string]any{"message": msg, "signature": "0x" + common.Bytes2Hex(sig), "chainId": fmt.Sprintf("eip155:%d", config.MainnetChainID),
		"walletClientType": "metamask", "connectorType": "injected", "mode": "login-or-sign-up"}, &auth)
	if auth.Token == "" {
		t.Fatal("privy siwe: no token")
	}
	return auth.Token
}

func TestE2E(t *testing.T) {
	// E2E_DRY=1: config/auth/read-only checks only, stops before the first tx (no funds needed)
	dry := os.Getenv("E2E_DRY") == "1"
	if os.Getenv("E2E_REAL_MONEY") != "1" && !dry {
		t.Skip("spends real mainnet funds: set E2E_REAL_MONEY=1 (make e2e), or E2E_DRY=1 for the no-spend checks")
	}
	start := time.Now().Add(-time.Minute) // clock skew slack for the executions check
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
	defer cancel()
	dep, err := config.LoadDeployments143(envOr("E2E_DEPLOYMENTS_PATH", "../contracts/deployments/143.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CHAIN_ID", fmt.Sprint(config.MainnetChainID)) // chain.Dial refuses any other chain
	eth, err := chain.Dial(ctx, envOr("MAINNET_RPC_URL", "https://rpc.monad.xyz"))
	if err != nil {
		t.Fatal(err)
	}
	usdc := common.HexToAddress(dep.Tokens["USDC"].Address)
	weth := common.HexToAddress(dep.Tokens[mkt].Address)
	m := dep.Kuru.Markets[mkt]
	minSize := decimal.NewFromInt(int64(m.MinSize)).Div(decimal.NewFromInt(int64(m.SizePrecision)))
	walletID, waddr := env(t, "E2E_PRIVY_WALLET_ID"), common.HexToAddress(env(t, "E2E_PRIVY_WALLET_ADDRESS"))

	// stop at the first failing step: later steps spend money on top of a broken state
	step := func(name string, f func(t *testing.T)) {
		if !t.Run(name, f) {
			t.FailNow()
		}
	}

	step("precheck funds", func(t *testing.T) {
		mon, err := eth.BalanceAt(ctx, waddr, nil)
		if err != nil {
			t.Fatal(err)
		}
		u := balanceOf(t, ctx, eth, usdc, waddr)
		if mon.Cmp(big.NewInt(1e18)) < 0 || u.Cmp(big.NewInt(8_000_000)) < 0 {
			if dry {
				t.Logf("dry: wallet %s has %s MON, %s USDC (real run needs >= 1 MON, 8 USDC)", waddr.Hex(), decimal.NewFromBigInt(mon, -18), decimal.NewFromBigInt(u, -6))
				return
			}
			t.Fatalf("fund wallet %s: has %s MON, %s USDC; need >= 1 MON and >= 8 USDC",
				waddr.Hex(), decimal.NewFromBigInt(mon, -18), decimal.NewFromBigInt(u, -6))
		}
	})

	step("health", func(t *testing.T) {
		// api, publisher, market-data, scheduler, indexer, history
		for _, p := range []int{8080, 8081, 8082, 8084, 8085, 8087} {
			get(t, fmt.Sprintf("http://localhost:%d/healthz", p), nil)
		}
	})

	var token string
	step("siwe login", func(t *testing.T) {
		lkey, err := crypto.HexToECDSA(strings.TrimPrefix(env(t, "E2E_LOGIN_PRIVATE_KEY"), "0x"))
		if err != nil {
			t.Fatal(err)
		}
		token = siweLogin(t, env(t, "PRIVY_APP_ID"), lkey)
		var me struct{ ID string }
		must(t, http.MethodGet, api+"/me", token, nil, http.StatusOK, &me)
		if want := os.Getenv("E2E_PRIVY_USER_ID"); want != "" && me.ID != want {
			t.Fatalf("me %q, want %q", me.ID, want)
		}
	})

	var schedules []string
	t.Cleanup(func() {
		for _, id := range schedules {
			do(t, http.MethodDelete, api+"/me/schedules/"+id, token, nil, nil)
		}
		if c, raw := do(t, http.MethodDelete, api+"/me/delegation", token, nil, nil); c != http.StatusOK {
			t.Errorf("revoke delegation: %d %s", c, raw)
		}
	})

	// confirm polls an execution to confirmed, failing fast on failed/rejected.
	confirm := func(t *testing.T, id string) execution {
		t.Helper()
		var e execution
		eventually(t, 5*time.Minute, func() string {
			must(t, http.MethodGet, api+"/me/executions/"+id, token, nil, http.StatusOK, &e)
			switch e.Status {
			case "confirmed":
				return ""
			case "failed", "rejected":
				t.Fatalf("execution %s (%s) %s: %v txs %v", id, e.Kind, e.Status, deref(e.Error), e.TxHashes)
			}
			return fmt.Sprintf("execution %s (%s) %s", id, e.Kind, e.Status)
		})
		t.Logf("%s %s confirmed: %v", e.Kind, id, e.TxHashes)
		return e
	}
	run := func(t *testing.T, method, path string, body any) accepted {
		t.Helper()
		var a accepted
		must(t, method, api+path, token, body, http.StatusAccepted, &a)
		confirm(t, a.ExecutionID)
		return a
	}
	// sellAll market-sells the wallet's whole WETH balance (skips dust below Kuru min size).
	sellAll := func(t *testing.T) {
		bal := decimal.NewFromBigInt(balanceOf(t, ctx, eth, weth, waddr), -int32(dep.Tokens[mkt].Decimals))
		if bal.LessThan(minSize) {
			t.Logf("WETH %s below min %s, left in wallet", bal, minSize)
			return
		}
		run(t, http.MethodPost, "/me/trade", map[string]any{"market": mkt, "side": "sell", "amount": bal.String(), "slippageBps": 100})
	}

	// smallest buy that clears Kuru min size: min × best ask + 15%, rounded up to the cent. Must be ≤ MAX_STEP_USD.
	var buyUSDC string
	step("delegation", func(t *testing.T) {
		must(t, http.MethodPost, api+"/me/delegation", token, map[string]string{"walletId": walletID, "walletAddress": waddr.Hex()}, http.StatusOK, nil)
		var b struct{ Asks [][2]string }
		get(t, md+"/books/"+strings.ToLower(m.Orderbook), &b)
		if len(b.Asks) == 0 {
			t.Fatalf("%s book has no asks", mkt)
		}
		ask := decimal.RequireFromString(b.Asks[0][0])
		buyUSDC = minSize.Mul(ask).Mul(decimal.NewFromFloat(1.15)).RoundUp(2).String()
		t.Logf("%s ask %s, buy size %s USDC", mkt, ask, buyUSDC)
	})

	// orders left open by an earlier failed run (their funding stays in margin; the margin step withdraws it)
	step("cancel stale orders", func(t *testing.T) {
		var list []struct {
			ID, Status  string
			KuruOrderID *string
		}
		must(t, http.MethodGet, api+"/me/orders", token, nil, http.StatusOK, &list)
		for _, o := range list {
			if o.Status == "open" && o.KuruOrderID != nil && !dry {
				t.Logf("cancelling stale order %s (kuru %s)", o.ID, *o.KuruOrderID)
				run(t, http.MethodDelete, "/me/orders/"+o.ID, nil)
			}
		}
	})

	if dry {
		step("dry read-only", func(t *testing.T) {
			for _, p := range []string{"/me/holdings", "/me/executions", "/me/earnings", "/me/positions", "/me/orders", "/me/schedules", "/me/strategies"} {
				must(t, http.MethodGet, api+p, token, nil, http.StatusOK, nil)
			}
			get(t, api+"/markets", nil)
			get(t, api+"/strategies", nil)
			get(t, md+"/rates", nil)
			var q map[string]any
			must(t, http.MethodGet, md+"/quote/"+strings.ToLower(m.Orderbook)+"?side=buy&amount=5000000", "", nil, http.StatusOK, &q)
			t.Logf("quote 5 USDC -> %v", q)

			// multi-token strategy: create, read back, and a below-min deploy is refused at planning (no tx)
			var st struct {
				ID      string
				Asset   string
				Sleeves []map[string]any
				APY     map[string]any
			}
			must(t, http.MethodPost, api+"/strategies", token, map[string]any{"name": "e2e multi", "public": false, "rules": map[string]any{},
				"sleeves": []map[string]any{
					{"asset": "USDC", "weightBps": 4000, "sources": []map[string]any{{"kind": "lending", "venue": "aave", "weightBps": 10000}}},
					{"asset": "WETH", "weightBps": 6000, "sources": []map[string]any{{"kind": "lending", "venue": "aave", "weightBps": 10000}}},
				}}, http.StatusCreated, &st)
			if st.Asset != "MULTI" || len(st.Sleeves) != 2 {
				t.Fatalf("multi strategy: %+v", st)
			}
			must(t, http.MethodGet, api+"/strategies/"+st.ID, token, nil, http.StatusOK, &st)
			c, raw := do(t, http.MethodPost, api+"/me/strategies/"+st.ID+"/deploy", token, map[string]any{"amountUsdc": "0.5"}, nil)
			if c != http.StatusUnprocessableEntity || !strings.Contains(raw, "below_min_size") {
				t.Fatalf("below-min deploy: %d %s", c, raw)
			}
			t.Logf("multi %s apy %v; below-min deploy -> %s", st.ID, st.APY, raw)
		})
		return // cleanup revokes delegation
	}

	step("market buy and sell", func(t *testing.T) {
		run(t, http.MethodPost, "/me/trade", map[string]any{"market": mkt, "side": "buy", "amount": buyUSDC, "slippageBps": 100})
		sellAll(t)
	})

	// 0.2 USDC resting buy far below mid; the funded USDC stays in the margin account (withdrawn by the margin step)
	const limitPx, limitFund = "100", "0.2"
	step("limit order and cancel", func(t *testing.T) {
		a := run(t, http.MethodPost, "/me/orders", map[string]any{"market": mkt, "side": "buy", "price": limitPx, "size": minSize.String(), "postOnly": true, "fund": true})
		type order struct {
			ID, Status  string
			KuruOrderID *string
		}
		find := func() (o order) {
			var list []order
			must(t, http.MethodGet, api+"/me/orders", token, nil, http.StatusOK, &list)
			for _, x := range list {
				if x.ID == a.OrderID {
					return x
				}
			}
			return
		}
		eventually(t, 3*time.Minute, func() string {
			if o := find(); o.Status != "open" || o.KuruOrderID == nil {
				return fmt.Sprintf("order %s not live yet: %+v", a.OrderID, o)
			}
			return ""
		})
		run(t, http.MethodDelete, "/me/orders/"+a.OrderID, nil)
		eventually(t, 3*time.Minute, func() string {
			if o := find(); o.Status != "cancelled" {
				return "order status " + o.Status
			}
			return ""
		})
	})

	step("margin deposit and withdraw", func(t *testing.T) {
		run(t, http.MethodPost, "/me/margin/deposit", map[string]any{"token": "USDC", "amount": "1"})
		out := decimal.RequireFromString("1").Add(decimal.RequireFromString(limitFund)).String()
		run(t, http.MethodPost, "/me/margin/withdraw", map[string]any{"token": "USDC", "amount": out})
	})

	step("strategy deploy and withdraw", func(t *testing.T) {
		var s struct{ ID string }
		must(t, http.MethodPost, api+"/strategies", token, map[string]any{"name": "e2e aave usdc", "asset": "USDC",
			"sources": []map[string]any{{"kind": "lending", "venue": "aave", "weightBps": 10_000}}, "public": false}, http.StatusCreated, &s)
		d := run(t, http.MethodPost, "/me/strategies/"+s.ID+"/deploy", map[string]any{"amountUsdc": "2"})
		position := func() (status string) {
			var ps []struct{ ID, Status string }
			must(t, http.MethodGet, api+"/me/positions", token, nil, http.StatusOK, &ps)
			for _, p := range ps {
				if p.ID == d.PositionID {
					return p.Status
				}
			}
			return "missing"
		}
		eventually(t, time.Minute, func() string {
			if st := position(); st != "open" {
				return "position " + st
			}
			return ""
		})
		var w accepted
		switch c, raw := do(t, http.MethodPost, api+"/me/positions/"+d.PositionID+"/withdraw", token, map[string]any{}, &w); c {
		case http.StatusAccepted:
			confirm(t, w.ExecutionID)
		case http.StatusOK: // nothing on chain to unwind
		default:
			t.Fatalf("withdraw: %d %s", c, raw)
		}
		eventually(t, time.Minute, func() string {
			if st := position(); st != "closed" {
				return "position " + st
			}
			return ""
		})
	})

	step("dca fires once", func(t *testing.T) {
		var s struct{ ID string }
		// interval 1 day: next_run_at = now, so exactly one run inside the test window
		must(t, http.MethodPost, api+"/me/schedules", token, map[string]any{"kind": "dca", "target": map[string]string{"type": "market", "id": mkt},
			"amountUsdc": buyUSDC, "intervalSeconds": 86_400, "maxSlippageBps": 100, "maxPerRunUsdc": buyUSDC}, http.StatusCreated, &s)
		schedules = append(schedules, s.ID)
		var id string
		eventually(t, 3*time.Minute, func() string {
			var es []execution
			must(t, http.MethodGet, api+"/me/executions", token, nil, http.StatusOK, &es)
			for _, e := range es {
				if e.ScheduleID != nil && *e.ScheduleID == s.ID {
					id = e.ID
					return ""
				}
			}
			return "scheduler has not run dca " + s.ID
		})
		confirm(t, id)
		must(t, http.MethodDelete, api+"/me/schedules/"+s.ID, token, nil, http.StatusOK, nil)
		sellAll(t)
	})

	step("executions terminal", func(t *testing.T) {
		var es []execution
		must(t, http.MethodGet, api+"/me/executions", token, nil, http.StatusOK, &es)
		n := 0
		for _, e := range es {
			if e.CreatedAt.Before(start) {
				continue
			}
			n++
			if e.Status != "confirmed" {
				t.Errorf("execution %s (%s) %s: %v", e.ID, e.Kind, e.Status, deref(e.Error))
			}
		}
		t.Logf("%d executions this run", n)
	})

	step("earnings", func(t *testing.T) {
		switch c, raw := do(t, http.MethodGet, api+"/me/earnings", token, nil, nil); c {
		case http.StatusOK:
		case http.StatusNotFound:
			t.Logf("GET /me/earnings 404 (route not shipped yet): %s", raw)
		default:
			t.Fatalf("earnings: %d %s", c, raw)
		}
	})
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
