package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// probe reports the principal the middleware chain sees: user id or anonymous.
func probe(t *testing.T, mw *Middleware, use func(http.Handler) http.Handler, bearer string) (int, string) {
	t.Helper()
	next := use(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := PrincipalFrom(r.Context())
		if !ok {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("anon"))
			return
		}
		_, _ = w.Write([]byte(principal.UserID))
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if bearer != "" {
		req.Header.Set("Authorization", bearer)
	}
	res := httptest.NewRecorder()
	next.ServeHTTP(res, req)
	return res.Code, res.Body.String()
}

func voteTestTokens() *Tokens {
	return NewTokens("test-secret-for-middleware-vote-tests", time.Hour, time.Hour)
}

func voteTestBearer(t *testing.T, tokens *Tokens, userID string) string {
	t.Helper()
	pair, _, err := tokens.Issue(userID, "voter")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	return "Bearer " + pair.AccessToken
}

// OptionalAuth fills the principal for a valid token and serves the request.
func TestOptionalAuthValidTokenFillsPrincipal(t *testing.T) {
	tokens := voteTestTokens()
	mw := NewMiddleware(tokens)
	code, body := probe(t, mw, mw.OptionalAuth, voteTestBearer(t, tokens, "user-1"))
	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if body != "user-1" {
		t.Fatalf("principal = %q, want user-1", body)
	}
}

// OptionalAuth never rejects: a missing or garbage token serves anonymously.
func TestOptionalAuthInvalidOrMissingTokenServesAnonymously(t *testing.T) {
	tokens := voteTestTokens()
	mw := NewMiddleware(tokens)

	for _, tc := range []struct {
		name   string
		bearer string
	}{
		{"missing header", ""},
		{"garbage token", "Bearer not-a-token"},
	} {
		code, body := probe(t, mw, mw.OptionalAuth, tc.bearer)
		if code != http.StatusOK {
			t.Fatalf("%s: status = %d, want %d", tc.name, code, http.StatusOK)
		}
		if body != "anon" {
			t.Fatalf("%s: body = %q, want anon", tc.name, body)
		}
	}
}

// RequireAuth still rejects the same invalid tokens OptionalAuth lets pass.
func TestRequireAuthStillRejectsInvalidToken(t *testing.T) {
	tokens := voteTestTokens()
	mw := NewMiddleware(tokens)

	for _, tc := range []struct {
		name   string
		bearer string
	}{
		{"missing header", ""},
		{"garbage token", "Bearer not-a-token"},
	} {
		code, _ := probe(t, mw, mw.RequireAuth, tc.bearer)
		if code != http.StatusUnauthorized {
			t.Fatalf("%s: status = %d, want %d", tc.name, code, http.StatusUnauthorized)
		}
	}
}
