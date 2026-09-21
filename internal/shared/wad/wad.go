// Package wad is 18-decimal fixed point mirroring prb-math UD60x18, so an
// off-chain number and its on-chain counterpart agree bit for bit. All
// division truncates toward zero, like Solidity's `/`.
package wad

import (
	"fmt"
	"math/big"
	"strings"
)

const Decimals = 18

// do not mutate
var (
	WAD  = new(big.Int).Exp(big.NewInt(10), big.NewInt(Decimals), nil)
	zero = new(big.Int)
)

func MulWad(x, y *big.Int) *big.Int {
	return new(big.Int).Quo(new(big.Int).Mul(x, y), WAD)
}

// panics on a zero divisor, matching the on-chain revert
func DivWad(x, y *big.Int) *big.Int {
	return new(big.Int).Quo(new(big.Int).Mul(x, WAD), y)
}

// sqrt(x * WAD), floored. big.Int.Sqrt is Newton's method.
func SqrtWad(x *big.Int) *big.Int {
	if x.Sign() < 0 {
		panic("wad: sqrt of negative")
	}
	return new(big.Int).Sqrt(new(big.Int).Mul(x, WAD))
}

// Scale converts a scaled integer between decimal precisions, e.g. 6dp USDC to
// 18dp WAD. Scaling down truncates.
func Scale(x *big.Int, from, to int) *big.Int {
	switch {
	case from == to:
		return new(big.Int).Set(x)
	case from < to:
		return new(big.Int).Mul(x, pow10(to-from))
	default:
		return new(big.Int).Quo(x, pow10(from-to))
	}
}

func pow10(n int) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil)
}

// FromDecimalString parses a human decimal ("1.5", "-0.25") into a WAD.
// More than 18 fraction digits is an error rather than a silent truncation.
func FromDecimalString(s string) (*big.Int, error) {
	return parseDecimal(s, Decimals)
}

func parseDecimal(s string, decimals int) (*big.Int, error) {
	raw := strings.TrimSpace(s)
	if raw == "" {
		return nil, fmt.Errorf("empty string is not a decimal number")
	}
	neg := false
	switch raw[0] {
	case '-':
		neg, raw = true, raw[1:]
	case '+':
		raw = raw[1:]
	}
	intPart, fracPart, _ := strings.Cut(raw, ".")
	if intPart == "" && fracPart == "" {
		return nil, fmt.Errorf("%q has no digits either side of the decimal point", s)
	}
	if len(fracPart) > decimals {
		return nil, fmt.Errorf("%q has %d fraction digits but only %d are representable", s, len(fracPart), decimals)
	}
	digits := intPart + fracPart + strings.Repeat("0", decimals-len(fracPart))
	out, ok := new(big.Int).SetString(digits, 10)
	if !ok {
		return nil, fmt.Errorf("%q is not a decimal number", s)
	}
	if neg {
		out.Neg(out)
	}
	return out, nil
}

// ToDecimalString renders a scaled integer as a decimal string, trimming
// trailing fraction zeros. Round trips with FromDecimalString.
func ToDecimalString(x *big.Int, decimals int) string {
	if decimals <= 0 {
		return x.String()
	}
	neg := x.Sign() < 0
	abs := new(big.Int).Abs(x)
	q, r := new(big.Int).QuoRem(abs, pow10(decimals), new(big.Int))
	out := q.String()
	if r.Cmp(zero) != 0 {
		frac := strings.TrimRight(fmt.Sprintf("%0*s", decimals, r.String()), "0")
		out += "." + frac
	}
	if neg {
		out = "-" + out
	}
	return out
}
