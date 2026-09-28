package main

import "testing"

func TestDeployed(t *testing.T) {
	for a, want := range map[string]bool{
		"":   false,
		"0x": false,
		"0x0000000000000000000000000000000000000000": false,
		"0x1964c32f0be608e7d29302aff5e61268e72080cc": true,
		"0x0000000000000000000000000000000000000001": true,
	} {
		if got := deployed(a); got != want {
			t.Errorf("deployed(%q) = %v, want %v", a, got, want)
		}
	}
}
