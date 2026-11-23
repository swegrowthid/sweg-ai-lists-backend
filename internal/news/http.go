package news

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
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
		panic("news: nil Service")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Handler{svc: svc, log: log}
}

// RegisterRoutes mounts the news endpoints on a stdlib mux. Reads are public:
// the list is written by the source sync, never by a client.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /news", h.list)
	mux.HandleFunc("GET /news/sources", h.sources)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	filter := ListFilter{
		Query:  query.Get("q"),
		Group:  query.Get("group"),
		Source: query.Get("source"),
	}
	var err error
	if filter.Page, err = intParam(query, "page"); err != nil {
		http.Error(w, "invalid page", http.StatusBadRequest)
		return
	}
	if filter.PerPage, err = intParam(query, "per_page"); err != nil {
		http.Error(w, "invalid per_page", http.StatusBadRequest)
		return
	}

	result, err := h.svc.List(r.Context(), filter)
	if errors.Is(err, ErrInvalidInput) || errors.Is(err, ErrUnknownGroup) {
		http.Error(w, "invalid query", http.StatusBadRequest)
		return
	}
	if err != nil {
		h.log.Error("news list failed", "error", err, "path", r.URL.Path)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) sources(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.Sources(r.Context())
	if err != nil {
		h.log.Error("news sources failed", "error", err, "path", r.URL.Path)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// intParam reads one optional integer query parameter. Empty means unset (0);
// anything that is not a plain integer is a client error.
func intParam(query url.Values, key string) (int, error) {
	raw := query.Get(key)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	return value, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
