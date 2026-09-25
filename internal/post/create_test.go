package post

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
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
	tokens := auth.NewTokens(testSecret, 15*time.Minute, 24*time.Hour)
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

// createCategory seeds one category through the service.
func createCategory(t *testing.T, svc *Service, slug, name string) Category {
	t.Helper()
	created, err := svc.CreateCategory(context.Background(), CreateCategoryInput{Slug: slug, Name: name})
	if err != nil {
		t.Fatalf("CreateCategory(%q) error = %v", slug, err)
	}
	return created
}

// textValue reads one non-nil item text field.
func textValue(t *testing.T, field string, value *string) string {
	t.Helper()
	if value == nil {
		t.Fatalf("%s = nil, want a value", field)
	}
	return *value
}

// text wraps one payload value in the pointer ItemInput now wants: nil means
// the client did not send the key, a pointer means the client sent it, even
// when the value is empty.
func text(value string) *string { return &value }

// slugsInOrder lists category slugs in response order.
func slugsInOrder(categories []Category) string {
	slugs := make([]string, 0, len(categories))
	for _, category := range categories {
		slugs = append(slugs, category.Slug)
	}
	return strings.Join(slugs, ",")
}

// positionsInOrder lists item positions in response order.
func positionsInOrder(items []Item) string {
	positions := make([]string, 0, len(items))
	for _, item := range items {
		positions = append(positions, strconv.Itoa(item.Position))
	}
	return strings.Join(positions, ",")
}

