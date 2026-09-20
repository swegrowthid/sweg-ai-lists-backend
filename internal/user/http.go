package user

import (
	"encoding/json"
	"errors"
	"io"
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
		panic("user: nil Service")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Handler{svc: svc, log: log}
}

// RegisterRoutes mounts user endpoints on a stdlib mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /users", h.list)
	mux.HandleFunc("POST /users/register", h.register)
}

type registerRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	var request registerRequest
	if err := decoder.Decode(&request); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		http.Error(w, "request body must contain one JSON object", http.StatusBadRequest)
		return
	}

	created, err := h.svc.Register(r.Context(), RegisterInput{
		Username: request.Username,
		Email:    request.Email,
		Password: request.Password,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			http.Error(w, "invalid username, email, or password", http.StatusBadRequest)
		case errors.Is(err, ErrConflict):
			http.Error(w, "username or email already exists", http.StatusConflict)
		default:
			h.log.Error("user register failed", "error", err, "path", r.URL.Path)
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(created)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	users, err := h.svc.List(r.Context())
	if err != nil {
		h.log.Warn("users list failed", "error", err, "path", r.URL.Path)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if users == nil {
		users = []User{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(users)
}
