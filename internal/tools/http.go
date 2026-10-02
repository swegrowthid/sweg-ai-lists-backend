package tools

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
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
	list, err := h.svc.List(r.Context(), filter)
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
	tool, err := h.svc.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, tool)
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
