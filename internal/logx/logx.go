package logx

import (
	"log/slog"
	"os"
)

// Setup installs a JSON slog default at LOG_LEVEL (debug|info|warn|error, default info).
func Setup(service string) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(os.Getenv("LOG_LEVEL"))); err != nil {
		lvl = slog.LevelInfo
	}
	l := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})).With("svc", service)
	slog.SetDefault(l)
	return l
}
