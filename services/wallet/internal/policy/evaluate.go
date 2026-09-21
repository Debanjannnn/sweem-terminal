// Package policy is the real delegation guard. The Privy policy is a backstop
// only: its aggregations update after signing, and it fails open when the
// calldata ABI does not match. Everything here rejects whole, never clamps.
package policy

import (
	"fmt"
	"math/big"
	"strings"
	"time"
)

// Delegation is the delegations row and the guard input; one type so the two
// cannot drift.
type Delegation struct {
	UserID              string     `json:"user_id"`
	SignerID            string     `json:"signer_id"`
	PolicyID            string     `json:"policy_id,omitempty"`
	Venues              []string   `json:"venues"`
	Markets             []string   `json:"markets"`
	MaxNotionalPerSlice string     `json:"max_notional_per_slice"`
	MaxNotionalTotal    string     `json:"max_notional_total"`
	ExpiresAt           time.Time  `json:"expires_at"`
	RevokedAt           *time.Time `json:"revoked_at,omitempty"`
}

type Intent struct {
	Venue    string
	MarketID string
	// base units, never a float
	Notional *big.Int
}

type Rejection struct {
	Code   string `json:"code"`
	Reason string `json:"reason"`
}

func (r *Rejection) Error() string { return r.Reason }

// EvaluateIntent checks revoked, expired, venue, market, per-slice cap then
// total cap, in that order, and returns the first failure.
func EvaluateIntent(d Delegation, in Intent, spentTotal *big.Int, now time.Time) *Rejection {
	if d.RevokedAt != nil && !d.RevokedAt.After(now) {
		return &Rejection{"delegation_revoked", fmt.Sprintf(
			"This delegation was revoked at %s, so nothing can be placed under it until you delegate again.",
			d.RevokedAt.UTC().Format(time.RFC3339))}
	}
	if !d.ExpiresAt.After(now) {
		return &Rejection{"delegation_expired", fmt.Sprintf(
			"This delegation expired at %s, so nothing can be placed under it until you delegate again.",
			d.ExpiresAt.UTC().Format(time.RFC3339))}
	}
	if !contains(d.Venues, in.Venue) {
		return &Rejection{"venue_not_allowed", fmt.Sprintf(
			"Venue %q is not one of the venues you delegated (%s), so the order was refused.",
			in.Venue, list(d.Venues))}
	}
	if !contains(d.Markets, in.MarketID) {
		return &Rejection{"market_not_allowed", fmt.Sprintf(
			"Market %q is not one of the markets you delegated (%s), so the order was refused.",
			in.MarketID, list(d.Markets))}
	}

	perSlice, err := ParseAmount(d.MaxNotionalPerSlice)
	if err != nil {
		return &Rejection{"delegation_unreadable", fmt.Sprintf(
			"The per-slice cap on this delegation is unreadable (%s), so no order can be checked against it.", err)}
	}
	if in.Notional.Cmp(perSlice) > 0 {
		return &Rejection{"over_slice_limit", fmt.Sprintf(
			"This order is %s but you delegated at most %s per slice, so it was refused rather than shrunk.",
			in.Notional, perSlice)}
	}

	total, err := ParseAmount(d.MaxNotionalTotal)
	if err != nil {
		return &Rejection{"delegation_unreadable", fmt.Sprintf(
			"The total cap on this delegation is unreadable (%s), so no order can be checked against it.", err)}
	}
	if spentTotal == nil {
		spentTotal = new(big.Int)
	}
	after := new(big.Int).Add(spentTotal, in.Notional)
	if after.Cmp(total) > 0 {
		return &Rejection{"over_total_limit", fmt.Sprintf(
			"This order would take the total spent under this delegation to %s but you delegated at most %s, so it was refused rather than shrunk.",
			after, total)}
	}
	return nil
}

// Live reports whether the delegation can authorize anything at all.
func Live(d Delegation, now time.Time) bool {
	return (d.RevokedAt == nil || d.RevokedAt.After(now)) && d.ExpiresAt.After(now)
}

// ParseAmount reads a base-unit integer string. Floats are refused outright.
func ParseAmount(s string) (*big.Int, error) {
	v, ok := new(big.Int).SetString(strings.TrimSpace(s), 10)
	if !ok {
		return nil, fmt.Errorf("%q is not an integer amount in base units", s)
	}
	if v.Sign() < 0 {
		return nil, fmt.Errorf("%q is negative, and an amount cannot be", s)
	}
	return v, nil
}

func contains(allowed []string, want string) bool {
	for _, a := range allowed {
		if strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(want)) {
			return true
		}
	}
	return false
}

func list(vs []string) string {
	if len(vs) == 0 {
		return "none"
	}
	return strings.Join(vs, ", ")
}
