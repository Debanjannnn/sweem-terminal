package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/httpx"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/privyauth"
	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/policy"
	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/store"
)

type scheduleRequest struct {
	Kind           string          `json:"kind"`
	MarketID       string          `json:"market_id"`
	Venue          string          `json:"venue"`
	Side           string          `json:"side"`
	Total          string          `json:"total"`
	SliceCount     int             `json:"slice_count"`
	WindowSeconds  int64           `json:"window_seconds"`
	CadenceSeconds int64           `json:"cadence_seconds"`
	Levels         json.RawMessage `json:"levels"`
	IdempotencyKey string          `json:"idempotency_key"`
}

const maxSlices = 500

func (s *Server) listSchedules(w http.ResponseWriter, r *http.Request) {
	u, ok := s.caller(w, r)
	if !ok {
		return
	}
	rows, err := s.Store.Schedules(r.Context(), u.ID)
	if err != nil {
		s.fail(w, "list schedules", err)
		return
	}
	httpx.OK(w, map[string]any{"schedules": rows})
}

func (s *Server) getSchedule(w http.ResponseWriter, r *http.Request) {
	u, ok := s.caller(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if !uuidRE.MatchString(id) {
		httpx.Fail(w, http.StatusNotFound, "not_found", "No schedule with that id belongs to this account.")
		return
	}
	sc, err := s.Store.Schedule(r.Context(), id, u.ID)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Fail(w, http.StatusNotFound, "not_found", "No schedule with that id belongs to this account.")
		return
	}
	if err != nil {
		s.fail(w, "load schedule", err)
		return
	}
	httpx.OK(w, sc)
}

func (s *Server) createSchedule(w http.ResponseWriter, r *http.Request) {
	u, ok := s.caller(w, r)
	if !ok {
		return
	}
	var req scheduleRequest
	if !decode(w, r, &req) {
		return
	}
	sc, slices, reason := s.buildSchedule(r.Context(), u.ID, req, time.Now())
	if reason != "" {
		httpx.Fail(w, http.StatusBadRequest, "bad_request", reason)
		return
	}

	// a schedule authorizes future spending, so it is refused now if the
	// delegation could not cover its first slice
	probe := store.Intent{MarketID: sc.MarketID, Venue: sc.Venue, Size: slices[0].Amount}
	rej, err := s.evaluate(r.Context(), u.ID, probe)
	if err != nil {
		s.fail(w, "evaluate delegation", err)
		return
	}
	if rej != nil {
		httpx.Fail(w, http.StatusPreconditionFailed, rej.Code, rej.Reason)
		return
	}

	saved, err := s.Store.CreateSchedule(r.Context(), sc, slices)
	if err != nil {
		s.fail(w, "create schedule", err)
		return
	}
	httpx.JSON(w, http.StatusCreated, httpx.Envelope{Data: saved})
}

func (s *Server) cancelSchedule(w http.ResponseWriter, r *http.Request) {
	u, ok := s.caller(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if !uuidRE.MatchString(id) {
		httpx.Fail(w, http.StatusNotFound, "not_found", "No schedule with that id belongs to this account.")
		return
	}
	err := s.Store.CancelSchedule(r.Context(), id, u.ID)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Fail(w, http.StatusNotFound, "not_found",
			"No live schedule with that id belongs to this account, so there was nothing to cancel.")
		return
	}
	if err != nil {
		s.fail(w, "cancel schedule", err)
		return
	}
	httpx.OK(w, map[string]any{"cancelled": true})
}

type dispatchResponse struct {
	ScheduleID string `json:"schedule_id"`
	SliceIndex int    `json:"slice_index"`
	State      string `json:"state"`
	IntentKey  string `json:"intent_key,omitempty"`
	TxHash     string `json:"tx_hash,omitempty"`
	Error      string `json:"error,omitempty"`
	Replayed   bool   `json:"replayed,omitempty"`
}

