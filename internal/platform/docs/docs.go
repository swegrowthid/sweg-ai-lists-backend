package docs

import (
	"log/slog"
	"net/http"

	apiSpec "github.com/swegrowthid/sweg-ai-lists-backend/api"
)

// Handler serves API docs. It owns the docs routes only.
// Spec stays in api/openapi.yaml. Scalar renders it in the browser.
type Handler struct {
	log *slog.Logger
}

// NewHandler wires the logger. Nil log falls back to slog.Default.
func NewHandler(log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	return &Handler{log: log}
}

// RegisterRoutes mounts the docs UI and the raw spec on a stdlib mux.
// enableUI gates the Scalar shell; the spec always serves for tooling.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, enableUI bool) {
	if enableUI {
		mux.HandleFunc("GET /docs", h.ui)
	}
	mux.HandleFunc("GET /docs/openapi.yaml", h.spec)
}

func (h *Handler) ui(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(htmlPage))
}

func (h *Handler) spec(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	_, _ = w.Write(apiSpec.Spec())
}

// htmlPage is a small Scalar shell. No build step. No npm.
// data-url points to the same host, so staging works without CORS tweaks.
const htmlPage = `<!doctype html>
<html lang="id">
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<title>sweg-ai API Docs</title>
<style>
body { margin: 0; }
</style>
</head>
<body>
<script
id="api-reference"
data-url="/docs/openapi.yaml"
src="https://cdn.jsdelivr.net/npm/@scalar/api-reference/dist/browser/standalone.js"
></script>
</body>
</html>`
