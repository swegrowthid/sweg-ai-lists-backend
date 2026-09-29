package user

import (
	"context"
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
	// principal resolves the caller ID from the request context. It is
	// injected by app wiring (backed by auth.PrincipalFrom) so this package
	// never imports auth and avoids an import cycle.
	principal func(context.Context) (string, bool)
	// onPasswordChanged runs after a successful password change. App wiring
	// sets it to revoke the user's refresh sessions.
	onPasswordChanged func(context.Context, string) error
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
// protect wraps authenticated routes; nil means no protection.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, protect func(http.Handler) http.Handler) {
	if protect == nil {
		protect = func(next http.Handler) http.Handler { return next }
	}
	mux.Handle("GET /users", protect(http.HandlerFunc(h.list)))
	mux.Handle("PUT /users/password", protect(http.HandlerFunc(h.changePassword)))
	mux.HandleFunc("POST /users/register", h.register)
}

// SetAuth wires the caller identity resolver and the post-change hook.
// Both are set once by app wiring; the hook revokes refresh sessions.
func (h *Handler) SetAuth(principal func(context.Context) (string, bool), onPasswordChanged func(context.Context, string) error) {
	h.principal = principal
	h.onPasswordChanged = onPasswordChanged
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

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// changePassword replaces the caller's own password. The user ID comes from
// the verified access token, never from the body. A success revokes every
// refresh session through the onPasswordChanged hook, then answers 204.
func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) {
	if h.principal == nil {
		h.log.Error("user password change without principal resolver", "path", r.URL.Path)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	userID, ok := h.principal(r.Context())
	if !ok || userID == "" {
		http.Error(w, "missing bearer token", http.StatusUnauthorized)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	var request changePasswordRequest
	if err := decoder.Decode(&request); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		http.Error(w, "request body must contain one JSON object", http.StatusBadRequest)
		return
	}

	if err := h.svc.UpdatePassword(r.Context(), UpdatePasswordInput{
		UserID:          userID,
		CurrentPassword: request.CurrentPassword,
		NewPassword:     request.NewPassword,
	}); err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			http.Error(w, "invalid current or new password", http.StatusBadRequest)
		case errors.Is(err, ErrInvalidCredentials):
			http.Error(w, "current password is incorrect", http.StatusUnauthorized)
		case errors.Is(err, ErrNotFound):
			http.Error(w, "user not found", http.StatusUnauthorized)
		default:
			h.log.Error("user password change failed", "error", err, "path", r.URL.Path)
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}

	if h.onPasswordChanged != nil {
		if err := h.onPasswordChanged(r.Context(), userID); err != nil {
			h.log.Error("user sessions revoke failed", "error", err, "path", r.URL.Path)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
