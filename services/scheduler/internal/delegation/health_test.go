package delegation

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCheck(t *testing.T) {
	boom := errors.New("wallet unreachable")
	tests := []struct {
		name     string
		status   Status
		err      error
		expected string
		want     error
	}{
		{name: "delegated is healthy", status: Status{Live: true, SignerID: "kq_1"}},
		{name: "not live is revoked", status: Status{}, want: ErrRevoked},
		{name: "a revoked answer from wallet passes straight through", err: ErrRevoked, want: ErrRevoked},
		{name: "signer swapped under us is revoked", status: Status{Live: true, SignerID: "kq_other"}, expected: "kq_1", want: ErrRevoked},
		{name: "matching signer is healthy", status: Status{Live: true, SignerID: "kq_1"}, expected: "kq_1"},
		{name: "wallet being down is not a revocation", err: boom, want: boom},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := &Checker{
				Fetch:          func(context.Context, string) (Status, error) { return tc.status, tc.err },
				ExpectedSigner: tc.expected,
			}
			err := c.Check(context.Background(), "did:privy:u1")
			if tc.want == nil {
				if err != nil {
					t.Fatalf("got %v, want healthy", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			if tc.want == boom && errors.Is(err, ErrRevoked) {
				t.Fatal("a transient error must never read as a revocation")
			}
		})
	}
}

func TestCheckCachesHealthyOnly(t *testing.T) {
	now := time.Unix(0, 0)
	calls := 0
	st := Status{Live: true}
	c := &Checker{
		Fetch: func(context.Context, string) (Status, error) {
			calls++
			return st, nil
		},
		TTL: time.Minute,
		Now: func() time.Time { return now },
	}
	ctx := context.Background()
	for range 3 {
		if err := c.Check(ctx, "did:privy:u1"); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("polled %d times inside the ttl, want 1", calls)
	}
	if err := c.Check(ctx, "did:privy:u2"); err != nil || calls != 2 {
		t.Fatalf("a second user must be polled on its own, calls=%d err=%v", calls, err)
	}
	now = now.Add(2 * time.Minute)
	st = Status{}
	if err := c.Check(ctx, "did:privy:u1"); !errors.Is(err, ErrRevoked) {
		t.Fatalf("after the ttl a revocation must surface, got %v", err)
	}
}
