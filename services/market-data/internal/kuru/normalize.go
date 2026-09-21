package kuru

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/wad"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/store"
)

func normalizeTestnet(m testnetMarket) (store.Market, error) {
	priceDecimals, err := precisionDecimals("pricePrecision", m.PricePrecision)
	if err != nil {
		return store.Market{}, err
	}
	sizeDecimals, err := precisionDecimals("sizePrecision", m.SizePrecision)
	if err != nil {
		return store.Market{}, err
	}
	if m.MarketAddress == "" {
		return store.Market{}, fmt.Errorf("market has no marketAddress")
	}
	return store.Market{
		ID:            strings.ToLower(m.MarketAddress),
		Venue:         store.VenueKuru,
		Kind:          "spot",
		Symbol:        m.Symbol,
		Base:          m.BaseToken.Symbol,
		Quote:         m.QuoteToken.Symbol,
		BaseDecimals:  m.BaseToken.Decimals,
		QuoteDecimals: m.QuoteToken.Decimals,
		PriceDecimals: priceDecimals,
		SizeDecimals:  sizeDecimals,
		TickSize:      m.TickSize,
		// already 1e18, and a quote notional rather than a base size
		MinSize:            orZero(m.MinQuoteNotionalX18),
		MinIsQuoteNotional: true,
		// canonical fee unit is micros (1e-6): pps, bps and micros all lift losslessly
		TakerFeeMicros: m.TakerFeePps * 10,
		MakerFeeMicros: m.MakerFeePps * 10,
		Status:         m.Status,
		Address:        strings.ToLower(m.MarketAddress),
	}, nil
}

func normalizeMainnet(m mainnetMarket) (store.Market, error) {
	priceDecimals, err := precisionDecimals("priceprecision", m.PricePrecision)
	if err != nil {
		return store.Market{}, err
	}
	sizeDecimals, err := precisionDecimals("sizeprecision", m.SizePrecision)
	if err != nil {
		return store.Market{}, err
	}
	if m.Market == "" {
		return store.Market{}, fmt.Errorf("market has no market address")
	}
	base, quote := m.BaseToken.Ticker, m.QuoteToken.Ticker
	minSize := new(big.Int)
	if n, ok := new(big.Int).SetString(orZero(m.MinSize), 10); ok {
		minSize = wad.Scale(n, sizeDecimals, wad.Decimals)
	}
	return store.Market{
		ID:            strings.ToLower(m.Market),
		Venue:         store.VenueKuru,
		Kind:          "spot",
		Symbol:        base + quote,
		Base:          base,
		Quote:         quote,
		BaseDecimals:  atoi(m.BaseToken.Decimal),
		QuoteDecimals: atoi(m.QuoteToken.Decimal),
		PriceDecimals: priceDecimals,
		SizeDecimals:  sizeDecimals,
		TickSize:      m.TickSize,
		// mainnet floors a base size, testnet floors a quote notional
		MinSize:            minSize.String(),
		MinIsQuoteNotional: false,
		TakerFeeMicros:     atoi(m.TakerFeeBps) * 100,
		MakerFeeMicros:     atoi(m.MakerFeeBps) * 100,
		Status:             "active",
		Address:            strings.ToLower(m.Market),
	}, nil
}

// precisionDecimals turns a per-market scaled-integer precision into a decimal
// exponent. These are NOT token decimals, and a wrong one is a wrong order size.
func precisionDecimals(field, raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	n, ok := new(big.Int).SetString(raw, 10)
	if !ok || n.Sign() <= 0 {
		return 0, fmt.Errorf("%s %q is not a positive integer", field, raw)
	}
	s := n.String()
	if s[0] != '1' || strings.Trim(s[1:], "0") != "" {
		return 0, fmt.Errorf("%s %q is not a power of ten", field, raw)
	}
	return len(s) - 1, nil
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func orZero(s string) string {
	if strings.TrimSpace(s) == "" {
		return "0"
	}
	return s
}