// dispatchSlice is called by the scheduler when a slice comes due. The guard
// runs here, not there: the scheduler holds no delegation.
func (s *Server) dispatchSlice(w http.ResponseWriter, r *http.Request) {
	did, ok := privyauth.DIDFromContext(r.Context())
	if !ok {
		httpx.Fail(w, http.StatusUnauthorized, "unauthorized", "This request carried no verified identity, so it was refused.")
		return
	}
	scheduleID := r.PathValue("schedule_id")
	index, convErr := strconv.Atoi(r.PathValue("slice_index"))
	if !uuidRE.MatchString(scheduleID) || convErr != nil || index < 0 {
		httpx.Fail(w, http.StatusNotFound, "not_found", "That schedule id and slice index do not name a slice.")
		return
	}

	sc, sl, err := s.Store.DispatchTarget(r.Context(), scheduleID, index, did)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Fail(w, http.StatusNotFound, "not_found", "That schedule id and slice index do not name a slice of this user's.")
		return
	}
	if err != nil {
		s.fail(w, "load slice", err)
		return
	}
	if sl.State != "pending" {
		httpx.OK(w, dispatchResponse{ScheduleID: sc.ID, SliceIndex: index, State: sl.State,
			IntentKey: sl.IntentKey, Error: sl.SkipReason, Replayed: true})
		return
	}
	if sc.State != "active" {
		httpx.Fail(w, http.StatusConflict, "schedule_not_active",
			fmt.Sprintf("Schedule %s is %s, so slice %d was not dispatched.", sc.ID, sc.State, index))
		return
	}

	u, err := s.Store.User(r.Context(), did)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Fail(w, http.StatusPreconditionFailed, "user_not_registered",
			"No wallet is registered for this account, so its schedule slices cannot be executed.")
		return
	}
	if err != nil {
		s.fail(w, "load user", err)
		return
	}

	in := store.Intent{
		Key:            intentKey("schedule", sc.ID, strconv.Itoa(index), strconv.Itoa(sl.Generation)),
		UserID:         did,
		MarketID:       sc.MarketID,
		Venue:          sc.Venue,
		Side:           sc.Side,
		OrderType:      "market",
		Size:           sl.Amount,
		MaxSlippageBps: s.DefaultSlippageBps,
		ChainID:        s.ChainID,
		State:          "pending",
	}

	rej, err := s.evaluate(r.Context(), did, in)
	if err != nil {
		s.fail(w, "evaluate delegation", err)
		return
	}
	if rej != nil {
		if err := s.Store.MarkSlice(r.Context(), sc.ID, index, "skipped", "", rej.Reason); err != nil {
			s.log().Error("mark slice skipped", "schedule_id", sc.ID, "slice", index, "err", err)
		}
		// nothing later will pass either, so stop the schedule instead of
		// skipping every remaining slice one by one
		if rej.Code == "no_delegation" || rej.Code == "delegation_revoked" || rej.Code == "delegation_expired" {
			if err := s.Store.PauseScheduleRevoked(r.Context(), sc.ID); err != nil {
				s.log().Error("pause schedule", "schedule_id", sc.ID, "err", err)
			}
		}
		httpx.Fail(w, http.StatusPreconditionFailed, rej.Code, rej.Reason)
		return
	}

	created, err := s.Store.InsertIntent(r.Context(), in)
	if err != nil {
		s.fail(w, "insert intent", err)
		return
	}
	if !created {
		if done, ok := s.replay(r.Context(), in.Key); ok {
			httpx.OK(w, dispatchResponse{ScheduleID: sc.ID, SliceIndex: index, State: done.State,
				IntentKey: in.Key, TxHash: done.TxHash, Error: done.Error, Replayed: true})
			return
		}
	}

	resp, execErr := s.submit(r.Context(), u, in)
	state := "dispatched"
	if execErr != nil {
		state = "failed"
	}
	if err := s.Store.MarkSlice(r.Context(), sc.ID, index, state, in.Key, ""); err != nil {
		s.log().Error("mark slice", "schedule_id", sc.ID, "slice", index, "state", state, "err", err)
	}
	out := dispatchResponse{ScheduleID: sc.ID, SliceIndex: index, State: state,
		IntentKey: in.Key, TxHash: resp.TxHash, Error: resp.Error}
	if execErr != nil {
		httpx.JSON(w, http.StatusBadGateway, httpx.Envelope{Data: out,
			Error: &httpx.Err{Code: "execution_failed", Message: resp.Error}})
		return
	}
	httpx.OK(w, out)
}

