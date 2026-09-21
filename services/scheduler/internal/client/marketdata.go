package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

type MarketData struct {
	BaseURL string
	HTTP    *http.Client
}

func NewMarketData(baseURL string) *MarketData {
	return &MarketData{BaseURL: baseURL, HTTP: &http.Client{Timeout: 5 * time.Second}}
}

// Mid is only used to decide whether to fire, never to price a fill.
func (m *MarketData) Mid(ctx context.Context, marketID string) (float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.BaseURL+"/v1/book/"+marketID, nil)
	if err != nil {
		return 0, err
	}
	resp, err := m.HTTP.Do(req)
	if err != nil {
		return 0, fmt.Errorf("call market-data book %s: %w", marketID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("market-data book %s returned %d", marketID, resp.StatusCode)
	}
	var env struct {
		Data struct {
			Bids []struct{ Price string } `json:"bids"`
			Asks []struct{ Price string } `json:"asks"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return 0, fmt.Errorf("decode market-data book %s: %w", marketID, err)
	}
	var bid, ask float64
	if len(env.Data.Bids) > 0 {
		bid, _ = strconv.ParseFloat(env.Data.Bids[0].Price, 64)
	}
	if len(env.Data.Asks) > 0 {
		ask, _ = strconv.ParseFloat(env.Data.Asks[0].Price, 64)
	}
	switch {
	case bid > 0 && ask > 0:
		return (bid + ask) / 2, nil
	case bid > 0:
		return bid, nil
	case ask > 0:
		return ask, nil
	}
	return 0, fmt.Errorf("market-data book %s has no priced level on either side", marketID)
}
