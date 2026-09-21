package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/swegrowthid/sweg-ai-lists-backend/internal/user"
)

const testSecret = "test-secret-for-auth-tests"

// newTestMux builds the same graph as app.New with memory stores (swap R).
func newTestMux() *http.ServeMux {
	store := user.NewMemoryStore()
	userSvc := user.NewService(store)
	tokens := NewTokens(testSecret, 15*time.Minute, 24*time.Hour)
	mux := http.NewServeMux()
	user.NewHandler(userSvc, nil).RegisterRoutes(mux, NewMiddleware(tokens).RequireAuth)
	NewHandler(NewService(userSvc, tokens, NewMemoryRefreshStore()), nil).RegisterRoutes(mux)
	return mux
}

func doJSON(t *testing.T, mux *http.ServeMux, method, path, body, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	return res
}

func login(t *testing.T, mux *http.ServeMux, identifier, password string) Pair {
	t.Helper()
	res := doJSON(t, mux, http.MethodPost, "/auth/login",
		`{"identifier":"`+identifier+`","password":"`+password+`"}`, "")
	if res.Code != http.StatusOK {
		t.Fatalf("login status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	var pair Pair
	if err := json.Unmarshal(res.Body.Bytes(), &pair); err != nil {
		t.Fatalf("decode token pair: %v", err)
	}
	return pair
}

func TestLoginReturnsTokensWithoutPassword(t *testing.T) {
	mux := newTestMux()
	res := doJSON(t, mux, http.MethodPost, "/users/register",
		`{"username":"budi","email":"budi@example.com","password":"password"}`, "")
	if res.Code != http.StatusCreated {
		t.Fatalf("register status = %d, want %d", res.Code, http.StatusCreated)
	}

	res = doJSON(t, mux, http.MethodPost, "/auth/login",
		`{"identifier":"budi","password":"password"}`, "")
	if res.Code != http.StatusOK {
		t.Fatalf("login status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["access_token"] == "" || body["refresh_token"] == "" {
		t.Fatal("login response misses tokens")
	}
	if body["token_type"] != "Bearer" {
		t.Fatalf("token_type = %v, want Bearer", body["token_type"])
	}
	if _, ok := body["password"]; ok {
		t.Fatal("response exposed password")
	}
	if _, ok := body["password_hash"]; ok {
		t.Fatal("response exposed password_hash")
	}
}

func TestLoginAcceptsEmailCaseInsensitive(t *testing.T) {
	mux := newTestMux()
	res := doJSON(t, mux, http.MethodPost, "/users/register",
		`{"username":"budi","email":"budi@example.com","password":"password"}`, "")
	if res.Code != http.StatusCreated {
		t.Fatalf("register status = %d, want %d", res.Code, http.StatusCreated)
	}
	pair := login(t, mux, "BUDI@Example.COM", "password")
	if pair.AccessToken == "" {
		t.Fatal("login by email returned no access token")
	}
}

func TestLoginWrongPasswordAndUnknownUserShareMessage(t *testing.T) {
	mux := newTestMux()
	res := doJSON(t, mux, http.MethodPost, "/users/register",
		`{"username":"budi","email":"budi@example.com","password":"password"}`, "")
	if res.Code != http.StatusCreated {
		t.Fatalf("register status = %d, want %d", res.Code, http.StatusCreated)
	}

	for _, body := range []string{
		`{"identifier":"budi","password":"wrong-password"}`,
		`{"identifier":"nobody","password":"password"}`,
	} {
		res := doJSON(t, mux, http.MethodPost, "/auth/login", body, "")
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("login status = %d, want %d; body = %s", res.Code, http.StatusUnauthorized, res.Body.String())
		}
		if got := strings.TrimSpace(res.Body.String()); got != "invalid credentials" {
			t.Fatalf("login message = %q, want generic message", got)
		}
	}
}

func TestProtectedUsersRejectsMissingAndGarbageTokens(t *testing.T) {
	mux := newTestMux()

	res := doJSON(t, mux, http.MethodGet, "/users", "", "")
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("GET /users without token status = %d, want %d", res.Code, http.StatusUnauthorized)
	}
	if res.Header().Get("WWW-Authenticate") == "" {
		t.Fatal("401 misses WWW-Authenticate header")
	}

	res = doJSON(t, mux, http.MethodGet, "/users", "", "not-a-token")
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("GET /users with garbage token status = %d, want %d", res.Code, http.StatusUnauthorized)
	}
}

