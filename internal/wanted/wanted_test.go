package wanted

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/swegrowthid/sweg-ai-lists-backend/internal/auth"
	"github.com/swegrowthid/sweg-ai-lists-backend/internal/post"
	"github.com/swegrowthid/sweg-ai-lists-backend/internal/user"
)

const testSecret = "test-secret-for-wanted-tests"

// newTestMux builds the same graph as app.New with memory stores: post plus
// wanted on one mux, so add/list/remove travel the full handler path.
func newTestMux() *http.ServeMux {
	userSvc := user.NewService(user.NewMemoryStore())
	tokens := auth.NewTokens(testSecret, 24*time.Hour, 24*time.Hour)
	requireAuth := auth.NewMiddleware(tokens).RequireAuth

	postSvc := post.NewService(post.NewMemoryStore())
	mux := http.NewServeMux()
	user.NewHandler(userSvc, nil).RegisterRoutes(mux, requireAuth)
	auth.NewHandler(auth.NewService(userSvc, tokens, auth.NewMemoryRefreshStore()), nil).RegisterRoutes(mux)
	post.NewHandler(postSvc, nil).RegisterRoutes(mux, requireAuth)
	NewHandler(NewService(NewMemoryStore(), postSvc), nil).RegisterRoutes(mux)
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

// seedPost registers one user, logs in, creates one category and one post,
// then returns the post slug.
func seedPost(t *testing.T, mux *http.ServeMux, username, slug string) {
	t.Helper()
	res := doJSON(t, mux, http.MethodPost, "/users/register",
		`{"username":"`+username+`","email":"`+username+`@example.com","password":"password"}`, "")
	if res.Code != http.StatusCreated {
		t.Fatalf("register status = %d, want %d; body = %s", res.Code, http.StatusCreated, res.Body.String())
	}
	res = doJSON(t, mux, http.MethodPost, "/auth/login",
		`{"identifier":"`+username+`","password":"password"}`, "")
	if res.Code != http.StatusOK {
		t.Fatalf("login status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	var pair auth.Pair
	if err := json.Unmarshal(res.Body.Bytes(), &pair); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	res = doJSON(t, mux, http.MethodPost, "/categories",
		`{"slug":"coding","name":"Coding"}`, pair.AccessToken)
	if res.Code != http.StatusCreated {
		t.Fatalf("create category status = %d, want %d; body = %s", res.Code, http.StatusCreated, res.Body.String())
	}
	res = doJSON(t, mux, http.MethodPost, "/posts", `{
		"slug": "`+slug+`",
		"title": "Title `+slug+`",
		"category": "coding",
		"items": [{"kind": "markdown", "body_text": "# Intro"}]
	}`, pair.AccessToken)
	if res.Code != http.StatusCreated {
		t.Fatalf("create post status = %d, want %d; body = %s", res.Code, http.StatusCreated, res.Body.String())
	}
}

// TestWantedFlowIsPublicAndIdempotent walks the whole flow without auth:
// empty list, add (201), duplicate add (200), full-post list, remove (204),
// then remove again (404) and add of a missing slug (404).
func TestWantedFlowIsPublicAndIdempotent(t *testing.T) {
	mux := newTestMux()
	seedPost(t, mux, "wantuser", "setup-claude-code")

	res := doJSON(t, mux, http.MethodGet, "/wanted", "", "")
	if res.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	if body := strings.TrimSpace(res.Body.String()); body != "[]" {
		t.Fatalf("list body = %q, want an empty list", body)
	}

	res = doJSON(t, mux, http.MethodPost, "/wanted", `{"slug":"setup-claude-code"}`, "")
	if res.Code != http.StatusCreated {
		t.Fatalf("add status = %d, want %d; body = %s", res.Code, http.StatusCreated, res.Body.String())
	}
	var added post.Post
	if err := json.Unmarshal(res.Body.Bytes(), &added); err != nil {
		t.Fatalf("decode add response: %v", err)
	}
	if added.Slug != "setup-claude-code" {
		t.Fatalf("added slug = %q, want setup-claude-code", added.Slug)
	}
	if len(added.Items) != 0 {
		t.Fatalf("added items = %d, want list shape without items", len(added.Items))
	}

	res = doJSON(t, mux, http.MethodPost, "/wanted", `{"slug":"setup-claude-code"}`, "")
	if res.Code != http.StatusOK {
		t.Fatalf("duplicate status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}

	res = doJSON(t, mux, http.MethodGet, "/wanted", "", "")
	if res.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	var listed []post.Post
	if err := json.Unmarshal(res.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed) != 1 || listed[0].Slug != "setup-claude-code" {
		t.Fatalf("listed = %+v, want the one wanted post", listed)
	}
	if listed[0].AuthorUsername == "" || len(listed[0].Categories) != 1 {
		t.Fatalf("listed post = %+v, want full card fields", listed[0])
	}

	res = doJSON(t, mux, http.MethodDelete, "/wanted/setup-claude-code", "", "")
	if res.Code != http.StatusNoContent {
		t.Fatalf("remove status = %d, want %d; body = %s", res.Code, http.StatusNoContent, res.Body.String())
	}

	res = doJSON(t, mux, http.MethodDelete, "/wanted/setup-claude-code", "", "")
	if res.Code != http.StatusNotFound {
		t.Fatalf("second remove status = %d, want %d; body = %s", res.Code, http.StatusNotFound, res.Body.String())
	}

	res = doJSON(t, mux, http.MethodPost, "/wanted", `{"slug":"missing-post"}`, "")
	if res.Code != http.StatusNotFound {
		t.Fatalf("add missing status = %d, want %d; body = %s", res.Code, http.StatusNotFound, res.Body.String())
	}
}

// TestWantedRejectsBadInput pins the 400 edges: blank slug and unknown field.
func TestWantedRejectsBadInput(t *testing.T) {
	mux := newTestMux()

	for _, body := range []string{`{"slug":""}`, `{"slug":"a","extra":1}`, `{}`} {
		res := doJSON(t, mux, http.MethodPost, "/wanted", body, "")
		if res.Code != http.StatusBadRequest {
			t.Fatalf("add %s status = %d, want %d; body = %s", body, res.Code, http.StatusBadRequest, res.Body.String())
		}
	}
}
