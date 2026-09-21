package chains

import "testing"

func TestRoundTrip(t *testing.T) {
	for _, id := range Supported() {
		l := Label(id)
		if l == "" {
			t.Fatalf("no label for supported chain %d", id)
		}
		if got := ID(l); got != id {
			t.Fatalf("round trip %d -> %q -> %d", id, l, got)
		}
	}
}

func TestNormalize(t *testing.T) {
	tests := []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"monad", MonadMainnet, false},
		{" MONAD ", MonadMainnet, false},
		{"Monad-Testnet", MonadTestnet, false},
		{"143", MonadMainnet, false},
		{"10143", MonadTestnet, false},
		// near misses must fail loudly rather than land on the wrong network
		{"", 0, true},
		{"testnet", 0, true},
		{"monad testnet", 0, true},
		{"monadtestnet", 0, true},
		{"monad-mainnet", 0, true},
		{"8453", 0, true},
		{"1", 0, true},
	}
	for _, tt := range tests {
		got, err := Normalize(tt.in)
		if (err != nil) != tt.wantErr {
			t.Fatalf("Normalize(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
		}
		if got != tt.want {
			t.Fatalf("Normalize(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestIsSupported(t *testing.T) {
	for _, tt := range []struct {
		id   int
		want bool
	}{{MonadMainnet, true}, {MonadTestnet, true}, {0, false}, {8453, false}} {
		if got := IsSupported(tt.id); got != tt.want {
			t.Fatalf("IsSupported(%d) = %v", tt.id, got)
		}
	}
}
