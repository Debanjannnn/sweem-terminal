package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/chains"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/finality"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/httpx"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/natsbus"
	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/executor"
	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/policy"
	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/store"
)

type orderRequest struct {
	MarketID  string `json:"market_id"`
	Venue     string `json:"venue"`
	Side      string `json:"side"`
	OrderType string `json:"order_type"`
	// market only: raw atoms of the input token, quote for a buy, base for a sell
	Size string `json:"size"`
	// limit only: raw tick integer
	Price string `json:"price"`
	// limit only: book units
	Quantity       string `json:"quantity"`
	Tif            string `json:"tif"`
	PostOnly       bool   `json:"post_only"`
	MaxSlippageBps int    `json:"max_slippage_bps"`
	ChainID        int64  `json:"chain_id"`
	IdempotencyKey string `json:"idempotency_key"`
	// perpl only: hundredths, 500 = 5x
	Leverage int `json:"leverage"`
}

type orderResponse struct {
	IntentKey string          `json:"intent_key"`
	State     string          `json:"state"`
	TxHash    string          `json:"tx_hash,omitempty"`
	Steps     []executor.Step `json:"steps,omitempty"`
	Info      json.RawMessage `json:"info,omitempty"`
	Error     string          `json:"error,omitempty"`
	Replayed  bool            `json:"replayed,omitempty"`
}

func (s *Server) createOrder(w http.ResponseWriter, r *http.Request) {
	u, ok := s.caller(w, r)
	if !ok {
		return
	}
	var req orderRequest
	if !decode(w, r, &req) {
		return
	}
	in, reason := s.buildIntent(r.Context(), u.ID, req)
	if reason != "" {
		httpx.Fail(w, http.StatusBadRequest, "bad_request", reason)
		return
	}
	s.place(w, r, u, in)
}

// cancelOrder is slot-indexed because Kuru has no cancel-by-order-id. Slots are
// reused, so the order the caller believes occupies the slot is required.
func (s *Server) cancelOrder(w http.ResponseWriter, r *http.Request) {
	u, ok := s.caller(w, r)
	if !ok {
		return
	}
	marketID := r.PathValue("market_id")
	slot, slotErr := strconv.Atoi(r.PathValue("slot_idx"))
	if slotErr != nil || slot < 0 || slot > maxSlotIdx {
		httpx.Fail(w, http.StatusBadRequest, "bad_request",
			fmt.Sprintf("slot_idx must be a maker slot between 0 and %d.", maxSlotIdx))
		return
	}
	if !s.knownMarket(r.Context(), marketID) {
		httpx.Fail(w, http.StatusBadRequest, "bad_request",
			fmt.Sprintf("Market %q is not on the executor's allowlist, so nothing can be cancelled on it.", marketID))
		return
	}
	expected, err := policy.ParseAmount(r.URL.Query().Get("expected_order_id"))
	if err != nil || expected.Sign() == 0 {
		httpx.Fail(w, http.StatusBadRequest, "bad_request",
			"expected_order_id must name the order this slot holds: slots are reused, and a stale id would cancel whatever moved in.")
		return
	}

	s.place(w, r, u, store.Intent{
		Key:      intentKey("cancel", u.ID, marketID, strconv.Itoa(slot), expected.String()),
		UserID:   u.ID,
		MarketID: marketID,
		Venue:    "kuru",
		// a cancel spends nothing, so it consumes none of the delegation budget
		OrderType:       "cancel",
		Size:            "0",
		SlotIdx:         &slot,
		ExpectedOrderID: expected.String(),
		ChainID:         s.ChainID,
		State:           "pending",
	})
}

// place is the guarded path every trading call takes: the delegation and Kuru
// preconditions first, then one recorded executor call.
func (s *Server) place(w http.ResponseWriter, r *http.Request, u store.User, in store.Intent) {
	rej, err := s.evaluate(r.Context(), u, in)
	if err != nil {
		s.fail(w, "evaluate delegation", err)
		return
	}
	if rej != nil {
		httpx.Fail(w, http.StatusPreconditionFailed, rej.Code, rej.Reason)
		return
	}

	resp, execErr := s.submit(r.Context(), u, in)
	if execErr != nil {
		httpx.JSON(w, http.StatusBadGateway, httpx.Envelope{Data: resp, Error: &httpx.Err{
			Code: "execution_failed", Message: execErr.Error()}})
		return
	}
	httpx.OK(w, resp)
}

