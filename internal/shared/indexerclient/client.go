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
	"golang.org/x/crypto/sha3"
)

// ponytail: fixed page cap, add cursor paging when a market outgrows it
const pageLimit = 1000

type Client struct {
	url  string
	http *http.Client
}

func New() *Client {
	return &Client{
		url:  config.GetEnv("INDEXER_URL", "http://localhost:8090/v1/graphql"),
		http: &http.Client{Timeout: 15 * time.Second},
	}
}

// All amounts are decimal strings, never floats. Hasura renders every BigInt
// column as a JSON string, so integer fields decode with the ,string option.
// Addresses are stored EIP-55 checksummed, exactly as envio receives them;
// queries checksum theirs so an exact match hits the column as written.

// Position is one open IndexPerpEngine position. Size is unsigned and IsLong
// carries the side; the indexer stores a signed size.
type Position struct {
	ID         string `json:"id"`
	Owner      string `json:"owner"`
	MarketID   string `json:"marketId"`
	IsLong     bool   `json:"-"`
	Size       string `json:"size"`
	EntryPrice string `json:"entryPrice"`
	Collateral string `json:"collateral"`
	Block      uint64 `json:"blockNumber,string"`
	UpdatedAt  int64  `json:"updatedAt,string"`
}

type PositionEvent struct {
	ID       string `json:"id"`
	MarketID string `json:"marketId"`
	Owner    string `json:"owner"`
	Kind     string `json:"kind"`
	IsLong   bool   `json:"-"`
	Size     string `json:"size"`
	Price    string `json:"price"`
	Fee      string `json:"fee"`
	Block    uint64 `json:"blockNumber,string"`
	TxHash   string `json:"txHash"`
	Ts       int64  `json:"timestamp,string"`
}

type RatePoint struct {
	MarketID string `json:"marketId"`
	Rate     string `json:"fundingRate"`
	Ts       int64  `json:"timestamp,string"`
}

type VaultFlow struct {
	ID     string `json:"id"`
	Owner  string `json:"owner"`
	Kind   string `json:"kind"`
	Assets string `json:"assets"`
	Shares string `json:"shares"`
	Block  uint64 `json:"blockNumber,string"`
	TxHash string `json:"txHash"`
	Ts     int64  `json:"timestamp,string"`
}

type KuruTrade struct {
	ID       string `json:"id"`
	MarketID string `json:"marketId"`
	Price    string `json:"price"`
	Size     string `json:"size"`
	TxHash   string `json:"txHash"`
	Ts       int64  `json:"timestamp,string"`

	// the event carries the MAKER side; a resting bid means the taker sold
	MakerIsBuy bool `json:"makerIsBuy"`
	// taker side, which is the direction a tape prints
	IsBuy bool `json:"-"`
}

// splitSigned turns the indexer's signed size into (magnitude, isLong).
func splitSigned(v string) (string, bool) {
	if mag, neg := strings.CutPrefix(v, "-"); neg {
		return mag, false
	}
	return v, true
}

const positionsDoc = `query Positions($owner: String!, $limit: Int!) {
  Position(where: {trader_id: {_eq: $owner}, isOpen: {_eq: true}}, order_by: {openedAtTimestamp: desc}, limit: $limit) {
    id owner: trader_id marketId: market_id size entryPrice: entryIndex collateral
    blockNumber: openedAtBlock updatedAt: openedAtTimestamp
  }
}`

func (c *Client) Positions(ctx context.Context, owner string) ([]Position, error) {
	var out struct {
		Position []Position `json:"Position"`
	}
	err := c.query(ctx, positionsDoc, map[string]any{"owner": Checksum(owner), "limit": pageLimit}, &out)
	for i := range out.Position {
		out.Position[i].Size, out.Position[i].IsLong = splitSigned(out.Position[i].Size)
	}
	return out.Position, err
}

// marketID is the registry's bytes32 key as lowercase hex, which is the
// indexer's Market id; the human feed label is not.
const positionEventsDoc = `query PositionEvents($marketId: String!, $fromBlock: numeric!, $limit: Int!) {
  PositionEvent(where: {market_id: {_eq: $marketId}, blockNumber: {_gte: $fromBlock}}, order_by: {blockNumber: asc, logIndex: asc}, limit: $limit) {
    id marketId: market_id owner: trader_id kind: action size: sizeAfter price: indexPrice fee blockNumber txHash timestamp
  }
}`

