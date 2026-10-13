package sched

import (
	"context"
	_ "embed"

	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/config"
	"github.com/snehendu098/sweem-terminal/internal/perpl"
	"github.com/snehendu098/sweem-terminal/internal/planner"
)

// RPC is Chain over an eth client (*ethclient.Client).
type RPC struct {
	C   planner.Chain
	Dep *config.Deployments143
}

func (r RPC) Wallet(ctx context.Context, wallet, sym string) (decimal.Decimal, error) {
	return planner.WalletBalance(ctx, r.C, r.Dep, wallet, sym)
}

func (r RPC) Balances(ctx context.Context, wallet string, s planner.Strategy, legs []planner.LegState, peers []planner.Peer) ([]planner.Balance, error) {
	return planner.Balances(ctx, r.C, r.Dep, wallet, s, legs, peers)
}

func (r RPC) Live(ctx context.Context, wallet string, s planner.Strategy, legs []planner.LegState, peers []planner.Peer) (planner.Live, error) {
	return planner.ReadLive(ctx, r.C, r.Dep, wallet, s, legs, peers)
}

// Perp reads the wallet's short on perpID.
func (r RPC) Perp(ctx context.Context, wallet string, perpID int) (perpl.Perp, error) {
	return perpl.Reader{C: r.C, Exchange: r.Dep.Perpl.Exchange}.Position(ctx, wallet, perpID)
}
