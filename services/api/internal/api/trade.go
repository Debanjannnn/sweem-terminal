package api

import (
	"context"
	"errors"
	"log/slog"
	"math/big"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/google/uuid"
	"github.com/snehendu098/sweem-terminal/internal/config"

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

// TradeInput is the body of /me/trade, /me/baskets/{id}/buy|sell and /me/trade/prepare (Kind picks which).
type TradeInput struct {
	Kind        string `json:"kind"` // trade | basket_buy | basket_sell
	Market      string `json:"market"`
	Side        string `json:"side"`
	Amount      string `json:"amount"`
	BasketID    string `json:"basketId"`
	AmountUsdc  string `json:"amountUsdc"`
	Bps         *int   `json:"bps"`
	SlippageBps *int   `json:"slippageBps"`
	Wallet      string `json:"wallet"` // prepare only: the Privy embedded wallet that will sign
}

var errBad = &planner.Error{Code: "bad_request", Detail: "invalid trade input"}

// planTrade turns a trade input into a plan for wallet; basket runs return their basket id for attribution.
func (s *Server) planTrade(ctx context.Context, me, wallet string, in TradeInput) ([]planner.Step, *string, error) {
	sl, ok := slip(in.SlippageBps)
	if !ok {
		return nil, nil, errBad
	}
	switch in.Kind {
	case "trade":
		m, okm := s.Plan.Dep.Kuru.Markets[in.Market]
		amt, oka := human(in.Amount)
		if !okm || !oka {
			return nil, nil, errBad
		}
		switch in.Side {
		case "buy":
			atoms, _ := s.Plan.ToAtoms(m.Quote, amt)
			steps, err := s.Plan.MarketBuy(ctx, in.Market, atoms, sl)
			return steps, nil, err
		case "sell":
			atoms, _ := s.Plan.ToAtoms(m.Base, amt)
			steps, err := s.Plan.MarketSell(ctx, in.Market, atoms, sl)
			return steps, nil, err
		}
		return nil, nil, errBad
	case "basket_buy", "basket_sell":
		if uuid.Validate(in.BasketID) != nil {
			return nil, nil, errBad
		}
		bid := in.BasketID
		b, err := s.Store.BasketDef(ctx, me, bid)
		if err != nil {
			return nil, nil, err
		}
		if in.Kind == "basket_buy" {
			amt, ok := human(in.AmountUsdc)
			if !ok {
				return nil, nil, errBad
			}
			atoms, _ := s.Plan.ToAtoms("USDC", amt)
			steps, err := s.Plan.BasketBuy(ctx, b.Legs, atoms, sl)
			return steps, &bid, err
		}
		part := 10_000
		if in.Bps != nil {
			part = *in.Bps
		}
		if part < 1 || part > 10_000 {
			return nil, nil, errBad
		}
		fills, err := s.Store.BasketFills(ctx, me, wallet, bid)
		if err != nil {
			return nil, nil, err
		}
		held := s.Plan.AttributeFills(fills)
		var legs []planner.SellLeg
		for _, l := range b.Legs {
			amt := held[l.Market].Amount.Mul(decimal.NewFromInt(int64(part))).Div(decimal.NewFromInt(10_000)).Floor()
			if !amt.IsPositive() {
				continue
			}
			bal, err := planner.WalletBalance(ctx, s.Chain, s.Plan.Dep, wallet, s.Plan.Dep.Kuru.Markets[l.Market].Base)
			if err != nil {
				slog.Error("rpc balance", "err", err)
				return nil, nil, ErrRPC
			}
			legs = append(legs, planner.SellLeg{Market: l.Market, Amount: decimal.Min(amt, bal)})
		}
		steps, err := s.Plan.BasketSell(ctx, legs, sl)
		return steps, &bid, err
	}
	return nil, nil, errBad
}

// delegatedTrade runs a trade on the executor from the delegated wallet.
func (s *Server) delegatedTrade(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in TradeInput
		if !decode(w, r, &in) {
			return
		}
		in.Kind = kind
		if kind != "trade" {
			var ok bool
			if in.BasketID, ok = id(w, r); !ok {
				return
			}
		}
		wallet, err := s.Store.Wallet(r.Context(), did(r))
		var steps []planner.Step
		var bid *string
		if err == nil {
			steps, bid, err = s.planTrade(r.Context(), did(r), wallet, in)
		}
		if err != nil {
			fail(w, "plan "+kind, err)
			return
		}
		s.launch(w, r, NewExec{Kind: kind, Steps: steps, BasketID: bid}, nil, nil)
	}
}

const prepareTTL = 2 * time.Minute

// prepare plans a client-signed trade: ordered unsigned txs for the user's own Privy wallet, no delegation needed.
func (s *Server) prepare(w http.ResponseWriter, r *http.Request) {
	var in TradeInput
	if !decode(w, r, &in) {
		return
	}
	if !isAddress(in.Wallet) {
		httpx.Error(w, http.StatusBadRequest, "invalid_wallet")
		return
	}
	ctx, me, wallet := r.Context(), did(r), strings.ToLower(in.Wallet)
	steps, bid, err := s.planTrade(ctx, me, wallet, in)
	if err != nil {
		fail(w, "plan prepare", err)
		return
	}
	txs, err := s.Plan.Render(common.HexToAddress(wallet), steps, planner.ChainAllowance(ctx, s.Chain, common.HexToAddress(wallet)))
	if err != nil {
		fail(w, "render", err)
		return
	}
	expires := time.Now().Add(prepareTTL).UTC().Truncate(time.Second)
	pid, err := s.Store.CreatePrepare(ctx, me, Prepare{Wallet: wallet, Kind: in.Kind, BasketID: bid, Steps: steps, Txs: txs, ExpiresAt: expires})
	if err != nil {
		fail(w, "create prepare", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"prepareId": pid, "kind": in.Kind, "steps": steps, "txs": txs, "expiresAt": expires})
}

