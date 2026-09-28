package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/httpx"
	"github.com/snehendu098/sweem-terminal/services/api/internal/privy"
)

var (
	ErrNotFound     = errors.New("not_found")
	ErrNoBasket     = errors.New("basket_not_found")
	ErrNoDelegation = errors.New("delegation_inactive")
)

type Store interface {
	EnsureUser(ctx context.Context, did string) error
	User(ctx context.Context, did string) (User, error)
	Baskets(ctx context.Context) ([]Basket, error)
	Basket(ctx context.Context, addr string) (Basket, error)
	NavHistory(ctx context.Context, addr string, from, to time.Time) ([]NavPoint, error)
	Positions(ctx context.Context, did string) ([]Position, error)
	History(ctx context.Context, did string) ([]Transfer, error)
	PutDelegation(ctx context.Context, did, walletID, addr, quorumID string) (Delegation, error)
	RevokeDelegation(ctx context.Context, did string) error
	Schedules(ctx context.Context, did string) ([]Schedule, error)
	CreateSchedule(ctx context.Context, did string, in ScheduleInput) (Schedule, error)
	DeactivateSchedule(ctx context.Context, did, id string) error
	Executions(ctx context.Context, did string) ([]Execution, error)
}

type Verifier interface {
	Verify(ctx context.Context, token string) (string, error)
}

type Privy interface {
	VerifyDelegation(ctx context.Context, did, walletID, address string) error
}

type Server struct {
	Store    Store
	Auth     Verifier
	Privy    Privy
	QuorumID string
}

// Routes registers every /api/v1 route on mux.
func (s *Server) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/baskets", s.baskets)
	mux.HandleFunc("GET /api/v1/baskets/{address}", s.basket)
	mux.HandleFunc("GET /api/v1/baskets/{address}/nav/history", s.navHistory)
	mux.Handle("GET /api/v1/me", s.me(s.user))
	mux.Handle("GET /api/v1/me/positions", s.me(s.positions))
	mux.Handle("GET /api/v1/me/history", s.me(s.history))
	mux.Handle("POST /api/v1/me/delegation", s.me(s.putDelegation))
	mux.Handle("DELETE /api/v1/me/delegation", s.me(s.deleteDelegation))
	mux.Handle("GET /api/v1/me/schedules", s.me(s.schedules))
	mux.Handle("POST /api/v1/me/schedules", s.me(s.createSchedule))
	mux.Handle("DELETE /api/v1/me/schedules/{id}", s.me(s.deleteSchedule))
	mux.Handle("GET /api/v1/me/executions", s.me(s.executions))
}

type didKey struct{}

func did(r *http.Request) string { return r.Context().Value(didKey{}).(string) }

