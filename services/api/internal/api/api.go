package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/httpx"
	"github.com/snehendu098/sweem-terminal/internal/planner"
	"github.com/snehendu098/sweem-terminal/internal/streams"
	"github.com/snehendu098/sweem-terminal/services/api/internal/privy"
)

var (
	ErrNotFound     = errors.New("not_found")
	ErrNoDelegation = errors.New("delegation_inactive")
	ErrNotOpen      = errors.New("position_not_open")
	ErrOrderNotOpen = errors.New("order_not_open")
	ErrWithdrawBusy = errors.New("withdraw_in_progress")
	ErrRPC          = errors.New("rpc_unavailable")
)

type Verifier interface {
	Verify(ctx context.Context, token string) (string, error)
}

type Privy interface {
	VerifyDelegation(ctx context.Context, did, walletID, address string) error
}

// Runner runs a pending execution (planner.Executor).
type Runner interface {
	Run(ctx context.Context, executionID string) (planner.Outcome, error)
}

type Rater interface {
	Rates(ctx context.Context) (streams.RatesSnapshot, error)
}

// Backtester proxies history's POST /api/v1/backtest: returns its status and body verbatim.
type Backtester interface {
	Backtest(ctx context.Context, body []byte) (int, []byte, error)
}

type Server struct {
	Store                 Store
	Auth                  Verifier
	Privy                 Privy
	QuorumID              string
	Plan                  *planner.Planner
	Exec                  Runner
	Chain                 planner.Chain
	Txs                   TxReader
	Rates                 Rater
	History               Backtester
	GuardBuffer, GateBand decimal.Decimal // scheduler's risk params, shown to users
	bg                    sync.WaitGroup  // background executor runs; tests Wait on it
}

// Routes registers every /api/v1 route on mux.
func (s *Server) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/markets", s.markets)
	mux.HandleFunc("GET /api/v1/baskets", s.publicBaskets)
	mux.HandleFunc("GET /api/v1/baskets/{id}", s.publicBasket)
	mux.HandleFunc("GET /api/v1/strategies", s.strategies)
	mux.Handle("GET /api/v1/strategies/{id}", s.optionalMe(s.strategy))
	mux.HandleFunc("POST /api/v1/strategies/{id}/backtest", s.backtest)
	mux.Handle("POST /api/v1/strategies", s.me(s.createStrategy))
	mux.HandleFunc("GET /api/v1/perpl/markets", s.perplMarkets)

	mux.Handle("GET /api/v1/me", s.me(s.user))
	mux.Handle("POST /api/v1/me/delegation", s.me(s.putDelegation))
	mux.Handle("DELETE /api/v1/me/delegation", s.me(s.deleteDelegation))
	mux.Handle("GET /api/v1/me/baskets", s.me(s.myBaskets))
	mux.Handle("POST /api/v1/me/baskets", s.me(s.saveBasket))
	mux.Handle("POST /api/v1/me/baskets/{id}", s.me(s.saveBasket))
	mux.Handle("DELETE /api/v1/me/baskets/{id}", s.me(s.deleteBasket))
	mux.Handle("POST /api/v1/me/baskets/{id}/buy", s.me(s.delegatedTrade("basket_buy")))
	mux.Handle("POST /api/v1/me/baskets/{id}/sell", s.me(s.delegatedTrade("basket_sell")))
	mux.Handle("POST /api/v1/me/trade", s.me(s.delegatedTrade("trade")))
	mux.Handle("POST /api/v1/me/trade/prepare", s.me(s.prepare))
	mux.Handle("POST /api/v1/me/trade/record", s.me(s.record))
	mux.Handle("POST /api/v1/me/margin/deposit", s.me(s.margin(true)))
	mux.Handle("POST /api/v1/me/margin/withdraw", s.me(s.margin(false)))
	mux.Handle("POST /api/v1/me/orders", s.me(s.placeOrder))
	mux.Handle("GET /api/v1/me/orders", s.me(s.orders))
	mux.Handle("DELETE /api/v1/me/orders/{id}", s.me(s.cancelOrder))
	mux.Handle("GET /api/v1/me/holdings", s.me(s.holdings))
	mux.Handle("GET /api/v1/me/strategies", s.me(s.myStrategies))
	mux.Handle("POST /api/v1/me/strategies/{id}/deploy", s.me(s.deploy))
	mux.Handle("GET /api/v1/me/positions", s.me(s.positions))
	mux.Handle("POST /api/v1/me/positions/{id}/withdraw", s.me(s.withdraw))
	mux.Handle("GET /api/v1/me/positions/{id}/perpl", s.me(s.positionPerpl))
	mux.Handle("GET /api/v1/me/executions", s.me(s.executions))
	mux.Handle("GET /api/v1/me/earnings", s.me(s.earnings))
	mux.Handle("GET /api/v1/me/executions/{id}", s.me(s.execution))
	mux.Handle("GET /api/v1/me/schedules", s.me(s.schedules))
	mux.Handle("POST /api/v1/me/schedules", s.me(s.createSchedule))
	mux.Handle("DELETE /api/v1/me/schedules/{id}", s.me(s.deleteSchedule))
}

