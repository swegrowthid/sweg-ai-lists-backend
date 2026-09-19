package app

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/swegrowthid/sweg-ai-lists-backend/internal/health"
	"github.com/swegrowthid/sweg-ai-lists-backend/internal/platform/config"
	"github.com/swegrowthid/sweg-ai-lists-backend/internal/platform/db"
	"github.com/swegrowthid/sweg-ai-lists-backend/internal/platform/httpserver"
	"github.com/swegrowthid/sweg-ai-lists-backend/internal/user"
)

// App wires the graph. main passes R in, App builds the graph.
// Graph: mux -> health.Handler -> health.Service -> db.Pinger.
type App struct {
	cfg    config.Config
	log    *slog.Logger
	server *httpserver.Server
	mux    *http.ServeMux
}

// New builds App. Nil logger is a programmer bug, so fail fast.
func New(cfg config.Config, log *slog.Logger, checker db.Pinger) *App {
	if log == nil {
		panic("app: nil logger")
	}
	if checker == nil {
		checker = db.NoopPinger{}
	}

	mux := http.NewServeMux()

	healthSvc := health.NewService(cfg.Version, checker)
	health.NewHandler(healthSvc, log).RegisterRoutes(mux)

	userStore := user.NewMemoryStore()
	user.NewHandler(user.NewService(userStore), log).RegisterRoutes(mux)

	a := &App{cfg: cfg, log: log, mux: mux}
	a.server = httpserver.New(cfg.Addr, a.withLogging(mux))
	return a
}

// Handler exposes the mux for tests. Swap R, same graph.
func (a *App) Handler() http.Handler { return a.mux }

// Run serves until ctx ends. It blocks. Start/stop lines live in main.
func (a *App) Run(ctx context.Context) error {
	return a.server.Run(ctx)
}

func (a *App) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, code: http.StatusOK}
		next.ServeHTTP(rec, r)
		a.log.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.code,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	code int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.code = code
	r.ResponseWriter.WriteHeader(code)
}
