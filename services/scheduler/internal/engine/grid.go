package engine

import (
	"fmt"
	"math"
	"strconv"

	"github.com/sweem-terminal/sweem-terminal/services/scheduler/internal/store"
)

// ponytail: synthetic grid, market orders; swap to resting limit orders if the kuru orderbook write ABI lands
//
// a real grid rests N limit orders at N price levels, which needs OrderBook
// writes whose selectors are currently unrecoverable. so the levels live as
// rows, the price is polled, and a crossing fires a market swap.

// gridDue reports levels crossed since the previous poll and levels that have
// travelled far enough to re-arm. a fired level stays fired until price leaves
// its band, so oscillation around a level cannot re-fire it.
func gridDue(s store.Schedule, prev, cur float64, bandBps int, fired map[int]bool) (fire []due, rearm []int, err error) {
	if cur <= 0 {
		return nil, nil, fmt.Errorf("grid schedule %s has no usable price, got %v", s.ID, cur)
	}
	if len(s.Levels) == 0 {
		return nil, nil, fmt.Errorf("grid schedule %s carries no levels", s.ID)
	}
	sizes, err := amount(s.Total, len(s.Levels))
	if err != nil {
		return nil, nil, fmt.Errorf("grid schedule %s: %w", s.ID, err)
	}
	side := "buy"
	if cur > prev {
		side = "sell"
	}
	for i, raw := range s.Levels {
		lv, convErr := strconv.ParseFloat(raw, 64)
		if convErr != nil || lv <= 0 {
			return nil, nil, fmt.Errorf("grid schedule %s level %d is %q, which is not a positive price", s.ID, i, raw)
		}
		switch {
		case fired[i]:
			if math.Abs(cur-lv) > lv*float64(bandBps)/10_000 {
				rearm = append(rearm, i)
			}
		case prev > 0 && crossed(prev, cur, lv):
			fire = append(fire, due{Index: i, Amount: sizes[i].String(), Side: side})
		}
	}
	return fire, rearm, nil
}

func crossed(prev, cur, level float64) bool {
	return (prev < level && cur >= level) || (prev > level && cur <= level)
}
