package book

import "testing"

func lv(p, s string) Level { return Level{Price: p, Size: s} }

func TestApply(t *testing.T) {
	cases := []struct {
		name             string
		start            Book
		bids, asks       []Level
		wantBid, wantAsk []Level
	}{
		{
			name:    "insert keeps bids descending and asks ascending",
			bids:    []Level{lv("100", "1"), lv("102", "2"), lv("101", "3")},
			asks:    []Level{lv("105", "1"), lv("103", "2")},
			wantBid: []Level{lv("102", "2"), lv("101", "3"), lv("100", "1")},
			wantAsk: []Level{lv("103", "2"), lv("105", "1")},
		},
		{
			name:    "zero size removes the level",
			start:   Book{Bids: []Level{lv("102", "2"), lv("101", "3")}},
			bids:    []Level{lv("102", "0")},
			wantBid: []Level{lv("101", "3")},
			wantAsk: []Level{},
		},
		{
			name:    "same price replaces rather than duplicates",
			start:   Book{Bids: []Level{lv("102", "2")}},
			bids:    []Level{lv("102", "9")},
			wantBid: []Level{lv("102", "9")},
			wantAsk: []Level{},
		},
		{
			name:    "prices compare numerically, not lexically",
			bids:    []Level{lv("9", "1"), lv("10", "1")},
			wantBid: []Level{lv("10", "1"), lv("9", "1")},
			wantAsk: []Level{},
		},
		{
			name:    "wad-sized prices do not overflow",
			bids:    []Level{lv("100000000000000000000000", "1"), lv("99999999999999999999999", "1")},
			wantBid: []Level{lv("100000000000000000000000", "1"), lv("99999999999999999999999", "1")},
			wantAsk: []Level{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := tc.start
			b.Apply(tc.bids, tc.asks)
			assertSide(t, "bids", b.Bids, tc.wantBid)
			assertSide(t, "asks", b.Asks, tc.wantAsk)
		})
	}
}

func assertSide(t *testing.T, name string, got, want []Level) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %v, want %v", name, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s[%d] = %v, want %v", name, i, got[i], want[i])
		}
	}
}

func TestMidAndTrim(t *testing.T) {
	b := Book{Bids: []Level{lv("100", "1"), lv("99", "1")}, Asks: []Level{lv("104", "1")}}
	if got := b.Mid(); got != "102" {
		t.Errorf("Mid = %q, want 102", got)
	}
	b.Trim(1)
	if len(b.Bids) != 1 || len(b.Asks) != 1 {
		t.Errorf("Trim left %d bids %d asks", len(b.Bids), len(b.Asks))
	}
	empty := Book{}
	if got := empty.Mid(); got != "" {
		t.Errorf("Mid of an empty book = %q, want empty", got)
	}
}
