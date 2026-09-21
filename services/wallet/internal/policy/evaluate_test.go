package policy

import (
	"math/big"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func base() Delegation {
	return Delegation{
		UserID:              "did:privy:u1",
		SignerID:            "signer-1",
		Venues:              []string{"kuru"},
		Markets:             []string{"MON-USDC"},
		MaxNotionalPerSlice: "100",
		MaxNotionalTotal:    "500",
		ExpiresAt:           now.Add(time.Hour),
	}
}

func intent(n int64) Intent {
	return Intent{Venue: "kuru", MarketID: "MON-USDC", Notional: big.NewInt(n)}
}

// The order is the guard: a delegation that is revoked AND over every cap must
// say "revoked", because that is the fact the user has to act on.
func TestEvaluateIntentOrder(t *testing.T) {
	revoked := now.Add(-time.Minute)

	tests := []struct {
		name   string
		mutate func(*Delegation)
		in     Intent
		spent  *big.Int
		want   string
	}{
		{
			name: "revoked beats every other failure",
			mutate: func(d *Delegation) {
				d.RevokedAt = &revoked
				d.ExpiresAt = now.Add(-time.Hour)
				d.Venues = nil
				d.Markets = nil
			},
			in:    intent(1_000_000),
			spent: big.NewInt(1_000_000),
			want:  "delegation_revoked",
		},
		{
			name:   "expired beats venue, market and caps",
			mutate: func(d *Delegation) { d.ExpiresAt = now.Add(-time.Second); d.Venues = nil; d.Markets = nil },
			in:     intent(1_000_000),
			spent:  big.NewInt(1_000_000),
			want:   "delegation_expired",
		},
		{
			name:   "venue beats market and caps",
			mutate: func(d *Delegation) { d.Venues = []string{"perpl"}; d.Markets = nil },
			in:     intent(1_000_000),
			want:   "venue_not_allowed",
		},
		{
			name:   "market beats caps",
			mutate: func(d *Delegation) { d.Markets = []string{"WETH-USDC"} },
			in:     intent(1_000_000),
			want:   "market_not_allowed",
		},
		{
			name:  "per-slice cap beats total cap",
			in:    intent(101),
			spent: big.NewInt(499),
			want:  "over_slice_limit",
		},
		{
			name:  "total cap is the last gate",
			in:    intent(100),
			spent: big.NewInt(401),
			want:  "over_total_limit",
		},
		{
			name:   "revocation in the future does not reject",
			mutate: func(d *Delegation) { at := now.Add(time.Minute); d.RevokedAt = &at },
			in:     intent(100),
			spent:  big.NewInt(400),
		},
		{
			name:  "exactly at both caps passes",
			in:    intent(100),
			spent: big.NewInt(400),
		},
		{
			name:   "venue and market match case-insensitively",
			mutate: func(d *Delegation) { d.Venues = []string{"KURU"}; d.Markets = []string{"mon-usdc"} },
			in:     intent(1),
		},
		{
			name:   "an unreadable cap rejects instead of passing",
			mutate: func(d *Delegation) { d.MaxNotionalPerSlice = "1.5" },
			in:     intent(1),
			want:   "delegation_unreadable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := base()
			if tt.mutate != nil {
				tt.mutate(&d)
			}
			got := EvaluateIntent(d, tt.in, tt.spent, now)
			switch {
			case tt.want == "" && got != nil:
				t.Fatalf("rejected with %s: %s, want accepted", got.Code, got.Reason)
			case tt.want == "":
				return
			case got == nil:
				t.Fatalf("accepted, want rejection %s", tt.want)
			case got.Code != tt.want:
				t.Fatalf("code = %s, want %s (%s)", got.Code, tt.want, got.Reason)
			}
			if !strings.HasSuffix(strings.TrimSpace(got.Reason), ".") {
				t.Errorf("reason %q is not a full sentence", got.Reason)
			}
		})
	}
}

// Rejection is whole: no accepted result may carry a shrunk size.
func TestEvaluateIntentNeverClamps(t *testing.T) {
	in := intent(101)
	if got := EvaluateIntent(base(), in, nil, now); got == nil {
		t.Fatal("an over-cap order was accepted")
	}
	if in.Notional.Cmp(big.NewInt(101)) != 0 {
		t.Fatalf("notional was mutated to %s", in.Notional)
	}
}

func TestParseAmount(t *testing.T) {
	tests := []struct {
		in   string
		want string
		bad  bool
	}{
		{in: "0", want: "0"},
		{in: " 1000000000000000000 ", want: "1000000000000000000"},
		{in: "1.5", bad: true},
		{in: "-1", bad: true},
		{in: "", bad: true},
		{in: "1e18", bad: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseAmount(tt.in)
			if tt.bad {
				if err == nil {
					t.Fatalf("parsed %q as %s, want an error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseAmount(%q): %v", tt.in, err)
			}
			if got.String() != tt.want {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestKnownMarket(t *testing.T) {
	ms := []Market{{ID: "MON-USDC"}, {ID: "WETH-USDC"}}
	for _, id := range []string{"MON-USDC", "mon-usdc", "MON/USDC", "MONUSDC"} {
		if !KnownMarket(ms, id) {
			t.Errorf("KnownMarket(%q) = false, want true", id)
		}
	}
	if KnownMarket(ms, "DOGE-USDC") {
		t.Error("an unlisted market was accepted")
	}
}
