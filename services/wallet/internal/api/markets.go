package api

import (
	"context"
	"strings"
	"sync"
	"time"
)

// market-data owns the registry. wallet caches the id set only to turn an
// unknown market into a sentence instead of an executor round trip; the
// executor's committed allowlist is still the guard that matters.
type marketCache struct {
	mu      sync.RWMutex
	ids     map[string]bool
	fetched time.Time
	ttl     time.Duration
}

func newMarketCache(ttl time.Duration) *marketCache {
	return &marketCache{ids: map[string]bool{}, ttl: ttl}
}

func normalizeMarketID(id string) string {
	r := strings.NewReplacer("-", "", "_", "", "/", "", " ", "")
	return r.Replace(strings.ToLower(strings.TrimSpace(id)))
}

// known reports false only when market-data answered and did not list the id.
// an unreachable market-data returns true: refusing every order because a read
// service blinked would be worse than letting the executor fail closed.
func (c *marketCache) known(ctx context.Context, fetch func(context.Context) ([]string, error), id string) bool {
	c.mu.RLock()
	fresh := time.Since(c.fetched) < c.ttl && len(c.ids) > 0
	hit := c.ids[normalizeMarketID(id)]
	c.mu.RUnlock()
	if fresh {
		return hit
	}

	ids, err := fetch(ctx)
	if err != nil || len(ids) == 0 {
		c.mu.RLock()
		defer c.mu.RUnlock()
		return len(c.ids) == 0 || c.ids[normalizeMarketID(id)]
	}

	set := make(map[string]bool, len(ids))
	for _, m := range ids {
		set[normalizeMarketID(m)] = true
	}
	c.mu.Lock()
	c.ids, c.fetched = set, time.Now()
	c.mu.Unlock()
	return set[normalizeMarketID(id)]
}

func (s *Server) knownMarket(ctx context.Context, id string) bool {
	if s.Market == nil || s.marketIDs == nil {
		return true
	}
	return s.marketIDs.known(ctx, s.Market.Markets, id)
}
