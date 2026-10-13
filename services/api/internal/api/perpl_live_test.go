//go:build live

package api

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/planner"
	"github.com/snehendu098/sweem-terminal/internal/streams"
)

type liveRates struct{}

func (liveRates) Rates(context.Context) (streams.RatesSnapshot, error) {
	return streams.RatesSnapshot{Funding: []streams.FundingInfo{{PerpID: 20, AprShort7d: decimal.RequireFromString("0.07")}}}, nil
}

// Evaluates a real third-party mainnet ETH short (account 533) as if it were a position's basis leg. Read-only.
func TestLivePerplRisk(t *testing.T) {
	c, err := ethclient.Dial("https://rpc.monad.xyz")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Chain: c, Plan: &planner.Planner{Dep: testDep(t)}, Rates: liveRates{},
		GuardBuffer: decimal.RequireFromString("0.03"), GateBand: decimal.RequireFromString("0.02")}
	st := planner.Strategy{Asset: "WETH", Sources: []planner.Source{{Kind: "basis", Venue: "perpl", WeightBps: 10000}},
		Rules: planner.Rules{FundingGate: &planner.Gate{APR: "0.10"}}}
	v, err := s.perplRisk(context.Background(), "0xaAcFd199e3e5c1f750978a3b6ca4E9150571B9A2", st, planner.LegState{Lot: "660"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.MarshalIndent(v, "", "  ")
	t.Log(string(b))
	if v.Account != 533 || v.Size != "0.66" || v.LiqPrice == "" || v.Gate == nil || v.Gate.Want || v.Next != "gate_close" {
		t.Fatal("unexpected")
	}
}
