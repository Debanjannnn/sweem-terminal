package market

import (
	"math"
	"time"
)

// ewma is a time-weighted EWMA of the market price (mid, else last trade) and its mean absolute deviation.
type ewma struct {
	ref, vol float64
	at       time.Time
}

func (e *ewma) observe(x float64, now time.Time, tau time.Duration) {
	if x <= 0 {
		return
	}
	if e.ref == 0 {
		e.ref, e.at = x, now
		return
	}
	a := 1 - math.Exp(-now.Sub(e.at).Seconds()/tau.Seconds())
	e.vol += a * (math.Abs(x/e.ref-1) - e.vol)
	e.ref += a * (x - e.ref)
	e.at = now
}

func deviated(mid, ref float64, bps int) bool {
	return mid > 0 && ref > 0 && math.Abs(mid-ref)/ref > float64(bps)/1e4
}
