package dailyterm

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
)

// Handler maps Service results to HTTP. It owns status codes.
type Handler struct {
	svc *Service
	log *slog.Logger
}

// NewHandler wires the service. Nil log falls back to slog.Default.
func NewHandler(svc *Service, log *slog.Logger) *Handler {
	if svc == nil {
		panic("dailyterm: nil Service")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Handler{svc: svc, log: log}
}

// RegisterRoutes mounts the daily term endpoints on a stdlib mux. Reads are
// public, like the rest of the read-only journal surface: the terms are written
// by the daily sync, never by a client, and the homepage shows them signed out.
// The literal /daily-terms/months wins over the {id} wildcard in stdlib mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /daily-terms", h.list)
	mux.HandleFunc("GET /daily-terms/months", h.months)
	mux.HandleFunc("GET /daily-terms/{id}", h.get)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.List(r.Context(), r.URL.Query().Get("month"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *Handler) months(w http.ResponseWriter, r *http.Request) {
	months, err := h.svc.Months(r.Context())
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, months)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	term, err := h.svc.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, term)
}

// writeError maps one service error to its status. The list and detail routes
// read differently on purpose: a missing month and a missing term are different
// mistakes, and the client can act on the difference. Unknown errors are 500.
func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput):
		http.Error(w, "invalid month", http.StatusBadRequest)
	case errors.Is(err, ErrNotFound):
		message := "month not found"
		if r.PathValue("id") != "" {
			message = "term not found"
		}
		http.Error(w, message, http.StatusNotFound)
	default:
		h.log.Error("daily term request failed", "error", err, "path", r.URL.Path)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
