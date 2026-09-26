package app

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/swegrowthid/sweg-ai-lists-backend/internal/auth"
	"github.com/swegrowthid/sweg-ai-lists-backend/internal/platform/config"
	"github.com/swegrowthid/sweg-ai-lists-backend/internal/post"
)

// newTestApp builds the production graph through app.New with R swapped: a nil
// pool makes every store in-memory, so no test opens Postgres. Same graph as
// production, different dependencies.
func newTestApp(t *testing.T) *App {
	t.Helper()
	cfg := config.Config{
		Service:       "sweg-ai-test",
		Env:           "test",
		Version:       "test",
		JWTSecret:     "test-secret-for-app-tests",
		JWTAccessTTL:  15 * time.Minute,
		JWTRefreshTTL: 24 * time.Hour,
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(cfg, log, nil)
}

// doJSON sends one JSON request through the app handler and records the reply.
func doJSON(t *testing.T, handler http.Handler, method, path, body, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	return res
}

// TestAppEndToEndWithMemoryStores walks the full production graph with
// in-memory stores: register, login, create a category, create a post, then
// read the post back. It proves the app.New wiring is correct and that the
// whole path runs without a database.
func TestAppEndToEndWithMemoryStores(t *testing.T) {
	handler := newTestApp(t).Handler()

	res := doJSON(t, handler, http.MethodPost, "/users/register",
		`{"username":"apptest","email":"apptest@example.com","password":"password"}`, "")
	if res.Code != http.StatusCreated {
		t.Fatalf("register status = %d, want %d; body = %s", res.Code, http.StatusCreated, res.Body.String())
	}

	res = doJSON(t, handler, http.MethodPost, "/auth/login",
		`{"identifier":"apptest","password":"password"}`, "")
	if res.Code != http.StatusOK {
		t.Fatalf("login status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	var pair auth.Pair
	if err := json.Unmarshal(res.Body.Bytes(), &pair); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if pair.AccessToken == "" {
		t.Fatal("login response misses the access token")
	}

	res = doJSON(t, handler, http.MethodPost, "/categories",
		`{"slug":"coding-agent","name":"Coding Agent"}`, pair.AccessToken)
	if res.Code != http.StatusCreated {
		t.Fatalf("create category status = %d, want %d; body = %s", res.Code, http.StatusCreated, res.Body.String())
	}

	res = doJSON(t, handler, http.MethodPost, "/posts", `{
		"slug": "setup-claude-code",
		"title": "Setup Claude Code",
		"categories": ["coding-agent"],
		"items": [
			{"kind": "markdown", "body_text": "# Intro"},
			{"kind": "link", "url": "https://example.com/docs"},
			{"kind": "file", "filename": "notes.md", "body_text": "# Notes"}
		]
	}`, pair.AccessToken)
	if res.Code != http.StatusCreated {
		t.Fatalf("create post status = %d, want %d; body = %s", res.Code, http.StatusCreated, res.Body.String())
	}
	var created post.Post
	if err := json.Unmarshal(res.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create post response: %v", err)
	}
	if len(created.Items) != 3 {
		t.Fatalf("created items = %d, want 3", len(created.Items))
	}

	res = doJSON(t, handler, http.MethodGet, "/posts/setup-claude-code", "", "")
	if res.Code != http.StatusOK {
		t.Fatalf("get post status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	var found post.Post
	if err := json.Unmarshal(res.Body.Bytes(), &found); err != nil {
		t.Fatalf("decode get post response: %v", err)
	}
	if found.ID != created.ID {
		t.Fatalf("detail id = %q, want %q", found.ID, created.ID)
	}
	if len(found.Items) != 3 {
		t.Fatalf("detail items = %d, want 3", len(found.Items))
	}

	res = doJSON(t, handler, http.MethodGet, "/posts?category=coding-agent", "", "")
	if res.Code != http.StatusOK {
		t.Fatalf("list posts status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
}
