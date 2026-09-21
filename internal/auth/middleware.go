package auth

import (
	"net/http"
	"strings"
)

// Middleware validates Bearer tokens before protected handlers.
type Middleware struct {
	tokens *Tokens
}

// NewMiddleware wires the token verifier. Nil is a programmer bug.
func NewMiddleware(tokens *Tokens) *Middleware {
	if tokens == nil {
		panic("auth: nil Tokens")
	}
	return &Middleware{tokens: tokens}
}

// RequireAuth wraps next: a valid access token becomes a Principal in ctx.
// Failures answer 401 with a WWW-Authenticate header (RFC 6750).
func (m *Middleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenStr, ok := bearerToken(r)
		if !ok {
			unauthorized(w, "missing bearer token")
			return
		}
		claims, err := m.tokens.ParseAccess(tokenStr)
		if err != nil {
			unauthorized(w, "invalid or expired token")
			return
		}
		ctx := WithPrincipal(r.Context(), Principal{UserID: claims.Subject, Username: claims.Username})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func bearerToken(r *http.Request) (string, bool) {
	scheme, token, found := strings.Cut(r.Header.Get("Authorization"), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
		return "", false
	}
	return token, true
}

func unauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="api"`)
	http.Error(w, msg, http.StatusUnauthorized)
}
