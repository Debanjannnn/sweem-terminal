// Package executor is the seeder's client for the executor bot routes (architecture §10).
package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/httpx"
)

type Order struct {
	Side  string `json:"side"`  // buy|sell
	Price string `json:"price"` // pricePrecision integer
	Size  string `json:"size"`  // sizePrecision integer
	Tif   string `json:"tif"`   // gtc|ioc
}

type OrdersReq struct {
	Market  string   `json:"market"`
	Cancels []string `json:"cancels"`
	Orders  []Order  `json:"orders"`
}

type DepositReq struct {
	Token  string `json:"token"`
	Amount string `json:"amount"`
}

type TxResp struct {
	TxHash   string   `json:"txHash"`
	Status   string   `json:"status"`
	OrderIDs []string `json:"orderIds"`
}

type Balance struct {
	Token  string          `json:"token"`
	Free   decimal.Decimal `json:"free"`
	Locked decimal.Decimal `json:"locked"`
}

type Balances struct {
	AccountID string    `json:"accountId"`
	Balances  []Balance `json:"balances"`
}

// Error is the executor's {"error":"<code>"} body.
type Error struct {
	Status int
	Code   string `json:"error"`
}

func (e *Error) Error() string { return fmt.Sprintf("executor %d: %s", e.Status, e.Code) }

type Client struct {
	URL    string
	Secret string
	HTTP   *http.Client
}

func New(url, secret string) *Client {
	return &Client{URL: url, Secret: secret, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) Orders(ctx context.Context, r OrdersReq) (TxResp, error) {
	if r.Cancels == nil {
		r.Cancels = []string{}
	}
	if r.Orders == nil {
		r.Orders = []Order{}
	}
	var out TxResp
	return out, c.do(ctx, http.MethodPost, "/api/v1/bot/orders", r, &out)
}

func (c *Client) Deposit(ctx context.Context, r DepositReq) (TxResp, error) {
	var out TxResp
	return out, c.do(ctx, http.MethodPost, "/api/v1/bot/deposit", r, &out)
}

func (c *Client) Balances(ctx context.Context) (Balances, error) {
	var out Balances
	return out, c.do(ctx, http.MethodGet, "/api/v1/bot/balances", nil, &out)
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	body := bytes.NewReader(nil)
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.URL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	httpx.SetInternal(req, c.Secret, "seeder")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		e := &Error{Status: res.StatusCode}
		_ = json.NewDecoder(res.Body).Decode(e)
		return e
	}
	return json.NewDecoder(res.Body).Decode(out)
}
