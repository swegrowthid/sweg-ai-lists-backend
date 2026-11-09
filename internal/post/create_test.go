package post

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/swegrowthid/sweg-ai-lists-backend/internal/auth"
	"github.com/swegrowthid/sweg-ai-lists-backend/internal/user"
)

const testSecret = "test-secret-for-post-tests"

// newTestMux builds the same graph as app.New with memory stores (swap R).
func newTestMux() *http.ServeMux {
	mux, _ := newTestMuxWithStore()
	return mux
}

// newTestMuxWithStore also hands back the post store, so a test can seed or
// inspect it while every request still travels the full handler path.
func newTestMuxWithStore() (*http.ServeMux, *MemoryStore) {
	userSvc := user.NewService(user.NewMemoryStore())
	tokens := auth.NewTokens(testSecret, 24*time.Hour, 24*time.Hour)
	requireAuth := auth.NewMiddleware(tokens).RequireAuth

	store := NewMemoryStore()
	mux := http.NewServeMux()
	user.NewHandler(userSvc, nil).RegisterRoutes(mux, requireAuth)
	auth.NewHandler(auth.NewService(userSvc, tokens, auth.NewMemoryRefreshStore()), nil).RegisterRoutes(mux)
	NewHandler(NewService(store), nil).RegisterRoutes(mux, requireAuth)
	return mux, store
}

// doJSON sends one JSON request through the mux and records the response.
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

