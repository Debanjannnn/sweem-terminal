// Package delegation polls delegation health through wallet. Privy has no
// revocation webhook and no distinguishable "signer revoked" error, so the
// only way to learn a user unlinked us is to diff the signers before a leg.
package delegation

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrRevoked = errors.New("the delegated signer is no longer attached to this wallet")

// mirrors wallet's delegationView; live folds in revoked_at and expires_at
type Status struct {
	Live     bool   `json:"live"`
	SignerID string `json:"signer_id"`
	UserID   string `json:"user_id"`
}

type Checker struct {
	Fetch          func(context.Context, string) (Status, error)
	ExpectedSigner string // empty: trust wallet's own diff
	TTL            time.Duration
	Now            func() time.Time

	mu    sync.Mutex
	cache map[string]time.Time
}

// Check returns ErrRevoked when the schedule must be paused. Any other error is
// transient (wallet down, timeout) and must not pause anything.
func (c *Checker) Check(ctx context.Context, did string) error {
	if c.fresh(did) {
		return nil
	}
	st, err := c.Fetch(ctx, did)
	if errors.Is(err, ErrRevoked) {
		return err
	}
	if err != nil {
		return fmt.Errorf("read delegation health for %s: %w", did, err)
	}
	if !st.Live {
		return ErrRevoked
	}
	if c.ExpectedSigner != "" && st.SignerID != c.ExpectedSigner {
		return fmt.Errorf("%w: wallet reports signer %q, expected %q", ErrRevoked, st.SignerID, c.ExpectedSigner)
	}
	c.remember(did)
	return nil
}

func (c *Checker) fresh(did string) bool {
	if c.TTL <= 0 {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now().Before(c.cache[did].Add(c.TTL))
}

func (c *Checker) remember(did string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cache == nil {
		c.cache = map[string]time.Time{}
	}
	c.cache[did] = c.now()
}

func (c *Checker) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}