// me authenticates the Privy token and makes sure the users row exists (every /me table references it).
func (s *Server) me(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		d, err := s.Auth.Verify(r.Context(), strings.TrimSpace(tok))
		if !ok || err != nil {
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

func fail(w http.ResponseWriter, what string, err error) {
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrNoBasket):
		httpx.Error(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrNoDelegation):
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

func addr(r *http.Request) string { return strings.ToLower(r.PathValue("address")) }

func (s *Server) baskets(w http.ResponseWriter, r *http.Request) {
	v, err := s.Store.Baskets(r.Context())
	reply(w, "baskets", v, err)
}

func (s *Server) basket(w http.ResponseWriter, r *http.Request) {
	v, err := s.Store.Basket(r.Context(), addr(r))
	reply(w, "basket", v, err)
}

func (s *Server) navHistory(w http.ResponseWriter, r *http.Request) {
	to, from := time.Now(), time.Now().Add(-24*time.Hour)
	for _, p := range []struct {
		key string
		dst *time.Time
	}{{"from", &from}, {"to", &to}} {
		if q := r.URL.Query().Get(p.key); q != "" {
			t, err := time.Parse(time.RFC3339, q)
			if err != nil {
				httpx.Error(w, http.StatusBadRequest, "invalid_"+p.key)
				return
			}
			*p.dst = t
		}
	}
	if !from.Before(to) {
		httpx.Error(w, http.StatusBadRequest, "invalid_range")
		return
	}
	v, err := s.Store.NavHistory(r.Context(), addr(r), from, to)
	reply(w, "nav history", v, err)
}

func (s *Server) user(w http.ResponseWriter, r *http.Request) {
	v, err := s.Store.User(r.Context(), did(r))
	reply(w, "user", v, err)
}

func (s *Server) positions(w http.ResponseWriter, r *http.Request) {
	v, err := s.Store.Positions(r.Context(), did(r))
	reply(w, "positions", v, err)
}

func (s *Server) history(w http.ResponseWriter, r *http.Request) {
	v, err := s.Store.History(r.Context(), did(r))
	reply(w, "history", v, err)
}

func (s *Server) putDelegation(w http.ResponseWriter, r *http.Request) {
	var in struct{ WalletID, WalletAddress string }
	if json.NewDecoder(r.Body).Decode(&in) != nil || in.WalletID == "" || !isAddress(in.WalletAddress) {
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

func (s *Server) schedules(w http.ResponseWriter, r *http.Request) {
	v, err := s.Store.Schedules(r.Context(), did(r))
	reply(w, "schedules", v, err)
}

func (s *Server) createSchedule(w http.ResponseWriter, r *http.Request) {
	var in ScheduleInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_request")
		return
	}
	if code := in.Validate(); code != "" {
		httpx.Error(w, http.StatusBadRequest, code)
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
	id := r.PathValue("id")
	if uuid.Validate(id) != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_request")
		return
	}
	err := s.Store.DeactivateSchedule(r.Context(), did(r), id)
	reply(w, "deactivate schedule", map[string]bool{"deactivated": true}, err)
}

func (s *Server) executions(w http.ResponseWriter, r *http.Request) {
	v, err := s.Store.Executions(r.Context(), did(r))
	reply(w, "executions", v, err)
}

func isAddress(a string) bool {
	if len(a) != 42 || !strings.HasPrefix(a, "0x") {
		return false
	}
	return strings.Trim(strings.ToLower(a[2:]), "0123456789abcdef") == ""
}

// ScheduleInput amounts are human decimal strings: USDC dollars, NAV in USDC per share.
type ScheduleInput struct {
	Basket          string  `json:"basket"`
	Kind            string  `json:"kind"`
	AmountUSDC      *string `json:"amountUsdc"`
	IntervalSeconds *int64  `json:"intervalSeconds"`
	StopNavPerShare *string `json:"stopNavPerShare"`
	RedeemBps       *int    `json:"redeemBps"`
	MaxSlippageBps  int     `json:"maxSlippageBps"`
	MaxPerRunUSDC   string  `json:"maxPerRunUsdc"`
}

// Validate normalizes in place and returns a snake_case error code, or "".
func (in *ScheduleInput) Validate() string {
	if !isAddress(in.Basket) {
		return "invalid_basket"
	}
	in.Basket = strings.ToLower(in.Basket)
	if in.MaxSlippageBps < 1 || in.MaxSlippageBps > 1000 {
		return "invalid_max_slippage_bps"
	}
	maxRun, err := decimal.NewFromString(in.MaxPerRunUSDC)
	if err != nil || !maxRun.IsPositive() {
		return "invalid_max_per_run_usdc"
	}
	in.MaxPerRunUSDC = maxRun.String()
	switch in.Kind {
	case "recurring":
		amt, err := pdec(in.AmountUSDC)
		if err != nil || !amt.IsPositive() {
			return "invalid_amount_usdc"
		}
		if in.IntervalSeconds == nil || *in.IntervalSeconds < 60 {
			return "invalid_interval_seconds"
		}
		if maxRun.LessThan(amt) {
			return "max_per_run_below_amount"
		}
		*in.AmountUSDC = amt.String()
		in.StopNavPerShare, in.RedeemBps = nil, nil
	case "stop_loss":
		nav, err := pdec(in.StopNavPerShare)
		if err != nil || !nav.IsPositive() {
			return "invalid_stop_nav_per_share"
		}
		if in.RedeemBps == nil || *in.RedeemBps < 1 || *in.RedeemBps > 10000 {
			return "invalid_redeem_bps"
		}
		*in.StopNavPerShare = nav.String()
		in.AmountUSDC, in.IntervalSeconds = nil, nil
	default:
		return "invalid_kind"
	}
	return ""
}

func pdec(s *string) (decimal.Decimal, error) {
	if s == nil {
		return decimal.Decimal{}, errors.New("missing")
	}
	return decimal.NewFromString(*s)
}

type User struct {
	ID            string      `json:"id"`
	WalletAddress *string     `json:"walletAddress"`
	CreatedAt     time.Time   `json:"createdAt"`
	Delegation    *Delegation `json:"delegation"`
}

type Delegation struct {
	ID            string     `json:"id"`
	WalletID      string     `json:"walletId"`
	WalletAddress string     `json:"walletAddress"`
	KeyQuorumID   string     `json:"keyQuorumId"`
	Active        bool       `json:"active"`
	CreatedAt     time.Time  `json:"createdAt"`
	RevokedAt     *time.Time `json:"revokedAt"`
}

type Nav struct {
	NavPerShare string    `json:"navPerShare"`
	TotalNav    string    `json:"totalNav"`
	Supply      string    `json:"supply"`
	Ts          time.Time `json:"ts"`
}

type Basket struct {
	Address      string          `json:"address"`
	Name         string          `json:"name"`
	Symbol       string          `json:"symbol"`
	Components   json.RawMessage `json:"components"`
	CreatedBlock int64           `json:"createdBlock"`
	CreatedTx    string          `json:"createdTx"`
	Nav          *Nav            `json:"nav"`
	Stats        *BasketStats    `json:"stats,omitempty"`
}

// BasketStats: usdc raw 6dp, shares raw 18dp, as the indexer stores them.
type BasketStats struct {
	Mints      int64  `json:"mints"`
	Redeems    int64  `json:"redeems"`
	UsdcInRaw  string `json:"usdcInRaw"`
	UsdcOutRaw string `json:"usdcOutRaw"`
	SharesRaw  string `json:"sharesRaw"`
	Holders    int64  `json:"holders"`
}

type NavPoint = Nav

type Position struct {
	Basket      string     `json:"basket"`
	SharesRaw   string     `json:"sharesRaw"`
	NavPerShare *string    `json:"navPerShare"`
	ValueUSDC   *string    `json:"valueUsdc"`
	NavTs       *time.Time `json:"navTs"`
}

type Transfer struct {
	Kind      string    `json:"kind"`
	TxHash    string    `json:"txHash"`
	LogIndex  int       `json:"logIndex"`
	Basket    string    `json:"basket"`
	UsdcRaw   string    `json:"usdcRaw"`
	SharesRaw string    `json:"sharesRaw"`
	Block     int64     `json:"block"`
	Ts        time.Time `json:"ts"`
}

type Schedule struct {
	ID              string     `json:"id"`
	Basket          string     `json:"basket"`
	Kind            string     `json:"kind"`
	AmountUSDC      *string    `json:"amountUsdc"`
	IntervalSeconds *int64     `json:"intervalSeconds"`
	NextRunAt       *time.Time `json:"nextRunAt"`
	StopNavPerShare *string    `json:"stopNavPerShare"`
	RedeemBps       *int       `json:"redeemBps"`
	MaxSlippageBps  int        `json:"maxSlippageBps"`
	MaxPerRunUSDC   string     `json:"maxPerRunUsdc"`
	Active          bool       `json:"active"`
	CreatedAt       time.Time  `json:"createdAt"`
}

type Execution struct {
	ID         string    `json:"id"`
	ScheduleID string    `json:"scheduleId"`
	Basket     string    `json:"basket"`
	Slot       int64     `json:"slot"`
	Kind       string    `json:"kind"`
	Status     string    `json:"status"`
	TxHash     *string   `json:"txHash"`
	Error      *string   `json:"error"`
	AmountRaw  *string   `json:"amountRaw"`
	MinOutRaw  *string   `json:"minOutRaw"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}