type didKey struct{}

func did(r *http.Request) string { d, _ := r.Context().Value(didKey{}).(string); return d }

func (s *Server) auth(r *http.Request) (string, bool) {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	d, err := s.Auth.Verify(r.Context(), strings.TrimSpace(tok))
	return d, ok && err == nil
}

// me authenticates the Privy token and makes sure the users row exists (every /me table references it).
func (s *Server) me(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d, ok := s.auth(r)
		if !ok {
			httpx.Error(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if err := s.Store.EnsureUser(r.Context(), d); err != nil {
			fail(w, "ensure user", err)
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), didKey{}, d)))
	})
}

// optionalMe sets the did when a valid token is present, else serves anonymously.
func (s *Server) optionalMe(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if d, ok := s.auth(r); ok {
			r = r.WithContext(context.WithValue(r.Context(), didKey{}, d))
		}
		h(w, r)
	})
}

func fail(w http.ResponseWriter, what string, err error) {
	var pe *planner.Error
	switch {
	case errors.As(err, &pe):
		status := http.StatusUnprocessableEntity
		switch {
		case pe.Code == "quote_unavailable":
			status = http.StatusBadGateway
		case pe.Code == "bad_request", strings.HasPrefix(pe.Code, "invalid_"):
			status = http.StatusBadRequest
		}
		httpx.JSON(w, status, map[string]string{"error": pe.Code, "detail": pe.Detail})
	case errors.Is(err, ErrRPC):
		httpx.Error(w, http.StatusBadGateway, err.Error())
	case errors.Is(err, ErrNotFound):
		httpx.Error(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrNoDelegation), errors.Is(err, ErrNotOpen), errors.Is(err, ErrOrderNotOpen), errors.Is(err, ErrWithdrawBusy):
		httpx.Error(w, http.StatusConflict, err.Error())
	default:
		slog.Error(what, "err", err)
		httpx.Error(w, http.StatusInternalServerError, "internal")
	}
}

// reply writes v, or maps err.
func reply[T any](w http.ResponseWriter, what string, v T, err error) {
	if err != nil {
		fail(w, what, err)
		return
	}
	httpx.JSON(w, http.StatusOK, v)
}

// decode reads a JSON body; false means a 400 was written.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(v); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_request")
		return false
	}
	return true
}

// id reads a uuid path value; false means a 400 was written.
func id(w http.ResponseWriter, r *http.Request) (string, bool) {
	v := r.PathValue("id")
	if uuid.Validate(v) != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_request")
		return "", false
	}
	return v, true
}

func isAddress(a string) bool {
	if len(a) != 42 || !strings.HasPrefix(a, "0x") {
		return false
	}
	return strings.Trim(strings.ToLower(a[2:]), "0123456789abcdef") == ""
}

