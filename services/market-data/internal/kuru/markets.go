package kuru

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/chains"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/store"
)

// Mainnet and testnet are different APIs wearing the same name: snake-case with
// fees in bps on one, camelCase with fees in parts-per-100k on the other. Two
// wire structs, one normalizer -- a single deserializer cannot serve both.

type testnetToken struct {
	TokenAddress string `json:"tokenAddress"`
	Symbol       string `json:"symbol"`
	Decimals     int    `json:"decimals"`
}

type testnetMarket struct {
	MarketAddress       string       `json:"marketAddress"`
	Symbol              string       `json:"symbol"`
	BaseToken           testnetToken `json:"baseToken"`
	QuoteToken          testnetToken `json:"quoteToken"`
	Status              string       `json:"status"`
	PricePrecision      string       `json:"pricePrecision"`
	SizePrecision       string       `json:"sizePrecision"`
	TickSize            string       `json:"tickSize"`
	MinQuoteNotionalX18 string       `json:"minQuoteNotionalX18"`
	TakerFeePps         int          `json:"takerFeePps"`
	MakerFeePps         int          `json:"makerFeePps"`
}

type mainnetToken struct {
	Address string `json:"address"`
	Decimal string `json:"decimal"`
	Ticker  string `json:"ticker"`
}

type mainnetMarket struct {
	Market         string       `json:"market"`
	PricePrecision string       `json:"priceprecision"`
	SizePrecision  string       `json:"sizeprecision"`
	TickSize       string       `json:"ticksize"`
	MinSize        string       `json:"minsize"`
	TakerFeeBps    string       `json:"takerfeebps"`
	MakerFeeBps    string       `json:"makerfeebps"`
	BaseToken      mainnetToken `json:"basetoken"`
	QuoteToken     mainnetToken `json:"quotetoken"`
}

// Markets fetches /markets and refreshes the cache the book reader scales with.
func (c *Client) Markets(ctx context.Context) ([]store.Market, error) {
	var (
		out []store.Market
		err error
	)
	if c.chainID == chains.MonadMainnet {
		var env struct {
			Data struct {
				Data []mainnetMarket `json:"data"`
			} `json:"data"`
		}
		if err = c.get(ctx, "/markets", &env); err != nil {
			return nil, err
		}
		out = normalizeAll(len(env.Data.Data), func(i int) (store.Market, error) {
			return normalizeMainnet(env.Data.Data[i])
		})
	} else {
		var env struct {
			Data []testnetMarket `json:"data"`
		}
		if err = c.get(ctx, "/markets", &env); err != nil {
			return nil, err
		}
		out = normalizeAll(len(env.Data), func(i int) (store.Market, error) {
			return normalizeTestnet(env.Data[i])
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("kuru /markets on chain %d yielded no usable markets", c.chainID)
	}
	c.mu.Lock()
	c.markets = make(map[string]store.Market, len(out))
	c.order = c.order[:0]
	for _, m := range out {
		c.markets[m.ID] = m
		c.order = append(c.order, m.ID)
	}
	c.mu.Unlock()
	return out, nil
}

// Market reads the cache; it never fetches, because /markets is polled anyway.
func (c *Client) Market(id string) (store.Market, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m, ok := c.markets[strings.ToLower(id)]
	return m, ok
}

func (c *Client) Cached() []store.Market {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]store.Market, 0, len(c.order))
	for _, id := range c.order {
		out = append(out, c.markets[id])
	}
	return out
}

// one bad market must not blank the whole list
func normalizeAll(n int, fn func(int) (store.Market, error)) []store.Market {
	out := make([]store.Market, 0, n)
	for i := range n {
		m, err := fn(i)
		if err != nil {
			slog.Warn("skipping kuru market", "err", err)
			continue
		}
		out = append(out, m)
	}
	return out
}
