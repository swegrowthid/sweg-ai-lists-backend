package post

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestUpdateRewritesOwnPost(t *testing.T) {
	svc := NewService(NewMemoryStore())
	createCategory(t, svc, "alpha", "Alpha")
	createCategory(t, svc, "beta", "Beta")
	createDerivative(t, svc, "beta-child", "Beta Child", "beta")
	created := seedPost(t, svc, "my-post", "My Post", "alpha", "")

	updated, err := svc.Update(context.Background(), "my-post", created.AuthorID, UpdatePostInput{
		Title:          "  Renamed Post  ",
		CategorySlug:   "beta",
		DerivativeSlug: "beta-child",
		Value:          "# New Body",
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	if updated.ID != created.ID || updated.Slug != "my-post" {
		t.Fatalf("id/slug = %q/%q, want the created %q/my-post: slug never changes", updated.ID, updated.Slug, created.ID)
	}
	if updated.Title != "Renamed Post" {
		t.Fatalf("title = %q, want the trimmed Renamed Post", updated.Title)
	}
	if updated.AuthorID != created.AuthorID {
		t.Fatalf("author_id = %q, want %q", updated.AuthorID, created.AuthorID)
	}
	if got := slugsInOrder(updated.Categories); got != "beta,beta-child" {
		t.Fatalf("category slugs = %q, want beta,beta-child", got)
	}
	if len(updated.Items) != 1 || updated.Items[0].Position != 0 {
		t.Fatalf("items = %+v, want the one item at position 0", updated.Items)
	}
	if got := updated.Items[0].Value; got != "# New Body" {
		t.Fatalf("items[0].value = %q, want the new value", got)
	}
	if updated.Items[0].ID != created.Items[0].ID {
		t.Fatalf("item id = %q, want the stored %q", updated.Items[0].ID, created.Items[0].ID)
	}
	if !updated.CreatedAt.Equal(created.CreatedAt) {
		t.Fatalf("created_at = %v, want the stored %v", updated.CreatedAt, created.CreatedAt)
	}
	if updated.UpdatedAt.Before(created.UpdatedAt) {
		t.Fatalf("updated_at = %v, want it at or after %v", updated.UpdatedAt, created.UpdatedAt)
	}

	// the write persisted: the detail read returns the same shape
	stored, err := svc.Get(context.Background(), "my-post")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.Title != "Renamed Post" || stored.Items[0].Value != "# New Body" {
		t.Fatalf("stored post = %+v, want the renamed post with the new value", stored)
	}
	if got := slugsInOrder(stored.Categories); got != "beta,beta-child" {
		t.Fatalf("stored category slugs = %q, want beta,beta-child", got)
	}
}

// TestUpdateKeepsValueExactBytes proves the update keeps the byte-exact rule
// from create: leading spaces and the trailing newline survive the store.
func TestUpdateKeepsValueExactBytes(t *testing.T) {
	svc := NewService(NewMemoryStore())
	createCategory(t, svc, "alpha", "Alpha")
	created := seedPost(t, svc, "my-post", "My Post", "alpha", "")

	const value = "    kode\n\nakhir\n"
	updated, err := svc.Update(context.Background(), "my-post", created.AuthorID, UpdatePostInput{
		Title:        "My Post",
		CategorySlug: "alpha",
		Value:        value,
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if got := updated.Items[0].Value; got != value {
		t.Fatalf("items[0].value = %q, want the exact bytes %q", got, value)
	}
}

func TestUpdateRejectsInvalidInput(t *testing.T) {
	svc := NewService(NewMemoryStore())
	createCategory(t, svc, "alpha", "Alpha")
	createCategory(t, svc, "beta", "Beta")
	createDerivative(t, svc, "alpha-child", "Alpha Child", "alpha")
	created := seedPost(t, svc, "my-post", "My Post", "alpha", "")

	valid := func() UpdatePostInput {
		return UpdatePostInput{
			Title:        "Valid Post",
			CategorySlug: "alpha",
			Value:        "# Body",
		}
	}

	cases := []struct {
		name string
		with func(input *UpdatePostInput)
	}{
		{"empty title", func(in *UpdatePostInput) { in.Title = "" }},
		{"title over 200 runes", func(in *UpdatePostInput) { in.Title = strings.Repeat("é", 201) }},
		{"title with NUL byte", func(in *UpdatePostInput) { in.Title = "bad\x00title" }},
		{"empty category", func(in *UpdatePostInput) { in.CategorySlug = "" }},
		{"category derivative mismatches the category slug", func(in *UpdatePostInput) {
			in.CategorySlug = "alpha-child"
			in.DerivativeSlug = "beta"
		}},
		{"derivative belongs to another category", func(in *UpdatePostInput) {
			in.CategorySlug = "alpha"
			in.DerivativeSlug = "beta"
		}},
		{"empty value", func(in *UpdatePostInput) { in.Value = "" }},
		{"blank value", func(in *UpdatePostInput) { in.Value = "  \n\t " }},
		{"value with NUL byte", func(in *UpdatePostInput) { in.Value = "a\x00b" }},
		{"value over 64 KiB", func(in *UpdatePostInput) { in.Value = strings.Repeat("a", 64<<10+1) }},
	}

	for _, tc := range cases {
		input := valid()
		tc.with(&input)
		if _, err := svc.Update(context.Background(), "my-post", created.AuthorID, input); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("Update() with %s error = %v, want ErrInvalidInput", tc.name, err)
		}
	}

	// every rejected update left the stored post untouched
	stored, err := svc.Get(context.Background(), "my-post")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.Title != "My Post" || stored.Items[0].Value != "# my-post" {
		t.Fatalf("stored post = %+v, want the seeded My Post untouched", stored)
	}
}

func TestUpdateUnknownCategoryRejected(t *testing.T) {
	svc := NewService(NewMemoryStore())
	createCategory(t, svc, "alpha", "Alpha")
	seedPost(t, svc, "my-post", "My Post", "alpha", "")

	_, err := svc.Update(context.Background(), "my-post", "user-1", UpdatePostInput{
		Title:          "My Post",
		CategorySlug:   "alpha",
		DerivativeSlug: "nope",
		Value:          "body",
	})
	if !errors.Is(err, ErrUnknownCategory) {
		t.Fatalf("Update() error = %v, want ErrUnknownCategory", err)
	}
}

// TestUpdateForeignPostForbidden proves another author's update is refused
// before any field is checked, and the post survives untouched.
func TestUpdateForeignPostForbidden(t *testing.T) {
	svc := NewService(NewMemoryStore())
	createCategory(t, svc, "alpha", "Alpha")
	created := seedPost(t, svc, "my-post", "My Post", "alpha", "")

	if _, err := svc.Update(context.Background(), "my-post", "user-2", UpdatePostInput{
		Title:        "Stolen",
		CategorySlug: "alpha",
		Value:        "# Stolen",
	}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("Update() error = %v, want ErrForbidden", err)
	}

	stored, err := svc.Get(context.Background(), "my-post")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.ID != created.ID || stored.Title != "My Post" {
		t.Fatalf("stored post = %+v, want the seeded My Post untouched", stored)
	}
}

func TestUpdateMissingPostNotFound(t *testing.T) {
	svc := NewService(NewMemoryStore())

	if _, err := svc.Update(context.Background(), "missing", "user-1", UpdatePostInput{
		Title:        "Missing",
		CategorySlug: "alpha",
		Value:        "body",
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Update() error = %v, want ErrNotFound", err)
	}
}

// TestUpdateHandlerRewritesOwnPost walks the full handler path: PUT answers
// 200 with the updated post, and the detail read returns the same shape.
func TestUpdateHandlerRewritesOwnPost(t *testing.T) {
	mux := newTestMux()
	userID, token := registerAndLogin(t, mux, "budi")

	res := doJSON(t, mux, http.MethodPost, "/categories", `{"slug":"alpha","name":"Alpha"}`, token)
	if res.Code != http.StatusCreated {
		t.Fatalf("POST /categories status = %d, want %d; body = %s", res.Code, http.StatusCreated, res.Body.String())
	}
	res = doJSON(t, mux, http.MethodPost, "/categories", `{"slug":"beta","name":"Beta"}`, token)
	if res.Code != http.StatusCreated {
		t.Fatalf("POST beta status = %d, want %d; body = %s", res.Code, http.StatusCreated, res.Body.String())
	}
	res = doJSON(t, mux, http.MethodPost, "/posts", `{"slug":"my-post","title":"My Post","category":"alpha","value":"# Body"}`, token)
	if res.Code != http.StatusCreated {
		t.Fatalf("POST /posts status = %d, want %d; body = %s", res.Code, http.StatusCreated, res.Body.String())
	}

	res = doJSON(t, mux, http.MethodPut, "/posts/my-post", `{
		"title": "Renamed Post",
		"category": "beta",
		"value": "# New Body"
	}`, token)
	if res.Code != http.StatusOK {
		t.Fatalf("PUT /posts/my-post status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	var updated Post
	if err := json.Unmarshal(res.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode updated post: %v", err)
	}
	if updated.Slug != "my-post" || updated.Title != "Renamed Post" {
		t.Fatalf("slug/title = %q/%q, want my-post/Renamed Post", updated.Slug, updated.Title)
	}
	if updated.AuthorID != userID || updated.AuthorUsername != "budi" {
		t.Fatalf("author = %q/%q, want %q/budi", updated.AuthorID, updated.AuthorUsername, userID)
	}
	if got := slugsInOrder(updated.Categories); got != "beta" {
		t.Fatalf("category slugs = %q, want beta", got)
	}
	if len(updated.Items) != 1 || updated.Items[0].Value != "# New Body" {
		t.Fatalf("items = %+v, want the one item with the new value", updated.Items)
	}

	res = doJSON(t, mux, http.MethodGet, "/posts/my-post", "", "")
	if res.Code != http.StatusOK {
		t.Fatalf("GET /posts/my-post status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	var stored Post
	if err := json.Unmarshal(res.Body.Bytes(), &stored); err != nil {
		t.Fatalf("decode stored post: %v", err)
	}
	if stored.Title != "Renamed Post" || stored.Items[0].Value != "# New Body" {
		t.Fatalf("stored post = %+v, want the renamed post with the new value", stored)
	}
}

func TestUpdateHandlerMissingToken(t *testing.T) {
	mux := newTestMux()

	res := doJSON(t, mux, http.MethodPut, "/posts/anything", `{"title":"No Token","category":"alpha","value":"# Body"}`, "")
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("PUT without token status = %d, want %d; body = %s", res.Code, http.StatusUnauthorized, res.Body.String())
	}
	if res.Header().Get("WWW-Authenticate") == "" {
		t.Fatal("401 misses WWW-Authenticate header")
	}
}

// TestUpdateHandlerForeignPost registers a second user and lets it try to
// update the first user's post: 403 and the post stays readable, untouched.
func TestUpdateHandlerForeignPost(t *testing.T) {
	mux := newTestMux()
	_, ownerToken := registerAndLogin(t, mux, "budi")
	_, otherToken := registerAndLogin(t, mux, "siti")

	res := doJSON(t, mux, http.MethodPost, "/categories", `{"slug":"alpha","name":"Alpha"}`, ownerToken)
	if res.Code != http.StatusCreated {
		t.Fatalf("POST /categories status = %d, want %d; body = %s", res.Code, http.StatusCreated, res.Body.String())
	}
	res = doJSON(t, mux, http.MethodPost, "/posts", `{"slug":"my-post","title":"My Post","category":"alpha","value":"# Body"}`, ownerToken)
	if res.Code != http.StatusCreated {
		t.Fatalf("POST /posts status = %d, want %d; body = %s", res.Code, http.StatusCreated, res.Body.String())
	}

	res = doJSON(t, mux, http.MethodPut, "/posts/my-post", `{"title":"Stolen","category":"alpha","value":"# Stolen"}`, otherToken)
	if res.Code != http.StatusForbidden {
		t.Fatalf("PUT by another user status = %d, want %d; body = %s", res.Code, http.StatusForbidden, res.Body.String())
	}
	if got := strings.TrimSpace(res.Body.String()); got != "not your post" {
		t.Fatalf("PUT message = %q, want %q", got, "not your post")
	}

	res = doJSON(t, mux, http.MethodGet, "/posts/my-post", "", "")
	if res.Code != http.StatusOK {
		t.Fatalf("GET /posts/my-post status = %d, want %d", res.Code, http.StatusOK)
	}
	var stored Post
	if err := json.Unmarshal(res.Body.Bytes(), &stored); err != nil {
		t.Fatalf("decode stored post: %v", err)
	}
	if stored.Title != "My Post" || stored.Items[0].Value != "# Body" {
		t.Fatalf("stored post = %+v, want My Post untouched", stored)
	}
}

func TestUpdateHandlerUnknownSlug(t *testing.T) {
	mux := newTestMux()
	_, token := registerAndLogin(t, mux, "budi")

	res := doJSON(t, mux, http.MethodPut, "/posts/missing", `{"title":"Missing","category":"alpha","value":"body"}`, token)
	if res.Code != http.StatusNotFound {
		t.Fatalf("PUT /posts/missing status = %d, want %d; body = %s", res.Code, http.StatusNotFound, res.Body.String())
	}
	if got := strings.TrimSpace(res.Body.String()); got != "post not found" {
		t.Fatalf("PUT message = %q, want %q", got, "post not found")
	}
}

// TestUpdateHandlerRejectsBadRequests proves the create decode and validation
// rules apply verbatim on update, and every rejected call leaves the stored
// post untouched. slug is not a body field: the path names the post.
func TestUpdateHandlerRejectsBadRequests(t *testing.T) {
	mux := newTestMux()
	_, token := registerAndLogin(t, mux, "budi")

	res := doJSON(t, mux, http.MethodPost, "/categories", `{"slug":"alpha","name":"Alpha"}`, token)
	if res.Code != http.StatusCreated {
		t.Fatalf("POST /categories status = %d, want %d; body = %s", res.Code, http.StatusCreated, res.Body.String())
	}
	res = doJSON(t, mux, http.MethodPost, "/posts", `{"slug":"my-post","title":"My Post","category":"alpha","value":"# Body"}`, token)
	if res.Code != http.StatusCreated {
		t.Fatalf("POST /posts status = %d, want %d; body = %s", res.Code, http.StatusCreated, res.Body.String())
	}

	cases := []struct {
		name    string
		body    string
		message string
	}{
		{
			name:    "slug in body",
			body:    `{"slug":"renamed","title":"My Post","category":"alpha","value":"body"}`,
			message: "invalid request body",
		},
		{
			name:    "author_id in body",
			body:    `{"title":"My Post","category":"alpha","value":"body","author_id":"someone-else"}`,
			message: "invalid request body",
		},
		{
			name:    "malformed json",
			body:    `{"title":`,
			message: "invalid request body",
		},
		{
			name:    "blank title",
			body:    `{"title":"   ","category":"alpha","value":"body"}`,
			message: "invalid input",
		},
		{
			name:    "blank value",
			body:    `{"title":"My Post","category":"alpha","value":"   "}`,
			message: "invalid input",
		},
		{
			name:    "unknown category",
			body:    `{"title":"My Post","category":"nope","value":"body"}`,
			message: "unknown category",
		},
	}

	for _, tc := range cases {
		res := doJSON(t, mux, http.MethodPut, "/posts/my-post", tc.body, token)
		if res.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d, want %d; body = %s", tc.name, res.Code, http.StatusBadRequest, res.Body.String())
		}
		if got := strings.TrimSpace(res.Body.String()); got != tc.message {
			t.Fatalf("%s message = %q, want %q", tc.name, got, tc.message)
		}
	}

	res = doJSON(t, mux, http.MethodGet, "/posts/my-post", "", "")
	if res.Code != http.StatusOK {
		t.Fatalf("GET /posts/my-post status = %d, want %d", res.Code, http.StatusOK)
	}
	var stored Post
	if err := json.Unmarshal(res.Body.Bytes(), &stored); err != nil {
		t.Fatalf("decode stored post: %v", err)
	}
	if stored.Title != "My Post" || stored.Items[0].Value != "# Body" {
		t.Fatalf("stored post = %+v, want My Post untouched", stored)
	}
}
