package auth

import (
	"context"
	"strings"
	"testing"
)

// The guard is a feature: a wallet API that can see a signing secret must not
// start at all, however convenient it would be.
func TestGuardNoSigningSecrets(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{name: "clean environment boots", env: map[string]string{"PRIVY_APP_ID": "app1"}},
		{name: "app secret refuses", env: map[string]string{"PRIVY_APP_SECRET": "shh"}, want: "PRIVY_APP_SECRET"},
		{name: "authorization key refuses", env: map[string]string{"PRIVY_AUTHORIZATION_PRIVATE_KEY": "wallet-auth:x"}, want: "PRIVY_AUTHORIZATION_PRIVATE_KEY"},
		{name: "both are named", env: map[string]string{"PRIVY_APP_SECRET": "shh", "PRIVY_AUTHORIZATION_PRIVATE_KEY": "x"}, want: "PRIVY_APP_SECRET and PRIVY_AUTHORIZATION_PRIVATE_KEY"},
		{name: "whitespace is not a secret", env: map[string]string{"PRIVY_APP_SECRET": "   "}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := GuardNoSigningSecrets(func(k string) string { return tt.env[k] })
			switch {
			case tt.want == "" && err != nil:
				t.Fatalf("refused a clean environment: %v", err)
			case tt.want == "":
				return
			case err == nil:
				t.Fatal("booted with a signing secret in the environment")
			case !strings.Contains(err.Error(), tt.want):
				t.Fatalf("error %q does not name %s", err, tt.want)
			}
		})
	}
}

func TestNewVerifierRefusesWithSecret(t *testing.T) {
	t.Setenv("PRIVY_APP_ID", "app1")
	t.Setenv("PRIVY_VERIFICATION_KEY", "-----BEGIN PUBLIC KEY-----\nnope\n-----END PUBLIC KEY-----")
	t.Setenv("PRIVY_APP_SECRET", "shh")
	if _, err := NewVerifier(context.Background()); err == nil || !strings.Contains(err.Error(), "refusing to boot") {
		t.Fatalf("err = %v, want a refusal to boot", err)
	}
}
