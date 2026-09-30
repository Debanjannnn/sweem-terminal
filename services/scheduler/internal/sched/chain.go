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
	Free               decimal.Decimal // account balance not in any position (AUSD atoms)
	Deposit, PnL, Mark decimal.Decimal // position collateral; PnL at mark incl. funding (pnlCNS)
	Lot                decimal.Decimal
	PriceDec, LotDec   int32
	MaintHdths         decimal.Decimal // max leverage at maintenance ×100 (2000 = 20x ⇒ 5% margin)
	MarkValid          bool
}

// GuardAction lifts a short near maintenance back to maintenance + 2×buffer. Perpl margin is isolated (verified on a
// mainnet fork 2026-09-30): depositCollateral only credits the free account balance, and a partial close releases the closed
// share of (deposit + PnL) to it. So every action ends by moving free balance into the position (addMargin, orderType 5):
// Free first, then TopUp USDC from the wallet, then LotCut lots whose Released collateral is re-added; CloseAll as last resort.
type GuardAction struct {
	Fire, CloseAll          bool
	TopUp, LotCut, Released decimal.Decimal // USDC atoms, perp lots, AUSD atoms (conservative)
}

var (
	minTopUp = decimal.New(1, 6)                  // 1 USDC; smaller top-ups are not worth a swap
	swapKeep = decimal.RequireFromString("0.99")  // USDC→AUSD after slippage + pool fee, worst case
	closeFee = decimal.RequireFromString("0.001") // of closed notional: taker fee + mark drift before the close lands
)

// Guard fires when the position's margin fraction ((deposit + PnL) / notional) is within buffer of maintenance.
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
	short := target.Mul(notional).Sub(equity).Sub(p.Free) // still missing after moving all free balance in
	if !short.IsPositive() {
		return a
	}
	if topUp := short.Div(swapKeep).Ceil(); wallet.GreaterThanOrEqual(topUp) {
		a.TopUp = topUp
		return a
	}
	if wallet.GreaterThanOrEqual(minTopUp) {
		a.TopUp = wallet.Floor()
	}
	// equity (incl. released) stays, notional shrinks with the lot
	keep := p.Lot.Mul(equity.Add(p.Free).Add(a.TopUp.Mul(swapKeep))).Div(target.Mul(notional)).Floor()
	if !keep.IsPositive() || !equity.IsPositive() {
		return GuardAction{Fire: true, CloseAll: true}
	}
	a.LotCut = p.Lot.Sub(keep)
	a.Released = decimal.Max(decimal.Zero, equity.Sub(notional.Mul(closeFee)).Mul(a.LotCut).Div(p.Lot).Floor())
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

func (r RPC) Live(ctx context.Context, wallet string, s planner.Strategy, legs []planner.LegState, peers []planner.Peer) (planner.Live, error) {
	return planner.ReadLive(ctx, r.C, r.Dep, wallet, s, legs, peers)
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
	p := Perp{Account: id.BigInt().Uint64(), Free: field(acct[0], "BalanceCNS")}
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
