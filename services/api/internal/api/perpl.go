package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/perpl"
	"github.com/snehendu098/sweem-terminal/internal/planner"
	"github.com/snehendu098/sweem-terminal/internal/streams"
)

// perpOf maps a basis asset to its Perpl market (planner's hedge table), in display order.
var perpOf = []struct{ Asset, Perp string }{{"WETH", "ETH"}, {"cbBTC", "BTC"}, {"MON", "MON"}}

func perpFor(asset string) (string, bool) {
	for _, x := range perpOf {
		if x.Asset == asset {
			return x.Perp, true
		}
	}
	return "", false
}

func (s *Server) perplReader() perpl.Reader {
	return perpl.Reader{C: s.Chain, Exchange: s.Plan.Dep.Perpl.Exchange}
}

func funding(r streams.RatesSnapshot, perpID int) (streams.FundingInfo, bool) {
	for _, f := range r.Funding {
		if f.PerpID == perpID {
			return f, true
		}
	}
	return streams.FundingInfo{}, false
}

type PerplMarket struct {
	Asset        string `json:"asset"`
	PerpID       int    `json:"perpId"`
	Name         string `json:"name"`
	Mark         string `json:"mark"`
	Oracle       string `json:"oracle"`
	LongOI       string `json:"longOi"`
	ShortOI      string `json:"shortOi"`
	InitMargin   string `json:"initMargin"`
	MaintMargin  string `json:"maintMargin"`
	FundingAPR   string `json:"fundingApr"`   // short side, latest interval annualised
	FundingAPR7d string `json:"fundingApr7d"` // short side, trailing 7d
}

type PerplMarkets struct {
	GuardBuffer string        `json:"guardBuffer"`
	GateBand    string        `json:"gateBand"`
	Markets     []PerplMarket `json:"markets"`
}

// perplMarkets is the risk inputs the scheduler uses, per hedge market: Perpl contract state (mark, OI, margin
// fractions) plus Perpl API funding from the rates stream.
func (s *Server) perplMarkets(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rates, err := s.Rates.Rates(ctx)
	if err != nil {
		slog.Warn("rates", "err", err) // contract state still useful without funding
	}
	out := PerplMarkets{GuardBuffer: s.GuardBuffer.String(), GateBand: s.GateBand.String(), Markets: []PerplMarket{}}
	for _, x := range perpOf {
		id := s.Plan.Dep.Perpl.PerpIds[x.Perp]
		m, err := s.perplReader().Market(ctx, id)
		if err != nil {
			slog.Error("perpl market", "perp", id, "err", err)
			fail(w, "perpl markets", ErrRPC)
			return
		}
		pm := PerplMarket{Asset: x.Asset, PerpID: id, Name: x.Perp, Mark: m.Mark.String(), Oracle: m.Oracle.String(),
			LongOI: m.LongOI.String(), ShortOI: m.ShortOI.String(), InitMargin: m.Init.Round(6).String(), MaintMargin: m.Maint.Round(6).String()}
		if f, ok := funding(rates, id); ok {
			pm.FundingAPR, pm.FundingAPR7d = f.AprShort.String(), f.AprShort7d.String()
		}
		out.Markets = append(out.Markets, pm)
	}
	reply(w, "perpl markets", out, nil)
}

type PerplGate struct {
	APR          string `json:"apr"`
	Threshold    string `json:"threshold"`
	Band         string `json:"band"`
	LookbackDays int    `json:"lookbackDays"`
	Want         bool   `json:"want"` // basis should be on
}

type PerplRisk struct {
	Asset       string `json:"asset"`
	PerpID      int    `json:"perpId"`
	Name        string `json:"name"`
	Account     uint64 `json:"account"`
	Open        bool   `json:"open"` // the position's basis leg holds a short
	Size        string `json:"size"` // asset units
	Entry       string `json:"entry"`
	Mark        string `json:"mark"`
	MarkValid   bool   `json:"markValid"`
	Collateral  string `json:"collateral"` // USD, all money fields below too
	PnL         string `json:"pnl"`
	Funding     string `json:"funding"`
	Equity      string `json:"equity"`
	Notional    string `json:"notional"`
	Free        string `json:"free"`
	MarginFrac  string `json:"marginFrac"`
	MaintMargin string `json:"maintMargin"`
	GuardAt     string `json:"guardAt"` // margin fraction where the guard fires
	LiqPrice    string `json:"liqPrice"`
	GuardPrice  string `json:"guardPrice"`
	Guard       struct {
		Fire     bool   `json:"fire"`
		CloseAll bool   `json:"closeAll"`
		TopUp    string `json:"topUp"`  // USDC
		LotCut   string `json:"lotCut"` // asset units
	} `json:"guard"`
	Gate *PerplGate `json:"gate"`
	Next string     `json:"next"` // none | guard_top_up | guard_cut | guard_close | gate_close | gate_open
}

