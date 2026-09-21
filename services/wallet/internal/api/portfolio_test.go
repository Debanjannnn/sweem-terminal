package api

import (
	"testing"
	"time"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/indexerclient"
	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/marketdata"
	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/store"
)

// A number this service did not source is never invented, and a disagreement
// with the indexer is reported with its reason rather than smoothed over.
func TestReconcile(t *testing.T) {
	indexed := []indexerclient.Position{{
		MarketID: "MON-USDC", Owner: "0x1", IsLong: true,
		Size: "5000", EntryPrice: "1000", Collateral: "900", UpdatedAt: 42,
	}}
	ours := map[string]local{"MONUSDC": {MarketID: "MON-USDC", Side: "buy", Size: "5000", Orders: 1}}
	marks := map[string]marketdata.Mark{"MON-USDC": {MarketID: "MON-USDC", Price: "1100"}}

	tests := []struct {
		name       string
		positions  []indexerclient.Position
		marks      map[string]marketdata.Mark
		mine       map[string]local
		wantLen    int
		source     string
		reconciled bool
		mark       string
		says       string
	}{
		{
			name: "indexer and our rows agree", positions: indexed, marks: marks, mine: ours,
			wantLen: 1, source: "indexer", reconciled: true, mark: "1100",
		},
		{
			name: "a position we never ordered is not silently trusted", positions: indexed, marks: marks,
			mine: map[string]local{}, wantLen: 1, source: "indexer", reconciled: false, mark: "1100",
			says: "opened elsewhere",
		},
		{
			name: "our row with nothing indexed is reported, not dropped", positions: nil, marks: marks, mine: ours,
			wantLen: 1, source: "local", reconciled: false, mark: "1100", says: "not indexed yet",
		},
		{
			name: "a missing mark stays empty rather than becoming zero", positions: indexed,
			marks: map[string]marketdata.Mark{}, mine: ours, wantLen: 1, source: "indexer", reconciled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reconcile(tt.positions, tt.marks, tt.mine)
			if len(got) != tt.wantLen {
				t.Fatalf("%d positions, want %d", len(got), tt.wantLen)
			}
			p := got[0]
			if p.Source != tt.source {
				t.Errorf("source = %s, want %s", p.Source, tt.source)
			}
			if p.Reconciled != tt.reconciled {
				t.Errorf("reconciled = %v, want %v", p.Reconciled, tt.reconciled)
			}
			if p.Mark != tt.mark {
				t.Errorf("mark = %q, want %q", p.Mark, tt.mark)
			}
			if tt.says != "" && !containsFold(p.ReconcileReason, tt.says) {
				t.Errorf("reason %q does not say %q", p.ReconcileReason, tt.says)
			}
			if tt.reconciled && p.ReconcileReason != "" {
				t.Errorf("a reconciled position carried a reason: %q", p.ReconcileReason)
			}
			if !tt.reconciled && p.ReconcileReason == "" {
				t.Error("an unreconciled position carried no reason")
			}
		})
	}
}

// Only intents the executor accepted count as ours; a pending or failed one
// moved nothing.
func TestLocalActivityIgnoresUnsubmittedIntents(t *testing.T) {
	now := time.Now()
	intents := []store.Intent{
		{Key: "a", MarketID: "MON-USDC", Side: "buy", Size: "100", State: "submitted", CreatedAt: now},
		{Key: "b", MarketID: "MON-USDC", Side: "buy", Size: "50", State: "submitted", CreatedAt: now.Add(time.Minute)},
		{Key: "c", MarketID: "MON-USDC", Side: "buy", Size: "999", State: "failed", CreatedAt: now},
		{Key: "d", MarketID: "WETH-USDC", Side: "sell", Size: "7", State: "pending", CreatedAt: now},
	}
	got := localActivity(intents)
	if len(got) != 1 {
		t.Fatalf("%d markets, want 1", len(got))
	}
	l := got["MONUSDC"]
	if l.Orders != 2 || l.Size != "150" {
		t.Fatalf("orders = %d size = %s, want 2 and 150", l.Orders, l.Size)
	}
}
