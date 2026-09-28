package store

import (
	"slices"
	"testing"
)

func TestNormalizeList(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{name: "nil becomes empty, never null in json", want: []string{}},
		{name: "lowercased, trimmed, sorted", in: []string{" KURU ", "Perpl"}, want: []string{"kuru", "perpl"}},
		{name: "duplicates collapse", in: []string{"kuru", "KURU", "kuru "}, want: []string{"kuru"}},
		{name: "blanks dropped", in: []string{"", "  ", "kuru"}, want: []string{"kuru"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeList(tt.in); !slices.Equal(got, tt.want) {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClampLimit(t *testing.T) {
	tests := []struct{ in, want int }{{0, 50}, {-1, 50}, {10, 10}, {maxLimit, maxLimit}, {maxLimit + 1, maxLimit}}
	for _, tt := range tests {
		if got := clampLimit(tt.in); got != tt.want {
			t.Errorf("clampLimit(%d) = %d, want %d", tt.in, got, tt.want)
		}
	}
}
