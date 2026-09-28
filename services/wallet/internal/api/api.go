package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/httpx"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/natsbus"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/privyauth"
	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/executor"
	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/marketdata"
	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/policy"
	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/store"
)

// Store is everything the handlers need from postgres, named here so the
// handler tests can run against a fake instead of a database.
type Store interface {
	Ping(ctx context.Context) error
	UpsertUser(ctx context.Context, did, wallet, privyWalletID string) (store.User, error)
	User(ctx context.Context, did string) (store.User, error)

	PutDelegation(ctx context.Context, d policy.Delegation) (policy.Delegation, error)
	Delegation(ctx context.Context, userID string) (policy.Delegation, error)
	RevokeDelegation(ctx context.Context, userID string) error
	SpentTotal(ctx context.Context, userID string) (string, error)

	PutKuruAccount(ctx context.Context, ka store.KuruAccount) error
	KuruAccount(ctx context.Context, userID string) (store.KuruAccount, error)
	SetSignerExpiry(ctx context.Context, userID string, expiry int64) error

	InsertIntent(ctx context.Context, in store.Intent) (bool, error)
	Intent(ctx context.Context, key string) (store.Intent, error)
	Intents(ctx context.Context, userID string, limit int) ([]store.Intent, error)
	SetIntentState(ctx context.Context, key, state string) error
	SetOrderRef(ctx context.Context, key, orderID string, slot *int) error
	RecordExecution(ctx context.Context, e store.Execution) error
	Execution(ctx context.Context, key string) (store.Execution, error)
	Executions(ctx context.Context, userID string, limit int) ([]store.Execution, error)
}

type Server struct {
	Store    Store
	Market   *marketdata.Client
	Executor *executor.Client
	Auth     *privyauth.Verifier
	Internal privyauth.InternalAuth
	Bus      *natsbus.Bus
	Log      *slog.Logger

	// market-data owns the registry; this is a ttl cache of its ids
	marketIDs *marketCache
	// the executor owns the allowlist; this is a ttl cache of its scales
	marketMeta *metaCache

	ChainID            int64
	DefaultSlippageBps int
}

func (s *Server) Routes() http.Handler {
	if s.marketIDs == nil {
		s.marketIDs = newMarketCache(60 * time.Second)
	}
	if s.marketMeta == nil {
		s.marketMeta = newMetaCache(5 * time.Minute)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)

	authed := http.NewServeMux()
	authed.HandleFunc("GET /v1/me", s.getMe)
	authed.HandleFunc("POST /v1/me", s.upsertMe)
	authed.HandleFunc("PUT /v1/delegation", s.putDelegation)
	authed.HandleFunc("DELETE /v1/delegation", s.deleteDelegation)
	authed.HandleFunc("GET /v1/account", s.getAccount)
	authed.HandleFunc("POST /v1/account/deposit", s.deposit)
	authed.HandleFunc("POST /v1/account/authorize", s.authorizeSigner)
	authed.HandleFunc("DELETE /v1/account/authorize", s.revokeSigner)
	authed.HandleFunc("POST /v1/orders", s.createOrder)
	authed.HandleFunc("GET /v1/orders", s.listOrders)
	authed.HandleFunc("DELETE /v1/orders/{market_id}/{slot_idx}", s.cancelOrder)
	authed.HandleFunc("GET /v1/portfolio", s.portfolio)
	authed.HandleFunc("GET /v1/executions", s.listExecutions)
	authed.HandleFunc("GET /v1/perpl/account", s.getPerplAccount)
	authed.HandleFunc("POST /v1/perpl/deposit", s.perplDeposit)
	authed.HandleFunc("POST /v1/perpl/withdraw", s.perplWithdraw)
	authed.HandleFunc("DELETE /v1/perpl/orders/{market_id}/{order_id}", s.cancelPerplOrder)

	// an internal caller (keeper) reads a delegation for a user it cannot hold
	// a token for; every other route stays token-only
	internal := func(h http.HandlerFunc) http.Handler {
		return s.Auth.MiddlewareAllowingInternal(s.Internal, h)
	}
	mux.Handle("GET /v1/delegation", internal(s.getDelegation))

	mux.Handle("/v1/", s.Auth.Middleware(authed))
	return httpx.Logging(s.log(), mux)
}

func (s *Server) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	status := "ok"
	if s.Store != nil {
		if err := s.Store.Ping(r.Context()); err != nil {
			s.log().Error("health: database unreachable", "err", err)
			httpx.Fail(w, http.StatusServiceUnavailable, "degraded", "The database is unreachable, so this service cannot serve reads or writes.")
			return
		}
	}
	httpx.OK(w, map[string]any{"status": status})
}

// caller resolves the authenticated DID to a user row.
func (s *Server) caller(w http.ResponseWriter, r *http.Request) (store.User, bool) {
	did, ok := privyauth.DIDFromContext(r.Context())
	if !ok {
		httpx.Fail(w, http.StatusUnauthorized, "unauthorized", "This request carried no verified Privy identity, so it was refused.")
		return store.User{}, false
	}
	u, err := s.Store.User(r.Context(), did)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Fail(w, http.StatusPreconditionFailed, "user_not_registered",
			"No wallet is registered for this account yet, so POST /v1/me with your wallet address before trading.")
		return store.User{}, false
	}
	if err != nil {
		s.fail(w, "load user", err)
		return store.User{}, false
	}
	return u, true
}

func (s *Server) fail(w http.ResponseWriter, what string, err error) {
	s.log().Error(what, "err", err)
	httpx.Fail(w, http.StatusInternalServerError, "internal", "This service could not complete the request; the failure has been logged.")
}

// publish is best effort: the database row is the record, the event is a
// notification, and a bus outage must not fail an order that already landed.
func (s *Server) publish(ctx context.Context, subject, dedupeKey string, payload []byte) {
	if s.Bus == nil {
		return
	}
	if err := s.Bus.Publish(ctx, subject, dedupeKey, payload); err != nil {
		s.log().Warn("publish failed", "subject", subject, "key", dedupeKey, "err", err)
	}
}

var uuidRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
