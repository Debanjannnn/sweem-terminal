package store

import "testing"

func TestDecodeLevels(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
		fail bool
	}{
		{name: "absent", raw: "null"},
		{name: "empty", raw: ""},
		{name: "quoted decimals", raw: `["1.00","1.10"]`, want: []string{"1.00", "1.10"}},
		{name: "bare numbers", raw: `[1.0,1.1]`, want: []string{"1.0", "1.1"}},
		{name: "not an array", raw: `{"a":1}`, fail: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeLevels([]byte(tc.raw))
			if tc.fail {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("level %d is %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// the claim must never be writable as a blind update
func TestClaimSQLGuardsOnPending(t *testing.T) {
	for _, want := range []string{"ON CONFLICT (schedule_id, slice_index)", "WHERE slices.state = 'pending' AND slices.intent_key IS NULL"} {
		if !contains(claimSQL, want) {
			t.Fatalf("claim statement lost %q:\n%s", want, claimSQL)
		}
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