func TestCreateStoresCategoriesAndItems(t *testing.T) {
	svc := NewService(NewMemoryStore())
	createCategory(t, svc, "coding-agent", "Coding Agent")
	createCategory(t, svc, "setup", "Setup")

	created, err := svc.Create(context.Background(), CreatePostInput{
		Slug:          "setup-claude-code",
		Title:         "Setup Claude Code",
		AuthorID:      "user-1",
		CategorySlugs: []string{"setup", "coding-agent"},
		Items: []ItemInput{
			{Kind: KindMarkdown, BodyText: text("# Setup\n\nInstall the CLI.")},
			{Kind: KindLink, URL: text("https://docs.anthropic.com/claude-code")},
			{Kind: KindLink, URL: text("https://example.com/hooks")},
			{Kind: KindFile, BodyText: text("# Notes\n\nInline file."), Filename: text("notes.md")},
		},
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
	if len(created.Items) != 4 {
		t.Fatalf("len(items) = %d, want 4", len(created.Items))
	}
	if got := positionsInOrder(created.Items); got != "0,1,2,3" {
		t.Fatalf("item positions = %q, want 0,1,2,3", got)
	}
	for index, want := range []Kind{KindMarkdown, KindLink, KindLink, KindFile} {
		if got := created.Items[index].Kind; got != want {
			t.Fatalf("items[%d].kind = %q, want %q", index, got, want)
		}
	}

	// the markdown item carries the body and nothing of another kind
	if got := textValue(t, "items[0].body_text", created.Items[0].BodyText); got != "# Setup\n\nInstall the CLI." {
		t.Fatalf("items[0].body_text = %q, want the markdown body", got)
	}
	if created.Items[0].URL != nil || created.Items[0].Filename != nil || created.Items[0].MIME != nil {
		t.Fatal("markdown item carries a field of another kind")
	}

	// both links stay, each with its own url and no body
	if got := textValue(t, "items[1].url", created.Items[1].URL); got != "https://docs.anthropic.com/claude-code" {
		t.Fatalf("items[1].url = %q, want the first link", got)
	}
	if got := textValue(t, "items[2].url", created.Items[2].URL); got != "https://example.com/hooks" {
		t.Fatalf("items[2].url = %q, want the second link", got)
	}
	if created.Items[1].BodyText != nil || created.Items[2].BodyText != nil {
		t.Fatal("link item carries body_text")
	}

	// the file item keeps its inline body and filename, and defaults the mime
	if got := textValue(t, "items[3].body_text", created.Items[3].BodyText); got != "# Notes\n\nInline file." {
		t.Fatalf("items[3].body_text = %q, want the inline file body", got)
	}
	if got := textValue(t, "items[3].filename", created.Items[3].Filename); got != "notes.md" {
		t.Fatalf("items[3].filename = %q, want notes.md", got)
	}
	if got := textValue(t, "items[3].mime", created.Items[3].MIME); got != "text/markdown" {
		t.Fatalf("items[3].mime = %q, want the text/markdown default", got)
	}
	if created.Items[3].URL != nil {
		t.Fatal("file item carries a url")
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
	if got := positionsInOrder(stored.Items); got != "0,1,2,3" {
		t.Fatalf("stored item positions = %q, want 0,1,2,3", got)
	}
	if got := textValue(t, "stored items[3].mime", stored.Items[3].MIME); got != "text/markdown" {
		t.Fatalf("stored items[3].mime = %q, want the text/markdown default", got)
	}
}

func TestCreateKeepsExplicitFileMIME(t *testing.T) {
	svc := NewService(NewMemoryStore())
	createCategory(t, svc, "alpha", "Alpha")

	created, err := svc.Create(context.Background(), CreatePostInput{
		Slug:          "explicit-mime",
		Title:         "Explicit MIME",
		AuthorID:      "user-1",
		CategorySlugs: []string{"alpha"},
		Items:         []ItemInput{{Kind: KindFile, BodyText: text("# Notes"), Filename: text("notes.txt"), MIME: text("text/plain")}},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if got := textValue(t, "items[0].mime", created.Items[0].MIME); got != "text/plain" {
		t.Fatalf("items[0].mime = %q, want the sent text/plain", got)
	}
}

func TestCreateNormalizesSlugAndTitle(t *testing.T) {
	svc := NewService(NewMemoryStore())
	createCategory(t, svc, "setup", "Setup")

	// Only the edges are normalized: the slug must already be URL-safe, so an
	// inner space stays invalid (see the invalid-input table). body_text is
	// the one payload that keeps its exact bytes; url, filename, and mime
	// still lose their surrounding whitespace.
	created, err := svc.Create(context.Background(), CreatePostInput{
		Slug:          " Setup-Claude-Code ",
		Title:         "  Setup Claude Code  ",
		AuthorID:      "user-1",
		CategorySlugs: []string{" setup "},
		Items: []ItemInput{
			{Kind: KindText, BodyText: text("  hello  ")},
			{Kind: KindFile, BodyText: text("  file body\n"), Filename: text("  notes.md  "), MIME: text("  text/plain  ")},
		},
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
	if got := textValue(t, "items[0].body_text", created.Items[0].BodyText); got != "  hello  " {
		t.Fatalf("items[0].body_text = %q, want the exact bytes %q", got, "  hello  ")
	}
	if got := textValue(t, "items[1].body_text", created.Items[1].BodyText); got != "  file body\n" {
		t.Fatalf("items[1].body_text = %q, want the exact bytes %q", got, "  file body\n")
	}
	if got := textValue(t, "items[1].filename", created.Items[1].Filename); got != "notes.md" {
		t.Fatalf("items[1].filename = %q, want the trimmed notes.md", got)
	}
	if got := textValue(t, "items[1].mime", created.Items[1].MIME); got != "text/plain" {
		t.Fatalf("items[1].mime = %q, want the trimmed text/plain", got)
	}

	// the normalized slug is the stored handle, and the exact bytes persisted
	stored, err := svc.Get(context.Background(), "setup-claude-code")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.ID != created.ID {
		t.Fatalf("stored id = %q, want %q", stored.ID, created.ID)
	}
	if got := textValue(t, "stored items[0].body_text", stored.Items[0].BodyText); got != "  hello  " {
		t.Fatalf("stored items[0].body_text = %q, want the exact bytes %q", got, "  hello  ")
	}
}

func TestCreateRejectsInvalidInput(t *testing.T) {
	svc := NewService(NewMemoryStore())
	createCategory(t, svc, "alpha", "Alpha")
	for index := 1; index <= 8; index++ {
		createCategory(t, svc, "cat-"+strconv.Itoa(index), "Cat "+strconv.Itoa(index))
	}

	valid := func() CreatePostInput {
		return CreatePostInput{
			Slug:          "valid-post",
			Title:         "Valid Post",
			AuthorID:      "user-1",
			CategorySlugs: []string{"alpha"},
			Items:         []ItemInput{{Kind: KindMarkdown, BodyText: text("# Body")}},
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
		{"empty slug", func(in *CreatePostInput) { in.Slug = "" }},
		{"slug with inner space", func(in *CreatePostInput) { in.Slug = "setup-claude code" }},
		{"zero categories", func(in *CreatePostInput) { in.CategorySlugs = nil }},
		{"nine categories", func(in *CreatePostInput) {
			slugs := []string{"alpha"}
			for index := 1; index <= 8; index++ {
				slugs = append(slugs, "cat-"+strconv.Itoa(index))
			}
			in.CategorySlugs = slugs
		}},
		{"zero items", func(in *CreatePostInput) { in.Items = nil }},
		{"twenty-one items", func(in *CreatePostInput) {
			items := make([]ItemInput, 0, 21)
			for index := 0; index < 21; index++ {
				items = append(items, ItemInput{Kind: KindMarkdown, BodyText: text("# Body")})
			}
			in.Items = items
		}},
		{"unknown kind", func(in *CreatePostInput) { in.Items = []ItemInput{{Kind: "video"}} }},
		{"link without url", func(in *CreatePostInput) { in.Items = []ItemInput{{Kind: KindLink}} }},
		{"link with body_text", func(in *CreatePostInput) {
			in.Items = []ItemInput{{Kind: KindLink, URL: text("https://example.com"), BodyText: text("text")}}
		}},
		{"link with javascript url", func(in *CreatePostInput) {
			in.Items = []ItemInput{{Kind: KindLink, URL: text("javascript:alert(1)")}}
		}},
		{"markdown without body_text", func(in *CreatePostInput) { in.Items = []ItemInput{{Kind: KindMarkdown}} }},
		{"text with url", func(in *CreatePostInput) {
			in.Items = []ItemInput{{Kind: KindText, BodyText: text("hello"), URL: text("https://example.com")}}
		}},
		{"file without filename", func(in *CreatePostInput) {
			in.Items = []ItemInput{{Kind: KindFile, BodyText: text("# Body")}}
		}},
		{"file with url", func(in *CreatePostInput) {
			in.Items = []ItemInput{{Kind: KindFile, BodyText: text("# Body"), Filename: text("notes.md"), URL: text("https://example.com")}}
		}},
		{"body_text over 64 KiB", func(in *CreatePostInput) {
			in.Items = []ItemInput{{Kind: KindMarkdown, BodyText: text(strings.Repeat("a", 64<<10+1))}}
		}},
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
		Slug:          "first-post",
		Title:         "First Post",
		AuthorID:      "user-1",
		CategorySlugs: []string{"alpha"},
		Items:         []ItemInput{{Kind: KindMarkdown, BodyText: text("# First")}},
	}); err != nil {
		t.Fatalf("first Create() error = %v", err)
	}

	// the slug is normalized before the unique check
	if _, err := svc.Create(context.Background(), CreatePostInput{
		Slug:          " First-Post ",
		Title:         "Second Post",
		AuthorID:      "user-1",
		CategorySlugs: []string{"alpha"},
		Items:         []ItemInput{{Kind: KindText, BodyText: text("body")}},
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
		Slug:          "unknown-category",
		Title:         "Unknown Category",
		AuthorID:      "user-1",
		CategorySlugs: []string{"alpha", "nope"},
		Items:         []ItemInput{{Kind: KindText, BodyText: text("body")}},
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

func TestCreateDeduplicatesCategorySlugs(t *testing.T) {
	svc := NewService(NewMemoryStore())
	createCategory(t, svc, "alpha", "Alpha")
	createCategory(t, svc, "beta", "Beta")

	created, err := svc.Create(context.Background(), CreatePostInput{
		Slug:          "deduped",
		Title:         "Deduped",
		AuthorID:      "user-1",
		CategorySlugs: []string{"alpha", "alpha"},
		Items:         []ItemInput{{Kind: KindText, BodyText: text("body")}},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if got := slugsInOrder(created.Categories); got != "alpha" {
		t.Fatalf("category slugs = %q, want one alpha", got)
	}

	// repeats collapse, and what stays comes back in name order
	created, err = svc.Create(context.Background(), CreatePostInput{
		Slug:          "deduped-order",
		Title:         "Deduped Order",
		AuthorID:      "user-1",
		CategorySlugs: []string{"beta", "alpha", "beta"},
		Items:         []ItemInput{{Kind: KindText, BodyText: text("body")}},
	})
	if err != nil {
		t.Fatalf("second Create() error = %v", err)
	}
	if got := slugsInOrder(created.Categories); got != "alpha,beta" {
		t.Fatalf("category slugs = %q, want alpha,beta", got)
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
		"categories": ["coding-agent"],
		"items": [
			{"kind": "markdown", "body_text": "# Body"},
			{"kind": "file", "body_text": "# Notes", "filename": "notes.md"}
		]
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
	if created.Slug != "setup-claude-code" || created.Title != "Setup Claude Code" {
		t.Fatalf("slug/title = %q/%q, want setup-claude-code/Setup Claude Code", created.Slug, created.Title)
	}
	if got := slugsInOrder(created.Categories); got != "coding-agent" {
		t.Fatalf("category slugs = %q, want coding-agent", got)
	}
	if got := positionsInOrder(created.Items); got != "0,1" {
		t.Fatalf("item positions = %q, want 0,1", got)
	}
	if got := textValue(t, "items[1].mime", created.Items[1].MIME); got != "text/markdown" {
		t.Fatalf("items[1].mime = %q, want the text/markdown default", got)
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
}

func TestCreateHandlerRejectsMissingToken(t *testing.T) {
	mux := newTestMux()

	res := doJSON(t, mux, http.MethodPost, "/posts", `{"slug":"no-token","title":"No Token","categories":["alpha"],"items":[{"kind":"markdown","body_text":"# Body"}]}`, "")
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
		"categories": ["alpha"],
		"items": [{"kind": "markdown", "body_text": "# Body"}],
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
	res = doJSON(t, mux, http.MethodPost, "/posts", `{"slug":"first-post","title":"First Post","categories":["alpha"],"items":[{"kind":"markdown","body_text":"# First"}]}`, token)
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
			body:    `{"slug":"first-post","title":"Second Post","categories":["alpha"],"items":[{"kind":"text","body_text":"body"}]}`,
			status:  http.StatusConflict,
			message: "slug already exists",
		},
		{
			name:    "unknown category",
			body:    `{"slug":"unknown-category","title":"Unknown","categories":["nope"],"items":[{"kind":"text","body_text":"body"}]}`,
			status:  http.StatusBadRequest,
			message: "unknown category",
			slug:    "unknown-category",
		},
		{
			name:    "invalid item",
			body:    `{"slug":"bad-item","title":"Bad Item","categories":["alpha"],"items":[{"kind":"link"}]}`,
			status:  http.StatusBadRequest,
			message: "invalid input",
			slug:    "bad-item",
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

// TestCreateKeepsBodyTextExactBytes proves body_text is stored byte-for-byte:
// markdown indentation and an inline file's trailing newline survive both the
// create read-back and the detail read.
func TestCreateKeepsBodyTextExactBytes(t *testing.T) {
	svc := NewService(NewMemoryStore())
	createCategory(t, svc, "alpha", "Alpha")

	const markdownBody = "    kode\n\nakhir\n"
	const fileBody = "# Notes\n\nInline file.\n"

	created, err := svc.Create(context.Background(), CreatePostInput{
		Slug:          "exact-bytes",
		Title:         "Exact Bytes",
		AuthorID:      "user-1",
		CategorySlugs: []string{"alpha"},
		Items: []ItemInput{
			{Kind: KindMarkdown, BodyText: text(markdownBody)},
			{Kind: KindFile, BodyText: text(fileBody), Filename: text("notes.md")},
		},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if got := textValue(t, "items[0].body_text", created.Items[0].BodyText); got != markdownBody {
		t.Fatalf("items[0].body_text = %q, want the exact bytes %q", got, markdownBody)
	}
	if got := textValue(t, "items[1].body_text", created.Items[1].BodyText); got != fileBody {
		t.Fatalf("items[1].body_text = %q, want the exact bytes %q", got, fileBody)
	}

	stored, err := svc.Get(context.Background(), "exact-bytes")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got := textValue(t, "stored items[0].body_text", stored.Items[0].BodyText); got != markdownBody {
		t.Fatalf("stored items[0].body_text = %q, want the exact bytes %q", got, markdownBody)
	}
	if got := textValue(t, "stored items[1].body_text", stored.Items[1].BodyText); got != fileBody {
		t.Fatalf("stored items[1].body_text = %q, want the exact bytes %q", got, fileBody)
	}
}

// TestCreateRejectsBlankBodyText proves the blank check is TrimSpace-based
// while the stored bytes stay exact: a whitespace-only body never reaches the
// store.
func TestCreateRejectsBlankBodyText(t *testing.T) {
	svc := NewService(NewMemoryStore())
	createCategory(t, svc, "alpha", "Alpha")

	cases := []struct {
		name string
		item ItemInput
	}{
		{"markdown with spaces", ItemInput{Kind: KindMarkdown, BodyText: text("   ")}},
		{"text with a newline", ItemInput{Kind: KindText, BodyText: text("\n")}},
		{"file with spaces", ItemInput{Kind: KindFile, BodyText: text("   "), Filename: text("notes.md")}},
	}
	for _, tc := range cases {
		_, err := svc.Create(context.Background(), CreatePostInput{
			Slug:          "blank-body",
			Title:         "Blank Body",
			AuthorID:      "user-1",
			CategorySlugs: []string{"alpha"},
			Items:         []ItemInput{tc.item},
		})
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("Create() with %s error = %v, want ErrInvalidInput", tc.name, err)
		}
	}

	posts, err := svc.List(context.Background(), ListFilter{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(posts) != 0 {
		t.Fatalf("stored posts = %d, want 0 after blank bodies", len(posts))
	}
}

// TestCreateRejectsForbiddenFieldSentEmpty proves the shape check reacts to the
// key being sent, not to its content: an empty or blank forbidden field is
// still a field of another kind. An explicit JSON null decodes to nil and
// counts as absent, so it stays valid (see the handler test).
func TestCreateRejectsForbiddenFieldSentEmpty(t *testing.T) {
	svc := NewService(NewMemoryStore())
	createCategory(t, svc, "alpha", "Alpha")

	cases := []struct {
		name string
		item ItemInput
	}{
		{"link with empty body_text", ItemInput{Kind: KindLink, URL: text("https://example.com"), BodyText: text("")}},
		{"link with blank body_text", ItemInput{Kind: KindLink, URL: text("https://example.com"), BodyText: text("   ")}},
		{"markdown with empty url", ItemInput{Kind: KindMarkdown, BodyText: text("# Body"), URL: text("")}},
		{"file with empty url", ItemInput{Kind: KindFile, BodyText: text("# Body"), Filename: text("notes.md"), URL: text("")}},
		{"text with empty mime", ItemInput{Kind: KindText, BodyText: text("body"), MIME: text("")}},
	}
	for _, tc := range cases {
		_, err := svc.Create(context.Background(), CreatePostInput{
			Slug:          "forbidden-empty",
			Title:         "Forbidden Empty",
			AuthorID:      "user-1",
			CategorySlugs: []string{"alpha"},
			Items:         []ItemInput{tc.item},
		})
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("Create() with %s error = %v, want ErrInvalidInput", tc.name, err)
		}
	}
}

// TestCreateAcceptsAbsentForbiddenField proves a nil pointer means the key was
// absent, so the item keeps the one shape its kind allows and the other fields
// stay nil.
func TestCreateAcceptsAbsentForbiddenField(t *testing.T) {
	svc := NewService(NewMemoryStore())
	createCategory(t, svc, "alpha", "Alpha")

	created, err := svc.Create(context.Background(), CreatePostInput{
		Slug:          "absent-fields",
		Title:         "Absent Fields",
		AuthorID:      "user-1",
		CategorySlugs: []string{"alpha"},
		Items: []ItemInput{
			// url is still trimmed; only body_text keeps its exact bytes
			{Kind: KindLink, URL: text("  https://example.com/docs  ")},
			{Kind: KindMarkdown, BodyText: text("# Body")},
		},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Items[0].BodyText != nil || created.Items[0].Filename != nil || created.Items[0].MIME != nil {
		t.Fatal("link item carries a field of another kind")
	}
	if got := textValue(t, "items[0].url", created.Items[0].URL); got != "https://example.com/docs" {
		t.Fatalf("items[0].url = %q, want the trimmed url", got)
	}
	if created.Items[1].URL != nil || created.Items[1].Filename != nil || created.Items[1].MIME != nil {
		t.Fatal("markdown item carries a field of another kind")
	}

	stored, err := svc.Get(context.Background(), "absent-fields")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.Items[0].BodyText != nil {
		t.Fatalf("stored items[0].body_text = %q, want nil for an absent key", *stored.Items[0].BodyText)
	}
	if stored.Items[1].URL != nil {
		t.Fatalf("stored items[1].url = %q, want nil for an absent key", *stored.Items[1].URL)
	}
}

// TestCreateRejectsNULBytes proves the one byte Postgres text columns cannot
// store is rejected on every text field, before any store sees it.
func TestCreateRejectsNULBytes(t *testing.T) {
	svc := NewService(NewMemoryStore())
	createCategory(t, svc, "alpha", "Alpha")

	if _, err := svc.Create(context.Background(), CreatePostInput{
		Slug:          "nul-title",
		Title:         "Bad\x00Title",
		AuthorID:      "user-1",
		CategorySlugs: []string{"alpha"},
		Items:         []ItemInput{{Kind: KindMarkdown, BodyText: text("# Body")}},
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Create() with a NUL in the title error = %v, want ErrInvalidInput", err)
	}

	if _, err := svc.CreateCategory(context.Background(), CreateCategoryInput{Slug: "nul-name", Name: "Bad\x00Name"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("CreateCategory() with a NUL in the name error = %v, want ErrInvalidInput", err)
	}

	cases := []struct {
		name string
		item ItemInput
	}{
		{"markdown body_text", ItemInput{Kind: KindMarkdown, BodyText: text("a\x00b")}},
		{"text body_text", ItemInput{Kind: KindText, BodyText: text("a\x00b")}},
		{"file body_text", ItemInput{Kind: KindFile, BodyText: text("a\x00b"), Filename: text("notes.md")}},
		{"file filename", ItemInput{Kind: KindFile, BodyText: text("# Body"), Filename: text("notes\x00.md")}},
		{"file mime", ItemInput{Kind: KindFile, BodyText: text("# Body"), Filename: text("notes.md"), MIME: text("text/\x00plain")}},
		// the URL parse rejects control bytes too, so a NUL never reaches a store either way
		{"link url", ItemInput{Kind: KindLink, URL: text("https://example.com/\x00")}},
	}
	for _, tc := range cases {
		_, err := svc.Create(context.Background(), CreatePostInput{
			Slug:          "nul-field",
			Title:         "NUL Field",
			AuthorID:      "user-1",
			CategorySlugs: []string{"alpha"},
			Items:         []ItemInput{tc.item},
		})
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("Create() with a NUL in %s error = %v, want ErrInvalidInput", tc.name, err)
		}
	}
}

// TestMemoryStoreClonesItemPayloads proves a caller can never write into
// stored state: the create path copies the input pointer, and every read
// returns fresh pointers and slices.
func TestMemoryStoreClonesItemPayloads(t *testing.T) {
	svc := NewService(NewMemoryStore())
	createCategory(t, svc, "alpha", "Alpha")

	body := "# Original"
	created, err := svc.Create(context.Background(), CreatePostInput{
		Slug:          "clone-check",
		Title:         "Clone Check",
		AuthorID:      "user-1",
		CategorySlugs: []string{"alpha"},
		Items:         []ItemInput{{Kind: KindMarkdown, BodyText: &body}},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// the store copied the input pointer: a later write to it changes nothing
	body = "# Mutated Input"

	// the create read-back is a copy too
	*created.Items[0].BodyText = "# Mutated Read-back"
	created.Items[0].BodyText = text("# Replaced")
	created.Categories[0].Name = "Mutated"

	stored, err := svc.Get(context.Background(), "clone-check")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got := textValue(t, "stored items[0].body_text", stored.Items[0].BodyText); got != "# Original" {
		t.Fatalf("stored items[0].body_text = %q, want the original bytes", got)
	}
	if got := namesInOrder(stored.Categories); got != "Alpha" {
		t.Fatalf("stored category names = %q, want Alpha", got)
	}

	// the same holds for the value FindBySlug returns
	found, err := svc.Get(context.Background(), "clone-check")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	*found.Items[0].BodyText = "# Mutated Again"
	found.Items = append(found.Items, Item{Kind: KindText, BodyText: text("# Added")})
	found.Categories = append(found.Categories, Category{Slug: "fake", Name: "Fake"})

	again, err := svc.Get(context.Background(), "clone-check")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got := textValue(t, "items[0].body_text", again.Items[0].BodyText); got != "# Original" {
		t.Fatalf("items[0].body_text = %q, want the original bytes", got)
	}
	if len(again.Items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(again.Items))
	}
	if got := namesInOrder(again.Categories); got != "Alpha" {
		t.Fatalf("category names = %q, want Alpha", got)
	}
}

// TestCreateHandlerRejectsEmptyForbiddenField proves the wire keeps the
// absent-vs-empty distinction: a key the client sent is rejected even when it
// is empty, an explicit JSON null decodes to an absent key and stays valid.
func TestCreateHandlerRejectsEmptyForbiddenField(t *testing.T) {
	mux := newTestMux()
	_, token := registerAndLogin(t, mux, "budi")

	res := doJSON(t, mux, http.MethodPost, "/categories", `{"slug":"alpha","name":"Alpha"}`, token)
	if res.Code != http.StatusCreated {
		t.Fatalf("POST /categories status = %d, want %d; body = %s", res.Code, http.StatusCreated, res.Body.String())
	}

	// a link item that carries body_text breaks the kind shape, even empty
	res = doJSON(t, mux, http.MethodPost, "/posts", `{
		"slug": "empty-forbidden",
		"title": "Empty Forbidden",
		"categories": ["alpha"],
		"items": [{"kind": "link", "url": "https://example.com", "body_text": ""}]
	}`, token)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("POST /posts with an empty forbidden field status = %d, want %d; body = %s", res.Code, http.StatusBadRequest, res.Body.String())
	}
	if got := strings.TrimSpace(res.Body.String()); got != "invalid input" {
		t.Fatalf("400 body = %q, want invalid input", got)
	}
	res = doJSON(t, mux, http.MethodGet, "/posts/empty-forbidden", "", "")
	if res.Code != http.StatusNotFound {
		t.Fatalf("rejected post is visible, GET status = %d, want %d", res.Code, http.StatusNotFound)
	}

	// an explicit null decodes to an absent key, so the same link is valid
	res = doJSON(t, mux, http.MethodPost, "/posts", `{
		"slug": "null-forbidden",
		"title": "Null Forbidden",
		"categories": ["alpha"],
		"items": [{"kind": "link", "url": "https://example.com", "body_text": null}]
	}`, token)
	if res.Code != http.StatusCreated {
		t.Fatalf("POST /posts with an explicit null status = %d, want %d; body = %s", res.Code, http.StatusCreated, res.Body.String())
	}
	var created Post
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		t.Fatalf("decode created post: %v", err)
	}
	if created.Items[0].BodyText != nil {
		t.Fatalf("items[0].body_text = %q, want nil for an absent key", *created.Items[0].BodyText)
	}
}

// TestCreateHandlerKeepsExactBodyText proves the HTTP round-trip never trims:
// leading spaces and the trailing newline of a markdown body survive the
// create response and the detail read, decoded with json.Decoder.
func TestCreateHandlerKeepsExactBodyText(t *testing.T) {
	mux := newTestMux()
	_, token := registerAndLogin(t, mux, "budi")

	res := doJSON(t, mux, http.MethodPost, "/categories", `{"slug":"alpha","name":"Alpha"}`, token)
	if res.Code != http.StatusCreated {
		t.Fatalf("POST /categories status = %d, want %d; body = %s", res.Code, http.StatusCreated, res.Body.String())
	}

	const body = "    kode\n\nakhir\n"
	res = doJSON(t, mux, http.MethodPost, "/posts", `{
		"slug": "exact-bytes",
		"title": "Exact Bytes",
		"categories": ["alpha"],
		"items": [{"kind": "markdown", "body_text": "    kode\n\nakhir\n"}]
	}`, token)
	if res.Code != http.StatusCreated {
		t.Fatalf("POST /posts status = %d, want %d; body = %s", res.Code, http.StatusCreated, res.Body.String())
	}
	var created Post
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		t.Fatalf("decode created post: %v", err)
	}
	if got := textValue(t, "items[0].body_text", created.Items[0].BodyText); got != body {
		t.Fatalf("items[0].body_text = %q, want the exact bytes %q", got, body)
	}

	res = doJSON(t, mux, http.MethodGet, "/posts/exact-bytes", "", "")
	if res.Code != http.StatusOK {
		t.Fatalf("GET /posts/exact-bytes status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	var stored Post
	if err := json.NewDecoder(res.Body).Decode(&stored); err != nil {
		t.Fatalf("decode stored post: %v", err)
	}
	if got := textValue(t, "stored items[0].body_text", stored.Items[0].BodyText); got != body {
		t.Fatalf("stored items[0].body_text = %q, want the exact bytes %q", got, body)
	}
}
