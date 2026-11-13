package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/swegrowthid/sweg-ai-lists-backend/internal/user"
)

var (
	// ErrSupabaseTokenInvalid signals a swegrowth.id session Supabase rejected.
	ErrSupabaseTokenInvalid = errors.New("auth: invalid swegrowth session")

	// ErrSupabaseEmailUnverified signals a swegrowth.id account without a confirmed email.
	// Linking by email is only safe for an email the owner proved they hold.
	ErrSupabaseEmailUnverified = errors.New("auth: swegrowth email not confirmed")

	// ErrSupabaseUnavailable signals that Supabase could not be asked.
	ErrSupabaseUnavailable = errors.New("auth: swegrowth sign-in unavailable")
)

// SupabaseUsers is the user port the exchange needs. user.Service satisfies it.
type SupabaseUsers interface {
	SignInSupabase(ctx context.Context, identity user.SupabaseIdentity) (user.User, error)
}

// Supabase exchanges a swegrowth.id session (a Supabase access token) for a
// local token pair. Supabase itself answers who the token belongs to, so a
// revoked session or an unconfirmed email is caught at the source.
type Supabase struct {
	baseURL string // e.g. https://<ref>.supabase.co
	apiKey  string // the project's publishable key; public by design
	client  *http.Client
	users   SupabaseUsers
	tokens  *Tokens
	refresh RefreshStore
}

// NewSupabase wires the exchange. Missing settings are a wiring bug.
func NewSupabase(baseURL, apiKey string, users SupabaseUsers, tokens *Tokens, refresh RefreshStore) *Supabase {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || strings.TrimSpace(apiKey) == "" {
		panic("auth: empty Supabase URL or key")
	}
	if users == nil || tokens == nil || refresh == nil {
		panic("auth: nil Supabase dependency")
	}
	return &Supabase{
		baseURL: baseURL,
		apiKey:  strings.TrimSpace(apiKey),
		client:  &http.Client{Timeout: 8 * time.Second},
		users:   users,
		tokens:  tokens,
		refresh: refresh,
	}
}

// Exchange resolves the token's account, finds or links the local user, and
// issues the same pair /auth/login does.
func (s *Supabase) Exchange(ctx context.Context, accessToken string) (Pair, error) {
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return Pair{}, ErrSupabaseTokenInvalid
	}
	account, err := s.fetchUser(ctx, accessToken)
	if err != nil {
		return Pair{}, err
	}
	if account.Email == "" || account.EmailConfirmedAt == "" {
		return Pair{}, ErrSupabaseEmailUnverified
	}
	u, err := s.users.SignInSupabase(ctx, user.SupabaseIdentity{ID: account.ID, Email: account.Email})
	if err != nil {
		return Pair{}, err
	}
	return issuePair(ctx, s.tokens, s.refresh, u.ID, u.Username)
}

type supabaseAccount struct {
	ID               string `json:"id"`
	Email            string `json:"email"`
	EmailConfirmedAt string `json:"email_confirmed_at"`
}

// fetchUser asks GET /auth/v1/user who the token belongs to.
func (s *Supabase) fetchUser(ctx context.Context, accessToken string) (supabaseAccount, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/auth/v1/user", nil)
	if err != nil {
		return supabaseAccount{}, fmt.Errorf("auth: build supabase request: %w", err)
	}
	req.Header.Set("apikey", s.apiKey)
	req.Header.Set("Authorization", "Bearer "+accessToken)

	res, err := s.client.Do(req)
	if err != nil {
		return supabaseAccount{}, fmt.Errorf("%w: %v", ErrSupabaseUnavailable, err)
	}
	defer res.Body.Close()

	switch {
	case res.StatusCode == http.StatusOK:
	case res.StatusCode == http.StatusUnauthorized, res.StatusCode == http.StatusForbidden:
		return supabaseAccount{}, ErrSupabaseTokenInvalid
	default:
		return supabaseAccount{}, fmt.Errorf("%w: status %d", ErrSupabaseUnavailable, res.StatusCode)
	}

	var account supabaseAccount
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&account); err != nil {
		return supabaseAccount{}, fmt.Errorf("%w: decode user: %v", ErrSupabaseUnavailable, err)
	}
	if account.ID == "" {
		return supabaseAccount{}, ErrSupabaseTokenInvalid
	}
	return account, nil
}
