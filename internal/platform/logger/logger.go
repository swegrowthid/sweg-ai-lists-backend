package logger

import (
	"log/slog"
	"os"
	"strings"
)

// New builds the process logger.
// Dev: text + source, level debug. Prod: JSON, level info.
// Base attrs (service, env, version) ride on every line.
// ponytail: no sampling or log rotation here; ceiling is journald or a collector, upgrade path is a fanout handler.
func New(service, env, version, level string) *slog.Logger {
	env = strings.ToLower(strings.TrimSpace(env))
	lvl := parseLevel(level, env)
	base := []any{
		slog.String("service", strings.TrimSpace(service)),
		slog.String("env", env),
		slog.String("version", strings.TrimSpace(version)),
	}
	if env == "prod" {
		return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})).With(base...)
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: lvl, AddSource: true})).With(base...)
}

// parseLevel maps text to slog level. Empty means debug in dev, info in prod.
func parseLevel(level, env string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	case "info":
		return slog.LevelInfo
	default:
		if env == "prod" {
			return slog.LevelInfo
		}
		return slog.LevelDebug
	}
}
