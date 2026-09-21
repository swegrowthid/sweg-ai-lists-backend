package auth

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/swegrowthid/sweg-ai-lists-backend/internal/user"
)

// Handler maps Service results to HTTP. It owns status codes.
type Handler struct {
	svc *Service
	log *slog.Logger
}

// NewHandler wires the service. Nil log falls back to slog.Default.
func NewHandler(svc *Service, log *slog.Logger) *Handler {
	if svc == nil {
		panic("auth: nil Service")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Handler{svc: svc, log: log}
}

// RegisterRoutes mounts auth endpoints on a stdlib mux. All public:
// the refresh token in the body is the credential for refresh and logout.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /auth/login", h.login)
	mux.HandleFunc("POST /auth/refresh", h.refresh)
	mux.HandleFunc("POST /auth/logout", h.logout)
}

type loginRequest struct {
	Identifier string `json:"identifier"`
	Password   string `json:"password"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var request loginRequest
	if err := decodeJSON(w, r, &request); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	pair, err := h.svc.Login(r.Context(), user.LoginInput{
		Identifier: request.Identifier,
		Password:   request.Password,
	})
	if err != nil {
		switch {
		case errors.Is(err, user.ErrInvalidCredentials):
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
		default:
			h.log.Error("login failed", "error", err, "path", r.URL.Path)
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}
	writeJSON(w, http.StatusOK, pair)
}

func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) {
	var request refreshRequest
	if err := decodeJSON(w, r, &request); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	pair, err := h.svc.Refresh(r.Context(), request.RefreshToken)
	if err != nil {
		switch {
		case errors.Is(err, ErrTokenInvalid), errors.Is(err, ErrTokenExpired),
			errors.Is(err, ErrRefreshInvalid), errors.Is(err, ErrRefreshReused):
			http.Error(w, "invalid or expired refresh token", http.StatusUnauthorized)
		default:
			h.log.Error("refresh failed", "error", err, "path", r.URL.Path)
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}
	writeJSON(w, http.StatusOK, pair)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	var request refreshRequest
	if err := decodeJSON(w, r, &request); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := h.svc.Logout(r.Context(), request.RefreshToken); err != nil {
		switch {
		case errors.Is(err, ErrTokenInvalid), errors.Is(err, ErrTokenExpired):
			http.Error(w, "invalid or expired refresh token", http.StatusUnauthorized)
		default:
			h.log.Error("logout failed", "error", err, "path", r.URL.Path)
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
