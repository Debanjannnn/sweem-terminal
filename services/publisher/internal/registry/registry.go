// Package registry mirrors the factory's allowed markets and baskets into Redis.
package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/chain/bindings/basket"
	"github.com/snehendu098/sweem-terminal/internal/chain/bindings/factory"
	"github.com/snehendu098/sweem-terminal/internal/streams"
)

// Only the Kuru/ERC-20 reads the publisher needs; full ABIs live in abi/kuru.
var kuruABI = mustABI(`[
{"type":"function","name":"getMarketParams","stateMutability":"view","inputs":[],"outputs":[{"type":"uint32"},{"type":"uint96"},{"type":"uint32"},{"type":"uint96"},{"type":"uint96"},{"type":"uint256"},{"type":"uint256"}]},
{"type":"function","name":"baseToken","stateMutability":"view","inputs":[],"outputs":[{"type":"address"}]},
{"type":"function","name":"quoteToken","stateMutability":"view","inputs":[],"outputs":[{"type":"address"}]},
{"type":"function","name":"decimals","stateMutability":"view","inputs":[],"outputs":[{"type":"uint8"}]},
{"type":"function","name":"getBalance","stateMutability":"view","inputs":[{"type":"address"},{"type":"address"}],"outputs":[{"type":"uint256"}]}]`)

func mustABI(s string) abi.ABI {
	a, err := abi.JSON(strings.NewReader(s))
	if err != nil {
		panic(err)
	}
	return a
}

// Call runs a read-only call against the minimal Kuru/ERC-20 ABI.
func Call(ctx context.Context, eth bind.ContractCaller, opts *bind.CallOpts, to common.Address, method string, args ...any) ([]any, error) {
	if opts == nil {
		opts = &bind.CallOpts{}
	}
	o := *opts
	o.Context = ctx
	var out []any
	err := bind.NewBoundContract(to, kuruABI, eth, nil, nil).Call(&o, &out, method, args...)
	return out, err
}

type Registry struct {
	ETH     *ethclient.Client
	Factory common.Address
	RDB     redis.Cmdable

	mu      sync.RWMutex
	markets []streams.MarketInfo
	baskets []streams.BasketInfo
	cache   map[common.Address]streams.MarketInfo
	bcache  map[common.Address]streams.BasketInfo // components are immutable
	lastM   []byte
	lastB   []byte
}

func (r *Registry) Markets() []streams.MarketInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.markets
}

func (r *Registry) Baskets() []streams.BasketInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.baskets
}

// Run re-reads the factory every interval; a periodic read picks up MarketAllowed/BasketCreated without log tracking.
func (r *Registry) Run(ctx context.Context, every time.Duration) error {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if err := r.Refresh(ctx); err != nil {
				slog.Warn("registry refresh", "err", err)
			}
		}
	}
}

func (r *Registry) Refresh(ctx context.Context) error {
	f, err := factory.NewSweemBasketFactoryCaller(r.Factory, r.ETH)
	if err != nil {
		return err
	}
	opts := &bind.CallOpts{Context: ctx}
	addrs, err := f.AllowedMarkets(opts)
	if err != nil {
		return err
	}
	if r.cache == nil {
		r.cache = map[common.Address]streams.MarketInfo{}
		r.bcache = map[common.Address]streams.BasketInfo{}
	}
	markets := make([]streams.MarketInfo, 0, len(addrs))
	for _, a := range addrs {
		m, ok := r.cache[a]
		if !ok {
			if m, err = r.market(ctx, a); err != nil {
				return err
			}
			r.cache[a] = m
		}
		markets = append(markets, m)
	}
	bs, err := f.Baskets(opts)
	if err != nil {
		return err
	}
	baskets := make([]streams.BasketInfo, 0, len(bs))
	for _, a := range bs {
		b, ok := r.bcache[a]
		if !ok {
			if b, err = r.basket(ctx, a); err != nil {
				return err
			}
			r.bcache[a] = b
		}
		baskets = append(baskets, b)
	}
	mj, _ := json.Marshal(markets)
	bj, _ := json.Marshal(baskets)
	if !bytes.Equal(mj, r.lastM) {
		if err := r.RDB.Set(ctx, streams.RegistryMarkets, mj, 0).Err(); err != nil {
			return err
		}
		slog.Info("registry markets", "n", len(markets))
	}
	if !bytes.Equal(bj, r.lastB) {
		if err := r.RDB.Set(ctx, streams.RegistryBaskets, bj, 0).Err(); err != nil {
			return err
		}
		slog.Info("registry baskets", "n", len(baskets))
	}
	r.mu.Lock()
	r.markets, r.baskets, r.lastM, r.lastB = markets, baskets, mj, bj
	r.mu.Unlock()
	return nil
}

func (r *Registry) decimals(ctx context.Context, token common.Address) (int, error) {
	if token == (common.Address{}) {
		return 18, nil // native MON
	}
	out, err := Call(ctx, r.ETH, nil, token, "decimals")
	if err != nil {
		return 0, err
	}
	return int(out[0].(uint8)), nil
}

func (r *Registry) market(ctx context.Context, a common.Address) (streams.MarketInfo, error) {
	var m streams.MarketInfo
	p, err := Call(ctx, r.ETH, nil, a, "getMarketParams")
	if err != nil {
		return m, err
	}
	base, err := Call(ctx, r.ETH, nil, a, "baseToken")
	if err != nil {
		return m, err
	}
	quote, err := Call(ctx, r.ETH, nil, a, "quoteToken")
	if err != nil {
		return m, err
	}
	bt, qt := base[0].(common.Address), quote[0].(common.Address)
	bd, err := r.decimals(ctx, bt)
	if err != nil {
		return m, err
	}
	qd, err := r.decimals(ctx, qt)
	if err != nil {
		return m, err
	}
	u := func(v any) decimal.Decimal {
		switch x := v.(type) {
		case uint32:
			return decimal.NewFromInt(int64(x))
		case *big.Int:
			return decimal.NewFromBigInt(x, 0)
		}
		panic("unexpected abi type")
	}
	return streams.MarketInfo{
		Market: streams.Addr(a.Hex()), Base: streams.Addr(bt.Hex()), Quote: streams.Addr(qt.Hex()),
		BaseDecimals: bd, QuoteDecimals: qd,
		PricePrecision: u(p[0]), SizePrecision: u(p[1]), TickSize: u(p[2]),
		MinSize:     decimal.Zero, // V2 has no size floor, only minQuoteNotional
		TakerFeePps: u(p[5]),
	}, nil
}

func (r *Registry) basket(ctx context.Context, a common.Address) (streams.BasketInfo, error) {
	var b streams.BasketInfo
	c, err := basket.NewSweemBasketCaller(a, r.ETH)
	if err != nil {
		return b, err
	}
	opts := &bind.CallOpts{Context: ctx}
	if b.Name, err = c.Name(opts); err != nil {
		return b, err
	}
	if b.Symbol, err = c.Symbol(opts); err != nil {
		return b, err
	}
	comps, err := c.Components(opts)
	if err != nil {
		return b, err
	}
	b.Address = streams.Addr(a.Hex())
	b.Components = []streams.Component{}
	for i, x := range comps {
		tok, err := c.Tokens(opts, big.NewInt(int64(i)))
		if err != nil {
			return b, err
		}
		b.Components = append(b.Components, streams.Component{Market: streams.Addr(x.Market.Hex()), Token: streams.Addr(tok.Hex()), WeightBps: int(x.WeightBps)})
	}
	return b, nil
}
