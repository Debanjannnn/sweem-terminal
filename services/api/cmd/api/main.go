package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"

	"github.com/snehendu098/sweem-terminal/internal/config"
	"github.com/snehendu098/sweem-terminal/internal/httpx"
	"github.com/snehendu098/sweem-terminal/internal/logx"
	"github.com/snehendu098/sweem-terminal/internal/pg"
	"github.com/snehendu098/sweem-terminal/services/api/internal/api"
	"github.com/snehendu098/sweem-terminal/services/api/internal/auth"
	"github.com/snehendu098/sweem-terminal/services/api/internal/privy"
)

func main() {
	logx.Setup("api")
	if err := run(); err != nil {
		slog.Error("api exited", "err", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var e config.Env
	dbURL, appID, secret, quorum := e.Str("DATABASE_URL"), e.Str("PRIVY_APP_ID"), e.Str("PRIVY_APP_SECRET"), e.Str("PRIVY_KEY_QUORUM_ID")
	addr := ":" + e.Opt("PORT", "8080")
	origins := strings.Split(e.Opt("CORS_ORIGINS", "http://localhost:3000"), ",")
	if err := e.Err(); err != nil {
		return err
	}
	db, err := pg.Open(ctx, dbURL)
	if err != nil {
		return err
	}
	defer db.Close()
	verifier, err := auth.New(ctx, appID, auth.JWKSURL(appID))
	if err != nil {
		return err
	}
	mux := httpx.NewMux(map[string]httpx.Check{"postgres": db.Ping})
	(&api.Server{Store: api.PG{DB: db}, Auth: verifier, Privy: privy.New(appID, secret, quorum), QuorumID: quorum}).Routes(mux)
	slog.Info("listening", "addr", addr)
	return httpx.Run(ctx, addr, cors(origins, mux))
}

// browser client calls :8080 directly with a bearer token
func cors(origins []string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if o := r.Header.Get("Origin"); o != "" && (slices.Contains(origins, o) || slices.Contains(origins, "*")) {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", o)
			h.Set("Vary", "Origin")
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			h.Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
