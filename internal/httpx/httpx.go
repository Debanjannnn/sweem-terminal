package httpx

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"time"
)

type Check func(context.Context) error

// NewMux returns a mux with GET /health (liveness) and GET /healthz (runs every check).
func NewMux(checks map[string]Check) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		failed := map[string]string{}
		for name, c := range checks {
			if err := c(ctx); err != nil {
				failed[name] = err.Error()
			}
		}
		if len(failed) > 0 {
			JSON(w, http.StatusServiceUnavailable, map[string]any{"status": "unavailable", "failed": failed})
			return
		}
		JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return mux
}

func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("write json", "err", err)
	}
}

func Error(w http.ResponseWriter, status int, msg string) {
	JSON(w, status, map[string]string{"error": msg})
}

const (
	HeaderSecret = "X-Internal-Secret"
	HeaderCaller = "X-Caller"
)

// SetInternal adds the headers RequireInternal checks; callers use it so both sides share header names.
func SetInternal(r *http.Request, secret, caller string) {
	r.Header.Set(HeaderSecret, secret)
	r.Header.Set(HeaderCaller, caller)
}

// RequireInternal fails closed: an empty secret rejects everything.
func RequireInternal(secret string, allowedCallers ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got := r.Header.Get(HeaderSecret)
			if secret == "" || subtle.ConstantTimeCompare([]byte(got), []byte(secret)) != 1 {
				Error(w, http.StatusUnauthorized, "missing or invalid internal secret")
				return
			}
			if !slices.Contains(allowedCallers, r.Header.Get(HeaderCaller)) {
				Error(w, http.StatusForbidden, "caller not allowed on this route")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Run serves until ctx is cancelled, then shuts down gracefully.
func Run(ctx context.Context, addr string, h http.Handler) error {
	srv := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		return err
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
