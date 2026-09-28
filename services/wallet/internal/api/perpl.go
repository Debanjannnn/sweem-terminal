package api

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/httpx"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/wad"
	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/executor"
	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/marketdata"
	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/policy"
	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/store"
)

// Perpl perps. The account is the wallet itself: the Exchange trades for
// msg.sender, so the user's own Privy wallet signs every call and there is no
// signer to authorize. Ids, scales and minimums are Perpl's, read live.

const venuePerpl = "perpl"

// perplTypes are the order kinds a perp takes. close reduces a position and
// spends nothing, so it consumes none of the delegation budget.
var perplTypes = []string{"market", "limit", "close"}

func (s *Server) perplAccount(ctx context.Context, u store.User) (executor.PerplAccount, error) {
	return s.Executor.PerplAccount(ctx, executor.PerplAccountRequest{
		PrivyWalletID: u.PrivyWalletID, UserWallet: u.WalletAddress})
}

func (s *Server) getPerplAccount(w http.ResponseWriter, r *http.Request) {
	u, ok := s.caller(w, r)
	if !ok {
		return
	}
	a, err := s.perplAccount(r.Context(), u)
	if err != nil {
		s.log().Error("read perpl account", "user_id", u.ID, "err", err)
		httpx.Fail(w, http.StatusBadGateway, "perpl_account_unknown",
			"Your Perpl account could not be read from the chain right now, so nothing about it can be shown.")
		return
	}
	httpx.OK(w, a)
}

// perplReady is the precondition every perp order clears: collateral on the
// Exchange is what an order margins against, and without it the call reverts.
func (s *Server) perplReady(ctx context.Context, u store.User) *policy.Rejection {
	a, err := s.perplAccount(ctx, u)
	if err != nil {
		s.log().Error("read perpl account", "user_id", u.ID, "err", err)
		return &policy.Rejection{Code: "perpl_account_unknown",
			Reason: "Your Perpl account could not be read, so this service cannot prove it can margin the order and nothing was signed."}
	}
	if !a.Registered {
		return &policy.Rejection{Code: "perpl_account_unfunded",
			Reason: "You have no Perpl account yet. Deposit collateral first with POST /v1/perpl/deposit; the first deposit opens it."}
	}
	if a.Frozen {
		return &policy.Rejection{Code: "perpl_account_frozen", Reason: "Perpl has frozen this account, so it cannot trade."}
	}
	return nil
}

type perplFundsRequest struct {
	// collateral atoms: AUSD has 6 decimals
	Amount         string `json:"amount"`
	IdempotencyKey string `json:"idempotency_key"`
}

func (s *Server) perplDeposit(w http.ResponseWriter, r *http.Request) {
	s.perplFunds(w, r, "perpl_deposit", s.Executor.PerplDeposit)
}

func (s *Server) perplWithdraw(w http.ResponseWriter, r *http.Request) {
	s.perplFunds(w, r, "perpl_withdraw", s.Executor.PerplWithdraw)
}

// a deposit funds the account and a withdrawal empties it; neither spends the
// delegation's budget, and both are signed by the owner in person.
func (s *Server) perplFunds(w http.ResponseWriter, r *http.Request, kind string,
	call func(context.Context, executor.PerplFundsRequest) (executor.RouteResponse, error)) {
	u, ok := s.caller(w, r)
	if !ok {
		return
	}
	var req perplFundsRequest
	if !decode(w, r, &req) {
		return
	}
	amount, err := policy.ParseAmount(req.Amount)
	if err != nil || amount.Sign() == 0 {
		httpx.Fail(w, http.StatusBadRequest, "bad_request",
			"amount must be a positive integer of collateral atoms (AUSD has 6 decimals).")
		return
	}
	in := store.Intent{
		Key:       intentKey(kind, u.ID, amount.String(), strings.TrimSpace(req.IdempotencyKey)),
		UserID:    u.ID,
		MarketID:  venuePerpl,
		Venue:     venuePerpl,
		OrderType: kind,
		Size:      amount.String(),
		ChainID:   s.ChainID,
		State:     "pending",
	}
	resp, execErr := s.perform(r.Context(), in, func(ctx context.Context) (executor.RouteResponse, error) {
		return call(ctx, executor.PerplFundsRequest{
			IntentKey: in.Key, PrivyWalletID: u.PrivyWalletID, UserWallet: u.WalletAddress, Amount: in.Size})
	})
	if execErr != nil {
		httpx.JSON(w, http.StatusBadGateway, httpx.Envelope{Data: resp,
			Error: &httpx.Err{Code: "execution_failed", Message: resp.Error}})
		return
	}
	httpx.OK(w, resp)
}

