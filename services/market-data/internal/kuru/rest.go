package kuru

import (
	"context"
	"fmt"
	"math/big"
	"net/url"
	"strconv"
	"time"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/finality"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/book"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/store"
)

const DefaultDepth = 20

// The gateway's routes are /api/depth and /api/bbo. The doc page titles say
// "L2 order book" and "best bid and offer"; /l2Book and /bbo are NotFound.
type gwLevel struct {
	Price     string `json:"price"`
	TotalBase string `json:"total_base"`
}

type gwEnvelope struct {
	Data struct {
		Bids []gwLevel `json:"bids"`
		Asks []gwLevel `json:"asks"`
		Bid  *gwLevel  `json:"bid"`
		Ask  *gwLevel  `json:"ask"`
	} `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// Book is the REST depth snapshot, used to warm a market before the websocket
// bootstrap frame lands. The live feed is Stream; this is not polled.
func (c *Client) Book(ctx context.Context, marketID string, depth int) (book.Book, error) {
	if depth <= 0 || depth > 200 {
		depth = DefaultDepth
	}
	return c.gatewayBook(ctx, "/api/depth", marketID, finality.Final,
		url.Values{"levels": {strconv.Itoa(depth)}})
}

// BBO is the touch only. Book supersedes it for our own use; it exists because
// the touch is the cheapest cross-check against bestBidAsk() on chain, where an
// absent bid is uint32.max and an absent ask is 0 rather than null.
func (c *Client) BBO(ctx context.Context, marketID string, tier finality.Tier) (book.Book, error) {
	return c.gatewayBook(ctx, "/api/bbo", marketID, tier, nil)
}

func (c *Client) gatewayBook(ctx context.Context, path, marketID string, tier finality.Tier, q url.Values) (book.Book, error) {
	m, ok := c.Market(marketID)
	if !ok {
		return book.Book{}, fmt.Errorf("market %q is not in the Kuru market list", marketID)
	}
	if q == nil {
		q = url.Values{}
	}
	q.Set("symbol", m.Symbol)
	q.Set("state", viewState(tier))
	var env gwEnvelope
	if err := c.getURL(ctx, c.gateway+path+"?"+q.Encode(), &env); err != nil {
		return book.Book{}, err
	}
	if env.Error != nil {
		return book.Book{}, fmt.Errorf("kuru gateway %s: %s (%s)", path, env.Error.Message, env.Error.Code)
	}
	bids := append(env.Data.Bids, optional(env.Data.Bid)...)
	asks := append(env.Data.Asks, optional(env.Data.Ask)...)
	return book.Book{
		MarketID: m.ID, Venue: m.Venue,
		Bids: restLevels(bids, m, true),
		Asks: restLevels(asks, m, false),
		Ts:   time.Now().UnixMilli(),
	}, nil
}

func restLevels(ls []gwLevel, m store.Market, desc bool) []book.Level {
	out := make([]book.Level, 0, len(ls))
	for _, l := range ls {
		price, pok := new(big.Int).SetString(l.Price, 10)
		size, sok := new(big.Int).SetString(l.TotalBase, 10)
		if !pok || !sok || price.Sign() <= 0 || size.Sign() <= 0 {
			continue
		}
		out = append(out, wadLevel(price, size, m))
	}
	return book.Sort(out, desc)
}

func optional(l *gwLevel) []gwLevel {
	if l == nil {
		return nil
	}
	return []gwLevel{*l}
}