// positionPerpl evaluates each basis sleeve of a position exactly as the scheduler does: live Perpl short, liquidation
// guard against the wallet's USDC, funding gate against trailing APR. Read-only.
func (s *Server) positionPerpl(w http.ResponseWriter, r *http.Request) {
	pid, ok := id(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	pos, st, err := s.Store.Position(ctx, did(r), pid)
	if err != nil {
		fail(w, "position perpl", err)
		return
	}
	parts := st.Parts()
	legs, err := planner.Split(parts, pos.Legs)
	if err != nil {
		fail(w, "position perpl", err)
		return
	}
	out := []PerplRisk{}
	for k, pt := range parts {
		for i, src := range pt.Sources {
			if src.Kind != "basis" {
				continue
			}
			v, err := s.perplRisk(ctx, pos.Wallet, st.Part(pt), legs[k][i])
			if err != nil {
				slog.Error("perpl risk", "position", pid, "err", err)
				fail(w, "position perpl", ErrRPC)
				return
			}
			out = append(out, v)
		}
	}
	reply(w, "position perpl", out, nil)
}

func (s *Server) perplRisk(ctx context.Context, wallet string, st planner.Strategy, leg planner.LegState) (PerplRisk, error) {
	name, ok := perpFor(st.Asset)
	if !ok {
		return PerplRisk{}, fmt.Errorf("no perp for %s", st.Asset)
	}
	id := s.Plan.Dep.Perpl.PerpIds[name]
	p, err := s.perplReader().Position(ctx, wallet, id)
	if err != nil {
		return PerplRisk{}, err
	}
	lot, _ := decimal.NewFromString(leg.Lot)
	v := PerplRisk{Asset: st.Asset, PerpID: id, Name: name, Account: p.Account, Open: lot.IsPositive(), MarkValid: p.MarkValid,
		Free: p.Free.Shift(-6).String(), Next: "none"}
	if p.Lot.IsPositive() {
		usd := func(x decimal.Decimal) string { return x.Shift(-6).StringFixed(2) }
		px := func(x decimal.Decimal) string { return x.Shift(-p.PriceDec).String() }
		equity, notional, maint := p.Deposit.Add(p.PnL), p.Notional(), p.Maint()
		v.Size, v.Entry, v.Mark = p.Lot.Shift(-p.LotDec).String(), px(p.Entry), px(p.Mark)
		v.Collateral, v.PnL, v.Funding, v.Equity, v.Notional = usd(p.Deposit), usd(p.PnL), usd(p.Funding), usd(equity), usd(notional)
		v.MarginFrac, v.MaintMargin, v.GuardAt = equity.Div(notional).Round(6).String(), maint.Round(6).String(), maint.Add(s.GuardBuffer).Round(6).String()
		v.LiqPrice, v.GuardPrice = p.LiqPrice(maint).StringFixed(2), p.LiqPrice(maint.Add(s.GuardBuffer)).StringFixed(2)
	}

	if v.Open {
		usdc, err := planner.WalletBalance(ctx, s.Chain, s.Plan.Dep, wallet, "USDC")
		if err != nil {
			return v, err
		}
		g := perpl.Guard(p, s.GuardBuffer, usdc)
		v.Guard.Fire, v.Guard.CloseAll, v.Guard.TopUp, v.Guard.LotCut = g.Fire, g.CloseAll, g.TopUp.Shift(-6).String(), g.LotCut.Shift(-p.LotDec).String()
		switch {
		case g.CloseAll || (g.Fire && g.LotCut.GreaterThanOrEqual(lot)):
			v.Next = "guard_close"
		case g.Fire && g.LotCut.IsPositive():
			v.Next = "guard_cut"
		case g.Fire:
			v.Next = "guard_top_up"
		}
	}

	if fg := st.Rules.FundingGate; fg != nil {
		apr, err := s.gateAPR(ctx, id, fg.LookbackDays)
		if err != nil {
			return v, err
		}
		th, _ := decimal.NewFromString(fg.APR)
		want := perpl.Gate(v.Open, apr, th, s.GateBand)
		v.Gate = &PerplGate{apr.Round(6).String(), th.String(), s.GateBand.String(), fg.LookbackDays, want}
		if v.Next == "none" && want != v.Open {
			v.Next = map[bool]string{true: "gate_open", false: "gate_close"}[want]
		}
	}
	return v, nil
}

// gateAPR mirrors the scheduler's fundingAPR: trailing daily average over lookbackDays, else the live 7d APR.
func (s *Server) gateAPR(ctx context.Context, perpID, days int) (decimal.Decimal, error) {
	if days > 0 {
		if apr, err := s.Store.FundingAPR(ctx, perpID, days); err != nil || apr != nil {
			return deref(apr), err
		}
	}
	rates, err := s.Rates.Rates(ctx)
	if err != nil {
		return decimal.Zero, err
	}
	if f, ok := funding(rates, perpID); ok {
		return f.AprShort7d, nil
	}
	return decimal.Zero, errors.New("funding missing")
}

func deref(d *decimal.Decimal) decimal.Decimal {
	if d == nil {
		return decimal.Zero
	}
	return *d
}
