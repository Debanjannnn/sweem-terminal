package engine

import (
	"fmt"
	"math/big"
	"time"

	"github.com/sweem-terminal/sweem-terminal/services/scheduler/internal/store"
)

type due struct {
	Index  int
	DueAt  time.Time
	Amount string
	Side   string
	// bumped each time a grid level re-arms; twap/dca never re-arm so stay 0
	Generation int
}

func twapDueAt(start time.Time, window time.Duration, n, i int) time.Time {
	return start.Add(time.Duration(int64(window) * int64(i) / int64(n)))
}

// splitEven distributes total over n parts by largest remainder, so the parts
// sum to exactly total: with equal weights the remainder goes to the first
// total%n parts. any other rounding leaks dust on every schedule.
func splitEven(total *big.Int, n int) []*big.Int {
	q, r := new(big.Int).QuoRem(total, big.NewInt(int64(n)), new(big.Int))
	out := make([]*big.Int, n)
	for i := range out {
		out[i] = new(big.Int).Set(q)
		if int64(i) < r.Int64() {
			out[i].Add(out[i], big.NewInt(1))
		}
	}
	return out
}

func amount(total string, n int) ([]*big.Int, error) {
	v, ok := new(big.Int).SetString(total, 10)
	if !ok || v.Sign() <= 0 {
		return nil, fmt.Errorf("total %q is not a positive base-unit amount", total)
	}
	if n <= 0 {
		return nil, fmt.Errorf("cannot split %s over %d parts", total, n)
	}
	return splitEven(v, n), nil
}

func twapDue(s store.Schedule, now time.Time, max int) ([]due, error) {
	if s.SliceCount <= 0 || s.WindowSeconds <= 0 {
		return nil, fmt.Errorf("twap schedule %s needs a positive slice_count and window_seconds, got %d and %d",
			s.ID, s.SliceCount, s.WindowSeconds)
	}
	parts, err := amount(s.Total, s.SliceCount)
	if err != nil {
		return nil, fmt.Errorf("twap schedule %s: %w", s.ID, err)
	}
	window := time.Duration(s.WindowSeconds) * time.Second
	var out []due
	for i := s.NextIndex; i < s.SliceCount && len(out) < max; i++ {
		at := twapDueAt(s.CreatedAt, window, s.SliceCount, i)
		if at.After(now) {
			break
		}
		out = append(out, due{Index: i, DueAt: at, Amount: parts[i].String(), Side: s.Side})
	}
	return out, nil
}
