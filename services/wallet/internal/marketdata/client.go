// Package marketdata reads marks from the market-data service. This service
// never computes a mark itself; it only asks for one and reports when it
// could not get it.
package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	base string
	http *http.Client
}

func New(base string) *Client {
	return &Client{base: strings.TrimRight(base, "/"), http: &http.Client{Timeout: 5 * time.Second}}
}

type Mark struct {
	MarketID string `json:"market_id"`
	Price    string `json:"price"`
	Ts       int64  `json:"ts"`
}

// Marks returns only the markets market-data knows; a missing entry is a
// missing mark, never a zero.
func (c *Client) Marks(ctx context.Context, marketIDs []string) (map[string]Mark, error) {
	out := map[string]Mark{}
	if len(marketIDs) == 0 {
		return out, nil
	}
	path := "/v1/marks?" + url.Values{"markets": {strings.Join(marketIDs, ",")}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("market-data at %s is unreachable: %w", c.base, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("market-data answered HTTP %d for %s", resp.StatusCode, path)
	}
	var env struct {
		Data struct {
			Marks []Mark `json:"marks"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil, fmt.Errorf("the market-data answer was not valid JSON: %w", err)
	}
	for _, m := range env.Data.Marks {
		if m.Price != "" {
			out[m.MarketID] = m
		}
	}
	return out, nil
}

// Markets returns the tradeable market ids. market-data owns the registry;
// wallet only asks, so there is no second copy to drift.
func (c *Client) Markets(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/v1/markets", nil)
	if err != nil {
		return nil, err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("market-data is unreachable, so the market list could not be read: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("market-data answered %d for the market list", res.StatusCode)
	}
	var body struct {
		Data struct {
			Markets []struct {
				ID     string `json:"id"`
				Venue  string `json:"venue"`
				Symbol string `json:"symbol"`
			} `json:"markets"`
		} `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("market-data returned a market list this service could not parse: %w", err)
	}
	ids := make([]string, 0, len(body.Data.Markets)*2)
	for _, m := range body.Data.Markets {
		ids = append(ids, m.ID)
		if m.Venue != "" && m.Symbol != "" {
			ids = append(ids, strings.ToLower(m.Venue+":"+m.Symbol))
		}
	}
	return ids, nil
}
