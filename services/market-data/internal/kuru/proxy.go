package kuru

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
)

// ImplementationSlot is EIP-1967's. Every Kuru contract is a proxy: reads
// succeed against the proxy by delegation, but the ABI that actually answers
// lives at the implementation, so that is what gets pinned in a test.
const ImplementationSlot = "0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc"

// Implementation resolves the ERC-1967 implementation behind a Kuru proxy.
func (c *Client) Implementation(ctx context.Context, proxy string) (string, error) {
	raw, err := c.rpc(ctx, "eth_getStorageAt", proxy, ImplementationSlot, "latest")
	if err != nil {
		return "", err
	}
	if len(raw) != 32 {
		return "", fmt.Errorf("implementation slot of %s returned %d bytes, not 32", proxy, len(raw))
	}
	addr := "0x" + hex.EncodeToString(raw[12:])
	if addr == "0x0000000000000000000000000000000000000000" {
		return "", fmt.Errorf("%s has an empty ERC-1967 implementation slot, so it is not a proxy", proxy)
	}
	return strings.ToLower(addr), nil
}
