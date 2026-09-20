package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/pressly/goose/v3"

	"github.com/swegrowthid/sweg-ai-lists-backend/internal/platform/config"
	"github.com/swegrowthid/sweg-ai-lists-backend/internal/platform/db"
	"github.com/swegrowthid/sweg-ai-lists-backend/internal/platform/logger"
)

// main is the migration boundary. It builds R, runs one goose command, exits.
// Usage: go run ./cmd/migrate [up|down|status]. Default is up.
func main() {
	cfg := config.Load()
	log := logger.New(cfg.Service, cfg.Env, cfg.Version, cfg.LogLevel)
	slog.SetDefault(log)

	cmd := "up"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Open(ctx, cfg.DBURL)
	if err != nil {
		log.Error("db connect failed", "error", err)
		os.Exit(1)
	}
	defer func() { _ = pool.Close() }()

	if err := goose.SetDialect("postgres"); err != nil {
		log.Error("goose dialect failed", "error", err)
		os.Exit(1)
	}

	var runErr error
	switch cmd {
	case "up":
		runErr = goose.UpContext(ctx, pool.DB, "migrations")
	case "down":
		runErr = goose.DownContext(ctx, pool.DB, "migrations")
	case "status":
		runErr = goose.StatusContext(ctx, pool.DB, "migrations")
	default:
		log.Error("unknown migrate command", "command", cmd)
		os.Exit(2)
	}
	if runErr != nil {
		log.Error("migrate failed", "command", cmd, "error", runErr)
		os.Exit(1)
	}
	log.Info("migrate ok", "command", cmd)
}
