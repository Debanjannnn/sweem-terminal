package engine

import (
	"testing"
	"time"

	"github.com/sweem-terminal/sweem-terminal/services/scheduler/internal/store"
)

func TestDcaDueAt(t *testing.T) {
	tests := []struct {
		cadence time.Duration
		i       int
		want    time.Duration
	}{
		{24 * time.Hour, 0, 0},
		{24 * time.Hour, 7, 168 * time.Hour},
		{15 * time.Minute, 4, time.Hour},
	}
	for _, tc := range tests {
		if got := dcaDueAt(start, tc.cadence, tc.i); !got.Equal(start.Add(tc.want)) {
			t.Fatalf("cadence %v index %d due at %v, want %v", tc.cadence, tc.i, got.Sub(start), tc.want)
		}
	}
}

func TestDcaDue(t *testing.T) {
	tests := []struct {
		name       string
		now        time.Duration
		nextIndex  int
		sliceCount int
		max        int
		wantIdx    []int
		wantAmount string
	}{
		{name: "nothing before the start", now: -time.Second, max: 3},
		{name: "first leg at the start", max: 3, wantIdx: []int{0}, wantAmount: "25000000"},
		{name: "indefinite, total is one leg", now: 100*time.Hour + 20*time.Minute, nextIndex: 400, max: 2,
			wantIdx: []int{400, 401}, wantAmount: "25000000"},
		{name: "two legs after 35 minutes", now: 35 * time.Minute, max: 3, wantIdx: []int{0, 1, 2}, wantAmount: "25000000"},
		{name: "a finite dca splits total and stops", now: 10 * time.Hour, sliceCount: 4, nextIndex: 3, max: 3,
			wantIdx: []int{3}, wantAmount: "6250000"},
		{name: "a finite dca past its last leg is empty", now: 10 * time.Hour, sliceCount: 4, nextIndex: 4, max: 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := store.Schedule{
				ID: "dca", Kind: store.KindDCA, Side: "buy", CreatedAt: start,
				NextIndex: tc.nextIndex, SliceCount: tc.sliceCount,
				Total: "25000000", CadenceSeconds: 900,
			}
			got, err := dcaDue(s, start.Add(tc.now), tc.max)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tc.wantIdx) {
				t.Fatalf("got %d due legs %v, want %v", len(got), got, tc.wantIdx)
			}
			for i, want := range tc.wantIdx {
				if got[i].Index != want || got[i].Amount != tc.wantAmount {
					t.Fatalf("leg %d is %+v, want index %d amount %s", i, got[i], want, tc.wantAmount)
				}
			}
		})
	}
}

func TestDcaDueNeedsCadence(t *testing.T) {
	if _, err := dcaDue(store.Schedule{ID: "dca", Total: "1", CreatedAt: start}, start, 1); err == nil {
		t.Fatal("a dca without a cadence must be an error")
	}
}