// buildSchedule validates the request and lays out every slice up front, so a
// schedule is a plan the user can read rather than a promise to improvise.
func (s *Server) buildSchedule(ctx context.Context, userID string, req scheduleRequest, now time.Time) (store.Schedule, []store.Slice, string) {
	req.Kind = strings.ToLower(strings.TrimSpace(req.Kind))
	req.Venue = strings.ToLower(strings.TrimSpace(req.Venue))
	req.Side = strings.ToLower(strings.TrimSpace(req.Side))
	req.MarketID = strings.TrimSpace(req.MarketID)
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)

	if req.IdempotencyKey == "" {
		return store.Schedule{}, nil, "idempotency_key is required: it is what makes a retried create the same schedule rather than a second one."
	}
	if !contains([]string{"twap", "dca", "grid"}, req.Kind) {
		return store.Schedule{}, nil, fmt.Sprintf("kind must be twap, dca or grid, not %q.", req.Kind)
	}
	if !policy.KnownVenue(req.Venue) {
		return store.Schedule{}, nil, fmt.Sprintf("Venue %q is not one this terminal can route to (%s).", req.Venue, strings.Join(policy.Venues, ", "))
	}
	if !s.knownMarket(ctx, req.MarketID) {
		return store.Schedule{}, nil, fmt.Sprintf("Market %q is not on the executor's allowlist, so no schedule can be run against it.", req.MarketID)
	}
	if !contains(sides, req.Side) {
		return store.Schedule{}, nil, fmt.Sprintf("side must be buy or sell, not %q.", req.Side)
	}
	total, err := policy.ParseAmount(req.Total)
	if err != nil || total.Sign() == 0 {
		return store.Schedule{}, nil, "total must be a positive integer amount in base units."
	}

	count, spacing := req.SliceCount, time.Duration(0)
	switch req.Kind {
	case "twap":
		if count < 1 || req.WindowSeconds <= 0 {
			return store.Schedule{}, nil, "a twap needs slice_count of at least 1 and a window_seconds greater than zero."
		}
		spacing = time.Duration(req.WindowSeconds) * time.Second / time.Duration(count)
	case "dca":
		if count < 1 || req.CadenceSeconds <= 0 {
			return store.Schedule{}, nil, "a dca needs slice_count of at least 1 and a cadence_seconds greater than zero."
		}
		spacing = time.Duration(req.CadenceSeconds) * time.Second
	case "grid":
		n, ok := levelCount(req.Levels)
		if !ok {
			return store.Schedule{}, nil, "a grid needs levels to be a non-empty JSON array, because each level is one slice."
		}
		// ponytail: every grid slice is due now and the scheduler decides on
		// price; give slices a trigger column if grids ever need their own clock
		count = n
	}
	if count > maxSlices {
		return store.Schedule{}, nil, fmt.Sprintf("slice_count must be at most %d.", maxSlices)
	}
	if total.Cmp(big.NewInt(int64(count))) < 0 {
		return store.Schedule{}, nil, "total is smaller than slice_count, so at least one slice would be zero."
	}

	sc := store.Schedule{
		UserID:         userID,
		Kind:           req.Kind,
		MarketID:       req.MarketID,
		Venue:          req.Venue,
		Side:           req.Side,
		Total:          total.String(),
		SliceCount:     count,
		WindowSeconds:  req.WindowSeconds,
		CadenceSeconds: req.CadenceSeconds,
		Levels:         req.Levels,
		State:          "active",
		IdempotencyKey: req.IdempotencyKey,
	}
	return sc, planSlices(total, count, spacing, now), ""
}

// planSlices splits total into count slices whose amounts sum back to total:
// integer division leaves a remainder, and it goes on the last slice rather
// than being rounded away.
func planSlices(total *big.Int, count int, spacing time.Duration, start time.Time) []store.Slice {
	n := big.NewInt(int64(count))
	each, rem := new(big.Int).QuoRem(total, n, new(big.Int))
	out := make([]store.Slice, count)
	for i := range out {
		amount := new(big.Int).Set(each)
		if i == count-1 {
			amount.Add(amount, rem)
		}
		out[i] = store.Slice{
			Index:  i,
			DueAt:  start.Add(time.Duration(i) * spacing),
			Amount: amount.String(),
			State:  "pending",
		}
	}
	return out
}

func levelCount(raw json.RawMessage) (int, bool) {
	var levels []json.RawMessage
	if err := json.Unmarshal(raw, &levels); err != nil || len(levels) == 0 {
		return 0, false
	}
	return len(levels), true
}
