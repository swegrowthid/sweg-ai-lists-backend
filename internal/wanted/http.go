package wanted

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/swegrowthid/sweg-ai-lists-backend/internal/post"
)

// Handler maps Service results to HTTP. It owns status codes.
type Handler struct {
	svc *Service
	log *slog.Logger
}

// NewHandler wires the service. Nil log falls back to slog.Default.
func NewHandler(svc *Service, log *slog.Logger) *Handler {
	if svc == nil {
		panic("wanted: nil Service")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Handler{svc: svc, log: log}
}

// RegisterRoutes mounts the wanted endpoints on a stdlib mux. Every route is
// public: the list needs no login.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /wanted", h.list)
	mux.HandleFunc("POST /wanted", h.add)
	mux.HandleFunc("DELETE /wanted/{slug}", h.remove)
}

type addRequest struct {
	Slug string `json:"slug"`
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	posts, err := h.svc.List(r.Context())
	if err != nil {
		h.log.Error("wanted list failed", "error", err, "path", r.URL.Path)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if posts == nil {
		posts = []post.Post{}
	}
	writeJSON(w, http.StatusOK, posts)
}

func (h *Handler) add(w http.ResponseWriter, r *http.Request) {
	var request addRequest
	if err := decodeJSON(w, r, &request); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	stored, added, err := h.svc.Add(r.Context(), AddInput{Slug: request.Slug})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	// A duplicate slug is idempotent: answer the stored post with 200 instead
	// of failing.
	if !added {
		writeJSON(w, http.StatusOK, stored)
		return
	}
	writeJSON(w, http.StatusCreated, stored)
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Delete(r.Context(), r.PathValue("slug")); err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeError maps domain errors to status codes. Everything else is a server
// fault: log it and answer a generic 500.
func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput):
		http.Error(w, "invalid input", http.StatusBadRequest)
	case errors.Is(err, ErrUnknownPost):
		http.Error(w, "post not found", http.StatusNotFound)
	case errors.Is(err, ErrNotFound):
		http.Error(w, "wanted entry not found", http.StatusNotFound)
	default:
		h.log.Error("wanted request failed", "error", err, "method", r.Method, "path", r.URL.Path)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// decodeJSON reads one JSON object, bounded and strict. Shared edge rule.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
