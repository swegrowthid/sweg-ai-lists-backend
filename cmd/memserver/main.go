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
		JWTAccessTTL:  24 * time.Hour,
		JWTRefreshTTL: 24 * time.Hour,
		CORSOrigins:   []string{"http://localhost:4321"},
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()
	a := app.New(cfg, log, nil)
	// Same graph as cmd/api: sync the news and tools lists now, then daily.
	go a.RunNewsSync(ctx)
	go a.RunToolsSync(ctx)
	_ = a.Run(ctx)
}
