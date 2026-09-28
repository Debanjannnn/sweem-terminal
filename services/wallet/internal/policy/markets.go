package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Venues the executor can route to. Mirrors executor/src/venues/mod.rs.
var Venues = []string{"kuru", "perpl"}

// Market is one entry of executor/kuru-markets.json, the single allowlist of
// callable addresses. The API validates delegations against it and sync-policy
// builds the Privy policy from it, so the two guards name the same markets.
type Market struct {
	ID            string `json:"id"`
	ChainID       int64  `json:"chain_id"`
	MarketAddress string `json:"market_address"`
	BaseToken     string `json:"base_token"`
	QuoteToken    string `json:"quote_token"`
	BaseDecimals  int    `json:"base_decimals"`
	QuoteDecimals int    `json:"quote_decimals"`
	// displayPrice = price / price_precision
	PricePrecision string `json:"price_precision"`
	// displayBase = quantity / size_precision
	SizePrecision string `json:"size_precision"`
	// book quantity -> raw base atoms; == 10^base_decimals / size_precision
	BaseSizeMultiplier string `json:"base_size_multiplier"`
	TickSize           string `json:"tick_size"`
	// raw quote atoms, not the x18 form the data api reports
	MinQuoteNotional string `json:"min_quote_notional"`
	MaxQuoteNotional string `json:"max_quote_notional"`
}

func LoadMarkets(path string) ([]Market, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read market allowlist %s: %w", path, err)
	}
	var out []Market
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("parse market allowlist %s: %w", path, err)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("market allowlist %s is empty, so nothing could ever be traded", path)
	}
	return out, nil
}

func MarketIDs(ms []Market) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.ID
	}
	return out
}

// KnownMarket matches the executor's id normalization: "mon-usdc", "MON/USDC"
// and "MONUSDC" are the same committed entry.
func KnownMarket(ms []Market, id string) bool {
	want := normID(id)
	for _, m := range ms {
		if normID(m.ID) == want {
			return true
		}
	}
	return false
}

func KnownVenue(v string) bool { return contains(Venues, v) }

func normID(id string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(id) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}
