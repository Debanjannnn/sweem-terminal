package policy

import (
	"fmt"
	"math/big"
)

// Both delegation caps, MaxNotionalPerSlice and MaxNotionalTotal, are
// denominated in RAW ATOMS OF THE TOKEN THE ORDER SPENDS: quote atoms for a
// buy, base atoms for a sell. That is the one unit every order type can be
// reduced to without a price lookup, and it is what Kuru actually reserves --
// a bid reserves quote notional, an ask reserves base quantity.
//
//	taker (market): the request's own size already is that number
//	maker buy:      QuoteNotional(price, quantity)
//	maker sell:     RawBaseAtoms(quantity)
//
// Nothing here is ever a display value and nothing here is ever scaled by 1e18.

// RawBaseAtoms converts book units to what the token contract moves. Mirrors
// executor kuru::raw_base_atoms.
func RawBaseAtoms(m Market, quantity *big.Int) (*big.Int, error) {
	mult, err := scale(m.ID, "base_size_multiplier", m.BaseSizeMultiplier)
	if err != nil {
		return nil, err
	}
	return new(big.Int).Mul(quantity, mult), nil
}

// QuoteNotional is the quote-atom value of a maker order. Mirrors executor
// kuru::quote_notional, including flooring twice: float math disagrees at the
// boundary and the venue floors.
func QuoteNotional(m Market, price, quantity *big.Int) (*big.Int, error) {
	sizePrec, err := scale(m.ID, "size_precision", m.SizePrecision)
	if err != nil {
		return nil, err
	}
	pricePrec, err := scale(m.ID, "price_precision", m.PricePrecision)
	if err != nil {
		return nil, err
	}
	base := new(big.Int).Quo(new(big.Int).Mul(price, quantity), sizePrec)
	base.Mul(base, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(m.QuoteDecimals)), nil))
	return base.Quo(base, pricePrec), nil
}

func scale(marketID, name, v string) (*big.Int, error) {
	n, err := ParseAmount(v)
	if err != nil {
		return nil, fmt.Errorf("market %s has an unreadable %s: %w", marketID, name, err)
	}
	if n.Sign() == 0 {
		return nil, fmt.Errorf("market %s has a zero %s, so no amount can be scaled by it", marketID, name)
	}
	return n, nil
}
