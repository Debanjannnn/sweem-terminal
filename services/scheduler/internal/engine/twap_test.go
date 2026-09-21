package engine

import (
	"math/big"
	"strconv"
	"testing"
	"time"

	"github.com/sweem-terminal/sweem-terminal/services/scheduler/internal/store"
)

var start = time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

func TestTwapDueAt(t *testing.T) {
	tests := []struct {
		name   string
		window time.Duration
		n, i   int
		want   time.Duration
	}{
		{"first slice fires at start", 150 * time.Minute, 10, 0, 0},
		{"third of ten over 2.5h", 150 * time.Minute, 10, 3, 45 * time.Minute},
		{"last slice is one step short of the window", 150 * time.Minute, 10, 9, 135 * time.Minute},
		{"window that does not divide evenly", 100 * time.Second, 3, 2, 66666666666},
		{"single slice", time.Hour, 1, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := twapDueAt(start, tc.window, tc.n, tc.i); !got.Equal(start.Add(tc.want)) {
				t.Fatalf("due at %v, want %v", got.Sub(start), tc.want)
			}
		})
	}
}

func TestSplitEvenSumsExactly(t *testing.T) {
	tests := []struct {
		total string
		n     int
		want  []string
	}{
		{"1000000000", 10, []string{"100000000", "100000000", "100000000", "100000000", "100000000", "100000000", "100000000", "100000000", "100000000", "100000000"}},
		{"10", 3, []string{"4", "3", "3"}},
		{"7", 7, []string{"1", "1", "1", "1", "1", "1", "1"}},
		{"1", 3, []string{"1", "0", "0"}},
		{"1000000000000000000000001", 7, nil},
	}
	for _, tc := range tests {
		t.Run(tc.total+"/"+strconv.Itoa(tc.n), func(t *testing.T) {
			total, _ := new(big.Int).SetString(tc.total, 10)
			parts := splitEven(total, tc.n)
			if len(parts) != tc.n {
				t.Fatalf("got %d parts, want %d", len(parts), tc.n)
			}
			sum := new(big.Int)
			for _, p := range parts {
				sum.Add(sum, p)
			}
			if sum.Cmp(total) != 0 {
				t.Fatalf("parts sum to %s, want %s: dust drift", sum, total)
			}
			for i, w := range tc.want {
				if parts[i].String() != w {
					t.Fatalf("part %d is %s, want %s", i, parts[i], w)
				}
			}
			// largest remainder: parts differ by at most one unit
			if parts[0].Cmp(new(big.Int).Add(parts[tc.n-1], big.NewInt(1))) > 0 {
				t.Fatalf("parts %s and %s differ by more than one unit", parts[0], parts[tc.n-1])
			}
		})
	}
}

func TestTwapDue(t *testing.T) {
	sched := store.Schedule{
		ID: "abc", Kind: store.KindTWAP, Side: "buy", CreatedAt: start,
		Total: "1000000000", SliceCount: 10, WindowSeconds: 9000,
	}
	tests := []struct {
		name      string
		now       time.Duration
		nextIndex int
		max       int
		wantIdx   []int
	}{
		{"nothing before the start", -time.Minute, 0, 5, nil},
		{"first slice at the start", 0, 0, 5, []int{0}},
		{"three slices due after 35 minutes", 35 * time.Minute, 0, 5, []int{0, 1, 2}},
		{"catchup capped at max", 35 * time.Minute, 0, 1, []int{0}},
		{"resumes from next index", 35 * time.Minute, 2, 5, []int{2}},
		{"never past the last slice", 10 * time.Hour, 8, 5, []int{8, 9}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := sched
			s.NextIndex = tc.nextIndex
			got, err := twapDue(s, start.Add(tc.now), tc.max)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tc.wantIdx) {
				t.Fatalf("got %d due slices, want %d", len(got), len(tc.wantIdx))
			}
			for i, want := range tc.wantIdx {
				if got[i].Index != want {
					t.Fatalf("slice %d is index %d, want %d", i, got[i].Index, want)
				}
				if got[i].Amount != "100000000" {
					t.Fatalf("slice %d carries %s", i, got[i].Amount)
				}
			}
		})
	}
}

func TestTwapDueRejectsBadParams(t *testing.T) {
	tests := []struct {
		name string
		s    store.Schedule
	}{
		{"no slices", store.Schedule{Total: "10", WindowSeconds: 60}},
		{"no window", store.Schedule{Total: "10", SliceCount: 4}},
		{"total is not a number", store.Schedule{Total: "lots", SliceCount: 4, WindowSeconds: 60}},
		{"total is zero", store.Schedule{Total: "0", SliceCount: 4, WindowSeconds: 60}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sc := tc.s
			sc.ID, sc.CreatedAt = "x", start
			if _, err := twapDue(sc, start, 5); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
