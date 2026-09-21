// Package perpl reads Perpl's public market data: the context document and the
// unauthenticated /ws/v1/market-data socket. The trading socket belongs to the
// executor and is never opened here.
package perpl

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/wad"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/store"
)

// Market ids, decimals and collateral all differ per network -- BTC is 1 on
// mainnet and 16 on testnet. Nothing here is hardcoded; it all comes from
// GET /v1/pub/context at runtime.
type wireContext struct {
	Chain struct {
		ChainID int `json:"chain_id"`
	} `json:"chain"`
	Instances []struct {
		ID                int `json:"id"`
		CollateralTokenID int `json:"collateral_token_id"`
	} `json:"instances"`
	Tokens []struct {
		ID       int    `json:"id"`
		Symbol   string `json:"symbol"`
		Decimals int    `json:"decimals"`
	} `json:"tokens"`
	Markets []wireMarket `json:"markets"`
}

type wireMarket struct {
	ID         int    `json:"id"`
	InstanceID int    `json:"instance_id"`
	Symbol     string `json:"symbol"`
	Config     struct {
		IsOpen           bool   `json:"is_open"`
		PriceDecimals    int    `json:"price_decimals"`
		SizeDecimals     int    `json:"size_decimals"`
		MinPostingAmount string `json:"min_posting_amount"`
		MakerFee         int    `json:"maker_fee"`
		TakerFee         int    `json:"taker_fee"`
	} `json:"config"`
	State struct {
		Mrk int64 `json:"mrk"`
		Lst int64 `json:"lst"`
	} `json:"state"`
}

// Context is what the rest of the service needs out of /v1/pub/context.
type Context struct {
	ChainID            int
	CollateralSymbol   string
	CollateralDecimals int
	Markets            []store.Market
	byID               map[int]store.Market
}

func (c *Context) Market(id int) (store.Market, bool) {
	m, ok := c.byID[id]
	return m, ok
}

func (c *Context) IDs() []int {
	out := make([]int, 0, len(c.Markets))
	for _, m := range c.Markets {
		id, err := strconv.Atoi(m.ID)
		if err == nil {
			out = append(out, id)
		}
	}
	return out
}

func normalizeContext(w wireContext) (*Context, error) {
	if len(w.Markets) == 0 {
		return nil, fmt.Errorf("perpl context on chain %d lists no markets", w.Chain.ChainID)
	}
	collateralID := 0
	if len(w.Instances) > 0 {
		collateralID = w.Instances[0].CollateralTokenID
	}
	out := &Context{ChainID: w.Chain.ChainID, byID: map[int]store.Market{}}
	for _, t := range w.Tokens {
		if t.ID == collateralID {
			out.CollateralSymbol, out.CollateralDecimals = t.Symbol, t.Decimals
		}
	}
	for _, m := range w.Markets {
		status := "halted"
		if m.Config.IsOpen {
			status = "active"
		}
		nm := store.Market{
			ID:            strconv.Itoa(m.ID),
			Venue:         store.VenuePerpl,
			Kind:          "perp",
			Symbol:        m.Symbol,
			Base:          m.Symbol,
			Quote:         out.CollateralSymbol,
			BaseDecimals:  m.Config.SizeDecimals,
			QuoteDecimals: out.CollateralDecimals,
			PriceDecimals: m.Config.PriceDecimals,
			SizeDecimals:  m.Config.SizeDecimals,
			TickSize:      "1",
			// an Amount, denominated in collateral
			MinSize:            scaleAmount(m.Config.MinPostingAmount, out.CollateralDecimals),
			MinIsQuoteNotional: true,
			// perpl publishes fees in micros already
			TakerFeeMicros: m.Config.TakerFee,
			MakerFeeMicros: m.Config.MakerFee,
			Status:         status,
			Mark:           scalePrice(m.State.Mrk, m.Config.PriceDecimals),
			Last:           scalePrice(m.State.Lst, m.Config.PriceDecimals),
		}
		out.Markets = append(out.Markets, nm)
		out.byID[m.ID] = nm
	}
	return out, nil
}

func scalePrice(v int64, decimals int) string {
	return wad.Scale(big.NewInt(v), decimals, wad.Decimals).String()
}

func scaleAmount(raw string, decimals int) string {
	n, ok := new(big.Int).SetString(strings.TrimSpace(raw), 10)
	if !ok {
		return "0"
	}
	return wad.Scale(n, decimals, wad.Decimals).String()
}
