package planner

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/snehendu098/sweem-terminal/internal/config"
)

// Call is one step as a transaction, byte-identical to the executor's steps::build (proved by the golden fixture).
// Spend is what the call takes from the wallet; Approve = To pulls Spend, so an ERC-20 allowance must exist first.
type Call struct {
	To          common.Address
	Value       *big.Int
	Data        []byte
	SpendToken  common.Address
	SpendAmount *big.Int
	Approve     bool
}

var maxU256 = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))

// enc ABI-encodes sig's args; a static tuple param is passed as its flattened fields (same words on the wire).
func enc(sig string, args ...any) []byte {
	name, list, _ := strings.Cut(strings.TrimSuffix(sig, ")"), "(")
	var in abi.Arguments
	if list != "" {
		for _, t := range strings.Split(list, ",") {
			typ, err := abi.NewType(t, "", nil)
			if err != nil {
				panic(err)
			}
			in = append(in, abi.Argument{Type: typ})
		}
	}
	body, err := in.Pack(args...)
	if err != nil {
		panic(fmt.Sprintf("%s: %v", name, err))
	}
	return append(crypto.Keccak256([]byte(sig))[:4], body...)
}

const orderDesc = "uint256,uint256,uint8,uint256,uint256,uint256,uint256,bool,bool,bool,uint256,uint256,uint256,uint256,uint256"

func bigOf(s string) (*big.Int, error) {
	v, ok := new(big.Int).SetString(s, 10)
	if !ok || v.Sign() < 0 || v.Cmp(maxU256) > 0 {
		return nil, fmt.Errorf("not an unsigned integer: %q", s)
	}
	return v, nil
}

func pos(s string) (*big.Int, error) {
	v, err := bigOf(s)
	if err == nil && v.Sign() == 0 {
		err = fmt.Errorf("zero amount")
	}
	return v, err
}

func u96(v *big.Int) (*big.Int, error) {
	if v.Sign() == 0 {
		return nil, fmt.Errorf("rounds to zero")
	}
	if v.BitLen() > 96 {
		return nil, fmt.Errorf("%s overflows uint96", v)
	}
	return v, nil
}

func (p *Planner) token(sym string) (common.Address, uint8, error) {
	t, ok := p.Dep.Tokens[sym]
	if !ok {
		return common.Address{}, 0, fmt.Errorf("unknown token %s", sym)
	}
	return common.HexToAddress(t.Address), t.Decimals, nil
}

func pow(d uint8) *big.Int { return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(d)), nil) }

