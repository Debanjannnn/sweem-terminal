package sched

import (
	"context"
	_ "embed"
	"math/big"
	"reflect"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/config"
	"github.com/snehendu098/sweem-terminal/internal/planner"
)

// Perp is one Perpl short as the exchange sees it. Prices in PNS (10^-PriceDec USD), lots in LNS (10^-LotDec units), collateral 6dp.
type Perp struct {
	Account            uint64          // 0 = wallet has no Perpl account
	Deposit, PnL, Mark decimal.Decimal // PnL at mark incl. funding (pnlCNS)
	Lot                decimal.Decimal
	PriceDec, LotDec   int32
	MaintHdths         decimal.Decimal // max leverage at maintenance ×100 (2000 = 20x ⇒ 5% margin)
	MarkValid          bool
}

// GuardAction lifts a short near maintenance back to maintenance + 2×buffer: TopUp USDC atoms from the wallet first,
// then LotCut perp lots; CloseAll only when no remaining size fits the equity.
type GuardAction struct {
	Fire, CloseAll bool
	TopUp, LotCut  decimal.Decimal
}

var minTopUp = decimal.New(1, 6) // 1 USDC; smaller top-ups are not worth a swap

// Guard fires when the margin fraction ((collateral + PnL) / notional) is within buffer of maintenance.
// Lot cut assumes a partial close leaves the position's collateral in place (Perpl isolated margin): hypothesis, verify on fork.
func Guard(p Perp, buffer, wallet decimal.Decimal) GuardAction {
	if !p.Lot.IsPositive() || !p.MarkValid || !p.MaintHdths.IsPositive() {
		return GuardAction{}
	}
	notional := p.Mark.Mul(p.Lot).Shift(6 - p.PriceDec - p.LotDec)
	equity := p.Deposit.Add(p.PnL)
	maint := decimal.NewFromInt(100).Div(p.MaintHdths)
	if equity.Div(notional).GreaterThanOrEqual(maint.Add(buffer)) {
		return GuardAction{}
	}
	target := maint.Add(buffer.Mul(decimal.NewFromInt(2)))
	a := GuardAction{Fire: true}
	if need := target.Mul(notional).Sub(equity).Ceil(); wallet.GreaterThanOrEqual(need) {
		a.TopUp = need
		return a
	}
	if wallet.GreaterThanOrEqual(minTopUp) {
		a.TopUp = wallet.Floor()
	}
	keep := p.Lot.Mul(equity.Add(a.TopUp)).Div(target.Mul(notional)).Floor()
	if !keep.IsPositive() {
		return GuardAction{Fire: true, CloseAll: true}
	}
	a.LotCut = p.Lot.Sub(keep)
	return a
}

//go:embed perpl_abi.json
var perplABIJSON string

var perplABI = func() abi.ABI {
	a, err := abi.JSON(strings.NewReader(perplABIJSON))
	if err != nil {
		panic(err)
	}
	return a
}()

// RPC is Chain over an eth client (*ethclient.Client).
type RPC struct {
	C   planner.Chain
	Dep *config.Deployments143
}

func (r RPC) Wallet(ctx context.Context, wallet, sym string) (decimal.Decimal, error) {
	return planner.WalletBalance(ctx, r.C, r.Dep, wallet, sym)
}

func (r RPC) Balances(ctx context.Context, wallet string, s planner.Strategy, legs []planner.LegState, peers []planner.Peer) ([]planner.Balance, error) {
	return planner.Balances(ctx, r.C, r.Dep, wallet, s, legs, peers)
}

func (r RPC) call(ctx context.Context, method string, args ...any) ([]any, error) {
	data, err := perplABI.Pack(method, args...)
	if err != nil {
		return nil, err
	}
	to := common.HexToAddress(r.Dep.Perpl.Exchange)
	out, err := r.C.CallContract(ctx, ethereum.CallMsg{To: &to, Data: data}, nil)
	if err != nil {
		return nil, err
	}
	return perplABI.Unpack(method, out)
}

// field reads a big.Int field of an abi-decoded tuple.
func field(v any, name string) decimal.Decimal {
	return decimal.NewFromBigInt(reflect.ValueOf(v).FieldByName(name).Interface().(*big.Int), 0)
}

// Perp reads the wallet's short on perpID. getAccountByAddr reverts when the wallet has no account.
func (r RPC) Perp(ctx context.Context, wallet string, perpID int) (Perp, error) {
	acct, err := r.call(ctx, "getAccountByAddr", common.HexToAddress(wallet))
	if err != nil {
		if strings.Contains(err.Error(), "revert") {
			return Perp{}, nil
		}
		return Perp{}, err
	}
	id := field(acct[0], "AccountId")
	p := Perp{Account: id.BigInt().Uint64()}
	pid := big.NewInt(int64(perpID))
	pos, err := r.call(ctx, "getPosition", pid, id.BigInt())
	if err != nil {
		return p, err
	}
	p.Deposit, p.Lot = field(pos[0], "DepositCNS"), field(pos[0], "LotLNS")
	p.PnL = decimal.NewFromBigInt(reflect.ValueOf(pos[0]).FieldByName("PnlCNS").Interface().(*big.Int), 0)
	p.Mark, p.MarkValid = decimal.NewFromBigInt(pos[1].(*big.Int), 0), pos[2].(bool)
	if !p.Lot.IsPositive() {
		return p, nil
	}
	info, err := r.call(ctx, "getPerpetualInfo", pid)
	if err != nil {
		return p, err
	}
	p.PriceDec, p.LotDec = int32(field(info[0], "PriceDecimals").IntPart()), int32(field(info[0], "LotDecimals").IntPart())
	mf, err := r.call(ctx, "getMarginFractions", pid, p.Lot.BigInt())
	if err != nil {
		return p, err
	}
	p.MaintHdths = decimal.NewFromBigInt(mf[1].(*big.Int), 0)
	return p, nil
}
