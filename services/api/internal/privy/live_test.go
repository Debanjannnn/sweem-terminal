//go:build privylive

package privy

import (
	"context"
	"os"
	"testing"
)

// GET-only against real Privy. Needs PRIVY_APP_ID, PRIVY_APP_SECRET, PRIVY_KEY_QUORUM_ID,
// PRIVY_LIVE_DID and PRIVY_LIVE_WALLET_ID (a wallet of that user with our quorum as signer).
func TestLiveVerifyDelegation(t *testing.T) {
	env := map[string]string{}
	for _, k := range []string{"PRIVY_APP_ID", "PRIVY_APP_SECRET", "PRIVY_KEY_QUORUM_ID", "PRIVY_LIVE_DID", "PRIVY_LIVE_WALLET_ID"} {
		if env[k] = os.Getenv(k); env[k] == "" {
			t.Fatalf("%s not set", k)
		}
	}
	c := New(env["PRIVY_APP_ID"], env["PRIVY_APP_SECRET"], env["PRIVY_KEY_QUORUM_ID"])
	ctx := context.Background()
	var w struct{ Address string }
	if err := c.get(ctx, "/v1/wallets/"+env["PRIVY_LIVE_WALLET_ID"], &w); err != nil {
		t.Fatal(err)
	}
	if err := c.VerifyDelegation(ctx, env["PRIVY_LIVE_DID"], env["PRIVY_LIVE_WALLET_ID"], w.Address); err != nil {
		t.Fatalf("real delegated wallet rejected: %v", err)
	}
	if err := c.VerifyDelegation(ctx, env["PRIVY_LIVE_DID"], env["PRIVY_LIVE_WALLET_ID"], "0x0000000000000000000000000000000000000001"); err != ErrNotOwned {
		t.Fatalf("foreign address: %v", err)
	}
}
