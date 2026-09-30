package api

import (
	"context"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/httpx"
	"github.com/snehendu098/sweem-terminal/internal/planner"
)

func (s *Server) markets(w http.ResponseWriter, _ *http.Request) {
	type market struct {
		Symbol         string `json:"symbol"`
		Orderbook      string `json:"orderbook"`
		Base           string `json:"base"`
		Quote          string `json:"quote"`
		PricePrecision uint64 `json:"pricePrecision"`
		SizePrecision  uint64 `json:"sizePrecision"`
		TickSize       uint64 `json:"tickSize"`
		MinSize        uint64 `json:"minSize"`
	}
	ms := []market{}
	for sym, m := range s.Plan.Dep.Kuru.Markets {
		ms = append(ms, market{sym, strings.ToLower(m.Orderbook), m.Base, m.Quote, m.PricePrecision, m.SizePrecision, m.TickSize, m.MinSize})
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].Symbol < ms[j].Symbol })
	httpx.JSON(w, http.StatusOK, map[string]any{"tokens": s.Plan.Dep.Tokens, "markets": ms})
}

func (s *Server) publicBaskets(w http.ResponseWriter, r *http.Request) {
	v, err := s.Store.BasketDefs(r.Context(), "")
	reply(w, "baskets", v, err)
}

func (s *Server) publicBasket(w http.ResponseWriter, r *http.Request) {
	if id, ok := id(w, r); ok {
		v, err := s.Store.BasketDef(r.Context(), "", id)
		reply(w, "basket", v, err)
	}
}

func (s *Server) myBaskets(w http.ResponseWriter, r *http.Request) {
	v, err := s.Store.BasketDefs(r.Context(), did(r))
	reply(w, "my baskets", v, err)
}

type BasketInput struct {
	Name   string        `json:"name"`
	Legs   []planner.Leg `json:"legs"`
	Public bool          `json:"public"`
}

// saveBasket creates (no id) or updates (own id) a basket definition.
func (s *Server) saveBasket(w http.ResponseWriter, r *http.Request) {
	bid := r.PathValue("id")
	if bid != "" {
		var ok bool
		if bid, ok = id(w, r); !ok {
			return
		}
	}
	var in BasketInput
	if !decode(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 64 {
		httpx.Error(w, http.StatusBadRequest, "invalid_name")
		return
	}
	if err := s.Plan.ValidateLegs(in.Legs); err != nil {
		fail(w, "legs", err)
		return
	}
	v, err := s.Store.SaveBasket(r.Context(), did(r), bid, in)
	if err != nil {
		fail(w, "save basket", err)
		return
	}
	status := http.StatusOK
	if bid == "" {
		status = http.StatusCreated
	}
	httpx.JSON(w, status, v)
}

func (s *Server) deleteBasket(w http.ResponseWriter, r *http.Request) {
	if id, ok := id(w, r); ok {
		err := s.Store.DeleteBasket(r.Context(), did(r), id)
		reply(w, "delete basket", map[string]bool{"deleted": true}, err)
	}
}

// launch inserts a plain execution and starts it.
func (s *Server) launch(w http.ResponseWriter, r *http.Request, e NewExec, extra map[string]any, after func(context.Context, bool)) {
	execID, err := s.Store.InsertExecution(r.Context(), did(r), e)
	if err != nil {
		fail(w, "insert execution", err)
		return
	}
	s.start(execID, after)
	accepted(w, execID, e.Kind, e.Steps, extra)
}

func (s *Server) trade(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Market, Side, Amount string
		SlippageBps          *int
	}
	if !decode(w, r, &in) {
		return
	}
	m, okm := s.Plan.Dep.Kuru.Markets[in.Market]
	amt, oka := human(in.Amount)
	sl, oks := slip(in.SlippageBps)
	if !okm || !oka || !oks || (in.Side != "buy" && in.Side != "sell") {
		httpx.Error(w, http.StatusBadRequest, "bad_request")
		return
	}
	if _, err := s.Store.Wallet(r.Context(), did(r)); err != nil {
		fail(w, "wallet", err)
		return
	}
	var steps []planner.Step
	var err error
	if in.Side == "buy" {
		atoms, _ := s.Plan.ToAtoms(m.Quote, amt)
		steps, err = s.Plan.MarketBuy(r.Context(), in.Market, atoms, sl)
	} else {
		atoms, _ := s.Plan.ToAtoms(m.Base, amt)
		steps, err = s.Plan.MarketSell(r.Context(), in.Market, atoms, sl)
	}
	if err != nil {
		fail(w, "plan trade", err)
		return
	}
	s.launch(w, r, NewExec{Kind: "trade", Steps: steps}, nil, nil)
}

