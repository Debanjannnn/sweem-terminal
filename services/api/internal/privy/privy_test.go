package privy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVerifyDelegation(t *testing.T) {
	signers := `[{"signer_id":"q1","override_policy_ids":[]}]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, _ := r.BasicAuth(); u != "app" || p != "sec" || r.Header.Get("privy-app-id") != "app" {
			w.WriteHeader(401)
			return
		}
		switch r.URL.EscapedPath() {
		case "/v1/users/did:privy:u1":
			w.Write([]byte(`{"id":"did:privy:u1","linked_accounts":[{"type":"email","address":"a@b.c"},{"type":"wallet","address":"0xAbC","chain_type":"ethereum"}]}`))
		case "/v1/wallets/w1":
			w.Write([]byte(`{"id":"w1","address":"0xabc","additional_signers":` + signers + `}`))
		case "/v1/wallets/w2":
			w.Write([]byte(`{"id":"w2","address":"0xdef","additional_signers":[]}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	c := New("app", "sec", "q1")
	c.Base = srv.URL
	ctx := context.Background()

	if err := c.VerifyDelegation(ctx, "did:privy:u1", "w1", "0xabc"); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		did, wallet, addr string
		want              error
	}{
		{"did:privy:u1", "w1", "0xdef", ErrNotOwned},
		{"did:privy:u1", "w2", "0xabc", ErrMismatch},
		{"did:privy:nobody", "w1", "0xabc", ErrNotFound},
		{"did:privy:u1", "nope", "0xabc", ErrNotFound},
	}
	for _, tc := range cases {
		if err := c.VerifyDelegation(ctx, tc.did, tc.wallet, tc.addr); !errors.Is(err, tc.want) {
			t.Errorf("%+v: got %v", tc, err)
		}
	}
	signers = `[{"signer_id":"someone-else"}]`
	if err := c.VerifyDelegation(ctx, "did:privy:u1", "w1", "0xabc"); !errors.Is(err, ErrNoSigner) {
		t.Errorf("foreign signer: %v", err)
	}
	c.AppSecret = "wrong"
	if err := c.VerifyDelegation(ctx, "did:privy:u1", "w1", "0xabc"); err == nil {
		t.Error("401 accepted")
	}
}
