//go:build e2e

// Package e2e drives a running `make up` stack on Monad testnet with real txs. Run via `make e2e`.
package e2e

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/redis/go-redis/v9"
	"github.com/snehendu098/sweem-terminal/internal/chain"
	"github.com/snehendu098/sweem-terminal/internal/chain/bindings/basket"
	"github.com/snehendu098/sweem-terminal/internal/config"
	"github.com/snehendu098/sweem-terminal/internal/streams"
)

const (
	api      = "http://localhost:8080"
	md       = "http://localhost:8082"
	seeder   = "http://localhost:8086"
	deployer = "0x6a8846d4c910911181a73cae3910cc981354ef74"
)

var erc20, _ = abi.JSON(strings.NewReader(`[
{"name":"approve","type":"function","inputs":[{"type":"address"},{"type":"uint256"}],"outputs":[{"type":"bool"}]},
{"name":"transfer","type":"function","inputs":[{"type":"address"},{"type":"uint256"}],"outputs":[{"type":"bool"}]},
{"name":"balanceOf","type":"function","stateMutability":"view","inputs":[{"type":"address"}],"outputs":[{"type":"uint256"}]}]`))

func env(t *testing.T, k string) string {
	v := os.Getenv(k)
	if v == "" {
		t.Fatalf("%s not set (make e2e loads it)", k)
	}
	return v
}

func do(t *testing.T, method, url, token string, body, out any) int {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
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
	if out != nil {
		_ = json.NewDecoder(res.Body).Decode(out)
	}
	return res.StatusCode
}

func get(t *testing.T, url string, out any) {
	t.Helper()
	if c := do(t, http.MethodGet, url, "", nil, out); c != http.StatusOK {
		t.Fatalf("GET %s: %d", url, c)
	}
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

type basketRow struct {
	Address    string `json:"address"`
	Symbol     string `json:"symbol"`
	Creator    string `json:"creator"`
	Components []struct {
		Market string `json:"market"`
	} `json:"components"`
}

type quote struct {
	ExpectedOut string `json:"expectedOut"`
	MinOut      string `json:"minOut"`
	Stale       bool   `json:"stale"`
}

type execution struct {
	ScheduleID string `json:"scheduleId"`
	Slot       int64  `json:"slot"`
	Status     string `json:"status"`
	TxHash     string `json:"txHash"`
	Error      string `json:"error"`
}

func big10(s string) *big.Int { v, _ := new(big.Int).SetString(s, 10); return v }

type wallet struct {
	eth *ethclient.Client
	key *ecdsa.PrivateKey
}

func (w wallet) send(t *testing.T, ctx context.Context, f func(*bind.TransactOpts) (*types.Transaction, error)) *types.Receipt {
	t.Helper()
	opts, _ := bind.NewKeyedTransactorWithChainID(w.key, big.NewInt(int64(config.ChainID)))
	opts.Context = ctx
	tx, err := f(opts)
	if err != nil {
		t.Fatal(err)
	}
	r, err := bind.WaitMined(ctx, w.eth, tx)
	if err != nil || r.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("tx %s: %v status %v", tx.Hash(), err, r)
	}
	return r
}

func (w wallet) usdc(t *testing.T, ctx context.Context, token common.Address, method string, args ...any) *types.Receipt {
	c := bind.NewBoundContract(token, erc20, w.eth, w.eth, w.eth)
	return w.send(t, ctx, func(o *bind.TransactOpts) (*types.Transaction, error) { return c.Transact(o, method, args...) })
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
		addr, config.ChainID, init.Nonce, time.Now().UTC().Format("2006-01-02T15:04:05.000Z"))
	sig, err := crypto.Sign(accounts.TextHash([]byte(msg)), key)
	if err != nil {
		t.Fatal(err)
	}
	sig[64] += 27
	var auth struct{ Token string }
	post("authenticate", map[string]any{"message": msg, "signature": "0x" + common.Bytes2Hex(sig), "chainId": fmt.Sprintf("eip155:%d", config.ChainID),
		"walletClientType": "metamask", "connectorType": "injected", "mode": "login-or-sign-up"}, &auth)
	if auth.Token == "" {
		t.Fatal("privy siwe: no token")
	}
	return auth.Token
}

