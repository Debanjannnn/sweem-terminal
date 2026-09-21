package finality

import "testing"

func TestBlockTag(t *testing.T) {
	for _, tt := range []struct {
		tier Tier
		want string
	}{{Seen, "latest"}, {Safe, "safe"}, {Final, "finalized"}, {Tier(99), "latest"}} {
		if got := tt.tier.BlockTag(); got != tt.want {
			t.Fatalf("Tier(%d).BlockTag() = %q, want %q", tt.tier, got, tt.want)
		}
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		in      string
		want    Tier
		wantErr bool
	}{
		{"latest", Seen, false},
		{" Proposed ", Seen, false},
		{"seen", Seen, false},
		{"SAFE", Safe, false},
		{"voted", Safe, false},
		{"finalized", Final, false},
		{"final", Final, false},
		{"", 0, true},
		{"pending", 0, true},
		{"earliest", 0, true},
	}
	for _, tt := range tests {
		got, err := Parse(tt.in)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Fatalf("Parse(%q) = %v, %v", tt.in, got, err)
		}
	}
}

// the D+2 guard: anything observed above the finalized head is still mutable
func TestSettled(t *testing.T) {
	tests := []struct {
		name       string
		observedAt uint64
		finalized  uint64
		want       bool
	}{
		{"finalized head behind observation", 100, 97, false},
		{"one block short", 100, 99, false},
		{"exactly finalized", 100, 100, true},
		{"well past", 100, 512, true},
		{"genesis observation", 0, 0, true},
		{"nothing finalized yet", 1, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Settled(tt.observedAt, tt.finalized); got != tt.want {
				t.Fatalf("Settled(%d, %d) = %v, want %v", tt.observedAt, tt.finalized, got, tt.want)
			}
		})
	}
}