func (c *Client) PositionEvents(ctx context.Context, marketID string, fromBlock uint64) ([]PositionEvent, error) {
	var out struct {
		PositionEvent []PositionEvent `json:"PositionEvent"`
	}
	err := c.query(ctx, positionEventsDoc, map[string]any{"marketId": strings.ToLower(marketID), "fromBlock": fromBlock, "limit": pageLimit}, &out)
	for i := range out.PositionEvent {
		out.PositionEvent[i].Size, out.PositionEvent[i].IsLong = splitSigned(out.PositionEvent[i].Size)
	}
	return out.PositionEvent, err
}

const fundingRateDoc = `query FundingRatePoints($marketId: String!, $fromTs: numeric!, $toTs: numeric!, $limit: Int!) {
  FundingRatePoint(where: {market_id: {_eq: $marketId}, timestamp: {_gte: $fromTs, _lte: $toTs}}, order_by: {timestamp: asc}, limit: $limit) {
    marketId: market_id fundingRate timestamp
  }
}`

func (c *Client) FundingRatePoints(ctx context.Context, marketID string, fromTs, toTs int64) ([]RatePoint, error) {
	var out struct {
		FundingRatePoint []RatePoint `json:"FundingRatePoint"`
	}
	err := c.query(ctx, fundingRateDoc, map[string]any{"marketId": strings.ToLower(marketID), "fromTs": fromTs, "toTs": toTs, "limit": pageLimit}, &out)
	return out.FundingRatePoint, err
}

// a VaultFlow's account is the LP's own address
const vaultFlowsDoc = `query VaultFlows($owner: String!, $limit: Int!) {
  VaultFlow(where: {account_id: {_eq: $owner}}, order_by: {timestamp: desc}, limit: $limit) {
    id owner: account_id kind assets shares blockNumber txHash timestamp
  }
}`

func (c *Client) VaultFlows(ctx context.Context, owner string) ([]VaultFlow, error) {
	var out struct {
		VaultFlow []VaultFlow `json:"VaultFlow"`
	}
	err := c.query(ctx, vaultFlowsDoc, map[string]any{"owner": Checksum(owner), "limit": pageLimit}, &out)
	return out.VaultFlow, err
}

// spot v2 emits packed TradesPacked logs, which the indexer decodes into
// KuruFill. prices and sizes are raw book units; the caller scales them.
const kuruTradesDoc = `query KuruTrades($market: String!, $fromTs: numeric!, $toTs: numeric!, $limit: Int!, $offset: Int!) {
  KuruFill(where: {market: {_eq: $market}, timestamp: {_gte: $fromTs, _lte: $toTs}}, order_by: {timestamp: asc, blockNumber: asc, logIndex: asc, recordIdx: asc}, limit: $limit, offset: $offset) {
    id
    marketId: market
    price
    size: filledSize
    makerIsBuy
    txHash
    timestamp
  }
}`

// maxKuruTrades bounds one backfill read; a window busier than this keeps its
// oldest fills and says so rather than paging forever.
const maxKuruTrades = 50 * pageLimit

// candle backfill source. Pages until the window is exhausted: a single page
// of a busy market is only its oldest fills.
func (c *Client) KuruTrades(ctx context.Context, marketID string, fromTs, toTs int64) ([]KuruTrade, error) {
	var all []KuruTrade
	for offset := 0; offset < maxKuruTrades; offset += pageLimit {
		var out struct {
			KuruFill []KuruTrade `json:"KuruFill"`
		}
		vars := map[string]any{"market": Checksum(marketID), "fromTs": fromTs, "toTs": toTs, "limit": pageLimit, "offset": offset}
		if err := c.query(ctx, kuruTradesDoc, vars, &out); err != nil {
			return all, err
		}
		for i := range out.KuruFill {
			out.KuruFill[i].IsBuy = !out.KuruFill[i].MakerIsBuy
		}
		all = append(all, out.KuruFill...)
		if len(out.KuruFill) < pageLimit {
			return all, nil
		}
	}
	return all, fmt.Errorf("more than %d kuru fills in the window; the newest were not read", maxKuruTrades)
}

// Checksum renders a hex address in EIP-55 mixed case. Anything that is not a
// 20-byte hex address is returned unchanged.
func Checksum(addr string) string {
	hexPart, ok := strings.CutPrefix(strings.ToLower(strings.TrimSpace(addr)), "0x")
	if !ok || len(hexPart) != 40 {
		return addr
	}
	h := sha3.NewLegacyKeccak256()
	h.Write([]byte(hexPart))
	sum := h.Sum(nil)
	out := []byte(hexPart)
	for i, c := range out {
		if c >= 'a' && c <= 'f' && (sum[i/2]>>(4*(1-uint(i%2))))&0xf >= 8 {
			out[i] = c - 32
		}
	}
	return "0x" + string(out)
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
