package auth

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/swegrowthid/sweg-ai-lists-backend/internal/user"
)

const magicLinkSentMessage = "if the email is registered, a login link is on its way"

// Handler maps Service results to HTTP. It owns status codes.
type Handler struct {
	svc      *Service
	magic    *Magic
	supabase *Supabase
	log      *slog.Logger
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

// SetMagic wires the passwordless magic link service onto Handler.
func (h *Handler) SetMagic(magic *Magic) {
	h.magic = magic
}

// SetSupabase turns on sign-in with a swegrowth.id account. Left unset,
// POST /auth/supabase answers 503.
func (h *Handler) SetSupabase(supabase *Supabase) {
	h.supabase = supabase
}

// RegisterRoutes mounts auth endpoints on a stdlib mux. All public:
// the refresh token in the body is the credential for refresh and logout.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /auth/login", h.login)
	mux.HandleFunc("POST /auth/refresh", h.refresh)
	mux.HandleFunc("POST /auth/logout", h.logout)
	mux.HandleFunc("POST /auth/magic-link", h.requestMagicLink)
	mux.HandleFunc("POST /auth/magic-link/verify", h.verifyMagicLink)
	mux.HandleFunc("POST /auth/supabase", h.exchangeSupabase)
}

type supabaseExchangeRequest struct {
	AccessToken string `json:"access_token"`
}

func (h *Handler) exchangeSupabase(w http.ResponseWriter, r *http.Request) {
	if h.supabase == nil {
		http.Error(w, "swegrowth sign-in is not configured", http.StatusServiceUnavailable)
		return
	}
	var request supabaseExchangeRequest
	if err := decodeJSON(w, r, &request); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	pair, err := h.supabase.Exchange(r.Context(), request.AccessToken)
	if err != nil {
		switch {
		case errors.Is(err, ErrSupabaseTokenInvalid):
			http.Error(w, "invalid or expired swegrowth session", http.StatusUnauthorized)
		case errors.Is(err, ErrSupabaseEmailUnverified):
			http.Error(w, "confirm your email on swegrowth.id first", http.StatusForbidden)
		case errors.Is(err, user.ErrLinkedElsewhere):
			http.Error(w, "this email is linked to another swegrowth account", http.StatusConflict)
		case errors.Is(err, ErrSupabaseUnavailable):
			h.log.Warn("supabase exchange upstream failed", "error", err, "path", r.URL.Path)
			http.Error(w, "could not reach swegrowth sign-in", http.StatusBadGateway)
		default:
			h.log.Error("supabase exchange failed", "error", err, "path", r.URL.Path)
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}
	writeJSON(w, http.StatusOK, pair)
}

type loginRequest struct {
	Identifier string `json:"identifier"`
	Password   string `json:"password"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type magicLinkRequest struct {
	Email string `json:"email"`
}

type magicLinkResponse struct {
	Message   string `json:"message"`
	ExpiresIn int    `json:"expires_in"`
}

type magicLinkVerifyRequest struct {
	Token string `json:"token"`
	Email string `json:"email"`
	Code  string `json:"code"`
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

func (h *Handler) requestMagicLink(w http.ResponseWriter, r *http.Request) {
	if h.magic == nil {
		h.log.Error("magic link request without magic service", "path", r.URL.Path)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	var request magicLinkRequest
	if err := decodeJSON(w, r, &request); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := h.magic.Request(r.Context(), request.Email); err != nil {
		switch {
		case errors.Is(err, ErrMagicEmailInvalid):
			http.Error(w, "invalid request body", http.StatusBadRequest)
		case errors.Is(err, ErrMagicRateLimited):
			http.Error(w, "too many requests", http.StatusTooManyRequests)
		default:
			h.log.Error("magic link request failed", "error", err, "path", r.URL.Path)
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}
	writeJSON(w, http.StatusOK, magicLinkResponse{Message: magicLinkSentMessage, ExpiresIn: h.magic.TTLSeconds()})
}

func (h *Handler) verifyMagicLink(w http.ResponseWriter, r *http.Request) {
	if h.magic == nil {
		h.log.Error("magic link verify without magic service", "path", r.URL.Path)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	var request magicLinkVerifyRequest
	if err := decodeJSON(w, r, &request); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	var pair Pair
	var err error
	if request.Code != "" {
		pair, err = h.magic.VerifyCode(r.Context(), request.Email, request.Code)
	} else {
		pair, err = h.magic.Verify(r.Context(), request.Token)
	}
	if err != nil {
		switch {
		case errors.Is(err, ErrMagicLinkInvalid) && request.Code != "":
			http.Error(w, "invalid or expired code", http.StatusUnauthorized)
		case errors.Is(err, ErrMagicLinkInvalid):
			http.Error(w, "invalid or expired link", http.StatusUnauthorized)
		default:
			h.log.Error("magic link verify failed", "error", err, "path", r.URL.Path)
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}
	writeJSON(w, http.StatusOK, pair)
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
