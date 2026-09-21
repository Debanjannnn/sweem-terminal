// Package kuru reads Kuru's spot markets. Two sources on purpose: REST for the
// market list, an eth_call for the book. Kuru's testnet REST surface is only
// /markets and /markets/:addr -- there is no book, trade or candle route.
package kuru

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/chains"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/config"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/finality"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/store"
)

type Client struct {
	chainID int
	api     string
	rpcURL  string
	http    *http.Client

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
		chainID: chainID,
		api:     strings.TrimRight(api, "/"),
		rpcURL:  config.GetEnv(fmt.Sprintf("MONAD_RPC_URL_%d", chainID), ""),
		http:    &http.Client{Timeout: 15 * time.Second},
		markets: map[string]store.Market{},
	}
}

func defaultAPI(chainID int) string {
	if chainID == chains.MonadMainnet {
		return "https://api.kuru.io/api/v1"
	}
	return "https://api.testnet.kuru.io/api/v1"
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.api+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("kuru %s is unreachable: %w", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("kuru %s returned HTTP %d: %s", path, resp.StatusCode, truncate(body))
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

// ethCall reads at `latest`, which on Monad is Proposed and therefore
// speculative. That is the right tier for a live book and the wrong one for
// anything settled.
func (c *Client) ethCall(ctx context.Context, to string, data []byte) ([]byte, error) {
	return c.rpc(ctx, "eth_call",
		map[string]string{"to": to, "data": "0x" + hex.EncodeToString(data)},
		finality.Seen.BlockTag())
}

func word(n *big.Int) []byte {
	b := make([]byte, 32)
	n.FillBytes(b)
	return b
}

func truncate(b []byte) string {
	if len(b) > 256 {
		return string(b[:256]) + "..."
	}
	return string(b)
}