// registerAndLogin registers one user and returns its id plus an access token.
func registerAndLogin(t *testing.T, mux *http.ServeMux, username string) (string, string) {
	t.Helper()
	res := doJSON(t, mux, http.MethodPost, "/users/register",
		`{"username":"`+username+`","email":"`+username+`@example.com","password":"password"}`, "")
	if res.Code != http.StatusCreated {
		t.Fatalf("register status = %d, want %d; body = %s", res.Code, http.StatusCreated, res.Body.String())
	}
	var registered user.User
	if err := json.Unmarshal(res.Body.Bytes(), &registered); err != nil {
		t.Fatalf("decode register response: %v", err)
	}
	if registered.ID == "" {
		t.Fatal("register response misses the user id")
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
	if pair.AccessToken == "" {
		t.Fatal("login response misses the access token")
	}
	return registered.ID, pair.AccessToken
}

// createCategory seeds one top-level category through the service.
func createCategory(t *testing.T, svc *Service, slug, name string) Category {
	t.Helper()
	created, err := svc.CreateCategory(context.Background(), CreateCategoryInput{Slug: slug, Name: name})
	if err != nil {
		t.Fatalf("CreateCategory(%q) error = %v", slug, err)
	}
	return created
}

// createDerivative seeds one derivative under an existing top-level category.
func createDerivative(t *testing.T, svc *Service, slug, name, parent string) Category {
	t.Helper()
	created, err := svc.CreateCategory(context.Background(), CreateCategoryInput{Slug: slug, Name: name, ParentSlug: parent})
	if err != nil {
		t.Fatalf("CreateCategory(%q, parent %q) error = %v", slug, parent, err)
	}
	return created
}

// slugsInOrder lists category slugs in response order.
func slugsInOrder(categories []Category) string {
	slugs := make([]string, 0, len(categories))
	for _, category := range categories {
		slugs = append(slugs, category.Slug)
	}
	return strings.Join(slugs, ",")
}

func TestCreateStoresCategoriesAndItem(t *testing.T) {
	svc := NewService(NewMemoryStore())
	createCategory(t, svc, "coding-agent", "Coding Agent")
	createDerivative(t, svc, "setup", "Setup", "coding-agent")

	created, err := svc.Create(context.Background(), CreatePostInput{
		Slug:           "setup-claude-code",
		Title:          "Setup Claude Code",
		AuthorID:       "user-1",
		CategorySlug:   "coding-agent",
		DerivativeSlug: "setup",
		Value:          "# Setup\n\nInstall the CLI.",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if created.ID == "" {
		t.Fatal("created post misses an id")
	}
	if created.Slug != "setup-claude-code" || created.Title != "Setup Claude Code" {
		t.Fatalf("slug/title = %q/%q, want setup-claude-code/Setup Claude Code", created.Slug, created.Title)
	}
	if created.AuthorID != "user-1" {
		t.Fatalf("author_id = %q, want user-1", created.AuthorID)
	}
	if got := slugsInOrder(created.Categories); got != "coding-agent,setup" {
		t.Fatalf("category slugs = %q, want the name order coding-agent,setup", got)
	}
	if len(created.Items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(created.Items))
	}
	if created.Items[0].ID == "" || created.Items[0].Position != 0 {
		t.Fatalf("items[0] = %+v, want an id at position 0", created.Items[0])
	}
	if got := created.Items[0].Value; got != "# Setup\n\nInstall the CLI." {
		t.Fatalf("items[0].value = %q, want the value", got)
	}

	// the write persisted: the detail read returns the same shape
	stored, err := svc.Get(context.Background(), "setup-claude-code")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.ID != created.ID {
		t.Fatalf("stored id = %q, want %q", stored.ID, created.ID)
	}
	if got := slugsInOrder(stored.Categories); got != "coding-agent,setup" {
		t.Fatalf("stored category slugs = %q, want coding-agent,setup", got)
	}
	if len(stored.Items) != 1 || stored.Items[0].Value != created.Items[0].Value {
		t.Fatalf("stored items = %+v, want the one item with the same value", stored.Items)
	}
}

func TestCreateNormalizesSlugAndTitle(t *testing.T) {
	svc := NewService(NewMemoryStore())
	createCategory(t, svc, "setup", "Setup")

	// Only the edges are normalized: the slug must already be URL-safe, so an
	// inner space stays invalid (see the invalid-input table). value is the
	// one payload that keeps its exact bytes.
	created, err := svc.Create(context.Background(), CreatePostInput{
		Slug:         " Setup-Claude-Code ",
		Title:        "  Setup Claude Code  ",
		AuthorID:     "user-1",
		CategorySlug: " setup ",
		Value:        "  hello  ",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Slug != "setup-claude-code" {
		t.Fatalf("slug = %q, want setup-claude-code", created.Slug)
	}
	if created.Title != "Setup Claude Code" {
		t.Fatalf("title = %q, want the trimmed title", created.Title)
	}
	if got := slugsInOrder(created.Categories); got != "setup" {
		t.Fatalf("category slugs = %q, want the trimmed setup", got)
	}
	if got := created.Items[0].Value; got != "  hello  " {
		t.Fatalf("items[0].value = %q, want the exact bytes %q", got, "  hello  ")
	}

	// the normalized slug is the stored handle, and the exact bytes persisted
	stored, err := svc.Get(context.Background(), "setup-claude-code")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.ID != created.ID {
		t.Fatalf("stored id = %q, want %q", stored.ID, created.ID)
	}
	if got := stored.Items[0].Value; got != "  hello  " {
		t.Fatalf("stored items[0].value = %q, want the exact bytes %q", got, "  hello  ")
	}
}

func TestCreateRejectsInvalidInput(t *testing.T) {
	svc := NewService(NewMemoryStore())
	createCategory(t, svc, "alpha", "Alpha")
	createCategory(t, svc, "beta", "Beta")
	createDerivative(t, svc, "alpha-child", "Alpha Child", "alpha")

	valid := func() CreatePostInput {
		return CreatePostInput{
			Slug:         "valid-post",
			Title:        "Valid Post",
			AuthorID:     "user-1",
			CategorySlug: "alpha",
			Value:        "# Body",
		}
	}

	cases := []struct {
		name string
		with func(input *CreatePostInput)
	}{
		{"empty title", func(in *CreatePostInput) { in.Title = "" }},
		{"title over 200 runes", func(in *CreatePostInput) { in.Title = strings.Repeat("é", 201) }},
		{"empty author", func(in *CreatePostInput) { in.AuthorID = " " }},
		{"slug with space and bang", func(in *CreatePostInput) { in.Slug = "bad slug!" }},
		{"slug with double dash", func(in *CreatePostInput) { in.Slug = "bad--slug" }},
		{"slug with inner space", func(in *CreatePostInput) { in.Slug = "setup-claude code" }},
		{"empty category", func(in *CreatePostInput) { in.CategorySlug = "" }},
		{"category derivative mismatches the category slug", func(in *CreatePostInput) {
			in.CategorySlug = "alpha-child"
			in.DerivativeSlug = "beta"
		}},
		{"derivative belongs to another category", func(in *CreatePostInput) {
			in.CategorySlug = "alpha"
			in.DerivativeSlug = "beta"
		}},
		{"empty value", func(in *CreatePostInput) { in.Value = "" }},
		{"blank value", func(in *CreatePostInput) { in.Value = "  \n\t " }},
		{"value with NUL byte", func(in *CreatePostInput) { in.Value = "a\x00b" }},
		{"value over 64 KiB", func(in *CreatePostInput) { in.Value = strings.Repeat("a", 64<<10+1) }},
	}

	for _, tc := range cases {
		input := valid()
		tc.with(&input)
		if _, err := svc.Create(context.Background(), input); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("Create() with %s error = %v, want ErrInvalidInput", tc.name, err)
		}
	}

	// a rejected input leaves no post behind
	posts, err := svc.List(context.Background(), ListFilter{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(posts) != 0 {
		t.Fatalf("stored posts = %d, want 0 after rejected input", len(posts))
	}
}

func TestCreateDuplicateCategorySlugConflicts(t *testing.T) {
	svc := NewService(NewMemoryStore())
	createCategory(t, svc, "alpha", "Alpha")

	// the slug is normalized before the unique check
	if _, err := svc.CreateCategory(context.Background(), CreateCategoryInput{Slug: " alpha ", Name: "Other"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("CreateCategory() duplicate error = %v, want ErrConflict", err)
	}

	categories, err := svc.ListCategories(context.Background())
	if err != nil {
		t.Fatalf("ListCategories() error = %v", err)
	}
	if len(categories) != 1 {
		t.Fatalf("stored categories = %d, want 1", len(categories))
	}
}

func TestCreateDuplicatePostSlugConflicts(t *testing.T) {
	svc := NewService(NewMemoryStore())
	createCategory(t, svc, "alpha", "Alpha")

	if _, err := svc.Create(context.Background(), CreatePostInput{
		Slug:         "first-post",
		Title:        "First Post",
		AuthorID:     "user-1",
		CategorySlug: "alpha",
		Value:        "# First",
	}); err != nil {
		t.Fatalf("first Create() error = %v", err)
	}

	// the slug is normalized before the unique check
	if _, err := svc.Create(context.Background(), CreatePostInput{
		Slug:         " First-Post ",
		Title:        "Second Post",
		AuthorID:     "user-1",
		CategorySlug: "alpha",
		Value:        "body",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate Create() error = %v, want ErrConflict", err)
	}

	posts, err := svc.List(context.Background(), ListFilter{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(posts) != 1 || posts[0].Title != "First Post" {
		t.Fatalf("stored posts = %+v, want only First Post", posts)
	}
}

func TestCreateUnknownCategoryRejected(t *testing.T) {
	svc := NewService(NewMemoryStore())
	createCategory(t, svc, "alpha", "Alpha")

	_, err := svc.Create(context.Background(), CreatePostInput{
		Slug:           "unknown-category",
		Title:          "Unknown Category",
		AuthorID:       "user-1",
		CategorySlug:   "alpha",
		DerivativeSlug: "nope",
		Value:          "body",
	})
	if !errors.Is(err, ErrUnknownCategory) {
		t.Fatalf("Create() error = %v, want ErrUnknownCategory", err)
	}

	posts, err := svc.List(context.Background(), ListFilter{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(posts) != 0 {
		t.Fatalf("stored posts = %d, want 0 after unknown category", len(posts))
	}
}

func TestCreateResolvesCategoryAndDerivative(t *testing.T) {
	svc := NewService(NewMemoryStore())
	createCategory(t, svc, "coding", "Coding")
	createDerivative(t, svc, "claude-code", "Claude Code", "coding")

	created, err := svc.Create(context.Background(), CreatePostInput{
		Slug:           "setup-claude-code",
		Title:          "Setup Claude Code",
		AuthorID:       "user-1",
		CategorySlug:   "coding",
		DerivativeSlug: "claude-code",
		Value:          "body",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if got := slugsInOrder(created.Categories); got != "coding,claude-code" {
		t.Fatalf("category slugs = %q, want coding,claude-code", got)
	}
	if created.Categories[0].ParentSlug != nil {
		t.Fatalf("parent_slug of the category = %v, want nil", *created.Categories[0].ParentSlug)
	}
	if created.Categories[1].ParentSlug == nil || *created.Categories[1].ParentSlug != "coding" {
		t.Fatalf("parent_slug of the derivative = %v, want coding", created.Categories[1].ParentSlug)
	}

	// a post that picks no derivative links the category alone
	plain, err := svc.Create(context.Background(), CreatePostInput{
		Slug:         "plain-post",
		Title:        "Plain Post",
		AuthorID:     "user-1",
		CategorySlug: "coding",
		Value:        "body",
	})
	if err != nil {
		t.Fatalf("second Create() error = %v", err)
	}
	if got := slugsInOrder(plain.Categories); got != "coding" {
		t.Fatalf("category slugs = %q, want coding alone", got)
	}
}

func TestCreateResolvesDerivativeAsCategory(t *testing.T) {
	svc := NewService(NewMemoryStore())
	createCategory(t, svc, "coding", "Coding")
	createDerivative(t, svc, "claude-code", "Claude Code", "coding")

	// naming a derivative as the category links its parent automatically
	created, err := svc.Create(context.Background(), CreatePostInput{
		Slug:         "setup-claude-code",
		Title:        "Setup Claude Code",
		AuthorID:     "user-1",
		CategorySlug: "claude-code",
		Value:        "body",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if got := slugsInOrder(created.Categories); got != "coding,claude-code" {
		t.Fatalf("category slugs = %q, want coding,claude-code", got)
	}
	if created.Categories[1].ParentSlug == nil || *created.Categories[1].ParentSlug != "coding" {
		t.Fatalf("parent_slug of the derivative = %v, want coding", created.Categories[1].ParentSlug)
	}

	// the derivative field may repeat the same slug
	repeated, err := svc.Create(context.Background(), CreatePostInput{
		Slug:           "repeated-derivative",
		Title:          "Repeated Derivative",
		AuthorID:       "user-1",
		CategorySlug:   "claude-code",
		DerivativeSlug: "claude-code",
		Value:          "body",
	})
	if err != nil {
		t.Fatalf("second Create() error = %v", err)
	}
	if got := slugsInOrder(repeated.Categories); got != "coding,claude-code" {
		t.Fatalf("category slugs = %q, want coding,claude-code", got)
	}
}

func TestSlugify(t *testing.T) {
	cases := []struct {
		title string
		want  string
	}{
		{"Setup Claude Code", "setup-claude-code"},
		{"Hello, World!", "hello-world"},
		{"  Trim Me  ", "trim-me"},
		{"already-a-slug", "already-a-slug"},
		{"under_score!bang", "under-score-bang"},
		{"Go 1.22 + generics", "go-1-22-generics"},
		{"Café à la mode", "caf-la-mode"},
		{"ÄBC def", "bc-def"},
		{"--dashes--", "dashes"},
		{"123", "123"},
		{"日本語のタイトル", "post"},
		{"éèê", "post"},
		{"", "post"},
		{strings.Repeat("a", 200), strings.Repeat("a", 90)},
		{strings.Repeat("a", 89) + " " + strings.Repeat("b", 20), strings.Repeat("a", 89)},
	}
	for _, tc := range cases {
		if got := slugify(tc.title); got != tc.want {
			t.Fatalf("slugify(%q) = %q, want %q", tc.title, got, tc.want)
		}
	}
}

// TestCreateGeneratesSlugFromTitle proves an absent or blank slug comes from
// the title and colliding titles take the next free -N suffix.
func TestCreateGeneratesSlugFromTitle(t *testing.T) {
	svc := NewService(NewMemoryStore())
	createCategory(t, svc, "alpha", "Alpha")

	created, err := svc.Create(context.Background(), CreatePostInput{
		Title:        "Setup Claude Code",
		AuthorID:     "user-1",
		CategorySlug: "alpha",
		Value:        "# Body",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Slug != "setup-claude-code" {
		t.Fatalf("slug = %q, want setup-claude-code", created.Slug)
	}

	// a blank slug also falls back to the title
	blank, err := svc.Create(context.Background(), CreatePostInput{
		Slug:         "   ",
		Title:        "Another Post",
		AuthorID:     "user-1",
		CategorySlug: "alpha",
		Value:        "body",
	})
	if err != nil {
		t.Fatalf("Create() with blank slug error = %v", err)
	}
	if blank.Slug != "another-post" {
		t.Fatalf("slug = %q, want another-post", blank.Slug)
	}

	// the same title takes the next free suffix
	second, err := svc.Create(context.Background(), CreatePostInput{
		Title:        "Setup Claude Code",
		AuthorID:     "user-1",
		CategorySlug: "alpha",
		Value:        "body",
	})
	if err != nil {
		t.Fatalf("second Create() error = %v", err)
	}
	if second.Slug != "setup-claude-code-2" {
		t.Fatalf("slug = %q, want setup-claude-code-2", second.Slug)
	}

	// an explicit slug of that name still conflicts instead of suffixing
	if _, err := svc.Create(context.Background(), CreatePostInput{
		Slug:         "setup-claude-code",
		Title:        "Explicit",
		AuthorID:     "user-1",
		CategorySlug: "alpha",
		Value:        "body",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("explicit duplicate Create() error = %v, want ErrConflict", err)
	}
}

// TestCreateGeneratedSlugFallsBackToPost proves a title with no usable
// characters lands on the "post" base slug and suffixes from there.
func TestCreateGeneratedSlugFallsBackToPost(t *testing.T) {
	svc := NewService(NewMemoryStore())
	createCategory(t, svc, "alpha", "Alpha")

	for index, want := range []string{"post", "post-2"} {
		created, err := svc.Create(context.Background(), CreatePostInput{
			Title:        "日本語のタイトル",
			AuthorID:     "user-1",
			CategorySlug: "alpha",
			Value:        "body",
		})
		if err != nil {
			t.Fatalf("Create() #%d error = %v", index, err)
		}
		if created.Slug != want {
			t.Fatalf("slug #%d = %q, want %q", index, created.Slug, want)
		}
	}
}

// TestCreateGeneratedSlugSkipsTakenSuffixes seeds every candidate up to -3
// before the generated create, so only -4 is free.
func TestCreateGeneratedSlugSkipsTakenSuffixes(t *testing.T) {
	svc := NewService(NewMemoryStore())
	createCategory(t, svc, "alpha", "Alpha")
	seedPost(t, svc, "my-post", "My Post", "alpha", "")
	seedPost(t, svc, "my-post-2", "My Post Two", "alpha", "")
	seedPost(t, svc, "my-post-3", "My Post Three", "alpha", "")

	created, err := svc.Create(context.Background(), CreatePostInput{
		Title:        "My Post",
		AuthorID:     "user-1",
		CategorySlug: "alpha",
		Value:        "body",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Slug != "my-post-4" {
		t.Fatalf("slug = %q, want my-post-4", created.Slug)
	}
}

func TestCreateHandlerCreatesCategoryAndPost(t *testing.T) {
	mux := newTestMux()
	userID, token := registerAndLogin(t, mux, "budi")

	res := doJSON(t, mux, http.MethodPost, "/categories", `{"slug":"coding-agent","name":"Coding Agent"}`, token)
	if res.Code != http.StatusCreated {
		t.Fatalf("POST /categories status = %d, want %d; body = %s", res.Code, http.StatusCreated, res.Body.String())
	}
	var category Category
	if err := json.Unmarshal(res.Body.Bytes(), &category); err != nil {
		t.Fatalf("decode category: %v", err)
	}
	if category.ID == "" || category.Slug != "coding-agent" {
		t.Fatalf("category = %+v, want a stored coding-agent", category)
	}

	res = doJSON(t, mux, http.MethodPost, "/posts", `{
		"slug": "setup-claude-code",
		"title": "Setup Claude Code",
		"category": "coding-agent",
		"value": "# Body"
	}`, token)
	if res.Code != http.StatusCreated {
		t.Fatalf("POST /posts status = %d, want %d; body = %s", res.Code, http.StatusCreated, res.Body.String())
	}
	var created Post
	if err := json.Unmarshal(res.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode post: %v", err)
	}
	if created.AuthorID != userID {
		t.Fatalf("author_id = %q, want the registered user id %q", created.AuthorID, userID)
	}
	if created.AuthorUsername != "budi" {
		t.Fatalf("author_username = %q, want budi", created.AuthorUsername)
	}
	if created.Slug != "setup-claude-code" || created.Title != "Setup Claude Code" {
		t.Fatalf("slug/title = %q/%q, want setup-claude-code/Setup Claude Code", created.Slug, created.Title)
	}
	if got := slugsInOrder(created.Categories); got != "coding-agent" {
		t.Fatalf("category slugs = %q, want coding-agent", got)
	}
	if len(created.Items) != 1 || created.Items[0].Position != 0 || created.Items[0].Value != "# Body" {
		t.Fatalf("created items = %+v, want the one item at position 0 with the sent value", created.Items)
	}

	// the write is readable, and the author stays the token subject
	res = doJSON(t, mux, http.MethodGet, "/posts/setup-claude-code", "", "")
	if res.Code != http.StatusOK {
		t.Fatalf("GET /posts/setup-claude-code status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	var stored Post
	if err := json.Unmarshal(res.Body.Bytes(), &stored); err != nil {
		t.Fatalf("decode stored post: %v", err)
	}
	if stored.ID != created.ID || stored.AuthorID != userID {
		t.Fatalf("stored post = %+v, want the created post of %q", stored, userID)
	}
	if stored.AuthorUsername != "budi" {
		t.Fatalf("stored author_username = %q, want budi", stored.AuthorUsername)
	}
}

func TestCreateHandlerGeneratesSlugFromTitle(t *testing.T) {
	mux := newTestMux()
	_, token := registerAndLogin(t, mux, "budi")

	res := doJSON(t, mux, http.MethodPost, "/categories", `{"slug":"alpha","name":"Alpha"}`, token)
	if res.Code != http.StatusCreated {
		t.Fatalf("POST /categories status = %d, want %d; body = %s", res.Code, http.StatusCreated, res.Body.String())
	}

	// no slug in the body: the server derives one from the title
	res = doJSON(t, mux, http.MethodPost, "/posts", `{
		"title": "Setup Claude Code",
		"category": "alpha",
		"value": "# Body"
	}`, token)
	if res.Code != http.StatusCreated {
		t.Fatalf("POST /posts status = %d, want %d; body = %s", res.Code, http.StatusCreated, res.Body.String())
	}
	var created Post
	if err := json.Unmarshal(res.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode post: %v", err)
	}
	if created.Slug != "setup-claude-code" {
		t.Fatalf("slug = %q, want the generated setup-claude-code", created.Slug)
	}

	// a second post with the same title takes the next free suffix
	res = doJSON(t, mux, http.MethodPost, "/posts", `{
		"title": "Setup Claude Code",
		"category": "alpha",
		"value": "body"
	}`, token)
	if res.Code != http.StatusCreated {
		t.Fatalf("second POST /posts status = %d, want %d; body = %s", res.Code, http.StatusCreated, res.Body.String())
	}
	var second Post
	if err := json.Unmarshal(res.Body.Bytes(), &second); err != nil {
		t.Fatalf("decode second post: %v", err)
	}
	if second.Slug != "setup-claude-code-2" {
		t.Fatalf("slug = %q, want setup-claude-code-2", second.Slug)
	}
}

// TestCreateHandlerAcceptsDerivativeAsCategory proves category may name a
// derivative slug: the response links the parent and the derivative.
func TestCreateHandlerAcceptsDerivativeAsCategory(t *testing.T) {
	mux := newTestMux()
	_, token := registerAndLogin(t, mux, "budi")

	res := doJSON(t, mux, http.MethodPost, "/categories", `{"slug":"coding","name":"Coding"}`, token)
	if res.Code != http.StatusCreated {
		t.Fatalf("POST /categories status = %d, want %d; body = %s", res.Code, http.StatusCreated, res.Body.String())
	}
	res = doJSON(t, mux, http.MethodPost, "/categories", `{"slug":"claude-code","name":"Claude Code","parent":"coding"}`, token)
	if res.Code != http.StatusCreated {
		t.Fatalf("POST derivative status = %d, want %d; body = %s", res.Code, http.StatusCreated, res.Body.String())
	}

	res = doJSON(t, mux, http.MethodPost, "/posts", `{
		"title": "Setup Claude Code",
		"category": "claude-code",
		"value": "# Body"
	}`, token)
	if res.Code != http.StatusCreated {
		t.Fatalf("POST /posts status = %d, want %d; body = %s", res.Code, http.StatusCreated, res.Body.String())
	}
	var created Post
	if err := json.Unmarshal(res.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode post: %v", err)
	}
	if got := slugsInOrder(created.Categories); got != "coding,claude-code" {
		t.Fatalf("category slugs = %q, want coding,claude-code", got)
	}
}

func TestCreateHandlerRejectsMissingToken(t *testing.T) {
	mux := newTestMux()

	res := doJSON(t, mux, http.MethodPost, "/posts", `{"slug":"no-token","title":"No Token","category":"alpha","value":"# Body"}`, "")
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("POST /posts without token status = %d, want %d; body = %s", res.Code, http.StatusUnauthorized, res.Body.String())
	}
	if res.Header().Get("WWW-Authenticate") == "" {
		t.Fatal("401 misses WWW-Authenticate header")
	}

	// the rejected write never landed
	res = doJSON(t, mux, http.MethodGet, "/posts/no-token", "", "")
	if res.Code != http.StatusNotFound {
		t.Fatalf("GET /posts/no-token status = %d, want %d", res.Code, http.StatusNotFound)
	}
	res = doJSON(t, mux, http.MethodGet, "/posts", "", "")
	var posts []Post
	if err := json.Unmarshal(res.Body.Bytes(), &posts); err != nil {
		t.Fatalf("decode posts: %v", err)
	}
	if len(posts) != 0 {
		t.Fatalf("stored posts = %d, want 0", len(posts))
	}
}

func TestCreateHandlerRejectsUnknownField(t *testing.T) {
	mux := newTestMux()
	_, token := registerAndLogin(t, mux, "budi")

	// author_id is not a body field: the author comes from the token
	res := doJSON(t, mux, http.MethodPost, "/posts", `{
		"slug": "extra-field",
		"title": "Extra Field",
		"category": "alpha",
		"value": "# Body",
		"author_id": "someone-else"
	}`, token)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("POST /posts with unknown field status = %d, want %d; body = %s", res.Code, http.StatusBadRequest, res.Body.String())
	}

	res = doJSON(t, mux, http.MethodGet, "/posts/extra-field", "", "")
	if res.Code != http.StatusNotFound {
		t.Fatalf("rejected post is visible, GET status = %d, want %d", res.Code, http.StatusNotFound)
	}
}

func TestCreateHandlerRejectsBadRequestsWithoutStoring(t *testing.T) {
	mux := newTestMux()
	_, token := registerAndLogin(t, mux, "budi")

	res := doJSON(t, mux, http.MethodPost, "/categories", `{"slug":"alpha","name":"Alpha"}`, token)
	if res.Code != http.StatusCreated {
		t.Fatalf("POST /categories status = %d, want %d; body = %s", res.Code, http.StatusCreated, res.Body.String())
	}
	res = doJSON(t, mux, http.MethodPost, "/posts", `{"slug":"first-post","title":"First Post","category":"alpha","value":"# First"}`, token)
	if res.Code != http.StatusCreated {
		t.Fatalf("first POST /posts status = %d, want %d; body = %s", res.Code, http.StatusCreated, res.Body.String())
	}

	cases := []struct {
		name    string
		body    string
		status  int
		message string
		slug    string
	}{
		{
			name:    "duplicate slug",
			body:    `{"slug":"first-post","title":"Second Post","category":"alpha","value":"body"}`,
			status:  http.StatusConflict,
			message: "slug already exists",
		},
		{
			name:    "unknown category",
			body:    `{"slug":"unknown-category","title":"Unknown","category":"nope","value":"body"}`,
			status:  http.StatusBadRequest,
			message: "unknown category",
			slug:    "unknown-category",
		},
		{
			name:    "blank value",
			body:    `{"slug":"bad-value","title":"Bad Value","category":"alpha","value":"   "}`,
			status:  http.StatusBadRequest,
			message: "invalid input",
			slug:    "bad-value",
		},
		{
			name:    "legacy items payload",
			body:    `{"slug":"legacy-items","title":"Legacy Items","category":"alpha","items":[{"kind":"markdown","body_text":"# Body"}]}`,
			status:  http.StatusBadRequest,
			message: "invalid request body",
			slug:    "legacy-items",
		},
		{
			name:    "malformed json",
			body:    `{"slug":`,
			status:  http.StatusBadRequest,
			message: "invalid request body",
		},
	}

	for _, tc := range cases {
		res := doJSON(t, mux, http.MethodPost, "/posts", tc.body, token)
		if res.Code != tc.status {
			t.Fatalf("%s status = %d, want %d; body = %s", tc.name, res.Code, tc.status, res.Body.String())
		}
		if got := strings.TrimSpace(res.Body.String()); got != tc.message {
			t.Fatalf("%s message = %q, want %q", tc.name, got, tc.message)
		}
		if tc.slug != "" {
			check := doJSON(t, mux, http.MethodGet, "/posts/"+tc.slug, "", "")
			if check.Code != http.StatusNotFound {
				t.Fatalf("%s: rejected post is visible, GET status = %d, want %d", tc.name, check.Code, http.StatusNotFound)
			}
		}
	}

	// the duplicate attempt left the stored post untouched
	res = doJSON(t, mux, http.MethodGet, "/posts/first-post", "", "")
	if res.Code != http.StatusOK {
		t.Fatalf("GET /posts/first-post status = %d, want %d", res.Code, http.StatusOK)
	}
	var stored Post
	if err := json.Unmarshal(res.Body.Bytes(), &stored); err != nil {
		t.Fatalf("decode stored post: %v", err)
	}
	if stored.Title != "First Post" {
		t.Fatalf("stored title = %q, want First Post", stored.Title)
	}

	res = doJSON(t, mux, http.MethodGet, "/posts", "", "")
	var posts []Post
	if err := json.Unmarshal(res.Body.Bytes(), &posts); err != nil {
		t.Fatalf("decode posts: %v", err)
	}
	if len(posts) != 1 {
		t.Fatalf("stored posts = %d, want 1", len(posts))
	}
}

// TestCreateKeepsValueExactBytes proves value is stored byte-for-byte:
// markdown indentation and the trailing newline survive both the create
// read-back and the detail read.
func TestCreateKeepsValueExactBytes(t *testing.T) {
	svc := NewService(NewMemoryStore())
	createCategory(t, svc, "alpha", "Alpha")

	const value = "    kode\n\nakhir\n"

	created, err := svc.Create(context.Background(), CreatePostInput{
		Slug:         "exact-bytes",
		Title:        "Exact Bytes",
		AuthorID:     "user-1",
		CategorySlug: "alpha",
		Value:        value,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if got := created.Items[0].Value; got != value {
		t.Fatalf("items[0].value = %q, want the exact bytes %q", got, value)
	}

	stored, err := svc.Get(context.Background(), "exact-bytes")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got := stored.Items[0].Value; got != value {
		t.Fatalf("stored items[0].value = %q, want the exact bytes %q", got, value)
	}
}

// TestCreateRejectsBlankValue proves the blank check is TrimSpace-based while
// the stored bytes stay exact: a whitespace-only value never reaches the
// store.
func TestCreateRejectsBlankValue(t *testing.T) {
	svc := NewService(NewMemoryStore())
	createCategory(t, svc, "alpha", "Alpha")

	for _, value := range []string{"", "   ", "\n", " \t\n "} {
		_, err := svc.Create(context.Background(), CreatePostInput{
			Slug:         "blank-value",
			Title:        "Blank Value",
			AuthorID:     "user-1",
			CategorySlug: "alpha",
			Value:        value,
		})
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("Create() with value %q error = %v, want ErrInvalidInput", value, err)
		}
	}

	posts, err := svc.List(context.Background(), ListFilter{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(posts) != 0 {
		t.Fatalf("stored posts = %d, want 0 after blank values", len(posts))
	}
}

// TestCreateRejectsNULBytes proves the one byte Postgres text columns cannot
// store is rejected on every text field, before any store sees it.
func TestCreateRejectsNULBytes(t *testing.T) {
	svc := NewService(NewMemoryStore())
	createCategory(t, svc, "alpha", "Alpha")

	if _, err := svc.Create(context.Background(), CreatePostInput{
		Slug:         "nul-title",
		Title:        "Bad\x00Title",
		AuthorID:     "user-1",
		CategorySlug: "alpha",
		Value:        "# Body",
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Create() with a NUL in the title error = %v, want ErrInvalidInput", err)
	}

	if _, err := svc.Create(context.Background(), CreatePostInput{
		Slug:         "nul-value",
		Title:        "NUL Value",
		AuthorID:     "user-1",
		CategorySlug: "alpha",
		Value:        "a\x00b",
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Create() with a NUL in the value error = %v, want ErrInvalidInput", err)
	}

	if _, err := svc.CreateCategory(context.Background(), CreateCategoryInput{Slug: "nul-name", Name: "Bad\x00Name"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("CreateCategory() with a NUL in the name error = %v, want ErrInvalidInput", err)
	}
}

// TestMemoryStoreClonesPostReads proves a caller can never write into stored
// state: every read returns fresh slices, so appending to the returned items
// or categories changes nothing.
func TestMemoryStoreClonesPostReads(t *testing.T) {
	svc := NewService(NewMemoryStore())
	createCategory(t, svc, "alpha", "Alpha")

	created, err := svc.Create(context.Background(), CreatePostInput{
		Slug:         "clone-check",
		Title:        "Clone Check",
		AuthorID:     "user-1",
		CategorySlug: "alpha",
		Value:        "# Original",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// the create read-back is a copy
	created.Items[0].Value = "# Mutated Read-back"
	created.Categories[0].Name = "Mutated"

	stored, err := svc.Get(context.Background(), "clone-check")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got := stored.Items[0].Value; got != "# Original" {
		t.Fatalf("stored items[0].value = %q, want the original bytes", got)
	}
	if got := namesInOrder(stored.Categories); got != "Alpha" {
		t.Fatalf("stored category names = %q, want Alpha", got)
	}

	// the same holds for the post FindBySlug returns
	found, err := svc.Get(context.Background(), "clone-check")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	found.Items[0].Value = "# Mutated Again"
	found.Items = append(found.Items, Item{Value: "# Added"})
	found.Categories = append(found.Categories, Category{Slug: "fake", Name: "Fake"})

	again, err := svc.Get(context.Background(), "clone-check")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got := again.Items[0].Value; got != "# Original" {
		t.Fatalf("items[0].value = %q, want the original bytes", got)
	}
	if len(again.Items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(again.Items))
	}
	if got := namesInOrder(again.Categories); got != "Alpha" {
		t.Fatalf("category names = %q, want Alpha", got)
	}
}

// TestCreateHandlerKeepsExactValue proves the HTTP round-trip never trims:
// leading spaces and the trailing newline of the value survive the create
// response and the detail read, decoded with json.Decoder.
func TestCreateHandlerKeepsExactValue(t *testing.T) {
	mux := newTestMux()
	_, token := registerAndLogin(t, mux, "budi")

	res := doJSON(t, mux, http.MethodPost, "/categories", `{"slug":"alpha","name":"Alpha"}`, token)
	if res.Code != http.StatusCreated {
		t.Fatalf("POST /categories status = %d, want %d; body = %s", res.Code, http.StatusCreated, res.Body.String())
	}

	const value = "    kode\n\nakhir\n"
	res = doJSON(t, mux, http.MethodPost, "/posts", `{
		"slug": "exact-bytes",
		"title": "Exact Bytes",
		"category": "alpha",
		"value": "    kode\n\nakhir\n"
	}`, token)
	if res.Code != http.StatusCreated {
		t.Fatalf("POST /posts status = %d, want %d; body = %s", res.Code, http.StatusCreated, res.Body.String())
	}
	var created Post
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		t.Fatalf("decode created post: %v", err)
	}
	if got := created.Items[0].Value; got != value {
		t.Fatalf("items[0].value = %q, want the exact bytes %q", got, value)
	}

	res = doJSON(t, mux, http.MethodGet, "/posts/exact-bytes", "", "")
	if res.Code != http.StatusOK {
		t.Fatalf("GET /posts/exact-bytes status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	var stored Post
	if err := json.NewDecoder(res.Body).Decode(&stored); err != nil {
		t.Fatalf("decode stored post: %v", err)
	}
	if got := stored.Items[0].Value; got != value {
		t.Fatalf("stored items[0].value = %q, want the exact bytes %q", got, value)
	}
}
