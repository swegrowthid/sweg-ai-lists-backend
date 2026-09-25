package post

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

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
		panic("post: nil Service")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Handler{svc: svc, log: log}
}

// RegisterRoutes mounts post and category endpoints on a stdlib mux.
// Reads are public; writes need a Bearer token. protect wraps the write
// routes; nil means no protection.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, protect func(http.Handler) http.Handler) {
	if protect == nil {
		protect = func(next http.Handler) http.Handler { return next }
	}
	mux.HandleFunc("GET /categories", h.listCategories)
	mux.Handle("POST /categories", protect(http.HandlerFunc(h.createCategory)))
	mux.HandleFunc("GET /posts", h.list)
	mux.Handle("POST /posts", protect(http.HandlerFunc(h.create)))
	mux.HandleFunc("GET /posts/{slug}", h.get)
}

type createCategoryRequest struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

type createPostRequest struct {
	Slug       string                  `json:"slug"`
	Title      string                  `json:"title"`
	Categories []string                `json:"categories"`
	Items      []createPostItemRequest `json:"items"`
}

// createPostItemRequest keeps every payload field as a pointer, so the service
// can tell "key absent" from "key sent empty". A key that does not belong to
// the item's kind is rejected even when it is empty.
type createPostItemRequest struct {
	Kind     string  `json:"kind"`
	BodyText *string `json:"body_text"`
	URL      *string `json:"url"`
	Filename *string `json:"filename"`
	MIME     *string `json:"mime"`
}

func (h *Handler) listCategories(w http.ResponseWriter, r *http.Request) {
	categories, err := h.svc.ListCategories(r.Context())
	if err != nil {
		h.log.Warn("categories list failed", "error", err, "path", r.URL.Path)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if categories == nil {
		categories = []Category{}
	}
	writeJSON(w, http.StatusOK, categories)
}

func (h *Handler) createCategory(w http.ResponseWriter, r *http.Request) {
	var request createCategoryRequest
	if err := decodeJSON(w, r, &request); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	created, err := h.svc.CreateCategory(r.Context(), CreateCategoryInput{
		Slug: request.Slug,
		Name: request.Name,
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	posts, err := h.svc.List(r.Context(), ListFilter{
		CategorySlug: r.URL.Query().Get("category"),
		Query:        r.URL.Query().Get("q"),
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if posts == nil {
		posts = []Post{}
	}
	writeJSON(w, http.StatusOK, posts)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	found, err := h.svc.Get(r.Context(), r.PathValue("slug"))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			http.Error(w, "post not found", http.StatusNotFound)
			return
		}
		h.log.Warn("post get failed", "error", err, "path", r.URL.Path)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, found)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	// The author comes from the verified token, never from the body.
	principal, ok := auth.PrincipalFrom(r.Context())
	if !ok {
		http.Error(w, "missing bearer token", http.StatusUnauthorized)
		return
	}

	var request createPostRequest
	if err := decodeJSON(w, r, &request); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	items := make([]ItemInput, 0, len(request.Items))
	for _, item := range request.Items {
		items = append(items, ItemInput{
			Kind:     Kind(item.Kind),
			BodyText: item.BodyText,
			URL:      item.URL,
			Filename: item.Filename,
			MIME:     item.MIME,
		})
	}

	created, err := h.svc.Create(r.Context(), CreatePostInput{
		Slug:          request.Slug,
		Title:         request.Title,
		AuthorID:      principal.UserID,
		CategorySlugs: request.Categories,
		Items:         items,
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// writeError maps domain errors to status codes. Everything else is a server
// fault: log it and answer a generic 500.
func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput):
		http.Error(w, "invalid input", http.StatusBadRequest)
	case errors.Is(err, ErrUnknownCategory):
		http.Error(w, "unknown category", http.StatusBadRequest)
	case errors.Is(err, ErrUnknownAuthor):
		http.Error(w, "author not found", http.StatusUnauthorized)
	case errors.Is(err, ErrConflict):
		http.Error(w, "slug already exists", http.StatusConflict)
	default:
		h.log.Error("post request failed", "error", err, "method", r.Method, "path", r.URL.Path)
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
