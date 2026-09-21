package finality

import (
	"fmt"
	"strings"
)

// Monad has three block tiers. Seen is speculative: an identical retry against
// `latest` can return a different answer, so never treat it as truth.
type Tier int

const (
	Seen  Tier = iota // proposed, may still be reorged away
	Safe              // voted by a supermajority
	Final             // irreversible
)

// Monad executes behind consensus: the state root in block N reflects block N-3.
const StateRootLag = 3

// blocks behind head we consider irreversible when the node gives us no finalized tag
const FinalityDepth = 5

func (t Tier) BlockTag() string {
	switch t {
	case Safe:
		return "safe"
	case Final:
		return "finalized"
	default:
		return "latest"
	}
}

func (t Tier) String() string { return t.BlockTag() }

func Parse(s string) (Tier, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "latest", "seen", "proposed":
		return Seen, nil
	case "safe", "voted":
		return Safe, nil
	case "finalized", "final":
		return Final, nil
	}
	return 0, fmt.Errorf("finality tier %q is not one of latest, safe or finalized", s)
}

// Settled reports whether the block we observed at has finalized. While it has
// not, we are inside the D+2 window where the chain can still hand back a
// different answer: never diff, reconcile or liquidate off that state.
func Settled(observedAt, finalizedBlock uint64) bool {
	return finalizedBlock >= observedAt
}