// cancelPerplOrder names the order by perpl's own id. Ids are recycled, so the
// executor refuses unless the order is still this account's.
func (s *Server) cancelPerplOrder(w http.ResponseWriter, r *http.Request) {
	u, ok := s.caller(w, r)
	if !ok {
		return
	}
	marketID := strings.TrimSpace(r.PathValue("market_id"))
	orderID, err := policy.ParseAmount(r.PathValue("order_id"))
	if err != nil || orderID.Sign() == 0 {
		httpx.Fail(w, http.StatusBadRequest, "bad_request", "order_id must be the positive id Perpl gave the resting order.")
		return
	}
	s.place(w, r, u, store.Intent{
		Key:             intentKey("perpl_cancel", u.ID, marketID, orderID.String()),
		UserID:          u.ID,
		MarketID:        marketID,
		Venue:           venuePerpl,
		OrderType:       "cancel",
		Size:            "0",
		ExpectedOrderID: orderID.String(),
		ChainID:         s.ChainID,
		State:           "pending",
	})
}

// buildPerplIntent validates a perp order. Size is what the order can lock:
// its margin in collateral atoms, notional / leverage. That is the unit the
// delegation caps hold for perpl.
func (s *Server) buildPerplIntent(ctx context.Context, userID string, req orderRequest) (store.Intent, string) {
	if !contains(perplTypes, req.OrderType) {
		return store.Intent{}, fmt.Sprintf("order_type must be market, limit or close for a perp, not %q.", req.OrderType)
	}
	if req.OrderType != "close" && !contains(sides, req.Side) {
		return store.Intent{}, fmt.Sprintf("side must be buy (long) or sell (short), not %q.", req.Side)
	}
	if s.Market == nil {
		return store.Intent{}, "This service has no market-data to read the perp's scales from, so no perp order can be sized."
	}
	m, err := s.Market.Market(ctx, req.MarketID)
	if err != nil || m.Venue != venuePerpl {
		return store.Intent{}, fmt.Sprintf("Market %q is not a Perpl perp this terminal lists.", req.MarketID)
	}
	lot, err := policy.ParseAmount(req.Quantity)
	if req.Quantity == "" && req.OrderType == "close" {
		lot, err = new(big.Int), nil // the whole position
	}
	if err != nil || (lot.Sign() == 0 && req.OrderType != "close") {
		return store.Intent{}, "quantity must be a positive integer of size units: size times 10^sizeDecimals."
	}
	if req.Leverage < 0 {
		return store.Intent{}, "leverage is in hundredths and cannot be negative: 500 is 5x."
	}
	in := store.Intent{
		UserID:         userID,
		MarketID:       m.ID,
		Venue:          venuePerpl,
		Side:           req.Side,
		OrderType:      req.OrderType,
		Quantity:       lot.String(),
		PostOnly:       req.PostOnly,
		Leverage:       req.Leverage,
		MaxSlippageBps: req.MaxSlippageBps,
		ChainID:        s.ChainID,
		State:          "pending",
		Size:           "0",
	}
	if in.MaxSlippageBps == 0 {
		in.MaxSlippageBps = s.DefaultSlippageBps
	}
	switch req.OrderType {
	case "limit":
		price, err := policy.ParseAmount(req.Price)
		if err != nil || price.Sign() == 0 {
			return store.Intent{}, "price must be a positive integer for a limit order: price times 10^priceDecimals."
		}
		in.LimitPrice = price.String()
		in.Size = margin(lot, wad.Scale(price, m.PriceDecimals, wad.Decimals), m, req.Leverage).String()
	case "market":
		mark, ok := new(big.Int).SetString(m.Mark, 10)
		if !ok || mark.Sign() == 0 {
			return store.Intent{}, fmt.Sprintf("Perpl market %s has no mark right now, so this order's margin cannot be measured against your caps.", m.Symbol)
		}
		in.Size = margin(lot, mark, m, req.Leverage).String()
	}
	in.Key = intentKey(userID, in.MarketID, in.Venue, in.Side, in.OrderType, in.Size, in.LimitPrice,
		in.Quantity, fmt.Sprint(in.Leverage), req.IdempotencyKey)
	return in, ""
}

