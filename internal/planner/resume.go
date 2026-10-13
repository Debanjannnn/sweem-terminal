package planner

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/config"
)

// Live is what a position still holds on chain; ResumeWithdraw checks every unwind step against it.
type Live struct {
	Bals     []Balance                  // per source, as Balances
	Wallet   map[string]decimal.Decimal // wallet atoms by token symbol (asset, WMON, AUSD, USDC)
	PerpLot  map[string]decimal.Decimal // open short lots by perp name (ETH, BTC, MON)
	PerpFree decimal.Decimal            // free Perpl account collateral (AUSD atoms)
}

var (
	selAccountByAddr = crypto.Keccak256([]byte("getAccountByAddr(address)"))[:4]
	selGetPosition   = crypto.Keccak256([]byte("getPosition(uint256,uint256)"))[:4]
)

func callMsg(to common.Address, data []byte) ethereum.CallMsg {
	return ethereum.CallMsg{To: &to, Data: data}
}

// words eth_calls `to` and returns 32-byte words of the result.
func words(ctx context.Context, c Chain, to string, data []byte) ([]decimal.Decimal, error) {
	a := common.HexToAddress(to)
	out, err := c.CallContract(ctx, callMsg(a, data), nil)
	if err != nil {
		return nil, err
	}
	ws := make([]decimal.Decimal, len(out)/32)
	for i := range ws {
		ws[i] = decimal.NewFromBigInt(new(big.Int).SetBytes(out[i*32:(i+1)*32]), 0)
	}
	return ws, nil
}

// perplState reads the wallet's free collateral and short lots. No account (getAccountByAddr reverts) = zeros.
// Layouts: AccountInfo(accountId, balanceCNS, …) and PositionInfo(accountId, next, prev, type, depositCNS, pricePNS, lotLNS, …), all static.
func perplState(ctx context.Context, c Chain, dep *config.Deployments143, wallet string, perpID int) (lot, free decimal.Decimal, err error) {
	acct, err := words(ctx, c, dep.Perpl.Exchange, append(append([]byte{}, selAccountByAddr...), common.LeftPadBytes(common.HexToAddress(wallet).Bytes(), 32)...))
	if err != nil {
		if strings.Contains(err.Error(), "revert") {
			return decimal.Zero, decimal.Zero, nil
		}
		return lot, free, err
	}
	if len(acct) < 2 || acct[0].IsZero() {
		return decimal.Zero, decimal.Zero, nil
	}
	data := append(append([]byte{}, selGetPosition...), common.LeftPadBytes(big.NewInt(int64(perpID)).Bytes(), 32)...)
	pos, err := words(ctx, c, dep.Perpl.Exchange, append(data, common.LeftPadBytes(acct[0].BigInt().Bytes(), 32)...))
	if err != nil || len(pos) < 7 {
		return lot, free, fmt.Errorf("perpl getPosition: %v", err)
	}
	return pos[6], acct[1], nil
}

// ReadLive reads everything ResumeWithdraw needs for one position.
func ReadLive(ctx context.Context, c Chain, dep *config.Deployments143, wallet string, s Strategy, legs []LegState, peers []Peer) (Live, error) {
	var l Live
	var err error
	if l.Bals, err = Balances(ctx, c, dep, wallet, s, legs, peers); err != nil {
		return l, err
	}
	l.Wallet, l.PerpLot = map[string]decimal.Decimal{}, map[string]decimal.Decimal{}
	var syms []string
	for _, pt := range s.Parts() {
		syms = append(syms, pt.Asset)
	}
	for _, sym := range append(syms, "WMON", "AUSD", "USDC") {
		if _, ok := l.Wallet[sym]; ok {
			continue
		}
		if l.Wallet[sym], err = WalletBalance(ctx, c, dep, wallet, sym); err != nil {
			return l, err
		}
	}
	for _, sym := range syms {
		if pp, ok := perps[sym]; ok {
			if l.PerpLot[pp.Name], l.PerpFree, err = perplState(ctx, c, dep, wallet, dep.Perpl.PerpIds[pp.Name]); err != nil {
				return l, err
			}
		}
	}
	return l, nil
}

