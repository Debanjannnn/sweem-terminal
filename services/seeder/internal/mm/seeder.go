package mm

import (
	"context"
	"errors"
	"log/slog"
	"math/big"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/streams"
	"github.com/snehendu098/sweem-terminal/services/seeder/internal/executor"
	"github.com/snehendu098/sweem-terminal/services/seeder/internal/kuru"
)

const native = "0x0000000000000000000000000000000000000000"

type Config struct {
	Params
	Enabled        bool
	MaxNotionalUSD decimal.Decimal // across all markets and both sides
	RequoteBps     int64
	RequoteEvery   time.Duration
	MinGap         time.Duration // lets the previous tx land in Kuru's open-order view before we requote again
	DepositEvery   time.Duration
	DepositTargets map[string]decimal.Decimal // lowercase token -> raw atoms wanted in AccountCore
	GasReserveWei  decimal.Decimal
}

type MarketStatus struct {
	Ref         decimal.Decimal  `json:"ref"`
	Stopped     string           `json:"stopped,omitempty"`
	Bids        []executor.Order `json:"bids"`
	Asks        []executor.Order `json:"asks"`
	LiveOrders  int              `json:"liveOrders"`
	LastRequote time.Time        `json:"lastRequote"`
	LastTx      string           `json:"lastTx,omitempty"`
	LastErr     string           `json:"lastErr,omitempty"`
	quotedRef   decimal.Decimal
}

type Status struct {
	Enabled        bool                     `json:"enabled"`
	PublisherAlive bool                     `json:"publisherAlive"`
	AccountID      string                   `json:"accountId"`
	Inventory      map[string]string        `json:"inventory"` // token -> free+locked raw atoms
	Markets        map[string]*MarketStatus `json:"markets"`
	LastErr        string                   `json:"lastErr,omitempty"`
}

type Seeder struct {
	Cfg  Config
	RDB  *redis.Client
	Exec *executor.Client
	Kuru *kuru.Client
	// WalletBalance returns the bot wallet's native MON; nil skips native deposits.
	WalletBalance func(context.Context) (*big.Int, error)

	mu          sync.Mutex
	st          Status
	lastDeposit time.Time
}

func (s *Seeder) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.st
	out.Enabled = s.Cfg.Enabled
	out.Markets = map[string]*MarketStatus{}
	for k, v := range s.st.Markets {
		c := *v
		out.Markets[k] = &c
	}
	return out
}

func (s *Seeder) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		err := s.Cycle(ctx)
		s.mu.Lock()
		s.st.LastErr = ""
		if err != nil {
			s.st.LastErr = err.Error()
			slog.Warn("cycle", "err", err)
		}
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func getOr[T any](ctx context.Context, rdb *redis.Client, key string) (T, error) {
	v, err := streams.Get[T](ctx, rdb, key)
	if errors.Is(err, redis.Nil) {
		return v, nil
	}
	return v, err
}

