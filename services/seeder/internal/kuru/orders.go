// Package kuru reads the bot's open orders from the Kuru gateway (the executor response carries no order ids).
package kuru

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// id accepts a JSON string or number; docs show strings, unverified for every row.
type id string

func (i *id) UnmarshalJSON(b []byte) error {
	*i = id(strings.Trim(string(b), `"`))
	return nil
}

type Order struct {
	OrderID       id     `json:"orderId"`
	MarketAddress string `json:"marketAddress"`
}

type Client struct {
	URL  string
	HTTP *http.Client
}

func New(url string) *Client { return &Client{URL: url, HTTP: &http.Client{Timeout: 10 * time.Second}} }

// OpenOrderIDs returns our live order ids per lowercase market address. Proposed state so fresh placements show up fast.
func (c *Client) OpenOrderIDs(ctx context.Context, accountID string) (map[string][]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL+"/api/v1/users/"+accountID+"/orders?state=proposed", nil)
	if err != nil {
		return nil, err
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var body struct {
		Data  []Order `json:"data"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("kuru orders: %s: %w", res.Status, err)
	}
	if body.Error != nil || res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kuru orders: %s %+v", res.Status, body.Error)
	}
	out := map[string][]string{}
	for _, o := range body.Data {
		m := strings.ToLower(o.MarketAddress)
		out[m] = append(out[m], string(o.OrderID))
	}
	return out, nil
}
