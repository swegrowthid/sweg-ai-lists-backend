package health

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// Handler maps Service results to HTTP. It owns status codes.
// E scoping: service error -> 503. Encode failure -> 500.
type Handler struct {
	svc *Service
	log *slog.Logger
}

// NewHandler wires the service. Nil log falls back to slog.Default.
func NewHandler(svc *Service, log *slog.Logger) *Handler {
	if svc == nil {
		panic("health: nil Service")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Handler{svc: svc, log: log}
}

// RegisterRoutes mounts liveness and readiness on a stdlib mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", h.live)
	mux.HandleFunc("GET /readyz", h.ready)
}

func (h *Handler) live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.Live())
}

func (h *Handler) ready(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.Ready(r.Context())
	if err != nil {
		h.log.Warn("readiness failed", "error", err, "path", r.URL.Path)
		writeJSON(w, http.StatusServiceUnavailable, resp)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func writeJSON(w http.ResponseWriter, code int, v Response) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
