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

	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/policy"
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

// RouteRequest is the taker leg: OrderBook.swap, exact input.
type RouteRequest struct {
	IntentKey     string `json:"intent_key"`
	PrivyWalletID string `json:"privy_wallet_id"`
	UserWallet    string `json:"user_wallet"`
	MarketID      string `json:"market_id"`
	Venue         string `json:"venue"`
	Side          string `json:"side"`
	// raw atoms of the INPUT token: quote for a buy, base for a sell
	Amount         string `json:"amount"`
	MaxSlippageBps int    `json:"max_slippage_bps"`
	ChainID        int64  `json:"chain_id"`
}

// OrderRequest is the maker leg: OrderBook.batch with one order.
type OrderRequest struct {
	IntentKey     string `json:"intent_key"`
	PrivyWalletID string `json:"privy_wallet_id"`
	UserWallet    string `json:"user_wallet"`
	MarketID      string `json:"market_id"`
	Side          string `json:"side"`
	// raw tick integer: price * price_precision
	Price string `json:"price"`
	// book units: size * size_precision
	Quantity string `json:"quantity"`
	Tif      string `json:"tif"`
	PostOnly bool   `json:"post_only"`
	ChainID  int64  `json:"chain_id"`
}

// CancelRequest is slot-indexed because Kuru has no cancel-by-order-id, and
// slots are reused, so the order the slot is believed to hold is required.
type CancelRequest struct {
	IntentKey       string `json:"intent_key"`
	PrivyWalletID   string `json:"privy_wallet_id"`
	UserWallet      string `json:"user_wallet"`
	MarketID        string `json:"market_id"`
	SlotIdx         int    `json:"slot_idx"`
	ExpectedOrderID string `json:"expected_order_id"`
	ChainID         int64  `json:"chain_id"`
}

type AccountRequest struct {
	PrivyWalletID string `json:"privy_wallet_id"`
	UserWallet    string `json:"user_wallet"`
	ChainID       int64  `json:"chain_id"`
}

type DepositRequest struct {
	IntentKey     string `json:"intent_key"`
	PrivyWalletID string `json:"privy_wallet_id"`
	UserWallet    string `json:"user_wallet"`
	MarketID      string `json:"market_id"`
	// "base" or "quote", never an address
	Token string `json:"token"`
	// raw token atoms
	Amount  string `json:"amount"`
	ChainID int64  `json:"chain_id"`
}

type SignerRequest struct {
	IntentKey     string `json:"intent_key"`
	PrivyWalletID string `json:"privy_wallet_id"`
	UserWallet    string `json:"user_wallet"`
	// unix seconds; 0 means the executor's default of now + 30 days
	Expiry  int64 `json:"expiry"`
	ChainID int64 `json:"chain_id"`
}

type Step struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	TxHash string `json:"tx_hash,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// RouteResponse is the answer to every write endpoint. Status is truthful and
// venue-specific: "submitted", "confirmed", "resting", "not_resting",
// "filled_or_expired", "canceled", "not_canceled" or "failed". Error carries
// the sentence to show.
type RouteResponse struct {
	IntentKey string          `json:"intent_key"`
	TxHash    string          `json:"tx_hash"`
	Status    string          `json:"status"`
	Steps     []Step          `json:"steps"`
	Error     string          `json:"error"`
	Info      json.RawMessage `json:"info,omitempty"`
}

// Account is the read-only answer of /account/ensure. UserID 0 means the
// address has never deposited into AccountCore and cannot trade.
type Account struct {
	ChainID          int64  `json:"chain_id"`
	AccountCore      string `json:"account_core"`
	UserWallet       string `json:"user_wallet"`
	UserID           int64  `json:"user_id"`
	Registered       bool   `json:"registered"`
	Signer           string `json:"signer"`
	SignerAuthorized bool   `json:"signer_authorized"`
	AuthNonce        string `json:"auth_nonce"`
	Error            string `json:"error"`
}

// Route is idempotent on intent_key, as is every call below: the executor
// replays a recorded result rather than signing twice.
func (c *Client) Route(ctx context.Context, req RouteRequest) (RouteResponse, error) {
	return c.write(ctx, "/route", req.IntentKey, req)
}

func (c *Client) Order(ctx context.Context, req OrderRequest) (RouteResponse, error) {
	return c.write(ctx, "/order", req.IntentKey, req)
}

func (c *Client) Cancel(ctx context.Context, req CancelRequest) (RouteResponse, error) {
	return c.write(ctx, "/order/cancel", req.IntentKey, req)
}

func (c *Client) Deposit(ctx context.Context, req DepositRequest) (RouteResponse, error) {
	return c.write(ctx, "/account/deposit", req.IntentKey, req)
}

func (c *Client) Authorize(ctx context.Context, req SignerRequest) (RouteResponse, error) {
	return c.write(ctx, "/account/authorize", req.IntentKey, req)
}

func (c *Client) Revoke(ctx context.Context, req SignerRequest) (RouteResponse, error) {
	return c.write(ctx, "/account/revoke", req.IntentKey, req)
}

// EnsureAccount reads the account state. It signs nothing and costs no gas.
func (c *Client) EnsureAccount(ctx context.Context, req AccountRequest) (Account, error) {
	var out Account
	if err := c.post(ctx, "/account/ensure", "ensure", req, &out); err != nil {
		return out, err
	}
	if out.Error != "" {
		return out, fmt.Errorf("the executor could not read the kuru account: %s", out.Error)
	}
	return out, nil
}

// Markets is the committed allowlist with every scale on it. The executor owns
// it, so this is the only source of the precisions a maker order is sized in.
func (c *Client) Markets(ctx context.Context) ([]policy.Market, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/markets", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("the executor at %s is unreachable: %w", c.base, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the executor answered HTTP %d for the market allowlist", resp.StatusCode)
	}
	var body struct {
		Markets []policy.Market `json:"markets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("the executor market allowlist was not valid JSON: %w", err)
	}
	return body.Markets, nil
}

