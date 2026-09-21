package wad

import (
	"math/big"
	"testing"
)

func mustInt(t *testing.T, s string) *big.Int {
	t.Helper()
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		t.Fatalf("bad literal %q", s)
	}
	return v
}

func TestMulDivWad(t *testing.T) {
	tests := []struct {
		name       string
		op         string
		x, y, want string
	}{
		{"mul unit", "mul", "1000000000000000000", "1000000000000000000", "1000000000000000000"},
		{"mul 1.5 x 2", "mul", "1500000000000000000", "2000000000000000000", "3000000000000000000"},
		{"mul truncates down", "mul", "1", "1", "0"},
		{"mul by zero", "mul", "0", "123456", "0"},
		{"mul negative truncates toward zero", "mul", "-1", "1", "0"},
		{"div unit", "div", "1000000000000000000", "1000000000000000000", "1000000000000000000"},
		{"div one third", "div", "1000000000000000000", "3000000000000000000", "333333333333333333"},
		{"div two thirds", "div", "2000000000000000000", "3000000000000000000", "666666666666666666"},
		{"div by half doubles", "div", "1000000000000000000", "500000000000000000", "2000000000000000000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			x, y := mustInt(t, tt.x), mustInt(t, tt.y)
			var got *big.Int
			if tt.op == "mul" {
				got = MulWad(x, y)
			} else {
				got = DivWad(x, y)
			}
			if got.String() != tt.want {
				t.Fatalf("got %s want %s", got, tt.want)
			}
			if x.String() != tt.x || y.String() != tt.y {
				t.Fatal("operands mutated")
			}
		})
	}
}

func TestMulDivRoundTripLosesOnlyDust(t *testing.T) {
	x := mustInt(t, "123456789012345678901")
	y := mustInt(t, "3000000000000000000")
	back := MulWad(DivWad(x, y), y)
	diff := new(big.Int).Sub(x, back)
	if diff.Sign() < 0 || diff.Cmp(big.NewInt(10)) > 0 {
		t.Fatalf("round trip drifted by %s", diff)
	}
}

func TestSqrtWad(t *testing.T) {
	tests := []struct{ in, want string }{
		{"0", "0"},
		{"1000000000000000000", "1000000000000000000"},
		{"4000000000000000000", "2000000000000000000"},
		{"2000000000000000000", "1414213562373095048"},
		{"250000000000000000", "500000000000000000"},
		{"100000000000000000000", "10000000000000000000"},
	}
	for _, tt := range tests {
		if got := SqrtWad(mustInt(t, tt.in)); got.String() != tt.want {
			t.Fatalf("SqrtWad(%s) = %s want %s", tt.in, got, tt.want)
		}
	}
}

func TestScale(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		from, to int
		want     string
	}{
		{"usdc 6dp to wad", "1000000", 6, 18, "1000000000000000000"},
		{"wad to usdc 6dp", "1500000000000000000", 18, 6, "1500000"},
		{"down truncates dust", "1000000999999999999", 18, 6, "1000000"},
		{"same precision", "42", 18, 18, "42"},
		{"8dp to 18dp", "12345678", 8, 18, "123456780000000000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Scale(mustInt(t, tt.in), tt.from, tt.to); got.String() != tt.want {
				t.Fatalf("got %s want %s", got, tt.want)
			}
		})
	}
}

func TestFromDecimalString(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"1", "1000000000000000000", false},
		{" 1.5 ", "1500000000000000000", false},
		{"0.000000000000000001", "1", false},
		{"-2.25", "-2250000000000000000", false},
		{"+3", "3000000000000000000", false},
		{".5", "500000000000000000", false},
		{"1000000.000001", "1000000000001000000000000", false},
		{"", "", true},
		{".", "", true},
		{"abc", "", true},
		{"1.2.3", "", true},
		{"1e18", "", true},
		{"0.0000000000000000001", "", true}, // 19 fraction digits
	}
	for _, tt := range tests {
		got, err := FromDecimalString(tt.in)
		if (err != nil) != tt.wantErr {
			t.Fatalf("FromDecimalString(%q) err = %v", tt.in, err)
		}
		if err == nil && got.String() != tt.want {
			t.Fatalf("FromDecimalString(%q) = %s want %s", tt.in, got, tt.want)
		}
	}
}

func TestToDecimalString(t *testing.T) {
	tests := []struct {
		in       string
		decimals int
		want     string
	}{
		{"1000000000000000000", 18, "1"},
		{"1500000000000000000", 18, "1.5"},
		{"-1500000000000000000", 18, "-1.5"},
		{"1", 18, "0.000000000000000001"},
		{"0", 18, "0"},
		{"1000000", 6, "1"},
		{"1234567", 6, "1.234567"},
		{"1230000", 6, "1.23"},
		{"42", 0, "42"},
	}
	for _, tt := range tests {
		if got := ToDecimalString(mustInt(t, tt.in), tt.decimals); got != tt.want {
			t.Fatalf("ToDecimalString(%s, %d) = %q want %q", tt.in, tt.decimals, got, tt.want)
		}
	}
}

func TestDecimalStringRoundTrip(t *testing.T) {
	for _, s := range []string{"0", "1", "1.5", "-2.25", "0.000000000000000001", "123456789.987654321"} {
		v, err := FromDecimalString(s)
		if err != nil {
			t.Fatal(err)
		}
		if got := ToDecimalString(v, Decimals); got != s {
			t.Fatalf("round trip %q -> %q", s, got)
		}
	}
}
