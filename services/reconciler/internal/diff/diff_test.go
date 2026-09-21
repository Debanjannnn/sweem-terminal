package diff

import (
	"math/big"
	"strings"
	"testing"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/finality"
)

func TestExecutionTier(t *testing.T) {
	cases := []struct {
		name      string
		receipt   *Receipt
		safe, fin uint64
		want      finality.Tier
	}{
		{"null receipt right after broadcast is seen, not a failure", nil, 100, 90, finality.Seen},
		{"receipt in a proposed block is still seen", &Receipt{BlockNumber: 105}, 100, 90, finality.Seen},
		{"receipt at the safe head is safe", &Receipt{BlockNumber: 100}, 100, 90, finality.Safe},
		{"receipt below the safe head is safe", &Receipt{BlockNumber: 95}, 100, 90, finality.Safe},
		{"receipt at the finalized head is final", &Receipt{BlockNumber: 90}, 100, 90, finality.Final},
		{"receipt deeper than finalized is final", &Receipt{BlockNumber: 80}, 100, 90, finality.Final},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ExecutionTier(c.receipt, c.safe, c.fin); got != c.want {
				t.Fatalf("got %s want %s", got, c.want)
			}
		})
	}
}

func TestExecutionTierWalksSeenSafeFinal(t *testing.T) {
	rc := &Receipt{BlockNumber: 100, Success: true}
	want := []finality.Tier{finality.Seen, finality.Safe, finality.Final}
	got := []finality.Tier{
		ExecutionTier(rc, 99, 90),   // not voted yet
		ExecutionTier(rc, 100, 90),  // voted
		ExecutionTier(rc, 100, 100), // finalized
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("step %d: got %s want %s", i, got[i], want[i])
		}
	}
}

func n(s string) *big.Int { return Amount(s) }

func TestPositions(t *testing.T) {
	local := Position{Size: n("10"), Collateral: n("5")}
	same := Position{Size: n("10"), Collateral: n("5")}
	drifted := Position{Size: n("11"), Collateral: n("5")}

	cases := []struct {
		name                  string
		local, indexed, chain Position
		observedAt, finalized uint64
		want                  int
	}{
		{"refuses to diff inside the D+2 window", local, same, drifted, 100, 99, 0},
		{"refuses to diff when nothing has finalized", local, same, drifted, 1, 0, 0},
		{"agreement at a finalized block yields nothing", local, same, same, 90, 100, 0},
		{"db disagreeing with chain is one divergence per field", local, same, drifted, 90, 100, 2},
		{"indexer and db both disagreeing is four", local, local, Position{Size: n("11"), Collateral: n("6")}, 90, 100, 4},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Positions("0xmarket", "0xowner", c.local, c.indexed, c.chain, c.observedAt, c.finalized)
			if len(got) != c.want {
				t.Fatalf("got %d divergences want %d: %+v", len(got), c.want, got)
			}
			for _, d := range got {
				if !strings.Contains(d.Reason, "0xowner") || len(strings.Fields(d.Reason)) < 6 {
					t.Fatalf("reason is not a sentence: %q", d.Reason)
				}
			}
		})
	}
}

func TestVault(t *testing.T) {
	cases := []struct {
		name            string
		onchain, flows  *big.Int
		tol             int64
		observed, final uint64
		wantDivergence  bool
	}{
		{"inside D+2 nothing is compared", n("1000"), n("1"), 50, 100, 99, false},
		{"drift inside tolerance is silence", n("10000"), n("9990"), 50, 90, 100, false},
		{"drift beyond tolerance is a divergence", n("10000"), n("9000"), 50, 90, 100, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Vault(c.onchain, c.flows, c.tol, c.observed, c.final)
			if (got != nil) != c.wantDivergence {
				t.Fatalf("got %+v want divergence=%v", got, c.wantDivergence)
			}
		})
	}
}
