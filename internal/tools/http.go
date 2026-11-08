package tools

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/swegrowthid/sweg-ai-lists-backend/internal/auth"
)

// Handler maps Service results to HTTP. It owns status codes.
type Handler struct {
	svc *Service
	log *slog.Logger
}

// NewHandler wires the service. Nil log falls back to slog.Default.
func NewHandler(svc *Service, log *slog.Logger) *Handler {
	if svc == nil {
		panic("tools: nil Service")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Handler{svc: svc, log: log}
}

// RegisterRoutes mounts the tools endpoints on a stdlib mux. Reads are
// public: the catalog is written by the daily sync, never by a client.
// The literal /tools/categories wins over the {id} wildcard in stdlib mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /tools", h.list)
	mux.HandleFunc("GET /tools/categories", h.categories)
	mux.HandleFunc("GET /tools/{id}", h.get)
}

// RegisterRoutesWithAuth mounts the same reads behind optionalAuth and the
// vote writes behind requireAuth.Reads stay public: a valid token only fills
// the viewer voted flag. The legacy RegisterRoutes stays for tests that do
// not wire auth.
func (h *Handler) RegisterRoutesWithAuth(mux *http.ServeMux, requireAuth, optionalAuth func(http.Handler) http.Handler) {
	if requireAuth == nil || optionalAuth == nil {
		panic("tools: nil auth middleware")
	}
	mux.Handle("GET /tools", optionalAuth(http.HandlerFunc(h.list)))
	mux.Handle("GET /tools/categories", optionalAuth(http.HandlerFunc(h.categories)))
	mux.Handle("GET /tools/{id}", optionalAuth(http.HandlerFunc(h.get)))
	mux.Handle("POST /tools/{id}/votes", requireAuth(http.HandlerFunc(h.vote)))
	mux.Handle("DELETE /tools/{id}/votes", requireAuth(http.HandlerFunc(h.unvote)))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	// `group` is the documented spelling; `category` is the older name for the
	// same filter and still resolves through the service.
	group := q.Get("group")
	if group == "" {
		group = q.Get("category")
	}
	filter := ListFilter{
		Category: group,
		Query:    q.Get("q"),
		Sort:     q.Get("sort"),
		Order:    q.Get("order"),
	}
	var err error
	if filter.Page, err = pageParam(q.Get("page")); err != nil {
		h.writeError(w, r, err)
		return
	}
	if filter.PerPage, err = pageParam(q.Get("per_page")); err != nil {
		h.writeError(w, r, err)
		return
	}
	list, err := h.svc.ListFor(r.Context(), filter, viewerID(r))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// pageParam parses one optional paging integer. An absent parameter yields 0,
// which the service replaces with its default. Anything else must be a positive
// integer.
func pageParam(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, ErrInvalidInput
	}
	return n, nil
}

func (h *Handler) categories(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.Categories(r.Context())
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	tool, err := h.svc.GetFor(r.Context(), r.PathValue("id"), viewerID(r))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, tool)
}

// vote records one usage vote. The first vote answers 201, a repeat answers
// 200 with the latest summary; both are idempotent by design.
func (h *Handler) vote(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFrom(r.Context())
	if !ok {
		http.Error(w, "invalid or expired token", http.StatusUnauthorized)
		return
	}
	result, created, err := h.svc.Vote(r.Context(), principal.UserID, r.PathValue("id"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if created {
		writeJSON(w, http.StatusCreated, result)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// unvote withdraws one usage vote. It always answers 200 with the latest
// summary, even when no vote existed.
func (h *Handler) unvote(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFrom(r.Context())
	if !ok {
		http.Error(w, "invalid or expired token", http.StatusUnauthorized)
		return
	}
	result, err := h.svc.Unvote(r.Context(), principal.UserID, r.PathValue("id"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// viewerID reads the optional principal for list and detail: a valid token
// fills the voted flag, an anonymous reader gets voted false everywhere.
func viewerID(r *http.Request) string {
	principal, ok := auth.PrincipalFrom(r.Context())
	if !ok {
		return ""
	}
	return principal.UserID
}

// writeError maps one service error to its status. Unknown errors are 500.
func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput):
		http.Error(w, "invalid input", http.StatusBadRequest)
	case errors.Is(err, ErrUnknownCategory):
		http.Error(w, "unknown category", http.StatusBadRequest)
	case errors.Is(err, ErrNotFound):
		http.Error(w, "tool not found", http.StatusNotFound)
	case errors.Is(err, ErrUnknownUser):
		http.Error(w, "invalid or expired token", http.StatusUnauthorized)
	default:
		h.log.Error("tools request failed", "error", err, "path", r.URL.Path)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
