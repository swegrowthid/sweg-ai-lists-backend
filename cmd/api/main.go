package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/swegrowthid/sweg-ai-lists-backend/internal/app"
	"github.com/swegrowthid/sweg-ai-lists-backend/internal/platform/config"
	"github.com/swegrowthid/sweg-ai-lists-backend/internal/platform/db"
	"github.com/swegrowthid/sweg-ai-lists-backend/internal/platform/logger"
)

// main is the boundary. It builds R, then runs the graph.
func main() {
	cfg := config.Load()
	log := logger.New(cfg.Service, cfg.Env, cfg.Version, cfg.LogLevel)
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Info("starting", "addr", cfg.Addr)
	a := app.New(cfg, log, db.NoopPinger{})
	if err := a.Run(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("server stopped", "error", err)
		os.Exit(1)
	}
	log.Info("stopped")
}