func (s *Server) basketBuy(w http.ResponseWriter, r *http.Request) {
	bid, ok := id(w, r)
	var in struct {
		AmountUsdc  string
		SlippageBps *int
	}
	if !ok || !decode(w, r, &in) {
		return
	}
	amt, oka := human(in.AmountUsdc)
	sl, oks := slip(in.SlippageBps)
	if !oka || !oks {
		httpx.Error(w, http.StatusBadRequest, "bad_request")
		return
	}
	b, err := s.Store.BasketDef(r.Context(), did(r), bid)
	if err == nil {
		_, err = s.Store.Wallet(r.Context(), did(r))
	}
	if err != nil {
		fail(w, "basket", err)
		return
	}
	atoms, _ := s.Plan.ToAtoms("USDC", amt)
	steps, err := s.Plan.BasketBuy(r.Context(), b.Legs, atoms, sl)
	if err != nil {
		fail(w, "plan basket buy", err)
		return
	}
	s.launch(w, r, NewExec{Kind: "basket_buy", Steps: steps, BasketID: &bid}, nil, nil)
}

func (s *Server) basketSell(w http.ResponseWriter, r *http.Request) {
	bid, ok := id(w, r)
	var in struct {
		Bps         *int
		SlippageBps *int
	}
	if !ok || !decode(w, r, &in) {
		return
	}
	sl, oks := slip(in.SlippageBps)
	part := 10_000
	if in.Bps != nil {
		part = *in.Bps
	}
	if !oks || part < 1 || part > 10_000 {
		httpx.Error(w, http.StatusBadRequest, "bad_request")
		return
	}
	ctx := r.Context()
	b, err := s.Store.BasketDef(ctx, did(r), bid)
	var wallet string
	if err == nil {
		wallet, err = s.Store.Wallet(ctx, did(r))
	}
	var plans [][]byte
	if err == nil {
		plans, err = s.Store.BasketPlans(ctx, did(r), bid)
	}
	if err != nil {
		fail(w, "basket", err)
		return
	}
	held := planner.Attribute(plans)
	var legs []planner.SellLeg
	for _, l := range b.Legs {
		amt := held[l.Market].Mul(decimal.NewFromInt(int64(part))).Div(decimal.NewFromInt(10_000)).Floor()
		if !amt.IsPositive() {
			continue
		}
		bal, err := planner.WalletBalance(ctx, s.Chain, s.Plan.Dep, wallet, s.Plan.Dep.Kuru.Markets[l.Market].Base)
		if err != nil {
			slog.Error("rpc balance", "err", err)
			httpx.Error(w, http.StatusBadGateway, "rpc_unavailable")
			return
		}
		legs = append(legs, planner.SellLeg{Market: l.Market, Amount: decimal.Min(amt, bal)})
	}
	steps, err := s.Plan.BasketSell(ctx, legs, sl)
	if err != nil {
		fail(w, "plan basket sell", err)
		return
	}
	s.launch(w, r, NewExec{Kind: "basket_sell", Steps: steps, BasketID: &bid}, nil, nil)
}

func (s *Server) margin(deposit bool) http.HandlerFunc {
	kind := "margin_withdraw"
	if deposit {
		kind = "margin_deposit"
	}
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Token, Amount string }
		if !decode(w, r, &in) {
			return
		}
		amt, ok := human(in.Amount)
		atoms, err := s.Plan.ToAtoms(in.Token, amt)
		if !ok || err != nil {
			httpx.Error(w, http.StatusBadRequest, "bad_request")
			return
		}
		if _, err := s.Store.Wallet(r.Context(), did(r)); err != nil {
			fail(w, "wallet", err)
			return
		}
		steps, err := s.Plan.Margin(deposit, in.Token, atoms)
		if err != nil {
			fail(w, "plan margin", err)
			return
		}
		s.launch(w, r, NewExec{Kind: kind, Steps: steps}, nil, nil)
	}
}

