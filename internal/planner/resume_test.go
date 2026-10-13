package planner

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/shopspring/decimal"
)

func TestResumeWithdraw(t *testing.T) {
	p, _ := planner()
	ctx := context.Background()
	plan := []Step{
		{Action: "lend.withdraw", Venue: "neverland", Asset: "WMON", Amount: "13300000000000000000000"},
		{Action: "wmon.unwrap", Amount: "13300000000000000000000"},
		{Action: "shmonad.redeem", Shares: "6000000000000000000000"},
		{Action: "activeVault.withdraw", Shares: "777"},
		{Action: "perpl.order", Perp: "MON", Kind: "closeShort", Price: "30300", Lot: "4980", LeverageHdths: 100},
		{Action: "perpl.withdraw", Amount: "48976557"},
		{Action: "stable.swap", From: "AUSD", To: "USDC", Amount: "48976557", MinOut: "48952068"},
		{Action: "kuru.marketSell", Market: "MON", Size: "28183500000000000000000", MinOut: "845505000"},
	}
	zero := []Balance{{Value: "0"}, {Value: "0", Shares: "0"}, {Value: "0", Shares: "0"}, {Value: "0"}}
	mon := func(s string) map[string]decimal.Decimal {
		return map[string]decimal.Decimal{"MON": d(s), "WMON": decimal.Zero, "AUSD": decimal.Zero, "USDC": decimal.Zero}
	}

	// step 4 (closeShort) was in flight at a crash and landed: it and everything before are skipped; Perpl freed only 40 AUSD (loss) → capped, minOut scaled
	got, err := p.ResumeWithdraw(ctx, monStrat, plan, 4, Live{Bals: zero, Wallet: mon("28183500000000000000000"), PerpFree: d("40000000")}, 0)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "after close landed", got, `[
		{"action":"perpl.withdraw","amount":"40000000"},
		{"action":"stable.swap","from":"AUSD","to":"USDC","amount":"40000000","minOut":"39979999"},
		{"action":"kuru.marketSell","market":"MON","size":"28183500000000000000000","minOut":"845505000"}]`)

	// failed at step 0 but the withdraw landed (interrupted): unwrap still runs off the WMON it produced; the short is
	// still open (lot 4980) so it is closed once and its planned collateral withdrawn
	live := Live{Bals: []Balance{{Value: "0"}, {Value: "6650000000000000000000", Shares: "6000000000000000000000"}, {Value: "3320000000000000000000", Shares: "777"}, {Value: "0"}},
		Wallet:  map[string]decimal.Decimal{"MON": d("4980000000000000000000"), "WMON": d("13300000000000000000000"), "AUSD": decimal.Zero, "USDC": decimal.Zero},
		PerpLot: map[string]decimal.Decimal{"MON": d("4980")}, PerpFree: d("40000000")}
	got, err = p.ResumeWithdraw(ctx, monStrat, plan, 0, live, 0)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "after withdraw landed", got, `[
		{"action":"wmon.unwrap","amount":"13300000000000000000000"},
		{"action":"shmonad.redeem","shares":"6000000000000000000000"},
		{"action":"activeVault.withdraw","shares":"777"},
		{"action":"perpl.order","perp":"MON","kind":"closeShort","price":"30300","lot":"4980","leverageHdths":100},
		{"action":"perpl.withdraw","amount":"48976557"},
		{"action":"stable.swap","from":"AUSD","to":"USDC","amount":"48976557","minOut":"48952068"},
		{"action":"kuru.marketSell","market":"MON","size":"28183500000000000000000","minOut":"845505000"}]`)

	// everything already done: nothing to resume
	got, err = p.ResumeWithdraw(ctx, monStrat, plan, 7, Live{Bals: zero, Wallet: mon("0")}, 0)
	if err != nil || got != nil {
		t.Errorf("done: %v %v", got, err)
	}
	if _, err := p.ResumeWithdraw(ctx, monStrat, plan, 9, Live{Bals: zero}, 0); code(err) != "bad_request" {
		t.Errorf("bad done: %v", err)
	}
}

// fake Perpl: account 42 with 7 AUSD free; position lot 130 on perp 20 only.
type perplChain struct{ noAccount bool }

func (f perplChain) CallContract(_ context.Context, m ethereum.CallMsg, _ *big.Int) ([]byte, error) {
	w := func(xs ...int64) []byte {
		var out []byte
		for _, x := range xs {
			out = append(out, common.LeftPadBytes(big.NewInt(x).Bytes(), 32)...)
		}
		return out
	}
	switch string(m.Data[:4]) {
	case string(selAccountByAddr):
		if f.noAccount {
			return nil, errors.New("execution reverted")
		}
		return w(42, 7_000_000, 0, 0, 0, 0, 0, 0, 0), nil
	case string(selGetPosition):
		if new(big.Int).SetBytes(m.Data[4:36]).Int64() != 20 || new(big.Int).SetBytes(m.Data[36:68]).Int64() != 42 {
			return w(0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0), nil
		}
		return w(42, 0, 0, 2, 100, 4000, 130, 1, 0, 0, 0, 400000, 1), nil
	}
	return nil, errors.New("unexpected call")
}
func (perplChain) BalanceAt(context.Context, common.Address, *big.Int) (*big.Int, error) {
	return big.NewInt(0), nil
}

func TestPerplState(t *testing.T) {
	dep := testDep()
	lot, free, err := perplState(context.Background(), perplChain{}, dep, "0x1111111111111111111111111111111111111111", 20)
	if err != nil || lot.String() != "130" || free.String() != "7000000" {
		t.Errorf("%v %v %v", lot, free, err)
	}
	lot, free, err = perplState(context.Background(), perplChain{noAccount: true}, dep, "0x1111111111111111111111111111111111111111", 20)
	if err != nil || !lot.IsZero() || !free.IsZero() {
		t.Errorf("no account: %v %v %v", lot, free, err)
	}
}