// human parses a positive human decimal; "" when invalid.
func human(s string) (decimal.Decimal, bool) {
	v, err := decimal.NewFromString(s)
	return v, err == nil && v.IsPositive()
}

// slip returns slippage bps, default 50, allowed 0..1000.
func slip(p *int) (int, bool) {
	if p == nil {
		return 50, true
	}
	return *p, *p >= 0 && *p <= 1000
}

// start runs an inserted execution on the executor in the background; after sees whether it confirmed.
func (s *Server) start(execID string, after func(ctx context.Context, ok bool)) {
	s.bg.Add(1)
	go func() {
		defer s.bg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		out, err := s.Exec.Run(ctx, execID)
		var pe *planner.Error
		if err != nil && !errors.As(err, &pe) {
			slog.Error("executor", "execution", execID, "err", err)
			if ferr := s.Store.FailExecution(ctx, execID, "executor_unavailable"); ferr != nil {
				slog.Error("fail execution", "err", ferr)
			}
		}
		if after != nil {
			after(ctx, err == nil && out.Status == "confirmed")
		}
	}()
}

// accepted answers an execution-creating route.
func accepted(w http.ResponseWriter, execID, kind string, steps []planner.Step, extra map[string]any) {
	body := map[string]any{"executionId": execID, "kind": kind, "steps": steps}
	for k, v := range extra {
		body[k] = v
	}
	httpx.JSON(w, http.StatusAccepted, body)
}

func (s *Server) user(w http.ResponseWriter, r *http.Request) {
	v, err := s.Store.User(r.Context(), did(r))
	reply(w, "user", v, err)
}

func (s *Server) putDelegation(w http.ResponseWriter, r *http.Request) {
	var in struct{ WalletID, WalletAddress string }
	if !decode(w, r, &in) {
		return
	}
	if in.WalletID == "" || !isAddress(in.WalletAddress) {
		httpx.Error(w, http.StatusBadRequest, "bad_request")
		return
	}
	a := strings.ToLower(in.WalletAddress)
	if err := s.Privy.VerifyDelegation(r.Context(), did(r), in.WalletID, a); err != nil {
		for _, e := range []error{privy.ErrNotOwned, privy.ErrNoSigner, privy.ErrMismatch, privy.ErrNotFound} {
			if errors.Is(err, e) {
				httpx.Error(w, http.StatusForbidden, e.Error())
				return
			}
		}
		slog.Error("privy verify", "err", err)
		httpx.Error(w, http.StatusBadGateway, "privy_unavailable")
		return
	}
	v, err := s.Store.PutDelegation(r.Context(), did(r), in.WalletID, a, s.QuorumID)
	reply(w, "put delegation", v, err)
}

func (s *Server) deleteDelegation(w http.ResponseWriter, r *http.Request) {
	err := s.Store.RevokeDelegation(r.Context(), did(r))
	reply(w, "revoke delegation", map[string]bool{"revoked": true}, err)
}

func (s *Server) executions(w http.ResponseWriter, r *http.Request) {
	var before *time.Time
	if b := r.URL.Query().Get("before"); b != "" {
		t, err := time.Parse(time.RFC3339Nano, b)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "bad_request")
			return
		}
		before = &t
	}
	v, err := s.Store.Executions(r.Context(), did(r), before)
	reply(w, "executions", v, err)
}

func (s *Server) earnings(w http.ResponseWriter, r *http.Request) {
	v, err := s.Store.Earnings(r.Context(), did(r))
	reply(w, "earnings", v, err)
}

func (s *Server) execution(w http.ResponseWriter, r *http.Request) {
	if id, ok := id(w, r); ok {
		v, err := s.Store.Execution(r.Context(), did(r), id)
		reply(w, "execution", v, err)
	}
}

func (s *Server) schedules(w http.ResponseWriter, r *http.Request) {
	v, err := s.Store.Schedules(r.Context(), did(r))
	reply(w, "schedules", v, err)
}

