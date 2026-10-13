// Package perpl reads Perpl's exchange contract (markets, a wallet's short) and holds the risk rules shared by the
// scheduler (acts on them) and the api (shows them).
package perpl

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
)

// Env defaults, same for scheduler and api: margin-fraction headroom over maintenance; APR hysteresis to re-enable basis.
const (
	DefaultGuardBuffer = "0.03"
	DefaultGateBand    = "0.02"
)

// Perp is one Perpl short as the exchange sees it. Prices in PNS (10^-PriceDec USD), lots in LNS (10^-LotDec units), collateral 6dp.
type Perp struct {
	Account                   uint64          // 0 = wallet has no Perpl account
	Free                      decimal.Decimal // account balance not in any position (AUSD atoms)
	Deposit, PnL, Mark, Entry decimal.Decimal // position collateral; PnL at mark incl. funding (pnlCNS)
	Funding                   decimal.Decimal // premiumPnlCNS: funding accrued (pnlCNS = deltaPnlCNS + premiumPnlCNS)
	Lot                       decimal.Decimal
	PriceDec, LotDec          int32
	MaintHdths                decimal.Decimal // max leverage at maintenance ×100 (2000 = 20x ⇒ 5% margin)
	MarkValid                 bool
}

// Notional is mark × size in 6dp USD atoms.
func (p Perp) Notional() decimal.Decimal { return p.Mark.Mul(p.Lot).Shift(6 - p.PriceDec - p.LotDec) }

// Maint is the maintenance margin fraction (0.05 = 5%).
func (p Perp) Maint() decimal.Decimal { return decimal.NewFromInt(100).Div(p.MaintHdths) }

// LiqPrice is the USD mark at which the short's margin fraction falls to frac (maintenance = liquidation, maintenance + buffer = guard).
// equity E and size q are fixed as price moves: (E + (P − P')q) / (P'q) = frac ⇒ P' = (E/q + P) / (1 + frac).
func (p Perp) LiqPrice(frac decimal.Decimal) decimal.Decimal {
	q := p.Lot.Shift(-p.LotDec)
	return p.Deposit.Add(p.PnL).Shift(-6).Div(q).Add(p.Mark.Shift(-p.PriceDec)).Div(frac.Add(decimal.NewFromInt(1)))
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
	notional := p.Notional()
	equity := p.Deposit.Add(p.PnL)
	maint := p.Maint()
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

// Gate is the funding-gate hysteresis: an open basis leg stays while trailing APR ≥ threshold; a closed one reopens only above threshold+band.
func Gate(open bool, apr, threshold, band decimal.Decimal) bool {
	if open {
		return apr.GreaterThanOrEqual(threshold)
	}
	return apr.GreaterThan(threshold.Add(band))
}

//go:embed abi.json
var abiJSON string

var ABI = func() abi.ABI {
	a, err := abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		panic(err)
	}
	return a
}()

// Reader calls the exchange's view functions.
type Reader struct {
	C        ethereum.ContractCaller
	Exchange string
}

func (r Reader) call(ctx context.Context, method string, args ...any) ([]any, error) {
	data, err := ABI.Pack(method, args...)
	if err != nil {
		return nil, err
	}
	to := common.HexToAddress(r.Exchange)
	out, err := r.C.CallContract(ctx, ethereum.CallMsg{To: &to, Data: data}, nil)
	if err != nil {
		return nil, err
	}
	return ABI.Unpack(method, out)
}

// field reads a numeric field of an abi-decoded tuple.
func field(v any, name string) decimal.Decimal {
	return decimal.NewFromBigInt(reflect.ValueOf(v).FieldByName(name).Interface().(*big.Int), 0)
}

// Position reads the wallet's short on perpID. getAccountByAddr reverts when the wallet has no account.
func (r Reader) Position(ctx context.Context, wallet string, perpID int) (Perp, error) {
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
	p.Deposit, p.Lot, p.Entry, p.PnL, p.Funding = field(pos[0], "DepositCNS"), field(pos[0], "LotLNS"), field(pos[0], "PricePNS"), field(pos[0], "PnlCNS"), field(pos[0], "PremiumPnlCNS")
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

// Market is one perp's on-chain state in plain units: USD prices, asset-unit open interest, margin fractions (0.05 = 5%).
type Market struct {
	PerpID          int
	Name            string
	Mark, Oracle    decimal.Decimal
	LongOI, ShortOI decimal.Decimal
	Init, Maint     decimal.Decimal
}

// Market reads mark, oracle, open interest and the margin fractions for a one-unit position.
func (r Reader) Market(ctx context.Context, perpID int) (Market, error) {
	pid := big.NewInt(int64(perpID))
	info, err := r.call(ctx, "getPerpetualInfo", pid)
	if err != nil {
		return Market{}, err
	}
	pd, ld := int32(field(info[0], "PriceDecimals").IntPart()), int32(field(info[0], "LotDecimals").IntPart())
	m := Market{PerpID: perpID, Name: reflect.ValueOf(info[0]).FieldByName("Symbol").String(),
		Mark: field(info[0], "MarkPNS").Shift(-pd), Oracle: field(info[0], "OraclePNS").Shift(-pd),
		LongOI: field(info[0], "LongOpenInterestLNS").Shift(-ld), ShortOI: field(info[0], "ShortOpenInterestLNS").Shift(-ld)}
	mf, err := r.call(ctx, "getMarginFractions", pid, decimal.New(1, ld).BigInt())
	if err != nil {
		return m, err
	}
	hundred := decimal.NewFromInt(100)
	m.Init, m.Maint = hundred.Div(decimal.NewFromBigInt(mf[0].(*big.Int), 0)), hundred.Div(decimal.NewFromBigInt(mf[1].(*big.Int), 0))
	return m, nil
}
