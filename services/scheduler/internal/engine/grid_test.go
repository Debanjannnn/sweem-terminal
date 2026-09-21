package engine

import (
	"testing"

	"github.com/sweem-terminal/sweem-terminal/services/scheduler/internal/store"
)

func gridSchedule() store.Schedule {
	return store.Schedule{
		ID: "grid", Kind: store.KindGrid, MarketID: "MON-USDC",
		Levels: []string{"1.00", "1.10", "1.20"}, Total: "15000000",
	}
}

func TestGridDue(t *testing.T) {
	tests := []struct {
		name       string
		prev, cur  float64
		fired      map[int]bool
		wantFire   []int
		wantSide   string
		wantRearm  []int
		wantNoFire bool
	}{
		{name: "first poll after a restart never fires", prev: 0, cur: 1.15, wantNoFire: true},
		{name: "rising through a level sells", prev: 1.05, cur: 1.15, wantFire: []int{1}, wantSide: "sell"},
		{name: "falling through a level buys", prev: 1.15, cur: 1.05, wantFire: []int{1}, wantSide: "buy"},
		{name: "a jump crosses every level it passed", prev: 0.95, cur: 1.25, wantFire: []int{0, 1, 2}, wantSide: "sell"},
		{name: "landing exactly on a level counts as a crossing", prev: 1.05, cur: 1.10, wantFire: []int{1}, wantSide: "sell"},
		{name: "a fired level does not re-fire while price oscillates around it", prev: 1.15, cur: 1.09, fired: map[int]bool{1: true}, wantNoFire: true},
		{name: "a fired level re-arms once price leaves the band", prev: 1.15, cur: 1.05, fired: map[int]bool{1: true}, wantNoFire: true, wantRearm: []int{1}},
		{name: "untouched levels stay put", prev: 1.11, cur: 1.12, wantNoFire: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fire, rearm, err := gridDue(gridSchedule(), tc.prev, tc.cur, 100, tc.fired)
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantNoFire && len(fire) != 0 {
				t.Fatalf("fired %v, want nothing", fire)
			}
			if len(fire) != len(tc.wantFire) {
				t.Fatalf("fired %v, want levels %v", fire, tc.wantFire)
			}
			for i, want := range tc.wantFire {
				if fire[i].Index != want {
					t.Fatalf("fire %d is level %d, want %d", i, fire[i].Index, want)
				}
				if fire[i].Side != tc.wantSide {
					t.Fatalf("fire %d is a %s, want %s", i, fire[i].Side, tc.wantSide)
				}
				if fire[i].Amount != "5000000" {
					t.Fatalf("fire %d carries %s", i, fire[i].Amount)
				}
			}
			if len(rearm) != len(tc.wantRearm) {
				t.Fatalf("rearmed %v, want %v", rearm, tc.wantRearm)
			}
		})
	}
}

func TestGridDueRejectsBadInput(t *testing.T) {
	s := gridSchedule()
	if _, _, err := gridDue(s, 1.0, 0, 100, nil); err == nil {
		t.Fatal("a zero price must be an error, not a fire")
	}
	s.Levels = []string{"cheap"}
	if _, _, err := gridDue(s, 1.0, 1.1, 100, nil); err == nil {
		t.Fatal("an unparseable level must be an error")
	}
}
