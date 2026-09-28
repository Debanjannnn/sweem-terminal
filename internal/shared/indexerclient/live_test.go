//go:build integration

package indexerclient

import (
	"testing"
	"time"
)

// Every document this client sends, against the running indexer's schema. A
// renamed entity or field fails here instead of reading as "no data" in prod.
//
//	make indexer && go test -tags integration ./internal/shared/indexerclient/
func TestLiveSchemaAcceptsEveryQuery(t *testing.T) {
	c := New()
	ctx := t.Context()
	now := time.Now().Unix()
	const mon = "0xfdbe356828c8f5a5d5ed4f69dde0816f4058ef61"
	const owner = "0x0000000000000000000000000000000000000001"

	if _, err := c.Positions(ctx, owner); err != nil {
		t.Errorf("Positions: %v", err)
	}
	if _, err := c.PositionEvents(ctx, "sweem:vol:ETH/USD", 0); err != nil {
		t.Errorf("PositionEvents: %v", err)
	}
	if _, err := c.FundingRatePoints(ctx, "sweem:vol:ETH/USD", now-3600, now); err != nil {
		t.Errorf("FundingRatePoints: %v", err)
	}
	if _, err := c.VaultFlows(ctx, owner); err != nil {
		t.Errorf("VaultFlows: %v", err)
	}
	trades, err := c.KuruTrades(ctx, mon, 0, now)
	if err != nil {
		t.Fatalf("KuruTrades: %v", err)
	}
	t.Logf("%d MON/USDC fills indexed", len(trades))
	for _, tr := range trades {
		if tr.Price == "" || tr.Size == "" || tr.Ts == 0 {
			t.Fatalf("fill with an empty field: %+v", tr)
		}
	}
}