// ResumeWithdraw rebuilds the unfinished tail (steps[done:]) of a failed withdraw: each step is capped by what is still
// on chain and dropped when nothing is left, so a retry never re-closes a short or withdraws twice. Sells get fresh quotes.
func (p *Planner) ResumeWithdraw(ctx context.Context, s Strategy, steps []Step, done int, live Live, slip int) ([]Step, error) {
	type held struct {
		asset string
		src   Source
	}
	var all []held // flattened over sleeves, same order as live.Bals
	for _, pt := range s.Parts() {
		for _, x := range pt.Sources {
			all = append(all, held{pt.Asset, x})
		}
	}
	if done < 0 || done > len(steps) || len(live.Bals) != len(all) {
		return nil, fail("bad_request", "step_done or live state does not match the plan")
	}
	src := func(match func(asset string, x Source) bool) (Balance, bool) {
		for i, h := range all {
			if match(h.asset, h.src) {
				return live.Bals[i], true
			}
		}
		return Balance{}, false
	}
	capAt := func(planned string, have decimal.Decimal) (decimal.Decimal, bool) {
		v := decimal.Min(d(planned), have)
		return v, v.IsPositive()
	}
	// w simulates the wallet as kept steps run, so a step fed by an earlier resumed step (unwrap after withdraw) keeps its input
	w := map[string]decimal.Decimal{}
	for k, v := range live.Wallet {
		w[k] = v
	}
	perpFree, closing := live.PerpFree, false
	// share scales a source's asset Value by the part of its shares redeemed
	share := func(b Balance, shares decimal.Decimal) decimal.Decimal {
		if !d(b.Shares).IsPositive() {
			return decimal.Zero
		}
		return d(b.Value).Mul(shares).Div(d(b.Shares)).Floor()
	}
	var out []Step
	for _, st := range steps[done:] {
		var have decimal.Decimal
		field := &st.Amount
		var credit func(v decimal.Decimal)
		switch st.Action {
		case "lend.withdraw":
			venue := map[string]string{"aaveV3": "aave", "neverland": "neverland"}[st.Venue]
			b, _ := src(func(a string, x Source) bool {
				return x.Kind == "lending" && x.Venue == venue && lendAsset(a) == st.Asset
			})
			have, credit = d(b.Value), func(v decimal.Decimal) { w[st.Asset] = w[st.Asset].Add(v) }
		case "vault.redeem", "vault.withdraw":
			var asset string
			b, _ := src(func(_ string, x Source) bool {
				v, ok := p.vault(x.Venue)
				if ok && strings.EqualFold(v.Vault, st.Vault) {
					asset = v.Asset
					return true
				}
				return false
			})
			have, credit = d(b.Value), func(v decimal.Decimal) { w[asset] = w[asset].Add(v) }
			if st.Action == "vault.redeem" {
				have, field = d(b.Shares), &st.Shares
				credit = func(v decimal.Decimal) { w[asset] = w[asset].Add(share(b, v)) }
			}
		case "shmonad.redeem", "activeVault.withdraw":
			kind := map[string]string{"shmonad.redeem": "staking", "activeVault.withdraw": "lp"}[st.Action]
			b, _ := src(func(_ string, x Source) bool { return x.Kind == kind })
			have, field = d(b.Shares), &st.Shares
			credit = func(v decimal.Decimal) {
				got := share(b, v)
				if kind == "staking" {
					got = got.Mul(decimal.NewFromInt(10_000 - shmonHaircut)).Div(bps).Floor()
				}
				w["MON"] = w["MON"].Add(got)
			}
		case "wmon.unwrap":
			have = w["WMON"]
			credit = func(v decimal.Decimal) { w["WMON"], w["MON"] = w["WMON"].Sub(v), w["MON"].Add(v) }
		case "perpl.order":
			if st.Kind != "closeShort" {
				return nil, fail("bad_request", "withdraw plans only close shorts")
			}
			have, field = live.PerpLot[st.Perp], &st.Lot
			credit = func(decimal.Decimal) { closing = true }
		case "perpl.withdraw":
			// a close in this same plan releases the position's deposit, not yet visible as free collateral
			if have = perpFree; closing {
				have = d(st.Amount)
			}
			credit = func(v decimal.Decimal) { perpFree, w["AUSD"] = perpFree.Sub(v), w["AUSD"].Add(v) }
		case "stable.swap":
			have = w[st.From]
			planned := d(st.Amount)
			credit = func(v decimal.Decimal) {
				st.MinOut = d(st.MinOut).Mul(v).Div(planned).Floor().String()
				w[st.From], w[st.To] = w[st.From].Sub(v), w[st.To].Add(d(st.MinOut))
			}
		case "kuru.marketSell":
			v, ok := capAt(st.Size, w[st.Market])
			if !ok {
				continue
			}
			q, err := p.quote(ctx, st.Market, "sell", v, slip)
			if isCode(err, "below_min_size") {
				continue
			}
			if err != nil {
				return nil, err
			}
			st.Size, st.MinOut = v.String(), q.MinOut
			out = append(out, st)
			continue
		default:
			return nil, fail("bad_request", "%s is not an unwind step", st.Action)
		}
		v, ok := capAt(*field, have)
		if !ok {
			continue
		}
		*field = v.String()
		if credit != nil {
			credit(v)
		}
		out = append(out, st)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return p.check(p.Dep, out)
}