// Cycle runs one pass: read registry + inventory, then requote, cancel or skip each component market.
func (s *Seeder) Cycle(ctx context.Context) error {
	alive, err := streams.Alive(ctx, s.RDB)
	if err != nil {
		return err
	}
	baskets, err := getOr[[]streams.BasketInfo](ctx, s.RDB, streams.RegistryBaskets)
	if err != nil {
		return err
	}
	all, err := getOr[[]streams.MarketInfo](ctx, s.RDB, streams.RegistryMarkets)
	if err != nil {
		return err
	}
	var markets []streams.MarketInfo
	for _, m := range all {
		if slices.ContainsFunc(baskets, func(b streams.BasketInfo) bool {
			return slices.ContainsFunc(b.Components, func(c streams.Component) bool { return c.Market == m.Market })
		}) {
			markets = append(markets, m)
		}
	}
	bal, err := s.Exec.Balances(ctx)
	if err != nil {
		return err
	}
	hold := map[string]decimal.Decimal{}
	inv := map[string]string{}
	for _, b := range bal.Balances {
		t := strings.ToLower(b.Token)
		hold[t] = b.Free.Add(b.Locked)
		inv[t] = hold[t].String()
	}
	s.maybeDeposit(ctx, hold)
	open := map[string][]string{}
	if bal.AccountID != "" && bal.AccountID != "0" {
		// without our live ids a requote would stack orders, so skip the cycle
		if open, err = s.Kuru.OpenOrderIDs(ctx, bal.AccountID); err != nil {
			return err
		}
	}

	s.mu.Lock()
	s.st.PublisherAlive, s.st.AccountID, s.st.Inventory = alive, bal.AccountID, inv
	if s.st.Markets == nil {
		s.st.Markets = map[string]*MarketStatus{}
	}
	s.mu.Unlock()

	n := decimal.NewFromInt(int64(max(len(markets), 1)))
	sideUSD := s.Cfg.MaxNotionalUSD.Div(n.Mul(decimal.NewFromInt(2)))
	for _, m := range markets {
		key := string(m.Market)
		ref, rerr := streams.Get[streams.RefPrice](ctx, s.RDB, streams.Ref(key))
		stopped := ""
		switch {
		case !s.Cfg.Enabled:
			stopped = "disabled"
		case !alive:
			stopped = "publisher_heartbeat_missing"
		case rerr != nil || !ref.Ref.IsPositive():
			stopped = "no_ref"
		case ref.Deviated:
			stopped = "ref_deviated"
		}
		live := open[key]

		s.mu.Lock()
		ms := s.st.Markets[key]
		if ms == nil {
			ms = &MarketStatus{}
			s.st.Markets[key] = ms
		}
		ms.Ref, ms.Stopped, ms.LiveOrders = ref.Ref, stopped, len(live)
		since, quotedRef := time.Since(ms.LastRequote), ms.quotedRef
		s.mu.Unlock()

		var orders []executor.Order
		switch {
		case since < s.Cfg.MinGap:
			continue
		case stopped != "":
			if len(live) == 0 {
				s.mu.Lock()
				ms.Bids, ms.Asks, ms.quotedRef = nil, nil, decimal.Zero
				s.mu.Unlock()
				continue
			}
		case !Due(quotedRef, ref.Ref, since, s.Cfg.RequoteEvery, s.Cfg.RequoteBps):
			continue
		default:
			orders = Ladder(m, ref.Ref, s.Cfg.Params, Inventory{
				Base:    hold[string(m.Base)],
				Quote:   hold[string(m.Quote)].Div(n), // quote is shared by every market
				SideUSD: sideUSD,
			})
		}
		tx, err := s.Exec.Orders(ctx, executor.OrdersReq{Market: key, Cancels: live, Orders: orders})

		s.mu.Lock()
		ms.LastRequote, ms.LastErr = time.Now(), ""
		if err != nil {
			ms.LastErr = err.Error()
			slog.Warn("orders", "market", key, "err", err)
		} else {
			ms.LastTx, ms.quotedRef, ms.Bids, ms.Asks = tx.TxHash, ref.Ref, nil, nil
			if stopped != "" {
				ms.quotedRef = decimal.Zero
			}
			for _, o := range orders {
				if o.Side == "buy" {
					ms.Bids = append(ms.Bids, o)
				} else {
					ms.Asks = append(ms.Asks, o)
				}
			}
		}
		s.mu.Unlock()
	}
	return nil
}

// maybeDeposit tops AccountCore up to each target, keeping GasReserveWei of native MON in the wallet.
func (s *Seeder) maybeDeposit(ctx context.Context, hold map[string]decimal.Decimal) {
	if !s.Cfg.Enabled || time.Since(s.lastDeposit) < s.Cfg.DepositEvery {
		return
	}
	s.lastDeposit = time.Now()
	for token, target := range s.Cfg.DepositTargets {
		need := target.Sub(hold[token])
		if token == native {
			if s.WalletBalance == nil {
				continue
			}
			wb, err := s.WalletBalance(ctx)
			if err != nil {
				slog.Warn("wallet balance", "err", err)
				continue
			}
			need = decimal.Min(need, decimal.NewFromBigInt(wb, 0).Sub(s.Cfg.GasReserveWei))
		}
		if !need.IsPositive() {
			continue
		}
		tx, err := s.Exec.Deposit(ctx, executor.DepositReq{Token: token, Amount: need.String()})
		slog.Info("deposit", "token", token, "amount", need.String(), "tx", tx.TxHash, "err", err)
	}
}