func TestProtectedUsersAcceptsAccessToken(t *testing.T) {
	mux := newTestMux()
	res := doJSON(t, mux, http.MethodPost, "/users/register",
		`{"username":"budi","email":"budi@example.com","password":"password"}`, "")
	if res.Code != http.StatusCreated {
		t.Fatalf("register status = %d, want %d", res.Code, http.StatusCreated)
	}
	pair := login(t, mux, "budi", "password")

	res = doJSON(t, mux, http.MethodGet, "/users", "", pair.AccessToken)
	if res.Code != http.StatusOK {
		t.Fatalf("GET /users with token status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
}

func TestExpiredAccessTokenRejected(t *testing.T) {
	tokens := NewTokens(testSecret, 15*time.Minute, time.Hour)
	expired, _, err := tokens.issue("uid-1", "budi", tokenTypeAccess, "", -time.Minute)
	if err != nil {
		t.Fatalf("issue expired token: %v", err)
	}
	if _, err := tokens.ParseAccess(expired); err == nil {
		t.Fatal("ParseAccess accepted an expired token")
	}

	handler := NewMiddleware(tokens).RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	req.Header.Set("Authorization", "Bearer "+expired)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expired token status = %d, want %d", res.Code, http.StatusUnauthorized)
	}
}

func TestRefreshRotatesAndKillsFamilyOnReuse(t *testing.T) {
	mux := newTestMux()
	res := doJSON(t, mux, http.MethodPost, "/users/register",
		`{"username":"budi","email":"budi@example.com","password":"password"}`, "")
	if res.Code != http.StatusCreated {
		t.Fatalf("register status = %d, want %d", res.Code, http.StatusCreated)
	}
	first := login(t, mux, "budi", "password")

	res = doJSON(t, mux, http.MethodPost, "/auth/refresh",
		`{"refresh_token":"`+first.RefreshToken+`"}`, "")
	if res.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	var second Pair
	if err := json.Unmarshal(res.Body.Bytes(), &second); err != nil {
		t.Fatalf("decode refresh response: %v", err)
	}
	if second.RefreshToken == "" || second.RefreshToken == first.RefreshToken {
		t.Fatal("refresh did not rotate the refresh token")
	}

	res = doJSON(t, mux, http.MethodPost, "/auth/refresh",
		`{"refresh_token":"`+first.RefreshToken+`"}`, "")
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("reused refresh status = %d, want %d", res.Code, http.StatusUnauthorized)
	}

	res = doJSON(t, mux, http.MethodPost, "/auth/refresh",
		`{"refresh_token":"`+second.RefreshToken+`"}`, "")
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("family-kill refresh status = %d, want %d", res.Code, http.StatusUnauthorized)
	}
}

func TestLogoutRevokesRefreshToken(t *testing.T) {
	mux := newTestMux()
	res := doJSON(t, mux, http.MethodPost, "/users/register",
		`{"username":"budi","email":"budi@example.com","password":"password"}`, "")
	if res.Code != http.StatusCreated {
		t.Fatalf("register status = %d, want %d", res.Code, http.StatusCreated)
	}
	pair := login(t, mux, "budi", "password")

	res = doJSON(t, mux, http.MethodPost, "/auth/logout",
		`{"refresh_token":"`+pair.RefreshToken+`"}`, "")
	if res.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, want %d", res.Code, http.StatusNoContent)
	}

	res = doJSON(t, mux, http.MethodPost, "/auth/refresh",
		`{"refresh_token":"`+pair.RefreshToken+`"}`, "")
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("refresh after logout status = %d, want %d", res.Code, http.StatusUnauthorized)
	}

	res = doJSON(t, mux, http.MethodPost, "/auth/logout", `{"refresh_token":"garbage"}`, "")
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("logout with garbage status = %d, want %d", res.Code, http.StatusUnauthorized)
	}
}

func TestAccessTokenCannotRefresh(t *testing.T) {
	mux := newTestMux()
	res := doJSON(t, mux, http.MethodPost, "/users/register",
		`{"username":"budi","email":"budi@example.com","password":"password"}`, "")
	if res.Code != http.StatusCreated {
		t.Fatalf("register status = %d, want %d", res.Code, http.StatusCreated)
	}
	pair := login(t, mux, "budi", "password")

	res = doJSON(t, mux, http.MethodPost, "/auth/refresh",
		`{"refresh_token":"`+pair.AccessToken+`"}`, "")
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("access token as refresh status = %d, want %d", res.Code, http.StatusUnauthorized)
	}
}
