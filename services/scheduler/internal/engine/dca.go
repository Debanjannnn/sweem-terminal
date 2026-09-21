package engine

import (
	"fmt"
	"time"

	"github.com/sweem-terminal/sweem-terminal/services/scheduler/internal/store"
)

func dcaDueAt(start time.Time, cadence time.Duration, i int) time.Time {
	return start.Add(cadence * time.Duration(i))
}

// same slice machinery as twap: only the due-time function differs. slice_count
// 0 means indefinite, and then total is the amount of one leg.
func dcaDue(s store.Schedule, now time.Time, max int) ([]due, error) {
	if s.CadenceSeconds <= 0 {
		return nil, fmt.Errorf("dca schedule %s needs a positive cadence_seconds, got %d", s.ID, s.CadenceSeconds)
	}
	parts, err := amount(s.Total, maxInt(s.SliceCount, 1))
	if err != nil {
		return nil, fmt.Errorf("dca schedule %s: %w", s.ID, err)
	}
	cadence := time.Duration(s.CadenceSeconds) * time.Second
	var out []due
	for i := s.NextIndex; len(out) < max; i++ {
		if s.SliceCount > 0 && i >= s.SliceCount {
			break
		}
		at := dcaDueAt(s.CreatedAt, cadence, i)
		if at.After(now) {
			break
		}
		leg := parts[0]
		if s.SliceCount > 0 {
			leg = parts[i]
		}
		out = append(out, due{Index: i, DueAt: at, Amount: leg.String(), Side: s.Side})
	}
	return out, nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
