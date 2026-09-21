package auth

import (
	"context"
	"errors"
	"time"
)

// Principal is the authenticated caller carried in request context.
type Principal struct {
	UserID   string
	Username string
}

type ctxKeyPrincipal struct{}

// WithPrincipal attaches the authenticated principal to ctx.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, ctxKeyPrincipal{}, p)
}

// PrincipalFrom reads the authenticated principal. ok=false when absent.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(ctxKeyPrincipal{}).(Principal)
	return p, ok
}

// Pair is the token bundle returned by login and refresh.
type Pair struct {
	AccessToken      string `json:"access_token"`
	TokenType        string `json:"token_type"`
	ExpiresIn        int64  `json:"expires_in"`
	RefreshToken     string `json:"refresh_token"`
	RefreshExpiresIn int64  `json:"refresh_expires_in"`
}

// RefreshRecord is one issued refresh token's server-side lifecycle.
// JWTs stay stateless; this row only tracks revocation for rotation+logout.
type RefreshRecord struct {
	JTI       string
	UserID    string
	ExpiresAt time.Time
	RevokedAt *time.Time
	CreatedAt time.Time
}

var (
	// ErrTokenInvalid signals a malformed, wrongly-signed, or wrong-type token.
	ErrTokenInvalid = errors.New("auth: invalid token")

	// ErrTokenExpired signals a well-formed token past its expiry.
	ErrTokenExpired = errors.New("auth: token expired")

	// ErrRefreshInvalid signals an unknown or expired refresh record.
	ErrRefreshInvalid = errors.New("auth: refresh token not recognized")

	// ErrRefreshReused signals a revoked refresh token presented again.
	// Services respond by revoking the user's whole token family.
	ErrRefreshReused = errors.New("auth: refresh token already used")
)