func (s *Server) listOrders(w http.ResponseWriter, r *http.Request) {
	u, ok := s.caller(w, r)
	if !ok {
		return
	}
	rows, err := s.Store.Intents(r.Context(), u.ID, limitParam(r))
	if err != nil {
		s.fail(w, "list orders", err)
		return
	}
	httpx.OK(w, map[string]any{"orders": rows})
}

// evaluate runs the real guard. A non-nil rejection is a 412 naming what is
// missing or breached; an error is this service's own fault.
func (s *Server) evaluate(ctx context.Context, u store.User, in store.Intent) (*policy.Rejection, error) {
	ready := s.kuruReady
	if in.Venue == venuePerpl {
		ready = s.perplReady
	}
	if rej := ready(ctx, u); rej != nil {
		return rej, nil
	}
	userID := u.ID
	d, err := s.Store.Delegation(ctx, userID)
	if errors.Is(err, store.ErrNotFound) {
		return &policy.Rejection{Code: "no_delegation",
			Reason: "This account has not delegated a signer, so nothing can be executed for it until you PUT /v1/delegation."}, nil
	}
	if err != nil {
		return nil, err
	}
	spent, err := s.Store.SpentTotal(ctx, userID)
	if err != nil {
		return nil, err
	}
	spentAmount, err := policy.ParseAmount(spent)
	if err != nil {
		return nil, err
	}
	size, err := policy.ParseAmount(in.Size)
	if err != nil {
		return nil, err
	}
	return policy.EvaluateIntent(d, policy.Intent{Venue: in.Venue, MarketID: in.MarketID, Notional: size}, spentAmount, time.Now()), nil
}

// replay returns the recorded outcome of an intent this service already routed.
func (s *Server) replay(ctx context.Context, key string) (orderResponse, bool) {
	in, err := s.Store.Intent(ctx, key)
	if err != nil {
		return orderResponse{}, false
	}
	e, err := s.Store.Execution(ctx, key)
	if err != nil {
		return orderResponse{}, false
	}
	return orderResponse{IntentKey: key, State: in.State, TxHash: e.TxHash, Error: e.Error, Replayed: true}, true
}

// submit is the only path to the executor for a trade, shared by orders and
// schedule slices. An order type picks its endpoint here and nowhere else: a
// limit order routed as a swap would be a market order the user never asked for.
func (s *Server) submit(ctx context.Context, u store.User, in store.Intent) (orderResponse, error) {
	return s.perform(ctx, in, func(ctx context.Context) (executor.RouteResponse, error) {
		if in.Venue == venuePerpl {
			return s.submitPerpl(ctx, u, in)
		}
		marketID, err := s.executorMarket(ctx, in.MarketID)
		if err != nil {
			return executor.RouteResponse{}, err
		}
		switch in.OrderType {
		case "limit":
			return s.Executor.Order(ctx, executor.OrderRequest{
				IntentKey:     in.Key,
				PrivyWalletID: u.PrivyWalletID,
				UserWallet:    u.WalletAddress,
				MarketID:      marketID,
				Side:          in.Side,
				Price:         in.LimitPrice,
				Quantity:      in.Quantity,
				Tif:           in.Tif,
				PostOnly:      in.PostOnly,
				ChainID:       in.ChainID,
			})
		case "cancel":
			return s.Executor.Cancel(ctx, executor.CancelRequest{
				IntentKey:       in.Key,
				PrivyWalletID:   u.PrivyWalletID,
				UserWallet:      u.WalletAddress,
				MarketID:        marketID,
				SlotIdx:         slotOf(in),
				ExpectedOrderID: in.ExpectedOrderID,
				ChainID:         in.ChainID,
			})
		default:
			return s.Executor.Route(ctx, executor.RouteRequest{
				IntentKey:      in.Key,
				PrivyWalletID:  u.PrivyWalletID,
				UserWallet:     u.WalletAddress,
				MarketID:       marketID,
				Venue:          in.Venue,
				Side:           in.Side,
				Amount:         in.Size,
				MaxSlippageBps: in.MaxSlippageBps,
				ChainID:        in.ChainID,
			})
		}
	})
}

// the executor is truthful about what happened; none of these mean the call
// failed. a maker order that did not rest is still a landed transaction.
var landed = []string{"submitted", "confirmed", "resting", "not_resting", "filled_or_expired", "canceled",
	// perpl reads its outcome from the receipt: a landed IOC may match nothing
	"filled", "partial", "not_filled"}