// TxReader is the RPC surface record needs (*ethclient.Client satisfies it).
type TxReader interface {
	TransactionByHash(ctx context.Context, hash common.Hash) (*types.Transaction, bool, error)
	TransactionReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error)
}

// record stores a client-signed run as an execution after checking every hash on chain: sent by the prepared wallet,
// same to/data/value as prepared, in order. A prefix is fine (the client stopped at a revert); a reverted tx ends the run.
func (s *Server) record(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PrepareID string   `json:"prepareId"`
		TxHashes  []string `json:"txHashes"`
	}
	if !decode(w, r, &in) {
		return
	}
	ctx, me := r.Context(), did(r)
	if uuid.Validate(in.PrepareID) != nil || len(in.TxHashes) == 0 {
		httpx.Error(w, http.StatusBadRequest, "bad_request")
		return
	}
	pr, err := s.Store.Prepare(ctx, me, in.PrepareID)
	if err != nil {
		fail(w, "prepare", err)
		return
	}
	if pr.ExecutionID != nil {
		v, err := s.Store.Execution(ctx, me, *pr.ExecutionID)
		reply(w, "execution", v, err)
		return
	}
	if len(in.TxHashes) > len(pr.Txs) {
		httpx.Error(w, http.StatusBadRequest, "tx_mismatch")
		return
	}
	signer := types.LatestSignerForChainID(big.NewInt(config.MainnetChainID))
	stepHash := make([]string, len(pr.Steps))
	status, failed := "confirmed", false
	for i, h := range in.TxHashes {
		want := pr.Txs[i]
		hash := common.HexToHash(h)
		tx, pending, err := s.Txs.TransactionByHash(ctx, hash)
		if errors.Is(err, ethereum.NotFound) || pending {
			httpx.Error(w, http.StatusConflict, "tx_pending")
			return
		}
		if err != nil {
			slog.Error("tx lookup", "err", err)
			httpx.Error(w, http.StatusBadGateway, "rpc_unavailable")
			return
		}
		from, err := types.Sender(signer, tx)
		if err != nil || tx.To() == nil || !strings.EqualFold(from.Hex(), pr.Wallet) || !strings.EqualFold(tx.To().Hex(), want.To) ||
			hexutil.Encode(tx.Data()) != want.Data || hexutil.EncodeBig(tx.Value()) != want.Value {
			httpx.Error(w, http.StatusBadRequest, "tx_mismatch")
			return
		}
		rc, err := s.Txs.TransactionReceipt(ctx, hash)
		if errors.Is(err, ethereum.NotFound) {
			httpx.Error(w, http.StatusConflict, "tx_pending")
			return
		}
		if err != nil {
			httpx.Error(w, http.StatusBadGateway, "rpc_unavailable")
			return
		}
		if rc.Status != types.ReceiptStatusSuccessful {
			status, failed = "failed", true
			break
		}
		// the plan step's own tx (not its approve) carries the fills
		if i == len(pr.Txs)-1 || pr.Txs[i+1].Step != want.Step {
			stepHash[want.Step] = strings.ToLower(h)
		}
	}
	done := 0
	for done < len(stepHash) && stepHash[done] != "" {
		done++
	}
	if !failed && done < len(pr.Steps) {
		status = "failed" // client stopped early
	}
	execID, err := s.Store.RecordPrepare(ctx, me, pr, status, stepHash[:done])
	if err != nil {
		fail(w, "record", err)
		return
	}
	v, err := s.Store.Execution(ctx, me, execID)
	reply(w, "execution", v, err)
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
	Market      string `json:"market"`
	Raw         string `json:"raw"`
	Amount      string `json:"amount"`
	Provisional bool   `json:"provisional"` // some buy/sell not indexed yet: planned amount stands in for its fill
}

type BasketHolding struct {
	BasketID string       `json:"basketId"`
	Name     string       `json:"name"`
	Legs     []LegHolding `json:"legs"`
}

func (s *Server) holdings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	wallet, err := s.Store.KnownWallet(ctx, did(r))
	var att map[string][]planner.ExecFills
	var names map[string]string
	if err == nil {
		att, names, err = s.Store.Attributions(ctx, did(r), wallet)
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
	for bid, execs := range att {
		h := BasketHolding{BasketID: bid, Name: names[bid], Legs: []LegHolding{}}
		for m, v := range s.Plan.AttributeFills(execs) {
			h.Legs = append(h.Legs, LegHolding{m, v.Amount.String(), v.Amount.Shift(-int32(dep.Tokens[dep.Kuru.Markets[m].Base].Decimals)).String(), v.Provisional})
		}
		sort.Slice(h.Legs, func(i, j int) bool { return h.Legs[i].Market < h.Legs[j].Market })
		if len(h.Legs) > 0 {
			bs = append(bs, h)
		}
	}
	sort.Slice(bs, func(i, j int) bool { return bs[i].BasketID < bs[j].BasketID })
	httpx.JSON(w, http.StatusOK, map[string]any{"wallet": wallet, "tokens": toks, "baskets": bs})
}