func (c *Client) write(ctx context.Context, path, intentKey string, req any) (RouteResponse, error) {
	var out RouteResponse
	err := c.post(ctx, path, intentKey, req, &out)
	return out, err
}

func (c *Client) post(ctx context.Context, path, intentKey string, req, dst any) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("the executor at %s is unreachable: %w", c.base, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the executor answered HTTP %d for intent %s: %s",
			resp.StatusCode, intentKey, strings.TrimSpace(string(raw)))
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("the executor answer for intent %s was not valid JSON: %w", intentKey, err)
	}
	return nil
}

// PerplAccountRequest reads the Perpl account a wallet owns. The wallet id is
// checked against the address before anything is read.
type PerplAccountRequest struct {
	PrivyWalletID string `json:"privy_wallet_id"`
	UserWallet    string `json:"user_wallet"`
}

// PerplPosition is one open position. Integers are the venue's own units:
// price in PNS, lot in LNS, collateral in atoms; the decimals say how to read them.
type PerplPosition struct {
	MarketID       string  `json:"market_id"`
	Symbol         string  `json:"symbol"`
	Side           string  `json:"side"`
	Lot            string  `json:"lot"`
	EntryPrice     string  `json:"entry_price"`
	Deposit        string  `json:"deposit"`
	PnL            string  `json:"pnl"`
	MarkPrice      *string `json:"mark_price"`
	LiquidationEst *string `json:"liquidation_price_est"`
	PriceDecimals  int     `json:"price_decimals"`
	SizeDecimals   int     `json:"size_decimals"`
}

type PerplCollateral struct {
	Symbol   string `json:"symbol"`
	Address  string `json:"address"`
	Decimals int    `json:"decimals"`
}

type PerplAccount struct {
	ChainID     int64           `json:"chain_id"`
	Exchange    string          `json:"exchange"`
	Collateral  PerplCollateral `json:"collateral"`
	MinOpen     string          `json:"min_open"`
	MinDeposit  string          `json:"min_deposit"`
	MinWithdraw string          `json:"min_withdraw"`
	Registered  bool            `json:"registered"`
	AccountID   string          `json:"account_id,omitempty"`
	Balance     string          `json:"balance,omitempty"`
	Locked      string          `json:"locked,omitempty"`
	Frozen      bool            `json:"frozen,omitempty"`
	Positions   []PerplPosition `json:"positions"`
	Error       string          `json:"error,omitempty"`
}

// PerplFundsRequest moves collateral in or out: amount in collateral atoms.
type PerplFundsRequest struct {
	IntentKey     string `json:"intent_key"`
	PrivyWalletID string `json:"privy_wallet_id"`
	UserWallet    string `json:"user_wallet"`
	Amount        string `json:"amount"`
}

// PerplOrderRequest opens, adds to or closes a position. kind is market, limit
// or close; lot in LNS, price in PNS (limit only), leverage in hundredths.
type PerplOrderRequest struct {
	IntentKey      string `json:"intent_key"`
	PrivyWalletID  string `json:"privy_wallet_id"`
	UserWallet     string `json:"user_wallet"`
	MarketID       string `json:"market_id"`
	Side           string `json:"side"`
	Kind           string `json:"kind"`
	Lot            string `json:"lot"`
	Price          string `json:"price,omitempty"`
	Leverage       int    `json:"leverage"`
	PostOnly       bool   `json:"post_only"`
	MaxSlippageBps int    `json:"max_slippage_bps"`
}

type PerplCancelRequest struct {
	IntentKey     string `json:"intent_key"`
	PrivyWalletID string `json:"privy_wallet_id"`
	UserWallet    string `json:"user_wallet"`
	MarketID      string `json:"market_id"`
	OrderID       string `json:"order_id"`
}

func (c *Client) PerplAccount(ctx context.Context, req PerplAccountRequest) (PerplAccount, error) {
	var out PerplAccount
	if err := c.post(ctx, "/perpl/account", "perpl-account", req, &out); err != nil {
		return out, err
	}
	if out.Error != "" {
		return out, fmt.Errorf("the executor could not read the perpl account: %s", out.Error)
	}
	return out, nil
}

func (c *Client) PerplDeposit(ctx context.Context, req PerplFundsRequest) (RouteResponse, error) {
	return c.write(ctx, "/perpl/deposit", req.IntentKey, req)
}

func (c *Client) PerplWithdraw(ctx context.Context, req PerplFundsRequest) (RouteResponse, error) {
	return c.write(ctx, "/perpl/withdraw", req.IntentKey, req)
}

func (c *Client) PerplOrder(ctx context.Context, req PerplOrderRequest) (RouteResponse, error) {
	return c.write(ctx, "/perpl/order", req.IntentKey, req)
}

func (c *Client) PerplCancel(ctx context.Context, req PerplCancelRequest) (RouteResponse, error) {
	return c.write(ctx, "/perpl/cancel", req.IntentKey, req)
}
