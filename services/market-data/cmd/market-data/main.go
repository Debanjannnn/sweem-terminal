package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/redis/go-redis/v9"
	"github.com/snehendu098/sweem-terminal/internal/config"
	"github.com/snehendu098/sweem-terminal/internal/httpx"
	"github.com/snehendu098/sweem-terminal/internal/logx"
	"github.com/snehendu098/sweem-terminal/services/market-data/internal/routes"
)

func main() {
	logx.Setup("market-data")
	var e config.Env
	redisURL := e.Str("REDIS_URL")
	addr := ":" + e.Opt("PORT", "8082")
	origins := strings.Split(e.Opt("CORS_ORIGINS", "http://localhost:3000"), ",")
	if err := e.Err(); err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		slog.Error("redis url", "err", err)
		os.Exit(1)
	}
	rdb := redis.NewClient(opt)
	defer rdb.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	slog.Info("listening", "addr", addr)
	if err := httpx.Run(ctx, addr, routes.New(ctx, rdb, origins)); err != nil {
		slog.Error("serve", "err", err)
		os.Exit(1)
	}
}