func (s *Server) placeOrder(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Market, Side, Price, Size string
		PostOnly, Fund            *bool
	}
	if !decode(w, r, &in) {
		return
	}
	px, okp := human(in.Price)
	sz, oks := human(in.Size)
	if !okp || !oks {
		httpx.Error(w, http.StatusBadRequest, "bad_request")
		return
	}
	postOnly := in.PostOnly == nil || *in.PostOnly
	steps, err := s.Plan.Limit(in.Market, in.Side, px, sz, postOnly, in.Fund != nil && *in.Fund)
	if err == nil {
		_, err = s.Store.Wallet(r.Context(), did(r))
	}
	if err != nil {
		fail(w, "plan limit", err)
		return
	}
	lim := steps[len(steps)-1]
	orderID, execID, err := s.Store.CreateOrder(r.Context(), did(r), Order{Market: in.Market, Side: in.Side, Price: lim.Price, Size: lim.Size},
		NewExec{Kind: "limit", Steps: steps})
	if err != nil {
		fail(w, "create order", err)
		return
	}
	s.start(execID, func(ctx context.Context, ok bool) {
		status := "failed"
		if ok {
			status = "open"
		}
		if err := s.Store.SetOrderStatus(ctx, orderID, status); err != nil {
			slog.Error("order status", "err", err)
		}
	})
	accepted(w, execID, "limit", steps, map[string]any{"orderId": orderID})
}

func (s *Server) orders(w http.ResponseWriter, r *http.Request) {
	v, err := s.Store.Orders(r.Context(), did(r))
	for i := range v {
		v[i].human(s.Plan)
	}
	reply(w, "orders", v, err)
}

func (s *Server) cancelOrder(w http.ResponseWriter, r *http.Request) {
	oid, ok := id(w, r)
	if !ok {
		return
	}
	o, err := s.Store.Order(r.Context(), did(r), oid)
	if err == nil && (o.KuruOrderID == nil || o.Status != "open") {
		err = ErrOrderNotOpen
	}
	var steps []planner.Step
	if err == nil {
		steps, err = s.Plan.Cancel(o.Market, []string{*o.KuruOrderID})
	}
	if err != nil {
		fail(w, "cancel order", err)
		return
	}
	s.launch(w, r, NewExec{Kind: "cancel", Steps: steps}, map[string]any{"orderId": oid}, func(ctx context.Context, ok bool) {
		if ok {
			if err := s.Store.SetOrderStatus(ctx, oid, "cancelled"); err != nil {
				slog.Error("order status", "err", err)
			}
		}
	})
}

type TokenBalance struct {
	Symbol  string `json:"symbol"`
	Address string `json:"address"`
	Raw     string `json:"raw"`
	Amount  string `json:"amount"`
}

type LegHolding struct {
	Market string `json:"market"`
	Raw    string `json:"raw"`
	Amount string `json:"amount"`
}

type BasketHolding struct {
	BasketID string       `json:"basketId"`
	Name     string       `json:"name"`
	Legs     []LegHolding `json:"legs"`
}

func (s *Server) holdings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	wallet, err := s.Store.Wallet(ctx, did(r))
	var att map[string][][]byte
	var names map[string]string
	if err == nil {
		att, names, err = s.Store.Attributions(ctx, did(r))
	}
	if err != nil {
		fail(w, "holdings", err)
		return
	}
	dep := s.Plan.Dep
	syms := make([]string, 0, len(dep.Tokens))
	for sym := range dep.Tokens {
		syms = append(syms, sym)
	}
	sort.Strings(syms)
	toks := []TokenBalance{}
	for _, sym := range syms {
		// ponytail: one RPC call per token; batch (multicall) when the token list grows
		raw, err := planner.WalletBalance(ctx, s.Chain, dep, wallet, sym)
		if err != nil {
			slog.Error("rpc balance", "token", sym, "err", err)
			httpx.Error(w, http.StatusBadGateway, "rpc_unavailable")
			return
		}
		t := dep.Tokens[sym]
		toks = append(toks, TokenBalance{sym, strings.ToLower(t.Address), raw.String(), raw.Shift(-int32(t.Decimals)).String()})
	}
	bs := []BasketHolding{}
	for bid, plans := range att {
		h := BasketHolding{BasketID: bid, Name: names[bid], Legs: []LegHolding{}}
		for m, v := range planner.Attribute(plans) {
			h.Legs = append(h.Legs, LegHolding{m, v.String(), v.Shift(-int32(dep.Tokens[dep.Kuru.Markets[m].Base].Decimals)).String()})
		}
		sort.Slice(h.Legs, func(i, j int) bool { return h.Legs[i].Market < h.Legs[j].Market })
		if len(h.Legs) > 0 {
			bs = append(bs, h)
		}
	}
	sort.Slice(bs, func(i, j int) bool { return bs[i].BasketID < bs[j].BasketID })
	httpx.JSON(w, http.StatusOK, map[string]any{"wallet": wallet, "tokens": toks, "baskets": bs})
}
