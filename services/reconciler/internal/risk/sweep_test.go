package risk

import (
	"context"
	"math/big"
	"testing"
	"time"
)

func wadOf(units int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(units), big.NewInt(1e18))
}

func TestEvaluatePosition(t *testing.T) {
	cases := []struct {
		name       string
		collateral *big.Int
		want       string
	}{
		{"at maintenance is a liquidation candidate", wadOf(5), KindLiquidation},
		{"below maintenance is a liquidation candidate", wadOf(4), KindLiquidation},
		{"just above maintenance warns", wadOf(6), KindMarginWarning},
		{"healthy is silent", wadOf(20), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := EvaluatePosition(PositionInput{
				MarketID: "m", Owner: "0xowner", Block: 7,
				Size: wadOf(1), Collateral: c.collateral, IndexPrice: wadOf(100),
				MaintenanceMarginBps: big.NewInt(500), // 5%
			})
			if c.want == "" {
				if len(got) != 0 {
					t.Fatalf("expected silence, got %+v", got)
				}
				return
			}
			if len(got) != 1 || got[0].Kind != c.want {
				t.Fatalf("got %+v want kind %s", got, c.want)
			}
		})
	}
}

func TestLiquidationKeyIsPerPositionPerBlock(t *testing.T) {
	at := func(block uint64, owner string) string {
		return EvaluatePosition(PositionInput{
			MarketID: "m", Owner: owner, Block: block, Size: wadOf(1), Collateral: wadOf(4),
			IndexPrice: wadOf(100), MaintenanceMarginBps: big.NewInt(500),
		})[0].Key
	}
	if at(7, "a") != at(7, "a") {
		t.Fatal("same position at the same block must produce the same key")
	}
	if at(7, "a") == at(8, "a") || at(7, "a") == at(7, "b") {
		t.Fatal("key must vary by block and by owner")
	}
}

func TestEvaluateMarket(t *testing.T) {
	fresh := time.Now()
	base := MarketInput{
		MarketID: "m", Block: 3, LongOI: wadOf(10), ShortOI: wadOf(10), MaxOiPerSide: wadOf(100),
		Utilization: big.NewInt(1e17), MaxUtilization: big.NewInt(8e17),
		OracleUpdatedAt: fresh.Unix(), Now: fresh,
	}
	cases := []struct {
		name string
		in   func(MarketInput) MarketInput
		want []string
	}{
		{"healthy market is silent", func(m MarketInput) MarketInput { return m }, nil},
		{"long side at cap", func(m MarketInput) MarketInput { m.LongOI = wadOf(100); return m }, []string{KindOICap}},
		{"both sides at cap", func(m MarketInput) MarketInput {
			m.LongOI, m.ShortOI = wadOf(100), wadOf(200)
			return m
		}, []string{KindOICap, KindOICap}},
		{"utilization at cap", func(m MarketInput) MarketInput { m.Utilization = big.NewInt(9e17); return m }, []string{KindUtilizationCap}},
		{"oracle past the staleness limit", func(m MarketInput) MarketInput {
			m.OracleUpdatedAt = fresh.Add(-61 * time.Second).Unix()
			return m
		}, []string{KindOracleStale}},
		{"oracle never published", func(m MarketInput) MarketInput { m.OracleUpdatedAt = 0; return m }, []string{KindOracleStale}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := EvaluateMarket(c.in(base))
			if len(got) != len(c.want) {
				t.Fatalf("got %+v want %v", got, c.want)
			}
			for i := range c.want {
				if got[i].Kind != c.want[i] {
					t.Fatalf("alert %d: got %s want %s", i, got[i].Kind, c.want[i])
				}
			}
			seen := map[string]bool{}
			for _, a := range got {
				if seen[a.Key] {
					t.Fatalf("duplicate idempotency key %q", a.Key)
				}
				seen[a.Key] = true
			}
		})
	}
}

type fakeBus struct {
	claimed   map[string]bool
	published []string
	saved     []string
}

func newFakeBus() *fakeBus { return &fakeBus{claimed: map[string]bool{}} }

func (f *fakeBus) sweeper() *Sweeper {
	return &Sweeper{
		Claim: func(_ context.Context, key string) (bool, error) {
			if f.claimed[key] {
				return false, nil
			}
			f.claimed[key] = true
			return true, nil
		},
		Publish: func(_ context.Context, subject, key string, _ []byte) error {
			f.published = append(f.published, subject+"|"+key)
			return nil
		},
		Save: func(_ context.Context, a Alert) error { f.saved = append(f.saved, a.Key); return nil },
	}
}

func TestEmitDedupsWithinOneBlock(t *testing.T) {
	f := newFakeBus()
	s := f.sweeper()
	a := Alert{Key: "liq:m:0xowner:7", Kind: KindLiquidation, MarketID: "m", Owner: "0xowner", Block: 7}

	for i := 0; i < 5; i++ { // a 1s loop sees the same bad position many times per block
		if err := s.Emit(context.Background(), a); err != nil {
			t.Fatalf("emit %d: %v", i, err)
		}
	}
	if len(f.published) != 1 || len(f.saved) != 1 {
		t.Fatalf("published %v saved %v, want one of each", f.published, f.saved)
	}
	if f.published[0] != "risk.liquidation_candidate.v1|liq:m:0xowner:7" {
		t.Fatalf("wrong subject or key: %s", f.published[0])
	}

	next := a
	next.Key = "liq:m:0xowner:8"
	if err := s.Emit(context.Background(), next); err != nil {
		t.Fatal(err)
	}
	if len(f.published) != 2 {
		t.Fatalf("a new block must emit again, got %v", f.published)
	}
}

func TestEmitSendsAlertsToTheAlertSubject(t *testing.T) {
	f := newFakeBus()
	if err := f.sweeper().Emit(context.Background(), Alert{Key: "oracle_stale:m::3", Kind: KindOracleStale}); err != nil {
		t.Fatal(err)
	}
	if len(f.published) != 1 || f.published[0] != SubjRiskAlert+"|oracle_stale:m::3" {
		t.Fatalf("got %v", f.published)
	}
	if len(f.saved) != 1 {
		t.Fatal("every alert must be persisted, not only messaged")
	}
}
