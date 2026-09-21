package candle

import (
	"testing"
)

func TestOpenTimeBoundaries(t *testing.T) {
	cases := []struct {
		ms   int64
		res  int
		want int64
	}{
		{0, 60, 0},
		{59_999, 60, 0},
		{60_000, 60, 60},
		{60_001, 60, 60},
		{119_999, 60, 60},
		{1_700_000_123_456, 3600, 1_699_999_200},
		{1_700_000_123_456, 86400, 1_699_920_000},
	}
	for _, tc := range cases {
		if got := OpenTime(tc.ms, tc.res); got != tc.want {
			t.Errorf("OpenTime(%d, %d) = %d, want %d", tc.ms, tc.res, got, tc.want)
		}
	}
}

func tick(ms int64, price, size string) Tick {
	return Tick{MarketID: "m", Venue: "kuru", Price: price, Size: size, Ts: ms}
}

func find(cs []Candle, openTime int64) *Candle {
	for i := range cs {
		if cs[i].OpenTime == openTime {
			return &cs[i]
		}
	}
	return nil
}

func TestAggregate(t *testing.T) {
	t.Run("ohlcv within one bucket", func(t *testing.T) {
		a := New(60)
		a.Add(tick(10_000, "100", "1"))
		a.Add(tick(20_000, "130", "2"))
		a.Add(tick(30_000, "90", "3"))
		a.Add(tick(59_999, "110", "4"))
		out := a.Flush()
		if len(out) != 1 {
			t.Fatalf("got %d candles, want 1: %v", len(out), out)
		}
		c := out[0]
		if c.Open != "100" || c.High != "130" || c.Low != "90" || c.Close != "110" || c.Volume != "10" {
			t.Fatalf("candle = %+v", c)
		}
	})

	t.Run("a tick on the boundary opens the next bucket", func(t *testing.T) {
		a := New(60)
		a.Add(tick(59_999, "100", "1"))
		a.Add(tick(60_000, "200", "1"))
		out := a.Flush()
		if len(out) != 2 {
			t.Fatalf("got %d candles, want 2: %v", len(out), out)
		}
		if c := find(out, 0); c == nil || c.Close != "100" {
			t.Fatalf("first bucket = %+v", c)
		}
		if c := find(out, 60); c == nil || c.Open != "200" {
			t.Fatalf("second bucket = %+v", c)
		}
	})

	t.Run("one tick fans out to every resolution", func(t *testing.T) {
		a := New(60, 3600)
		a.Add(tick(3_600_000, "100", "1"))
		out := a.Flush()
		if len(out) != 2 {
			t.Fatalf("got %d candles, want 2: %v", len(out), out)
		}
		seen := map[int]int64{}
		for _, c := range out {
			seen[c.Resolution] = c.OpenTime
		}
		if seen[60] != 3600 || seen[3600] != 3600 {
			t.Errorf("resolutions = %v, want 60 and 3600 both opening at 3600", seen)
		}
	})

	t.Run("flush only returns candles touched since the last flush", func(t *testing.T) {
		a := New(60)
		a.Add(tick(10_000, "100", "1"))
		if len(a.Flush()) != 1 {
			t.Fatal("first flush should emit the open candle")
		}
		if got := a.Flush(); len(got) != 0 {
			t.Fatalf("second flush emitted %v, want nothing", got)
		}
		a.Add(tick(20_000, "105", "1"))
		out := a.Flush()
		if len(out) != 1 || out[0].Close != "105" {
			t.Fatalf("third flush = %v", out)
		}
	})

	t.Run("a late tick still lands in its own bucket", func(t *testing.T) {
		a := New(60)
		a.Add(tick(70_000, "200", "1"))
		a.Flush()
		a.Add(tick(10_000, "100", "1"))
		out := a.Flush()
		if len(out) != 1 || out[0].OpenTime != 0 || out[0].Close != "100" {
			t.Fatalf("out = %v", out)
		}
	})

	t.Run("closed buckets are evicted once the market moves on", func(t *testing.T) {
		a := New(60)
		a.Add(tick(0, "100", "1"))
		a.Flush()
		a.Add(tick(200_000, "100", "1")) // bucket 180, two resolutions past 0
		a.Flush()
		if len(a.open) != 1 {
			t.Fatalf("open buckets = %d, want 1", len(a.open))
		}
	})

	t.Run("junk prices are dropped rather than poisoning the candle", func(t *testing.T) {
		a := New(60)
		a.Add(tick(0, "", "1"))
		a.Add(tick(0, "0", "1"))
		a.Add(tick(0, "not-a-number", "1"))
		if got := a.Flush(); len(got) != 0 {
			t.Fatalf("flush = %v, want nothing", got)
		}
	})
}