// Encode renders one step for wallet exactly as the executor would sign it.
func (p *Planner) Encode(wallet common.Address, s Step) (c Call, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%s: %v", s.Action, r)
		}
	}()
	dep := p.Dep
	c.Value = new(big.Int)
	pull := func(to common.Address, data []byte, tok common.Address, amount *big.Int) Call {
		native := tok == (common.Address{})
		v := new(big.Int)
		if native {
			v = amount
		}
		return Call{To: to, Value: v, Data: data, SpendToken: tok, SpendAmount: amount, Approve: !native}
	}
	plain := func(to string, data []byte) Call {
		return Call{To: common.HexToAddress(to), Value: new(big.Int), Data: data}
	}
	market := func() (config.Market, error) {
		m, ok := dep.Kuru.Markets[s.Market]
		if !ok {
			return m, fmt.Errorf("unknown market %s", s.Market)
		}
		return m, nil
	}
	switch s.Action {
	case "kuru.marketBuy":
		m, err := market()
		if err != nil {
			return c, err
		}
		q, qdec, err := p.token(m.Quote)
		if err != nil {
			return c, err
		}
		quote, err := pos(s.Quote)
		if err != nil {
			return c, err
		}
		qs, err := u96(new(big.Int).Div(new(big.Int).Mul(quote, new(big.Int).SetUint64(m.PricePrecision)), pow(qdec)))
		minOut, err2 := bigOf(s.MinOut)
		if err != nil || err2 != nil {
			return c, fmt.Errorf("marketBuy: %v %v", err, err2)
		}
		return pull(common.HexToAddress(m.Orderbook), enc("placeAndExecuteMarketBuy(uint96,uint256,bool,bool)", qs, minOut, false, false), q, quote), nil
	case "kuru.marketSell":
		m, err := market()
		if err != nil {
			return c, err
		}
		b, bdec, err := p.token(m.Base)
		if err != nil {
			return c, err
		}
		size, err := pos(s.Size)
		if err != nil {
			return c, err
		}
		sp := new(big.Int).SetUint64(m.SizePrecision)
		sz, err := u96(new(big.Int).Div(new(big.Int).Mul(size, sp), pow(bdec)))
		minOut, err2 := bigOf(s.MinOut)
		if err != nil || err2 != nil {
			return c, fmt.Errorf("marketSell: %v %v", err, err2)
		}
		used := new(big.Int).Div(new(big.Int).Mul(sz, pow(bdec)), sp)
		return pull(common.HexToAddress(m.Orderbook), enc("placeAndExecuteMarketSell(uint96,uint256,bool,bool)", sz, minOut, false, false), b, used), nil
	case "kuru.marginDeposit":
		t, _, err := p.token(s.Token)
		a, err2 := pos(s.Amount)
		if err != nil || err2 != nil {
			return c, fmt.Errorf("marginDeposit: %v %v", err, err2)
		}
		return pull(common.HexToAddress(dep.Kuru.MarginAccount), enc("deposit(address,address,uint256)", wallet, t, a), t, a), nil
	case "kuru.marginWithdraw":
		t, _, err := p.token(s.Token)
		a, err2 := pos(s.Amount)
		if err != nil || err2 != nil {
			return c, fmt.Errorf("marginWithdraw: %v %v", err, err2)
		}
		return plain(dep.Kuru.MarginAccount, enc("withdraw(uint256,address)", a, t)), nil
	case "kuru.limit":
		m, err := market()
		if err != nil {
			return c, err
		}
		px, err := pos(s.Price)
		if err != nil || px.BitLen() > 32 {
			return c, fmt.Errorf("price is not a positive uint32")
		}
		sz, err := bigOf(s.Size)
		if err == nil {
			sz, err = u96(sz)
		}
		if err != nil || s.PostOnly == nil {
			return c, fmt.Errorf("limit: bad size or postOnly")
		}
		fn := map[string]string{"buy": "addBuyOrder", "sell": "addSellOrder"}[s.Side]
		if fn == "" {
			return c, fmt.Errorf("side %s", s.Side)
		}
		return plain(m.Orderbook, enc(fn+"(uint32,uint96,bool)", uint32(px.Uint64()), sz, *s.PostOnly)), nil
	case "kuru.cancel":
		m, err := market()
		if err != nil || len(s.OrderIDs) == 0 {
			return c, fmt.Errorf("cancel: bad market or no ids")
		}
		ids := make([]*big.Int, len(s.OrderIDs))
		for i, id := range s.OrderIDs {
			v, err := bigOf(id)
			if err != nil || v.BitLen() > 40 {
				return c, fmt.Errorf("order id %s", id)
			}
			ids[i] = v
		}
		return plain(m.Orderbook, enc("batchCancelOrders(uint40[])", ids)), nil
	case "kuru.swap":
		if len(s.Hops) == 0 {
			return c, fmt.Errorf("no hops")
		}
		var obs []common.Address
		var isBuy, native []bool
		debit, _, err := p.token(s.From)
		credit, _, err2 := p.token(s.To)
		a, err3 := pos(s.Amount)
		minOut, err4 := bigOf(s.MinOut)
		if err != nil || err2 != nil || err3 != nil || err4 != nil {
			return c, fmt.Errorf("swap: bad token or amount")
		}
		for i, h := range s.Hops {
			m, ok := dep.Kuru.Markets[h.Market]
			if !ok {
				return c, fmt.Errorf("unknown market %s", h.Market)
			}
			obs, isBuy, native = append(obs, common.HexToAddress(m.Orderbook)), append(isBuy, h.IsBuy), append(native, i == 0 && debit == (common.Address{}))
		}
		data := enc("anyToAnySwap(address[],bool[],bool[],address,address,uint256,uint256)", obs, isBuy, native, debit, credit, a, minOut)
		return pull(common.HexToAddress(dep.Kuru.Router), data, debit, a), nil
	case "lend.supply", "lend.withdraw":
		pool := map[string]string{"aaveV3": dep.Lending.AaveV3.Pool, "neverland": dep.Lending.Neverland.Pool}[s.Venue]
		t, _, err := p.token(s.Asset)
		if pool == "" || err != nil || t == (common.Address{}) {
			return c, fmt.Errorf("lend: bad venue or asset")
		}
		if s.Action == "lend.withdraw" {
			a := maxU256
			if s.Amount != "max" {
				if a, err = pos(s.Amount); err != nil {
					return c, err
				}
			}
			return plain(pool, enc("withdraw(address,uint256,address)", t, a, wallet)), nil
		}
		a, err := pos(s.Amount)
		if err != nil {
			return c, err
		}
		return pull(common.HexToAddress(pool), enc("supply(address,uint256,address,uint16)", t, a, wallet, uint16(0)), t, a), nil
	case "vault.deposit", "vault.redeem", "vault.withdraw":
		var asset string
		for _, v := range dep.Lending.ERC4626 {
			if strings.EqualFold(v.Vault, s.Vault) {
				asset = v.Asset
			}
		}
		t, _, err := p.token(asset)
		if asset == "" || err != nil {
			return c, fmt.Errorf("vault %s not allowlisted", s.Vault)
		}
		v := common.HexToAddress(s.Vault)
		switch s.Action {
		case "vault.deposit":
			a, err := pos(s.Amount)
			if err != nil {
				return c, err
			}
			return pull(v, enc("deposit(uint256,address)", a, wallet), t, a), nil
		case "vault.redeem":
			sh, err := pos(s.Shares)
			if err != nil {
				return c, err
			}
			return plain(s.Vault, enc("redeem(uint256,address,address)", sh, wallet, wallet)), nil
		}
		a, err := pos(s.Amount)
		if err != nil {
			return c, err
		}
		return plain(s.Vault, enc("withdraw(uint256,address,address)", a, wallet, wallet)), nil
	case "shmonad.deposit":
		a, err := pos(s.Amount)
		if err != nil {
			return c, err
		}
		return pull(common.HexToAddress(dep.Staking.Shmonad), enc("deposit(uint256,address)", a, wallet), common.Address{}, a), nil
	case "shmonad.redeem":
		sh, err := pos(s.Shares)
		if err != nil {
			return c, err
		}
		return plain(dep.Staking.Shmonad, enc("redeem(uint256,address,address)", sh, wallet, wallet)), nil
	case "activeVault.deposit":
		b, err := pos(s.Base)
		q, err2 := pos(s.Quote)
		usdc, _, _ := p.token("USDC")
		if err != nil || err2 != nil {
			return c, fmt.Errorf("activeVault: bad amounts")
		}
		return Call{To: common.HexToAddress(dep.Kuru.ActiveVault), Value: b, Data: enc("deposit(uint256,uint256)", b, q), SpendToken: usdc, SpendAmount: q, Approve: true}, nil
	case "activeVault.withdraw":
		sh, err := pos(s.Shares)
		if err != nil {
			return c, err
		}
		return plain(dep.Kuru.ActiveVault, enc("withdraw(uint256)", sh)), nil
	case "stable.swap":
		pool := dep.Swap.AUSD3Pool
		i, ok1 := pool.Coins[s.From]
		j, ok2 := pool.Coins[s.To]
		a, err := pos(s.Amount)
		minOut, err2 := bigOf(s.MinOut)
		t, _, err3 := p.token(s.From)
		if !ok1 || !ok2 || err != nil || err2 != nil || err3 != nil {
			return c, fmt.Errorf("stable.swap: bad coins or amounts")
		}
		return pull(common.HexToAddress(pool.Pool), enc("exchange(int128,int128,uint256,uint256)", big.NewInt(int64(i)), big.NewInt(int64(j)), a, minOut), t, a), nil
	case "perpl.createAccount", "perpl.deposit":
		a, err := pos(s.Amount)
		if err != nil {
			return c, err
		}
		ausd, _, _ := p.token("AUSD")
		fn := map[string]string{"perpl.createAccount": "createAccount(uint256)", "perpl.deposit": "depositCollateral(uint256)"}[s.Action]
		return pull(common.HexToAddress(dep.Perpl.Exchange), enc(fn, a), ausd, a), nil
	case "perpl.withdraw":
		a, err := pos(s.Amount)
		if err != nil {
			return c, err
		}
		return plain(dep.Perpl.Exchange, enc("withdrawCollateral(uint256)", a)), nil
	case "perpl.requestRemoveMargin":
		id, ok := dep.Perpl.PerpIds[s.Perp]
		a, err := pos(s.Amount)
		if !ok || err != nil {
			return c, fmt.Errorf("requestRemoveMargin: bad perp or amount")
		}
		return plain(dep.Perpl.Exchange, enc("requestDecreasePositionCollateral(uint256,uint256,bool)", big.NewInt(int64(id)), a, false)), nil
	case "perpl.order":
		id, ok := dep.Perpl.PerpIds[s.Perp]
		if !ok {
			return c, fmt.Errorf("unknown perp %s", s.Perp)
		}
		z := new(big.Int)
		var t uint8
		price, lot, amount := z, z, z
		lev, neg, ioc := int64(s.LeverageHdths), int64(1000), true
		switch {
		case (s.Kind == "openShort" || s.Kind == "closeShort") && s.Amount == "" && s.LeverageHdths > 0:
			t = map[string]uint8{"openShort": 1, "closeShort": 3}[s.Kind]
			var err, err2 error
			if price, err = pos(s.Price); err == nil {
				lot, err2 = pos(s.Lot)
			}
			if err != nil || err2 != nil {
				return c, fmt.Errorf("perpl.order: bad price or lot")
			}
		case s.Kind == "addMargin" && s.Price == "" && s.Lot == "" && s.LeverageHdths == 0:
			var err error
			if amount, err = pos(s.Amount); err != nil {
				return c, err
			}
			t, lev, neg, ioc = 5, 100, 0, false
		default:
			return c, fmt.Errorf("perpl.order kind %s: bad or missing fields", s.Kind)
		}
		data := enc("execOrder(("+orderDesc+"))", z, big.NewInt(int64(id)), t, z, price, lot, z, false, false, ioc, z, big.NewInt(lev), z, amount, big.NewInt(neg))
		return plain(dep.Perpl.Exchange, data), nil
	case "wmon.wrap":
		a, err := pos(s.Amount)
		if err != nil {
			return c, err
		}
		return Call{To: common.HexToAddress(dep.Tokens["WMON"].Address), Value: a, Data: enc("deposit()"), SpendAmount: a}, nil
	case "wmon.unwrap":
		a, err := pos(s.Amount)
		if err != nil {
			return c, err
		}
		return plain(dep.Tokens["WMON"].Address, enc("withdraw(uint256)", a)), nil
	case "fee.transfer":
		if dep.Treasury == "" {
			return c, fmt.Errorf("treasury not set in deployments")
		}
		usdc, _, _ := p.token("USDC")
		a, err := pos(s.Amount)
		if err != nil {
			return c, err
		}
		return Call{To: usdc, Value: new(big.Int), Data: enc("transfer(address,uint256)", common.HexToAddress(dep.Treasury), a), SpendToken: usdc, SpendAmount: a}, nil
	}
	return c, fmt.Errorf("unknown action %s", s.Action)
}

