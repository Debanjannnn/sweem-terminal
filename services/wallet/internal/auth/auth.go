// Package auth wires privyauth for the wallet API and keeps the signing
// secrets out of this process.
package auth

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/config"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/privyauth"
)

// Holding either of these would let this process sign. It never signs: the
// executor does, and cmd/sync-policy is a separate binary for the same reason.
var SigningSecrets = []string{"PRIVY_APP_SECRET", "PRIVY_AUTHORIZATION_PRIVATE_KEY"}

func GuardNoSigningSecrets(look func(string) string) error {
	var found []string
	for _, k := range SigningSecrets {
		if strings.TrimSpace(look(k)) != "" {
			found = append(found, k)
		}
	}
	if len(found) == 0 {
		return nil
	}
	return fmt.Errorf(
		"refusing to boot: %s is set in this process's environment, and the wallet API must never be able to sign; move it to the executor or to cmd/sync-policy and restart",
		strings.Join(found, " and "))
}

// NewVerifier refuses to boot before it builds anything. the key comes from
// privy's public jwks, so no app secret is ever needed here.
func NewVerifier(ctx context.Context) (*privyauth.Verifier, error) {
	if err := GuardNoSigningSecrets(os.Getenv); err != nil {
		return nil, err
	}
	appID := config.GetEnv("PRIVY_APP_ID", "")
	if appID == "" {
		return nil, fmt.Errorf("PRIVY_APP_ID is not set, so access tokens cannot be verified")
	}
	key, err := privyauth.FetchVerificationKey(ctx, appID, "")
	if err != nil {
		return nil, err
	}
	return privyauth.NewVerifier(appID, key)
}
