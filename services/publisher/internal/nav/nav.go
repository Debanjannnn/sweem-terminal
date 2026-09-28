// Package nav values every basket from chain holdings × Kuru prices and is the sole writer of nav:* and nav_points.
package nav

import (
	"context"
	"errors"
	"log/slog"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/chain/bindings/basket"
	"github.com/snehendu098/sweem-terminal/internal/streams"
	"github.com/snehendu098/sweem-terminal/services/publisher/internal/registry"
)

const (
	usdcDecimals  = 6
	shareDecimals = 18
)

type Leg struct {
	Market, Token streams.Addr
	Balance       *big.Int // raw atoms
	Decimals      int
	Ref           *streams.RefPrice // nil: no price published yet
}

func units(x *big.Int, dec int) decimal.Decimal { return decimal.NewFromBigInt(x, int32(-dec)) }

// Compute values legs at mid (ref when a side is missing). Supply 0 yields navPerShare 0 and stale, so nothing acts on it.
func Compute(b streams.Addr, block uint64, now time.Time, maxAge time.Duration, legs []Leg, usdcIdle, supply *big.Int) streams.NavPoint {
	p := streams.NavPoint{Basket: b, Block: block, Ts: now.UnixMilli(), Supply: units(supply, shareDecimals), Components: []streams.NavComponent{}}
	total := units(usdcIdle, usdcDecimals)
	for _, l := range legs {
		c := streams.NavComponent{Market: l.Market, Token: l.Token, Balance: units(l.Balance, l.Decimals)}
		r := l.Ref
		if r == nil || now.Sub(time.UnixMilli(r.Ts)) > maxAge || r.Deviated {
			p.Stale = true
		}
		if r != nil {
			c.Mid = r.Mid
			if c.Mid.IsZero() {
				c.Mid = r.Ref
			}
		}
		if c.Mid.IsZero() {
			p.Stale = true
		}
		c.Value = c.Balance.Mul(c.Mid)
		total = total.Add(c.Value)
		p.Components = append(p.Components, c)
	}
	p.TotalNav = total
	if supply.Sign() == 0 {
		p.Stale = true
		return p
	}
	p.NavPerShare = total.DivRound(p.Supply, 18)
	return p
}

type Service struct {
	ETH         *ethclient.Client
	RDB         redis.Cmdable
	PG          *pgxpool.Pool
	Registry    *registry.Registry
	AccountCore common.Address
	USDC        common.Address
	MaxAge      time.Duration

	lastPoint map[streams.Addr]time.Time
}

func (s *Service) Run(ctx context.Context, every time.Duration) error {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if err := s.Tick(ctx); err != nil {
				slog.Warn("nav tick", "err", err)
			}
		}
	}
}

// Tick values every basket at one block, publishes, and persists the first non-stale point of each minute.
func (s *Service) Tick(ctx context.Context) error {
	block, err := s.ETH.BlockNumber(ctx)
	if err != nil {
		return err
	}
	decs := map[string]int{}
	for _, m := range s.Registry.Markets() {
		decs[strings.ToLower(string(m.Market))] = m.BaseDecimals
	}
	var errs []error
	for _, b := range s.Registry.Baskets() {
		p, err := s.value(ctx, b, block, decs)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if err := streams.Publish(ctx, s.RDB, streams.Nav(string(b.Address)), streams.NavLatest(string(b.Address)), p); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := s.persist(ctx, p); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Service) value(ctx context.Context, b streams.BasketInfo, block uint64, decs map[string]int) (streams.NavPoint, error) {
	addr := common.HexToAddress(string(b.Address))
	opts := &bind.CallOpts{Context: ctx, BlockNumber: new(big.Int).SetUint64(block)}
	c, err := basket.NewSweemBasketCaller(addr, s.ETH)
	if err != nil {
		return streams.NavPoint{}, err
	}
	held, err := c.Holdings(opts)
	if err != nil {
		return streams.NavPoint{}, err
	}
	supply, err := c.TotalSupply(opts)
	if err != nil {
		return streams.NavPoint{}, err
	}
	idle, err := registry.Call(ctx, s.ETH, opts, s.AccountCore, "getBalance", addr, s.USDC)
	if err != nil {
		return streams.NavPoint{}, err
	}
	legs := make([]Leg, len(b.Components))
	for i, x := range b.Components {
		k := strings.ToLower(string(x.Market))
		d, ok := decs[k]
		if !ok || i >= len(held) {
			return streams.NavPoint{}, errors.New("basket " + string(b.Address) + ": market " + k + " not in registry")
		}
		legs[i] = Leg{Market: x.Market, Token: x.Token, Balance: held[i], Decimals: d}
		r, err := streams.Get[streams.RefPrice](ctx, s.RDB, streams.Ref(k))
		if err == nil {
			legs[i].Ref = &r
		} else if !errors.Is(err, redis.Nil) {
			return streams.NavPoint{}, err
		}
	}
	return Compute(b.Address, block, time.Now(), s.MaxAge, legs, idle[0].(*big.Int), supply), nil
}

func (s *Service) persist(ctx context.Context, p streams.NavPoint) error {
	if p.Stale {
		return nil
	}
	minute := time.UnixMilli(p.Ts).Truncate(time.Minute)
	if s.lastPoint == nil {
		s.lastPoint = map[streams.Addr]time.Time{}
	}
	if s.lastPoint[p.Basket].Equal(minute) {
		return nil
	}
	_, err := s.PG.Exec(ctx, `insert into nav_points (basket, ts, nav_per_share, total_nav, supply) values ($1,$2,$3,$4,$5)
		on conflict (basket, ts) do update set nav_per_share=excluded.nav_per_share, total_nav=excluded.total_nav, supply=excluded.supply`,
		strings.ToLower(string(p.Basket)), minute, p.NavPerShare, p.TotalNav, p.Supply)
	if err == nil {
		s.lastPoint[p.Basket] = minute
	}
	return err
}
