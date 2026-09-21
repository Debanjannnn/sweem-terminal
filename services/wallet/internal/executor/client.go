// Package executor is the only client of the Rust executor. Nothing else in
// the system may reach it, and it is the only process that signs.
package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	base string
	http *http.Client
}

// the executor waits for an approve receipt before swapping, so the timeout is
// generous rather than tight
func New(base string) *Client {
	return &Client{base: strings.TrimRight(base, "/"), http: &http.Client{Timeout: 120 * time.Second}}
}

type RouteRequest struct {
	IntentKey      string `json:"intent_key"`
	PrivyWalletID  string `json:"privy_wallet_id"`
	UserWallet     string `json:"user_wallet"`
	MarketID       string `json:"market_id"`
	Venue          string `json:"venue"`
	Side           string `json:"side"`
	Amount         string `json:"amount"`
	MaxSlippageBps int    `json:"max_slippage_bps"`
	ChainID        int64  `json:"chain_id"`
}

type Step struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	TxHash string `json:"tx_hash,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// Status is "submitted" or "failed"; Error carries the sentence to show.
type RouteResponse struct {
	IntentKey string `json:"intent_key"`
	TxHash    string `json:"tx_hash"`
	Status    string `json:"status"`
	Steps     []Step `json:"steps"`
	Error     string `json:"error"`
}

// Route is idempotent on intent_key: the executor replays a recorded result.
func (c *Client) Route(ctx context.Context, req RouteRequest) (RouteResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return RouteResponse{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/route", bytes.NewReader(body))
	if err != nil {
		return RouteResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return RouteResponse{}, fmt.Errorf("the executor at %s is unreachable: %w", c.base, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return RouteResponse{}, fmt.Errorf("the executor answered HTTP %d for intent %s: %s",
			resp.StatusCode, req.IntentKey, strings.TrimSpace(string(raw)))
	}
	var out RouteResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return RouteResponse{}, fmt.Errorf("the executor answer for intent %s was not valid JSON: %w", req.IntentKey, err)
	}
	return out, nil
}
