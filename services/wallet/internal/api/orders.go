package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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
	MarketID       string `json:"market_id"`
	Venue          string `json:"venue"`
	Side           string `json:"side"`
	OrderType      string `json:"order_type"`
	Size           string `json:"size"`
	LimitPrice     string `json:"limit_price"`
	MaxSlippageBps int    `json:"max_slippage_bps"`
	ChainID        int64  `json:"chain_id"`
	IdempotencyKey string `json:"idempotency_key"`
}

type orderResponse struct {
	IntentKey string          `json:"intent_key"`
	State     string          `json:"state"`
	TxHash    string          `json:"tx_hash,omitempty"`
	Steps     []executor.Step `json:"steps,omitempty"`
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

	rej, err := s.evaluate(r.Context(), u.ID, in)
	if err != nil {
		s.fail(w, "evaluate delegation", err)
		return
	}
	if rej != nil {
		httpx.Fail(w, http.StatusPreconditionFailed, rej.Code, rej.Reason)
		return
	}

	created, err := s.Store.InsertIntent(r.Context(), in)
	if err != nil {
		s.fail(w, "insert intent", err)
		return
	}
	if !created {
		// the key already exists: this is a retry, not a second order
		if done, ok := s.replay(r.Context(), in.Key); ok {
			httpx.OK(w, done)
			return
		}
		s.log().Warn("intent recorded but never routed, routing now", "intent_key", in.Key)
	}

	resp, execErr := s.submit(r.Context(), u, in)
	if execErr != nil {
		httpx.JSON(w, http.StatusBadGateway, httpx.Envelope{Error: &httpx.Err{
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
func (s *Server) evaluate(ctx context.Context, userID string, in store.Intent) (*policy.Rejection, error) {
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

// submit is the only path to the executor, shared by orders and schedule slices.
func (s *Server) submit(ctx context.Context, u store.User, in store.Intent) (orderResponse, error) {
	resp, err := s.Executor.Route(ctx, executor.RouteRequest{
		IntentKey:      in.Key,
		PrivyWalletID:  u.PrivyWalletID,
		UserWallet:     u.WalletAddress,
		MarketID:       in.MarketID,
		Venue:          in.Venue,
		Side:           in.Side,
		Amount:         in.Size,
		MaxSlippageBps: in.MaxSlippageBps,
		ChainID:        in.ChainID,
	})
	reason := ""
	switch {
	case err != nil:
		reason = err.Error()
	case resp.Status != "submitted":
		reason = resp.Error
		if reason == "" {
			reason = fmt.Sprintf("The executor reported status %q for this intent without saying why.", resp.Status)
		}
	}

	if reason != "" {
		s.record(ctx, in, store.Execution{IntentKey: in.Key, Error: reason}, "failed")
		s.emit(ctx, natsbus.SubjExecutionFailed, in, "", reason)
		return orderResponse{IntentKey: in.Key, State: "failed", Steps: resp.Steps, Error: reason}, errors.New(reason)
	}

	s.record(ctx, in, store.Execution{
		IntentKey:    in.Key,
		TxHash:       resp.TxHash,
		Confirmation: finality.Seen.String(),
	}, "submitted")
	s.emit(ctx, natsbus.SubjExecutionPending, in, resp.TxHash, "")
	return orderResponse{IntentKey: in.Key, State: "submitted", TxHash: resp.TxHash, Steps: resp.Steps}, nil
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
)

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
	size, err := policy.ParseAmount(req.Size)
	if err != nil || size.Sign() == 0 {
		return store.Intent{}, "size must be a positive integer amount in base units, because a float would not survive the round trip to the chain."
	}
	limit := ""
	if req.OrderType == "limit" {
		p, err := policy.ParseAmount(req.LimitPrice)
		if err != nil || p.Sign() == 0 {
			return store.Intent{}, "limit_price must be a positive integer amount in base units for a limit order."
		}
		limit = p.String()
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

	return store.Intent{
		Key:            intentKey(userID, req.MarketID, req.Venue, req.Side, req.OrderType, size.String(), req.IdempotencyKey),
		UserID:         userID,
		MarketID:       req.MarketID,
		Venue:          req.Venue,
		Side:           req.Side,
		OrderType:      req.OrderType,
		Size:           size.String(),
		LimitPrice:     limit,
		MaxSlippageBps: bps,
		ChainID:        chainID,
		State:          "pending",
	}, ""
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
