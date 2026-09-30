package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/snehendu098/sweem-terminal/internal/config"
	"github.com/snehendu098/sweem-terminal/internal/httpx"
	"github.com/snehendu098/sweem-terminal/internal/logx"
	"github.com/snehendu098/sweem-terminal/internal/pg"
	"github.com/snehendu098/sweem-terminal/services/history/internal/ingest"
	"github.com/snehendu098/sweem-terminal/services/history/internal/routes"
)

func main() {
	logx.Setup("history")
	if err := run(); err != nil {
		slog.Error("exit", "err", err)
		os.Exit(1)
	}
}

func run() error {
	var e config.Env
	dbURL, token := e.Str("DATABASE_URL"), e.Str("ENVIO_API_TOKEN")
	addr := ":" + e.Opt("PORT", "8087")
	every := e.Duration("INGEST_INTERVAL", 6*time.Hour)
	ingestOn := e.Bool("INGEST", true)
	origins := strings.Split(e.Opt("CORS_ORIGINS", "http://localhost:3000"), ",")
	if err := e.Err(); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	db, err := pg.Open(ctx, dbURL)
	if err != nil {
		return err
	}
	defer db.Close()

	if ingestOn {
		g := &ingest.Ingester{DB: db, HTTP: &http.Client{Timeout: 2 * time.Minute}, EnvioToken: token, Now: time.Now}
		go func() {
			for {
				g.RunAll(ctx)
				select {
				case <-ctx.Done():
					return
				case <-time.After(every):
				}
			}
		}()
	}

	mux := httpx.NewMux(map[string]httpx.Check{"postgres": func(ctx context.Context) error { return db.Ping(ctx) }})
	(&routes.Server{DB: db, Now: time.Now}).Routes(mux)
	return httpx.Run(ctx, addr, httpx.CORS(origins, mux))
}
