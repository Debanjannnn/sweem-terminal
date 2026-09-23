// Package kuru reads Kuru Spot V2. The live feed is the gateway websocket:
// binary KXMD frames whose per-kind layout lives in frame.go. REST serves the
// market catalog and a depth snapshot. Nothing here reads the book by eth_call
// -- getL2Book reports stored depth, which overstates what is executable once
// a resting order's minSizeAfterBlock has passed.
package kuru

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/chains"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/config"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/store"
)

type Client struct {
	chainID   int
	api       string
	gateway   string
	gatewayWS string
	rpcURL    string
	http      *http.Client

	mu      sync.RWMutex
	markets map[string]store.Market
	order   []string
}

func New(chainID int) *Client {
	api := config.GetEnv(fmt.Sprintf("KURU_API_URL_%d", chainID), "")
	if api == "" && chainID != chains.MonadMainnet {
		api = config.GetEnv("KURU_API_URL", "") // legacy single-chain key, testnet only
	}
	if api == "" {
		api = defaultAPI(chainID)
	}
	return &Client{
		chainID:   chainID,
		api:       withAPIVersion(api),
		gateway:   strings.TrimRight(config.GetEnv("KURU_GATEWAY_URL", gatewayHost), "/"),
		gatewayWS: config.GetEnv("KURU_GATEWAY_WS_URL", gatewayWSURL),
		rpcURL:    config.GetEnv(fmt.Sprintf("MONAD_RPC_URL_%d", chainID), ""),
		http:      &http.Client{Timeout: 15 * time.Second},
		markets:   map[string]store.Market{},
	}
}

// Spot V2 is testnet-only: mainnet has no deployment, so there is one gateway
// host and an env override for the day there are two.
const (
	gatewayHost  = "https://gateway.testnet.kuru.io"
	gatewayWSURL = "wss://gateway.testnet.kuru.io/ws"
)

// Every data-source route lives under /api/v1 and the bare host answers 404,
// but deployed env values point at the host.
func withAPIVersion(base string) string {
	base = strings.TrimRight(base, "/")
	if strings.HasSuffix(base, "/api/v1") {
		return base
	}
	return base + "/api/v1"
}

func defaultAPI(chainID int) string {
	if chainID == chains.MonadMainnet {
		return "https://api.kuru.io/api/v1"
	}
	return "https://api.testnet.kuru.io/api/v1"
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	return c.getURL(ctx, c.api+path, out)
}

func (c *Client) getURL(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("kuru %s is unreachable: %w", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("kuru %s returned HTTP %d: %s", url, resp.StatusCode, truncate(body))
	}
	return json.Unmarshal(body, out)
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

type rpcResponse struct {
	Result string `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// rpc returns the hex-decoded result of a single JSON-RPC call.
func (c *Client) rpc(ctx context.Context, method string, params ...any) ([]byte, error) {
	if c.rpcURL == "" {
		return nil, fmt.Errorf("MONAD_RPC_URL_%d is not set, so no Kuru contract can be read", c.chainID)
	}
	payload, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: 1, Method: method, Params: params})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.rpcURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("monad rpc at %s is unreachable: %w", c.rpcURL, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	var out rpcResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("monad rpc returned a body that is not JSON-RPC: %s", truncate(body))
	}
	if out.Error != nil {
		return nil, fmt.Errorf("%s reverted: %s (code %d)", method, out.Error.Message, out.Error.Code)
	}
	return hex.DecodeString(strings.TrimPrefix(out.Result, "0x"))
}

func truncate(b []byte) string {
	if len(b) > 256 {
		return string(b[:256]) + "..."
	}
	return string(b)
}