func TestE2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 14*time.Minute)
	defer cancel()
	dep, err := config.LoadDeployments(env(t, "E2E_DEPLOYMENTS_PATH"))
	if err != nil {
		t.Fatal(err)
	}
	core := strings.ToLower(dep.Baskets["CORE"])
	usdc := common.HexToAddress(dep.Kuru.USDC)
	eth, err := chain.Dial(ctx, env(t, "RPC_URL"))
	if err != nil {
		t.Fatal(err)
	}
	dkey, err := crypto.HexToECDSA(strings.TrimPrefix(env(t, "PRIVATE_KEY"), "0x"))
	if err != nil {
		t.Fatal(err)
	}
	dw := wallet{eth, dkey}
	var coreRow basketRow

	t.Run("health", func(t *testing.T) {
		for _, p := range []int{8080, 8081, 8082, 8084, 8085, 8086} {
			for _, h := range []string{"health", "healthz"} {
				get(t, fmt.Sprintf("http://localhost:%d/%s", p, h), nil)
			}
		}
		var st struct{ AccountID string } // executor has no host port: seeder reads bot balances through it
		get(t, seeder+"/api/v1/status", &st)
		if st.AccountID != "108" {
			t.Fatalf("seeder→executor bot account %q", st.AccountID)
		}
	})

	t.Run("basket", func(t *testing.T) {
		var bs []basketRow
		get(t, api+"/api/v1/baskets", &bs)
		i := slices.IndexFunc(bs, func(b basketRow) bool { return strings.EqualFold(b.Address, core) })
		if i < 0 {
			t.Fatalf("CORE %s not in api baskets", core)
		}
		coreRow = bs[i]
		if coreRow.Symbol != "CORE" || !strings.EqualFold(coreRow.Creator, deployer) || len(coreRow.Components) != 3 {
			t.Fatalf("CORE row %+v", coreRow)
		}
	})

	t.Run("liquidity", func(t *testing.T) {
		rdb := redis.NewClient(&redis.Options{Addr: "localhost:" + envOr("REDIS_HOST_PORT", "6380")})
		defer rdb.Close()
		var st struct {
			Markets map[string]struct{ LiveOrders int }
		}
		get(t, seeder+"/api/v1/status", &st)
		for _, c := range coreRow.Components {
			var b struct {
				Bids, Asks []streams.Level
				Stale      bool
			}
			get(t, md+"/api/v1/books/"+c.Market, &b)
			if len(b.Bids) == 0 || len(b.Asks) == 0 || b.Stale {
				t.Fatalf("book %s: %d bids %d asks stale=%v", c.Market, len(b.Bids), len(b.Asks), b.Stale)
			}
			ref, err := streams.Get[streams.RefPrice](ctx, rdb, streams.Ref(c.Market))
			if err != nil || time.Since(time.UnixMilli(ref.Ts)) > time.Minute || !ref.Ref.IsPositive() {
				t.Fatalf("ref %s: %+v %v", c.Market, ref, err)
			}
			if st.Markets[c.Market].LiveOrders == 0 {
				t.Fatalf("seeder has no resting orders on %s", c.Market)
			}
		}
	})

	t.Run("direct mint and redeem", func(t *testing.T) {
		b, _ := basket.NewSweemBasket(common.HexToAddress(core), eth)
		amt := big.NewInt(5_000_000)
		var q quote
		get(t, fmt.Sprintf("%s/api/v1/quote/%s?side=mint&amount=%s&slippageBps=100", md, core, amt), &q)
		if q.Stale || q.MinOut == "" {
			t.Fatalf("mint quote %+v", q)
		}
		dw.usdc(t, ctx, usdc, "approve", common.HexToAddress(core), amt)
		before, _ := b.BalanceOf(&bind.CallOpts{Context: ctx}, common.HexToAddress(deployer))
		deadline := big.NewInt(time.Now().Add(5 * time.Minute).Unix())
		mr := dw.send(t, ctx, func(o *bind.TransactOpts) (*types.Transaction, error) {
			return b.Mint(o, amt, big10(q.MinOut), deadline)
		})
		after, _ := b.BalanceOf(&bind.CallOpts{Context: ctx}, common.HexToAddress(deployer))
		shares := new(big.Int).Sub(after, before)
		t.Logf("mint %s: shares %s quoted %s (%s bps)", mr.TxHash, shares, q.ExpectedOut, diffBps(shares, big10(q.ExpectedOut)))

		get(t, fmt.Sprintf("%s/api/v1/quote/%s?side=redeem&amount=%s&slippageBps=100", md, core, shares), &q)
		u0 := balanceOf(t, ctx, eth, usdc, common.HexToAddress(deployer))
		rr := dw.send(t, ctx, func(o *bind.TransactOpts) (*types.Transaction, error) {
			return b.Redeem(o, shares, big10(q.MinOut), deadline)
		})
		out := new(big.Int).Sub(balanceOf(t, ctx, eth, usdc, common.HexToAddress(deployer)), u0)
		t.Logf("redeem %s: usdc %s quoted %s (%s bps)", rr.TxHash, out, q.ExpectedOut, diffBps(out, big10(q.ExpectedOut)))

		eventually(t, 2*time.Minute, func() string {
			var n struct {
				NavPerShare string
				Stale       bool
			}
			get(t, md+"/api/v1/baskets/"+core+"/nav", &n)
			if n.Stale {
				return "nav stale"
			}
			var pts []json.RawMessage
			from := time.Now().Add(-5 * time.Minute).UTC().Format(time.RFC3339)
			get(t, fmt.Sprintf("%s/api/v1/baskets/%s/nav/history?from=%s", api, core, from), &pts)
			if len(pts) == 0 {
				return "no recent nav_points"
			}
			return ""
		})
	})

	t.Run("privy scheduler", func(t *testing.T) {
		lkey, err := crypto.HexToECDSA(strings.TrimPrefix(env(t, "E2E_LOGIN_PRIVATE_KEY"), "0x"))
		if err != nil {
			t.Fatal(err)
		}
		walletID, waddr := env(t, "E2E_PRIVY_WALLET_ID"), common.HexToAddress(env(t, "E2E_PRIVY_WALLET_ADDRESS"))
		// two 10 USDC buys per run plus gas; the deployer tops the Privy wallet up
		if balanceOf(t, ctx, eth, usdc, waddr).Cmp(big.NewInt(20_000_000)) < 0 {
			dw.usdc(t, ctx, usdc, "transfer", waddr, big.NewInt(30_000_000))
		}
		if mon, _ := eth.BalanceAt(ctx, waddr, nil); mon.Cmp(big10("600000000000000000")) < 0 {
			dw.send(t, ctx, func(o *bind.TransactOpts) (*types.Transaction, error) {
				o.Value = big10("1000000000000000000")
				return bind.NewBoundContract(waddr, abi.ABI{}, eth, eth, eth).Transfer(o)
			})
		}

		token := siweLogin(t, env(t, "PRIVY_APP_ID"), lkey)
		var me struct{ ID string }
		if do(t, http.MethodGet, api+"/api/v1/me", token, nil, &me); me.ID != env(t, "E2E_PRIVY_USER_ID") {
			t.Fatalf("me %q", me.ID)
		}
		if c := do(t, http.MethodPost, api+"/api/v1/me/delegation", token, map[string]string{"walletId": walletID, "walletAddress": waddr.Hex()}, nil); c != http.StatusOK {
			t.Fatalf("delegation: %d", c)
		}
		create := func(body map[string]any) string {
			body["basket"], body["maxSlippageBps"], body["maxPerRunUsdc"] = core, 100, "10"
			var s struct{ ID string }
			if c := do(t, http.MethodPost, api+"/api/v1/me/schedules", token, body, &s); c != http.StatusCreated {
				t.Fatalf("schedule %v: %d", body, c)
			}
			return s.ID
		}
		execs := func(id string) (out []execution) {
			var all []execution
			do(t, http.MethodGet, api+"/api/v1/me/executions", token, nil, &all)
			for _, e := range all {
				if e.ScheduleID == id {
					if e.Status == "failed" || e.Status == "rejected" {
						t.Fatalf("execution %+v", e)
					}
					out = append(out, e)
				}
			}
			return out
		}
		confirmed := func(es []execution) (txs []string) {
			for _, e := range es {
				if e.Status == "confirmed" {
					txs = append(txs, e.TxHash)
				}
			}
			return txs
		}

		rec := create(map[string]any{"kind": "recurring", "amountUsdc": "10", "intervalSeconds": 60})
		var txs []string
		eventually(t, 4*time.Minute, func() string {
			if txs = confirmed(execs(rec)); len(txs) < 2 {
				return fmt.Sprintf("%d/2 recurring buys confirmed", len(txs))
			}
			return ""
		})
		do(t, http.MethodDelete, api+"/api/v1/me/schedules/"+rec, token, nil, nil)
		slots := map[int64]bool{}
		for _, e := range execs(rec) {
			if slots[e.Slot] {
				t.Fatalf("slot %d executed twice", e.Slot)
			}
			slots[e.Slot] = true
		}
		t.Logf("recurring buys %v", txs)

		sl := create(map[string]any{"kind": "stop_loss", "stopNavPerShare": "1000", "redeemBps": 5000})
		eventually(t, 2*time.Minute, func() string {
			if c := confirmed(execs(sl)); len(c) != 1 {
				return "stop-loss redeem not confirmed"
			} else {
				txs = append(txs, c[0])
			}
			return ""
		})
		var ss []struct {
			ID     string
			Active bool
		}
		do(t, http.MethodGet, api+"/api/v1/me/schedules", token, nil, &ss)
		if i := slices.IndexFunc(ss, func(s struct {
			ID     string
			Active bool
		}) bool {
			return s.ID == sl
		}); i < 0 || ss[i].Active {
			t.Fatal("stop-loss should be deactivated after firing")
		}
		t.Logf("stop-loss redeem %s", txs[len(txs)-1])

		eventually(t, 2*time.Minute, func() string {
			var h []struct{ TxHash string }
			do(t, http.MethodGet, api+"/api/v1/me/history", token, nil, &h)
			for _, tx := range txs {
				if !slices.ContainsFunc(h, func(r struct{ TxHash string }) bool { return strings.EqualFold(r.TxHash, tx) }) {
					return "indexer has not recorded " + tx
				}
			}
			var p []struct{ Basket string }
			do(t, http.MethodGet, api+"/api/v1/me/positions", token, nil, &p)
			if !slices.ContainsFunc(p, func(r struct{ Basket string }) bool { return r.Basket == core }) {
				return "no CORE position"
			}
			return ""
		})
	})
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func diffBps(got, want *big.Int) string {
	if want.Sign() == 0 {
		return "n/a"
	}
	d := new(big.Int).Mul(new(big.Int).Sub(got, want), big.NewInt(10_000))
	return new(big.Int).Quo(d, want).String()
}