// perform records the intent, replays a retry instead of signing again, calls
// the executor once and records the outcome. Every executor call goes through
// here: a deposit and an authorization are transactions too.
func (s *Server) perform(ctx context.Context, in store.Intent, call func(context.Context) (executor.RouteResponse, error)) (orderResponse, error) {
	created, err := s.Store.InsertIntent(ctx, in)
	if err != nil {
		return orderResponse{}, err
	}
	if !created {
		// the key already exists: this is a retry, not a second transaction
		if done, ok := s.replay(ctx, in.Key); ok {
			return done, nil
		}
		s.log().Warn("intent recorded but never routed, routing now", "intent_key", in.Key)
	}

	resp, err := call(ctx)
	reason := ""
	switch {
	case err != nil:
		reason = err.Error()
	case !contains(landed, resp.Status):
		reason = resp.Error
		if reason == "" {
			reason = fmt.Sprintf("The executor reported status %q for this intent without saying why.", resp.Status)
		}
	}

	if reason != "" {
		s.record(ctx, in, store.Execution{IntentKey: in.Key, Error: reason}, "failed")
		s.emit(ctx, natsbus.SubjExecutionFailed, in, "", reason)
		return orderResponse{IntentKey: in.Key, State: "failed", Steps: resp.Steps, Info: resp.Info, Error: reason},
			errors.New(reason)
	}

	s.record(ctx, in, store.Execution{
		IntentKey:    in.Key,
		TxHash:       resp.TxHash,
		Confirmation: finality.Seen.String(),
	}, resp.Status)
	if id, slot := orderRef(resp.Info); id != "" {
		if err := s.Store.SetOrderRef(ctx, in.Key, id, slot); err != nil {
			s.log().Error("record order ref", "intent_key", in.Key, "err", err)
		}
	}
	s.emit(ctx, natsbus.SubjExecutionPending, in, resp.TxHash, "")
	return orderResponse{IntentKey: in.Key, State: resp.Status, TxHash: resp.TxHash,
		Steps: resp.Steps, Info: resp.Info}, nil
}

func slotOf(in store.Intent) int {
	if in.SlotIdx == nil {
		return 0
	}
	return *in.SlotIdx
}

func (s *Server) record(ctx context.Context, in store.Intent, e store.Execution, state string) {
	if err := s.Store.RecordExecution(ctx, e); err != nil {
		s.log().Error("record execution", "intent_key", in.Key, "err", err)
	}
	if err := s.Store.SetIntentState(ctx, in.Key, state); err != nil {
		s.log().Error("set intent state", "intent_key", in.Key, "state", state, "err", err)
	}
}

type executionEvent struct {
	IntentKey string `json:"intent_key"`
	UserID    string `json:"user_id"`
	MarketID  string `json:"market_id"`
	Venue     string `json:"venue"`
	Side      string `json:"side"`
	Size      string `json:"size"`
	ChainID   int64  `json:"chain_id"`
	TxHash    string `json:"tx_hash,omitempty"`
	Error     string `json:"error,omitempty"`
	Ts        int64  `json:"ts"`
}

func (s *Server) emit(ctx context.Context, subject string, in store.Intent, txHash, errMsg string) {
	if !contains(orderTypes, in.OrderType) {
		return
	}
	payload, err := json.Marshal(executionEvent{
		IntentKey: in.Key, UserID: in.UserID, MarketID: in.MarketID, Venue: in.Venue,
		Side: in.Side, Size: in.Size, ChainID: in.ChainID, TxHash: txHash, Error: errMsg,
		Ts: time.Now().Unix(),
	})
	if err != nil {
		s.log().Error("marshal execution event", "err", err)
		return
	}
	s.publish(ctx, subject, subject+":"+in.Key, payload)
}

var (
	sides      = []string{"buy", "sell"}
	orderTypes = []string{"market", "limit"}
	tifs       = []string{"gtc", "ioc", "fok"}
)

// 62 maker slots per account, 0..61
const maxSlotIdx = 61

