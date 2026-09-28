package post

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestDeleteRemovesOwnPost(t *testing.T) {
	svc := NewService(NewMemoryStore())
	createCategory(t, svc, "alpha", "Alpha")
	created := seedPost(t, svc, "my-post", "My Post", "alpha", "", 2)

	if err := svc.Delete(context.Background(), "my-post", created.AuthorID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := svc.Get(context.Background(), "my-post"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() after delete error = %v, want ErrNotFound", err)
	}
	posts, err := svc.List(context.Background(), ListFilter{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(posts) != 0 {
		t.Fatalf("stored posts = %d, want 0 after delete", len(posts))
	}
}

func TestDeleteForeignPostForbidden(t *testing.T) {
	svc := NewService(NewMemoryStore())
	createCategory(t, svc, "alpha", "Alpha")
	created := seedPost(t, svc, "my-post", "My Post", "alpha", "", 1)

	// another author's delete is refused and the post survives
	if err := svc.Delete(context.Background(), "my-post", "user-2"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("Delete() error = %v, want ErrForbidden", err)
	}
	found, err := svc.Get(context.Background(), "my-post")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if found.ID != created.ID {
		t.Fatalf("stored id = %q, want %q", found.ID, created.ID)
	}
}

func TestDeleteMissingPostNotFound(t *testing.T) {
	svc := NewService(NewMemoryStore())

	if err := svc.Delete(context.Background(), "missing", "user-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete() error = %v, want ErrNotFound", err)
	}
}

func TestDeleteHandlerRemovesOwnPost(t *testing.T) {
	mux := newTestMux()
	_, token := registerAndLogin(t, mux, "budi")

	res := doJSON(t, mux, http.MethodPost, "/categories", `{"slug":"alpha","name":"Alpha"}`, token)
	if res.Code != http.StatusCreated {
		t.Fatalf("POST /categories status = %d, want %d; body = %s", res.Code, http.StatusCreated, res.Body.String())
	}
	res = doJSON(t, mux, http.MethodPost, "/posts", `{"title":"My Post","category":"alpha","items":[{"kind":"markdown","body_text":"# Body"}]}`, token)
	if res.Code != http.StatusCreated {
		t.Fatalf("POST /posts status = %d, want %d; body = %s", res.Code, http.StatusCreated, res.Body.String())
	}

	res = doJSON(t, mux, http.MethodDelete, "/posts/my-post", "", token)
	if res.Code != http.StatusNoContent {
		t.Fatalf("DELETE /posts/my-post status = %d, want %d; body = %s", res.Code, http.StatusNoContent, res.Body.String())
	}
	if strings.TrimSpace(res.Body.String()) != "" {
		t.Fatalf("DELETE body = %q, want empty", res.Body.String())
	}

	// the post is gone for reads and for a second delete
	res = doJSON(t, mux, http.MethodGet, "/posts/my-post", "", "")
	if res.Code != http.StatusNotFound {
		t.Fatalf("GET /posts/my-post status = %d, want %d", res.Code, http.StatusNotFound)
	}
	res = doJSON(t, mux, http.MethodDelete, "/posts/my-post", "", token)
	if res.Code != http.StatusNotFound {
		t.Fatalf("second DELETE status = %d, want %d; body = %s", res.Code, http.StatusNotFound, res.Body.String())
	}
}

func TestDeleteHandlerMissingToken(t *testing.T) {
	mux := newTestMux()

	res := doJSON(t, mux, http.MethodDelete, "/posts/anything", "", "")
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("DELETE without token status = %d, want %d; body = %s", res.Code, http.StatusUnauthorized, res.Body.String())
	}
	if res.Header().Get("WWW-Authenticate") == "" {
		t.Fatal("401 misses WWW-Authenticate header")
	}
}

// TestDeleteHandlerForeignPost registers a second user and lets it try to
// delete the first user's post: 403 and the post stays readable.
func TestDeleteHandlerForeignPost(t *testing.T) {
	mux := newTestMux()
	_, ownerToken := registerAndLogin(t, mux, "budi")
	_, otherToken := registerAndLogin(t, mux, "siti")

	res := doJSON(t, mux, http.MethodPost, "/categories", `{"slug":"alpha","name":"Alpha"}`, ownerToken)
	if res.Code != http.StatusCreated {
		t.Fatalf("POST /categories status = %d, want %d; body = %s", res.Code, http.StatusCreated, res.Body.String())
	}
	res = doJSON(t, mux, http.MethodPost, "/posts", `{"slug":"my-post","title":"My Post","category":"alpha","items":[{"kind":"markdown","body_text":"# Body"}]}`, ownerToken)
	if res.Code != http.StatusCreated {
		t.Fatalf("POST /posts status = %d, want %d; body = %s", res.Code, http.StatusCreated, res.Body.String())
	}

	res = doJSON(t, mux, http.MethodDelete, "/posts/my-post", "", otherToken)
	if res.Code != http.StatusForbidden {
		t.Fatalf("DELETE by another user status = %d, want %d; body = %s", res.Code, http.StatusForbidden, res.Body.String())
	}
	if got := strings.TrimSpace(res.Body.String()); got != "not your post" {
		t.Fatalf("DELETE message = %q, want %q", got, "not your post")
	}

	res = doJSON(t, mux, http.MethodGet, "/posts/my-post", "", "")
	if res.Code != http.StatusOK {
		t.Fatalf("GET /posts/my-post status = %d, want %d", res.Code, http.StatusOK)
	}
}

func TestDeleteHandlerUnknownSlug(t *testing.T) {
	mux := newTestMux()
	_, token := registerAndLogin(t, mux, "budi")

	res := doJSON(t, mux, http.MethodDelete, "/posts/missing", "", token)
	if res.Code != http.StatusNotFound {
		t.Fatalf("DELETE /posts/missing status = %d, want %d; body = %s", res.Code, http.StatusNotFound, res.Body.String())
	}
	if got := strings.TrimSpace(res.Body.String()); got != "post not found" {
		t.Fatalf("DELETE message = %q, want %q", got, "post not found")
	}
}