// margin in collateral atoms: lot/10^sd * price * 10^cd / (leverage/100),
// rounded up. An unset leverage counts as 1x, the most it could lock.
func margin(lot, priceWad *big.Int, m marketdata.Market, leverage int) *big.Int {
	if leverage <= 0 {
		leverage = 100
	}
	num := new(big.Int).Mul(lot, priceWad)
	num.Mul(num, pow10(m.QuoteDecimals))
	num.Mul(num, big.NewInt(100))
	den := new(big.Int).Mul(pow10(m.SizeDecimals+wad.Decimals), big.NewInt(int64(leverage)))
	q, rem := new(big.Int).QuoRem(num, den, new(big.Int))
	if rem.Sign() > 0 {
		q.Add(q, big.NewInt(1))
	}
	return q
}

func pow10(n int) *big.Int { return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil) }

// submitPerpl routes a perp intent to its executor endpoint.
func (s *Server) submitPerpl(ctx context.Context, u store.User, in store.Intent) (executor.RouteResponse, error) {
	if in.OrderType == "cancel" {
		return s.Executor.PerplCancel(ctx, executor.PerplCancelRequest{
			IntentKey: in.Key, PrivyWalletID: u.PrivyWalletID, UserWallet: u.WalletAddress,
			MarketID: in.MarketID, OrderID: in.ExpectedOrderID})
	}
	return s.Executor.PerplOrder(ctx, executor.PerplOrderRequest{
		IntentKey: in.Key, PrivyWalletID: u.PrivyWalletID, UserWallet: u.WalletAddress,
		MarketID: in.MarketID, Side: in.Side, Kind: in.OrderType, Lot: in.Quantity,
		Price: in.LimitPrice, Leverage: in.Leverage, PostOnly: in.PostOnly, MaxSlippageBps: in.MaxSlippageBps})
}

// orderRef pulls a resting order's handle out of the executor's answer.
func orderRef(info json.RawMessage) (string, *int) {
	var v struct {
		OrderID any  `json:"order_id"`
		SlotIdx *int `json:"slot_idx"`
	}
	if len(info) == 0 || json.Unmarshal(info, &v) != nil || v.OrderID == nil {
		return "", nil
	}
	id := strings.Trim(fmt.Sprint(v.OrderID), `"`)
	if id == "" || id == "0" || id == "<nil>" {
		return "", nil
	}
	return id, v.SlotIdx
}

// perplPositions renders the account's positions for the portfolio in the
// portfolio's own units: sizes and prices in WAD, the mark as a decimal string
// like every mark from /v1/marks.
func perplPositions(a executor.PerplAccount) []portfolioPosition {
	out := []portfolioPosition{}
	for _, p := range a.Positions {
		pos := portfolioPosition{
			MarketID:   p.MarketID,
			Side:       p.Side,
			Size:       toWad(p.Lot, p.SizeDecimals),
			EntryPrice: toWad(p.EntryPrice, p.PriceDecimals),
			Collateral: toWad(p.Deposit, a.Collateral.Decimals),
			PnL:        toWad(p.PnL, a.Collateral.Decimals),
			Source:     venuePerpl,
			Reconciled: true,
		}
		if p.MarkPrice != nil {
			if n, ok := new(big.Int).SetString(*p.MarkPrice, 10); ok {
				pos.Mark = wad.ToDecimalString(n, p.PriceDecimals)
			}
		}
		if p.LiquidationEst != nil {
			pos.LiquidationEst = toWad(*p.LiquidationEst, p.PriceDecimals)
		}
		out = append(out, pos)
	}
	return out
}

func toWad(v string, decimals int) string {
	n, ok := new(big.Int).SetString(strings.TrimSpace(v), 10)
	if !ok {
		return ""
	}
	return wad.Scale(n, decimals, wad.Decimals).String()
}