// buildIntent validates the request and derives the intent key. The key is the
// double-spend contract, so the client must name its own attempt.
func (s *Server) buildIntent(ctx context.Context, userID string, req orderRequest) (store.Intent, string) {
	req.MarketID = strings.TrimSpace(req.MarketID)
	req.Venue = strings.ToLower(strings.TrimSpace(req.Venue))
	req.Side = strings.ToLower(strings.TrimSpace(req.Side))
	req.OrderType = strings.ToLower(strings.TrimSpace(req.OrderType))
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)

	if req.IdempotencyKey == "" {
		return store.Intent{}, "idempotency_key is required: it is what makes a retry of this order the same order rather than a second one."
	}
	if !policy.KnownVenue(req.Venue) {
		return store.Intent{}, fmt.Sprintf("Venue %q is not one this terminal can route to (%s).", req.Venue, strings.Join(policy.Venues, ", "))
	}
	if req.Venue == venuePerpl {
		return s.buildPerplIntent(ctx, userID, req)
	}
	if !s.knownMarket(ctx, req.MarketID) {
		return store.Intent{}, fmt.Sprintf("Market %q is not on the executor's allowlist, so no order can be routed for it.", req.MarketID)
	}
	if !contains(sides, req.Side) {
		return store.Intent{}, fmt.Sprintf("side must be buy or sell, not %q.", req.Side)
	}
	if req.OrderType == "" {
		req.OrderType = "market"
	}
	if !contains(orderTypes, req.OrderType) {
		return store.Intent{}, fmt.Sprintf("order_type must be market or limit, not %q.", req.OrderType)
	}
	chainID := req.ChainID
	if chainID == 0 {
		chainID = s.ChainID
	}
	if !chains.IsSupported(int(chainID)) {
		return store.Intent{}, fmt.Sprintf("chain_id %d is not a chain this terminal trades on.", chainID)
	}
	bps := req.MaxSlippageBps
	if bps == 0 {
		bps = s.DefaultSlippageBps
	}
	in := store.Intent{
		UserID:         userID,
		MarketID:       req.MarketID,
		Venue:          req.Venue,
		Side:           req.Side,
		OrderType:      req.OrderType,
		MaxSlippageBps: bps,
		ChainID:        chainID,
		State:          "pending",
	}

	if req.OrderType == "limit" {
		reason := s.sizeMaker(ctx, &in, req)
		if reason != "" {
			return store.Intent{}, reason
		}
	} else {
		size, err := policy.ParseAmount(req.Size)
		if err != nil || size.Sign() == 0 {
			return store.Intent{}, "size must be a positive integer of raw atoms of the input token, quote for a buy and base for a sell, because a float would not survive the round trip to the chain."
		}
		in.Size = size.String()
	}

	in.Key = intentKey(userID, req.MarketID, req.Venue, req.Side, req.OrderType,
		in.Size, in.LimitPrice, in.Quantity, in.Tif, req.IdempotencyKey)
	return in, ""
}

// sizeMaker fills in the maker fields and derives Size, the cap denominator: a
// bid can spend its quote notional, an ask can spend its base quantity, and
// both delegation caps are denominated in exactly those raw atoms.
func (s *Server) sizeMaker(ctx context.Context, in *store.Intent, req orderRequest) string {
	price, err := policy.ParseAmount(req.Price)
	if err != nil || price.Sign() == 0 {
		return "price must be a positive raw tick integer for a limit order: displayPrice times the market's price_precision."
	}
	quantity, err := policy.ParseAmount(req.Quantity)
	if err != nil || quantity.Sign() == 0 {
		return "quantity must be a positive integer in book units for a limit order: displayBase times the market's size_precision."
	}
	tif := strings.ToLower(strings.TrimSpace(req.Tif))
	if tif == "" {
		tif = "gtc"
	}
	if !contains(tifs, tif) {
		return fmt.Sprintf("tif must be gtc, ioc or fok, not %q.", tif)
	}
	if req.PostOnly && tif != "gtc" {
		return "post_only is only valid on a gtc order, because an ioc or fok order never rests."
	}

	m, err := s.market(ctx, req.MarketID)
	if err != nil {
		return fmt.Sprintf("The scales of market %q could not be read, so this order's reservation cannot be measured against your caps: %s.", req.MarketID, err)
	}
	spend, err := policy.RawBaseAtoms(m, quantity)
	if req.Side == "buy" {
		spend, err = policy.QuoteNotional(m, price, quantity)
	}
	if err != nil {
		return fmt.Sprintf("This order's reservation could not be computed: %s.", err)
	}
	if spend.Sign() == 0 {
		return "This order rounds to nothing at the market's precision, so it was refused rather than sent as a zero."
	}
	in.LimitPrice, in.Quantity, in.Tif, in.PostOnly = price.String(), quantity.String(), tif, req.PostOnly
	in.Size = spend.String()
	return ""
}

// intentKey is deterministic: the same order retried derives the same key and
// the unique index turns the retry into a no-op.
func intentKey(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:16])
}

func contains(all []string, want string) bool {
	for _, a := range all {
		if a == want {
			return true
		}
	}
	return false
}
