// Package indexerclient is the only place GraphQL strings for the Envio
// indexer live. market-data, reconciler and wallet all go through it.
package indexerclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/config"
)

// ponytail: fixed page cap, add cursor paging when a market outgrows it
const pageLimit = 1000

type Client struct {
	url  string
	http *http.Client
}

func New() *Client {
	return &Client{
		url:  config.GetEnv("INDEXER_URL", "http://localhost:8080/v1/graphql"),
		http: &http.Client{Timeout: 15 * time.Second},
	}
}

// all amounts are decimal strings, never floats
type Position struct {
	ID         string `json:"id"`
	Owner      string `json:"owner"`
	MarketID   string `json:"marketId"`
	IsLong     bool   `json:"isLong"`
	Size       string `json:"size"`
	EntryPrice string `json:"entryPrice"`
	Collateral string `json:"collateral"`
	Block      uint64 `json:"blockNumber"`
	UpdatedAt  int64  `json:"updatedAt"`
}

type PositionEvent struct {
	ID       string `json:"id"`
	MarketID string `json:"marketId"`
	Owner    string `json:"owner"`
	Kind     string `json:"kind"`
	IsLong   bool   `json:"isLong"`
	Size     string `json:"size"`
	Price    string `json:"price"`
	Fee      string `json:"fee"`
	Block    uint64 `json:"blockNumber"`
	TxHash   string `json:"txHash"`
	Ts       int64  `json:"timestamp"`
}

type RatePoint struct {
	MarketID string `json:"marketId"`
	Rate     string `json:"fundingRate"`
	Ts       int64  `json:"timestamp"`
}

type VaultFlow struct {
	ID     string `json:"id"`
	Owner  string `json:"owner"`
	Kind   string `json:"kind"`
	Assets string `json:"assets"`
	Shares string `json:"shares"`
	Block  uint64 `json:"blockNumber"`
	TxHash string `json:"txHash"`
	Ts     int64  `json:"timestamp"`
}

type KuruTrade struct {
	ID       string `json:"id"`
	MarketID string `json:"marketId"`
	Price    string `json:"price"`
	Size     string `json:"size"`
	TxHash   string `json:"txHash"`
	Ts       int64  `json:"timestamp"`

	// the event carries the MAKER side; a resting bid means the taker sold
	MakerIsBuy bool `json:"makerIsBuy"`
	// taker side, which is the direction a tape prints
	IsBuy bool `json:"-"`
}

const positionsDoc = `query Positions($owner: String!, $limit: Int!) {
  Position(where: {owner: {_eq: $owner}}, order_by: {updatedAt: desc}, limit: $limit) {
    id owner marketId isLong size entryPrice collateral blockNumber updatedAt
  }
}`

func (c *Client) Positions(ctx context.Context, owner string) ([]Position, error) {
	var out struct {
		Position []Position `json:"Position"`
	}
	err := c.query(ctx, positionsDoc, map[string]any{"owner": strings.ToLower(owner), "limit": pageLimit}, &out)
	return out.Position, err
}

const positionEventsDoc = `query PositionEvents($marketId: String!, $fromBlock: numeric!, $limit: Int!) {
  PositionEvent(where: {marketId: {_eq: $marketId}, blockNumber: {_gte: $fromBlock}}, order_by: {blockNumber: asc}, limit: $limit) {
    id marketId owner kind isLong size price fee blockNumber txHash timestamp
  }
}`

func (c *Client) PositionEvents(ctx context.Context, marketID string, fromBlock uint64) ([]PositionEvent, error) {
	var out struct {
		PositionEvent []PositionEvent `json:"PositionEvent"`
	}
	err := c.query(ctx, positionEventsDoc, map[string]any{"marketId": marketID, "fromBlock": fromBlock, "limit": pageLimit}, &out)
	return out.PositionEvent, err
}

const fundingRateDoc = `query FundingRatePoints($marketId: String!, $fromTs: numeric!, $toTs: numeric!, $limit: Int!) {
  FundingRatePoint(where: {marketId: {_eq: $marketId}, timestamp: {_gte: $fromTs, _lte: $toTs}}, order_by: {timestamp: asc}, limit: $limit) {
    marketId fundingRate timestamp
  }
}`

func (c *Client) FundingRatePoints(ctx context.Context, marketID string, fromTs, toTs int64) ([]RatePoint, error) {
	var out struct {
		FundingRatePoint []RatePoint `json:"FundingRatePoint"`
	}
	err := c.query(ctx, fundingRateDoc, map[string]any{"marketId": marketID, "fromTs": fromTs, "toTs": toTs, "limit": pageLimit}, &out)
	return out.FundingRatePoint, err
}

const vaultFlowsDoc = `query VaultFlows($owner: String!, $limit: Int!) {
  VaultFlow(where: {owner: {_eq: $owner}}, order_by: {timestamp: desc}, limit: $limit) {
    id owner kind assets shares blockNumber txHash timestamp
  }
}`

func (c *Client) VaultFlows(ctx context.Context, owner string) ([]VaultFlow, error) {
	var out struct {
		VaultFlow []VaultFlow `json:"VaultFlow"`
	}
	err := c.query(ctx, vaultFlowsDoc, map[string]any{"owner": strings.ToLower(owner), "limit": pageLimit}, &out)
	return out.VaultFlow, err
}

// spot v2 emits packed TradesPacked logs, which the indexer decodes into
// KuruFill. prices and sizes are raw book units; the caller scales them.
const kuruTradesDoc = `query KuruTrades($market: String!, $fromTs: numeric!, $toTs: numeric!, $limit: Int!) {
  KuruFill(where: {market: {_eq: $market}, timestamp: {_gte: $fromTs, _lte: $toTs}}, order_by: {timestamp: asc, recordIdx: asc}, limit: $limit) {
    id
    marketId: market
    price
    size: filledSize
    makerIsBuy
    txHash
    timestamp
  }
}`

// candle backfill source
func (c *Client) KuruTrades(ctx context.Context, marketID string, fromTs, toTs int64) ([]KuruTrade, error) {
	var out struct {
		KuruFill []KuruTrade `json:"KuruFill"`
	}
	err := c.query(ctx, kuruTradesDoc, map[string]any{"market": strings.ToLower(marketID), "fromTs": fromTs, "toTs": toTs, "limit": pageLimit}, &out)
	for i := range out.KuruFill {
		out.KuruFill[i].IsBuy = !out.KuruFill[i].MakerIsBuy
	}
	return out.KuruFill, err
}

func (c *Client) query(ctx context.Context, doc string, vars map[string]any, out any) error {
	body, err := json.Marshal(map[string]any{"query": doc, "variables": vars})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("indexer at %s is unreachable: %w", c.url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("indexer at %s returned HTTP %d for a GraphQL query", c.url, resp.StatusCode)
	}
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("indexer response was not valid JSON: %w", err)
	}
	if len(envelope.Errors) > 0 {
		msgs := make([]string, len(envelope.Errors))
		for i, e := range envelope.Errors {
			msgs[i] = e.Message
		}
		return fmt.Errorf("indexer rejected the query: %s", strings.Join(msgs, "; "))
	}
	if len(envelope.Data) == 0 {
		return fmt.Errorf("indexer returned neither data nor errors, so the query result is unusable")
	}
	return json.Unmarshal(envelope.Data, out)
}