// TxRequest is an unsigned transaction for the client's Privy wallet (eth_sendTransaction shape).
type TxRequest struct {
	To          string `json:"to"`
	Data        string `json:"data"`
	Value       string `json:"value"` // hex wei
	ChainID     int    `json:"chainId"`
	Description string `json:"description"`
	Step        int    `json:"step"` // index into the plan; an approve carries the index of the step it enables
}

// Allowance reports the wallet's current ERC-20 allowance for spender (nil Allowance = always approve).
type Allowance func(token, spender common.Address) (*big.Int, error)

// Render turns a plan into ordered unsigned txs for wallet: an exact approve before each step that pulls an ERC-20
// the spender may not take yet (the executor does the same approve itself when signing).
func (p *Planner) Render(wallet common.Address, steps []Step, allowance Allowance) ([]TxRequest, error) {
	var out []TxRequest
	for i, s := range steps {
		c, err := p.Encode(wallet, s)
		if err != nil {
			return nil, fail("bad_request", "step %d: %v", i, err)
		}
		if c.Approve {
			need := true
			if allowance != nil {
				have, err := allowance(c.SpendToken, c.To)
				if err != nil {
					return nil, err
				}
				need = have.Cmp(c.SpendAmount) < 0
			}
			if need {
				out = append(out, TxRequest{To: c.SpendToken.Hex(), Data: hexutil.Encode(enc("approve(address,uint256)", c.To, c.SpendAmount)),
					Value: "0x0", ChainID: config.MainnetChainID, Description: "approve " + p.symbol(c.SpendToken) + " " + c.SpendAmount.String(), Step: i})
			}
		}
		out = append(out, TxRequest{To: c.To.Hex(), Data: hexutil.Encode(c.Data), Value: hexutil.EncodeBig(c.Value), ChainID: config.MainnetChainID,
			Description: describe(s), Step: i})
	}
	return out, nil
}

func (p *Planner) symbol(a common.Address) string {
	for sym, t := range p.Dep.Tokens {
		if strings.EqualFold(t.Address, a.Hex()) {
			return sym
		}
	}
	return a.Hex()
}

func describe(s Step) string {
	switch s.Action {
	case "kuru.marketBuy":
		return fmt.Sprintf("buy %s with %s USDC atoms (min %s)", s.Market, s.Quote, s.MinOut)
	case "kuru.marketSell":
		return fmt.Sprintf("sell %s %s atoms (min %s USDC atoms)", s.Size, s.Market, s.MinOut)
	}
	return s.Action
}

// ChainAllowance reads ERC-20 allowance(owner, spender) over c.
func ChainAllowance(ctx context.Context, c Chain, owner common.Address) Allowance {
	return func(token, spender common.Address) (*big.Int, error) {
		out, err := c.CallContract(ctx, callMsg(token, enc("allowance(address,address)", owner, spender)), nil)
		if err != nil {
			return nil, err
		}
		if len(out) < 32 {
			return nil, fmt.Errorf("allowance: short return")
		}
		return new(big.Int).SetBytes(out[:32]), nil
	}
}