func (s *Server) createSchedule(w http.ResponseWriter, r *http.Request) {
	var in ScheduleInput
	if !decode(w, r, &in) {
		return
	}
	if code := in.Validate(); code != "" {
		httpx.Error(w, http.StatusBadRequest, code)
		return
	}
	if in.Target.Type == "market" && s.Plan.Dep.Kuru.Markets[in.Target.ID].Orderbook == "" {
		httpx.Error(w, http.StatusBadRequest, "invalid_target")
		return
	}
	v, err := s.Store.CreateSchedule(r.Context(), did(r), in)
	if err != nil {
		fail(w, "create schedule", err)
		return
	}
	httpx.JSON(w, http.StatusCreated, v)
}

func (s *Server) deleteSchedule(w http.ResponseWriter, r *http.Request) {
	if id, ok := id(w, r); ok {
		err := s.Store.DeactivateSchedule(r.Context(), did(r), id)
		reply(w, "deactivate schedule", map[string]bool{"deactivated": true}, err)
	}
}

type Target struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// ScheduleInput amounts/prices are human decimal strings (tasks/api-v2.md §Schedules).
type ScheduleInput struct {
	Kind            string  `json:"kind"`
	Target          Target  `json:"target"`
	AmountUSDC      *string `json:"amountUsdc"`
	IntervalSeconds *int64  `json:"intervalSeconds"`
	TriggerPrice    *string `json:"triggerPrice"`
	SellBps         *int    `json:"sellBps"`
	DriftBps        *int    `json:"driftBps"`
	MaxSlippageBps  int     `json:"maxSlippageBps"`
	MaxPerRunUSDC   string  `json:"maxPerRunUsdc"`
}

// Validate normalizes in place and returns a snake_case error code, or "".
func (in *ScheduleInput) Validate() string {
	t := in.Target.Type
	switch {
	case t != "market" && t != "basket" && t != "position", in.Target.ID == "":
		return "invalid_target"
	case t != "market" && uuid.Validate(in.Target.ID) != nil:
		return "invalid_target"
	case in.MaxSlippageBps < 1 || in.MaxSlippageBps > 1000:
		return "invalid_max_slippage_bps"
	}
	maxRun, ok := human(in.MaxPerRunUSDC)
	if !ok {
		return "invalid_max_per_run_usdc"
	}
	in.MaxPerRunUSDC = maxRun.String()
	norm := func(p *string) bool {
		if p == nil {
			return false
		}
		v, ok := human(*p)
		*p = v.String()
		return ok
	}
	interval := in.IntervalSeconds != nil && *in.IntervalSeconds >= 60
	switch in.Kind {
	case "dca":
		if t == "position" {
			return "invalid_target"
		}
		if !norm(in.AmountUSDC) {
			return "invalid_amount_usdc"
		}
		if !interval {
			return "invalid_interval_seconds"
		}
		if maxRun.LessThan(decimal.RequireFromString(*in.AmountUSDC)) {
			return "max_per_run_below_amount"
		}
		in.TriggerPrice, in.SellBps, in.DriftBps = nil, nil, nil
	case "take_profit", "stop_loss":
		if !norm(in.TriggerPrice) {
			return "invalid_trigger_price"
		}
		if in.SellBps == nil || *in.SellBps < 1 || *in.SellBps > 10000 {
			return "invalid_sell_bps"
		}
		in.AmountUSDC, in.IntervalSeconds, in.DriftBps = nil, nil, nil
	case "rebalance":
		if t == "market" {
			return "invalid_target"
		}
		if !interval {
			return "invalid_interval_seconds"
		}
		if (t == "basket" && in.DriftBps == nil) || (in.DriftBps != nil && (*in.DriftBps < 1 || *in.DriftBps > 10000)) {
			return "invalid_drift_bps"
		}
		in.AmountUSDC, in.TriggerPrice, in.SellBps = nil, nil, nil
	default:
		return "invalid_kind"
	}
	return ""
}
