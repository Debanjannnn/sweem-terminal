package quote

import (
	"testing"

	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/book"
)

const (
	one  = "1000000000000000000"
	half = "500000000000000000"
	two  = "2000000000000000000"
)

func wadMul(n int64) string {
	out := []byte{}
	s := []byte("000000000000000000")
	return string(append(append(out, []byte(itoa(n))...), s...))
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func testBook() book.Book {
	return book.Book{
		MarketID: "m", Venue: "kuru",
		Asks: []book.Level{{Price: wadMul(100), Size: one}, {Price: wadMul(110), Size: one}},
		Bids: []book.Level{{Price: wadMul(90), Size: one}, {Price: wadMul(80), Size: one}},
	}
}

func TestFromBook(t *testing.T) {
	cases := []struct {
		name                string
		side, size          string
		wantFilled, wantAvg string
		wantNotional        string
		wantImpact          int64
		wantPartial         bool
		wantErr             bool
		bookOverride        *book.Book
	}{
		{
			name: "buy inside the touch has no impact",
			side: "buy", size: half,
			wantFilled: half, wantAvg: wadMul(100), wantNotional: wadMul(50), wantImpact: 0,
		},
		{
			name: "buy through two levels is size weighted",
			side: "buy", size: two,
			wantFilled: two, wantAvg: wadMul(105), wantNotional: wadMul(210), wantImpact: 500,
		},
		{
			name: "sell impact is reported positive-adverse",
			side: "sell", size: two,
			wantFilled: two, wantAvg: wadMul(85), wantNotional: wadMul(170), wantImpact: 555,
		},
		{
			name: "long is an alias for buy",
			side: "long", size: half,
			wantFilled: half, wantAvg: wadMul(100), wantNotional: wadMul(50),
		},
		{
			name: "a thin book fills what it can and says so",
			side: "buy", size: wadMul(5),
			wantFilled: two, wantAvg: wadMul(105), wantNotional: wadMul(210), wantImpact: 500, wantPartial: true,
		},
		{
			name: "an empty book fills nothing",
			side: "buy", size: one,
			bookOverride: &book.Book{}, wantFilled: "0", wantAvg: "0", wantNotional: "0", wantPartial: true,
		},
		{name: "unknown side is rejected", side: "sideways", size: one, wantErr: true},
		{name: "zero size is rejected", side: "buy", size: "0", wantErr: true},
		{name: "negative size is rejected", side: "buy", size: "-1", wantErr: true},
		{name: "non numeric size is rejected", side: "buy", size: "1.5", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := testBook()
			if tc.bookOverride != nil {
				b = *tc.bookOverride
			}
			q, err := FromBook(b, tc.side, tc.size)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want an error, got %+v", q)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if q.Filled != tc.wantFilled {
				t.Errorf("filled = %s, want %s", q.Filled, tc.wantFilled)
			}
			if q.Notional != tc.wantNotional {
				t.Errorf("notional = %s, want %s", q.Notional, tc.wantNotional)
			}
			if q.AvgPrice != tc.wantAvg {
				t.Errorf("avgPrice = %s, want %s", q.AvgPrice, tc.wantAvg)
			}
			if q.ImpactBps != tc.wantImpact {
				t.Errorf("impactBps = %d, want %d", q.ImpactBps, tc.wantImpact)
			}
			if q.Partial != tc.wantPartial {
				t.Errorf("partial = %v, want %v", q.Partial, tc.wantPartial)
			}
		})
	}
}
