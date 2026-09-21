// Package diff compares what we believed against what the chain finalized.
// Everything here is pure: no rpc, no db, no clock.
package diff

import (
	"fmt"
	"math/big"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/finality"
)

type Divergence struct {
	Kind            string
	MarketID        string
	Owner           string
	Expected        string
	Actual          string
	Reason          string
	ObservedAtBlock uint64
}

// ExecutionTier walks seen -> safe -> final. No receipt means mempool-only on
// Monad (eth_getTransactionByHash returns null), which is still `seen`.
func ExecutionTier(receipt *Receipt, safeBlock, finalizedBlock uint64) finality.Tier {
	if receipt == nil {
		return finality.Seen
	}
	if finality.Settled(receipt.BlockNumber, finalizedBlock) {
		return finality.Final
	}
	if receipt.BlockNumber <= safeBlock {
		return finality.Safe
	}
	return finality.Seen
}

type Receipt struct {
	BlockNumber uint64
	Success     bool
}

type Position struct {
	Size       *big.Int
	Collateral *big.Int
}

// Positions diffs our row, the indexer and an on-chain read of the same
// position. It corrects nothing: a mismatch is a row a human can read.
func Positions(marketID, owner string, local, indexed, onchain Position, observedAt, finalizedBlock uint64) []Divergence {
	// D+2 guard: inside the window an identical retry can return a different
	// answer, so a diff here would be noise, not a finding.
	if !finality.Settled(observedAt, finalizedBlock) {
		return nil
	}
	var out []Divergence
	add := func(field, src string, expected, actual *big.Int) {
		out = append(out, Divergence{
			Kind: "position", MarketID: marketID, Owner: owner,
			Expected: str(expected), Actual: str(actual),
			Reason: fmt.Sprintf("%s %s is %s in %s but the finalized chain read at block %d says %s",
				field, owner, str(expected), src, finalizedBlock, str(actual)),
			ObservedAtBlock: observedAt,
		})
	}
	if !eq(local.Size, onchain.Size) {
		add("size", "our database", local.Size, onchain.Size)
	}
	if !eq(local.Collateral, onchain.Collateral) {
		add("collateral", "our database", local.Collateral, onchain.Collateral)
	}
	if !eq(indexed.Size, onchain.Size) {
		add("size", "the indexer", indexed.Size, onchain.Size)
	}
	if !eq(indexed.Collateral, onchain.Collateral) {
		add("collateral", "the indexer", indexed.Collateral, onchain.Collateral)
	}
	return out
}

// Vault compares totalAssets() against assets reconstructed from indexed flows.
// toleranceBps absorbs fees and pnl that flows alone cannot explain.
func Vault(onchain, fromFlows *big.Int, toleranceBps int64, observedAt, finalizedBlock uint64) *Divergence {
	if !finality.Settled(observedAt, finalizedBlock) {
		return nil
	}
	if onchain == nil || fromFlows == nil {
		return nil
	}
	drift := new(big.Int).Abs(new(big.Int).Sub(onchain, fromFlows))
	allowed := new(big.Int).Quo(new(big.Int).Mul(new(big.Int).Abs(onchain), big.NewInt(toleranceBps)), big.NewInt(10_000))
	if drift.Cmp(allowed) <= 0 {
		return nil
	}
	return &Divergence{
		Kind: "vault", Expected: fromFlows.String(), Actual: onchain.String(),
		Reason: fmt.Sprintf("vault totalAssets at finalized block %d is %s but indexed deposits minus withdrawals reconstruct %s, a drift of %s beyond the %d bps allowance",
			finalizedBlock, onchain, fromFlows, drift, toleranceBps),
		ObservedAtBlock: observedAt,
	}
}

func eq(a, b *big.Int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Cmp(b) == 0
}

func str(v *big.Int) string {
	if v == nil {
		return "unknown"
	}
	return v.String()
}

// Amount parses a decimal integer string; "" and malformed input are zero.
func Amount(s string) *big.Int {
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return new(big.Int)
	}
	return v
}
