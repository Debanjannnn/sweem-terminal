package planner

import (
	"encoding/json"
	"math/big"

	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// every step of the executor's plan fixture encodes to the executor's `cast calldata` golden, byte for byte.
func TestEncodeMatchesExecutorGolden(t *testing.T) {
	p, _ := planner()
	var steps []Step
	if err := json.Unmarshal(fixture(t, "user_steps.plan.json"), &steps); err != nil {
		t.Fatal(err)
	}
	var golden []string
	for _, l := range strings.Split(string(fixture(t, "user_steps.golden.txt")), "\n") {
		if l != "" {
			golden = append(golden, l)
		}
	}
	if len(steps) != len(golden) {
		t.Fatalf("%d steps vs %d golden lines", len(steps), len(golden))
	}
	w := common.HexToAddress("0x1111111111111111111111111111111111111111")
	for i, s := range steps {
		c, err := p.Encode(w, s)
		if err != nil {
			t.Fatalf("step %d %s: %v", i, s.Action, err)
		}
		if got := hexutil.Encode(c.Data); got != golden[i] {
			t.Errorf("step %d %s:\n got %s\nwant %s", i, s.Action, got, golden[i])
		}
	}
}

func TestEncodeValueSpend(t *testing.T) {
	p, _ := planner()
	w := common.HexToAddress("0x1111111111111111111111111111111111111111")
	usdc := common.HexToAddress(p.Dep.Tokens["USDC"].Address)
	c, _ := p.Encode(w, Step{Action: "kuru.marketBuy", Market: "WETH", Quote: "100000000", MinOut: "0"})
	if c.Value.Sign() != 0 || c.SpendToken != usdc || c.SpendAmount.Int64() != 100_000_000 || !c.Approve {
		t.Errorf("buy %+v", c)
	}
	// MON sell: native value floored to sizePrecision (1e10 → 1e8 wei units), no approve
	c, _ = p.Encode(w, Step{Action: "kuru.marketSell", Market: "MON", Size: "250000000000000000123", MinOut: "1"})
	if c.Value.String() != "250000000000000000000" || c.Approve {
		t.Errorf("mon sell %+v", c)
	}
	for _, bad := range []Step{
		{Action: "kuru.marketBuy", Market: "PEPE", Quote: "1", MinOut: "0"},
		{Action: "kuru.marketBuy", Market: "WETH", Quote: "99", MinOut: "0"}, // < 1 quote tick
		{Action: "vault.deposit", Vault: "0x2222222222222222222222222222222222222222", Amount: "1"},
		{Action: "perpl.order", Perp: "BTC", Kind: "addMargin", Amount: "1", Price: "1"},
		{Action: "erc20.transfer", Amount: "1"},
	} {
		if _, err := p.Encode(w, bad); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func TestRender(t *testing.T) {
	p, _ := planner()
	w := common.HexToAddress("0x1111111111111111111111111111111111111111")
	steps := []Step{
		{Action: "kuru.marketBuy", Market: "WETH", Quote: "100000000", MinOut: "24000000000000000"},
		{Action: "kuru.marketSell", Market: "MON", Size: "250000000000000000000", MinOut: "6000000"},
		{Action: "kuru.marketBuy", Market: "cbBTC", Quote: "5000000", MinOut: "1"},
	}
	// WETH book already approved for enough; cbBTC not (5 USDC × pricePrecision 1e4 = quoteSize 0xc350)
	allow := func(token, spender common.Address) (*big.Int, error) {
		if strings.EqualFold(spender.Hex(), p.Dep.Kuru.Markets["WETH"].Orderbook) {
			return big.NewInt(100_000_000), nil
		}
		return big.NewInt(0), nil
	}
	txs, err := p.Render(w, steps, allow)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "render", txs, `[
		{"to":"0xa6aFD386135B7D41A6C40C525abC4A1019b0D132","data":"0x7c51d6cf00000000000000000000000000000000000000000000000000000000000f4240000000000000000000000000000000000000000000000000005543df729c000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000","value":"0x0","chainId":143,"description":"buy WETH with 100000000 USDC atoms (min 24000000000000000)","step":0},
		{"to":"0x065C9d28E428A0db40191a54d33d5b7c71a9C394","data":"0x532c46db00000000000000000000000000000000000000000000000000000246139ca80000000000000000000000000000000000000000000000000000000000005b8d8000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000","value":"0xd8d726b7177a80000","chainId":143,"description":"sell 250000000000000000000 MON atoms (min 6000000 USDC atoms)","step":1},
		{"to":"0x754704Bc059F8C67012fEd69BC8A327a5aafb603","data":"0x095ea7b300000000000000000000000040c49f171202f91ff5d2fae34c22dd2bfdd22af000000000000000000000000000000000000000000000000000000000004c4b40","value":"0x0","chainId":143,"description":"approve USDC 5000000","step":2},
		{"to":"0x40c49F171202F91ff5d2faE34c22dD2BFdD22aF0","data":"0x7c51d6cf000000000000000000000000000000000000000000000000000000000000c350000000000000000000000000000000000000000000000000000000000000000100000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000","value":"0x0","chainId":143,"description":"buy cbBTC with 5000000 USDC atoms (min 1)","step":2}]`)
}
