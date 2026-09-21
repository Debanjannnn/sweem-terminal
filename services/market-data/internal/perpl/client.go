package perpl

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/config"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/wad"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/candle"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/store"
)

// MaxCandles is Perpl's hard per-request cap.
const MaxCandles = 1024

// Resolutions Perpl serves, in seconds.
var Resolutions = []int{60, 300, 900, 1800, 3600, 7200, 14400, 28800, 43200, 86400}

type Client struct {
	api   string
	wsURL string
	http  *http.Client

	mu  sync.RWMutex
	ctx *Context
}

func New() *Client {
	return &Client{
		api:   strings.TrimRight(config.GetEnv("PERPL_API_URL", "https://testnet.perpl.xyz/api"), "/"),
		wsURL: strings.TrimRight(config.GetEnv("PERPL_WS_URL", "wss://testnet.perpl.xyz"), "/"),
		http:  &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.api+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("perpl %s is unreachable: %w", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("perpl %s returned HTTP %d", path, resp.StatusCode)
	}
	return json.Unmarshal(body, out)
}

// Refresh reloads /v1/pub/context, which is where every market id, decimal and
// address comes from.
func (c *Client) Refresh(ctx context.Context) (*Context, error) {
	var w wireContext
	if err := c.get(ctx, "/v1/pub/context", &w); err != nil {
		return nil, err
	}
	pc, err := normalizeContext(w)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.ctx = pc
	c.mu.Unlock()
	return pc, nil
}

func (c *Client) Cached() *Context {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ctx
}

func (c *Client) Market(id string) (store.Market, bool) {
	n, err := strconv.Atoi(id)
	pc := c.Cached()
	if err != nil || pc == nil {
		return store.Market{}, false
	}
	return pc.Market(n)
}

type wireCandle struct {
	T int64  `json:"t"`
	O int64  `json:"o"`
	C int64  `json:"c"`
	H int64  `json:"h"`
	L int64  `json:"l"`
	V string `json:"v"`
}

// Candles are Perpl's own and authoritative for Perpl markets; we never
// aggregate our own for them. from and to are milliseconds.
func (c *Client) Candles(ctx context.Context, marketID string, resolution int, fromMillis, toMillis int64) ([]candle.Candle, error) {
	m, ok := c.Market(marketID)
	if !ok {
		return nil, fmt.Errorf("market %q is not in the Perpl context", marketID)
	}
	span := int64(resolution) * 1000
	if fromMillis < toMillis-MaxCandles*span {
		fromMillis = toMillis - MaxCandles*span
	}
	var out struct {
		D []wireCandle `json:"d"`
	}
	path := fmt.Sprintf("/v1/market-data/%s/candles/%d/%d-%d", marketID, resolution, fromMillis, toMillis)
	if err := c.get(ctx, path, &out); err != nil {
		return nil, err
	}
	cs := make([]candle.Candle, 0, len(out.D))
	for _, w := range out.D {
		cs = append(cs, candle.Candle{
			MarketID:   m.ID,
			Resolution: resolution,
			OpenTime:   w.T / 1000,
			Open:       scalePrice(w.O, m.PriceDecimals),
			High:       scalePrice(w.H, m.PriceDecimals),
			Low:        scalePrice(w.L, m.PriceDecimals),
			Close:      scalePrice(w.C, m.PriceDecimals),
			// volume is a collateral Amount here, not a base size
			Volume: scaleAmount(w.V, m.QuoteDecimals),
		})
	}
	return cs, nil
}

func scaleSize(v int64, decimals int) string {
	return wad.Scale(big.NewInt(v), decimals, wad.Decimals).String()
}
