// Scratch dev server: production graph with in-memory stores, for local E2E.
// Not for deploy - `go run ./cmd/memserver` then delete this dir.
package main

import (
	"context"
	"io"
	"log/slog"
	"time"

	"github.com/swegrowthid/sweg-ai-lists-backend/internal/app"
	"github.com/swegrowthid/sweg-ai-lists-backend/internal/platform/config"
)

func main() {
	cfg := config.Config{
		Addr:          "127.0.0.1:8790",
		Env:           "dev",
		Service:       "sweg-ai-mem",
		Version:       "dev",
		JWTSecret:     "dev-secret",
		JWTAccessTTL:  15 * time.Minute,
		JWTRefreshTTL: 24 * time.Hour,
		CORSOrigins:   []string{"http://localhost:4321"},
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	_ = app.New(cfg, log, nil).Run(context.Background())
}
