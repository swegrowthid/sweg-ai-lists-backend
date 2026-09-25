package post

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// listFixture is the read-path seed: two categories and three posts, p1 in
// alpha only, p2 in alpha and beta, p3 in beta only.
type listFixture struct {
	svc *Service
	p1  Post
	p2  Post
	p3  Post
}

// newListFixture builds one store with its own two categories and three posts.
func newListFixture(t *testing.T) listFixture {
	t.Helper()
	svc := NewService(NewMemoryStore())
	createCategory(t, svc, "alpha", "Alpha")
	createCategory(t, svc, "beta", "Beta")
	return listFixture{
		svc: svc,
		p1:  seedPost(t, svc, "setup-claude-code", "Setup Claude Code", []string{"alpha"}, 2),
		p2:  seedPost(t, svc, "claude-code-hooks", "Claude Code Hooks", []string{"alpha", "beta"}, 3),
		p3:  seedPost(t, svc, "postgres-notes", "Postgres Notes", []string{"beta"}, 4),
	}
}

// seedPost creates one post through the service with markdown items.
func seedPost(t *testing.T, svc *Service, slug, title string, categories []string, itemCount int) Post {
	t.Helper()
	items := make([]ItemInput, 0, itemCount)
	for index := 0; index < itemCount; index++ {
		items = append(items, ItemInput{Kind: KindMarkdown, BodyText: text("# " + slug + " " + strconv.Itoa(index))})
	}
	created, err := svc.Create(context.Background(), CreatePostInput{
		Slug:          slug,
		Title:         title,
		AuthorID:      "user-1",
		CategorySlugs: categories,
		Items:         items,
	})
	if err != nil {
		t.Fatalf("Create(%q) error = %v", slug, err)
	}
	return created
}

// namesInOrder lists category names in response order.
func namesInOrder(categories []Category) string {
	names := make([]string, 0, len(categories))
	for _, category := range categories {
		names = append(names, category.Name)
	}
	return strings.Join(names, ",")
}

// sortedSlugs lists post slugs in a stable order, for set comparisons.
func sortedSlugs(posts []Post) string {
	slugs := make([]string, 0, len(posts))
	for _, entry := range posts {
		slugs = append(slugs, entry.Slug)
	}
	sort.Strings(slugs)
	return strings.Join(slugs, ",")
}

func TestListReturnsAllPostsNewestFirst(t *testing.T) {
	fixture := newListFixture(t)

	posts, err := fixture.svc.List(context.Background(), ListFilter{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(posts) != 3 {
		t.Fatalf("len(posts) = %d, want 3", len(posts))
	}

	seen := map[string]bool{}
	for index, entry := range posts {
		seen[entry.Slug] = true
		if index > 0 && posts[index-1].CreatedAt.Before(entry.CreatedAt) {
			t.Fatalf("posts[%d].created_at = %v, want newest first after %v", index, entry.CreatedAt, posts[index-1].CreatedAt)
		}
	}
	for _, want := range []string{fixture.p1.Slug, fixture.p2.Slug, fixture.p3.Slug} {
		if !seen[want] {
			t.Fatalf("List() slugs = %q, want %q", sortedSlugs(posts), want)
		}
	}
}

func TestListFiltersByCategorySlug(t *testing.T) {
	fixture := newListFixture(t)

	alpha, err := fixture.svc.List(context.Background(), ListFilter{CategorySlug: "alpha"})
	if err != nil {
		t.Fatalf("List(alpha) error = %v", err)
	}
	if got := sortedSlugs(alpha); got != "claude-code-hooks,setup-claude-code" {
		t.Fatalf("List(alpha) slugs = %q, want both alpha posts", got)
	}

	beta, err := fixture.svc.List(context.Background(), ListFilter{CategorySlug: "beta"})
	if err != nil {
		t.Fatalf("List(beta) error = %v", err)
	}
	if got := sortedSlugs(beta); got != "claude-code-hooks,postgres-notes" {
		t.Fatalf("List(beta) slugs = %q, want both beta posts", got)
	}

	// the service trims and lowercases the filter before the store call
	padded, err := fixture.svc.List(context.Background(), ListFilter{CategorySlug: " ALPHA "})
	if err != nil {
		t.Fatalf("List( ALPHA ) error = %v", err)
	}
	if got := sortedSlugs(padded); got != "claude-code-hooks,setup-claude-code" {
		t.Fatalf("List( ALPHA ) slugs = %q, want both alpha posts", got)
	}

	unknown, err := fixture.svc.List(context.Background(), ListFilter{CategorySlug: "nope"})
	if err != nil {
		t.Fatalf("List(nope) error = %v", err)
	}
	if unknown == nil {
		t.Fatal("List(nope) = nil, want an empty slice")
	}
	if len(unknown) != 0 {
		t.Fatalf("len(List(nope)) = %d, want 0", len(unknown))
	}
}

func TestListFiltersByQueryCaseInsensitive(t *testing.T) {
	fixture := newListFixture(t)

	for _, query := range []string{"claude", "CLAUDE", "Claude Code"} {
		posts, err := fixture.svc.List(context.Background(), ListFilter{Query: query})
		if err != nil {
			t.Fatalf("List(%q) error = %v", query, err)
		}
		if got := sortedSlugs(posts); got != "claude-code-hooks,setup-claude-code" {
			t.Fatalf("List(%q) slugs = %q, want both claude posts", query, got)
		}
	}

	// the query combines with the category filter
	combined, err := fixture.svc.List(context.Background(), ListFilter{CategorySlug: "beta", Query: "claude"})
	if err != nil {
		t.Fatalf("List(beta, claude) error = %v", err)
	}
	if got := sortedSlugs(combined); got != "claude-code-hooks" {
		t.Fatalf("List(beta, claude) slugs = %q, want only claude-code-hooks", got)
	}

	miss, err := fixture.svc.List(context.Background(), ListFilter{CategorySlug: "beta", Query: "setup"})
	if err != nil {
		t.Fatalf("List(beta, setup) error = %v", err)
	}
	if miss == nil {
		t.Fatal("List(beta, setup) = nil, want an empty slice")
	}
	if len(miss) != 0 {
		t.Fatalf("len(List(beta, setup)) = %d, want 0", len(miss))
	}
}

func TestListLeavesItemsEmpty(t *testing.T) {
	fixture := newListFixture(t)

	posts, err := fixture.svc.List(context.Background(), ListFilter{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(posts) != 3 {
		t.Fatalf("len(posts) = %d, want 3", len(posts))
	}
	for _, entry := range posts {
		if len(entry.Items) != 0 {
			t.Fatalf("post %q items = %d, want 0 on list responses", entry.Slug, len(entry.Items))
		}
	}

	// the detail read is the one that fills items
	found, err := fixture.svc.Get(context.Background(), fixture.p2.Slug)
	if err != nil {
		t.Fatalf("Get(%q) error = %v", fixture.p2.Slug, err)
	}
	if len(found.Items) != 3 {
		t.Fatalf("detail items = %d, want 3", len(found.Items))
	}
}

func TestFindBySlugReturnsCategoriesAndItemsInDisplayOrder(t *testing.T) {
	fixture := newListFixture(t)

	found, err := fixture.svc.Get(context.Background(), "claude-code-hooks")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if found.ID != fixture.p2.ID {
		t.Fatalf("id = %q, want %q", found.ID, fixture.p2.ID)
	}
	if got := namesInOrder(found.Categories); got != "Alpha,Beta" {
		t.Fatalf("category names = %q, want the name order Alpha,Beta", got)
	}
	if len(found.Items) != 3 {
		t.Fatalf("len(items) = %d, want 3", len(found.Items))
	}
	if got := positionsInOrder(found.Items); got != "0,1,2" {
		t.Fatalf("item positions = %q, want 0,1,2", got)
	}
	if got := textValue(t, "items[0].body_text", found.Items[0].BodyText); got != "# claude-code-hooks 0" {
		t.Fatalf("items[0].body_text = %q, want the first markdown body", got)
	}
}

func TestGetUnknownSlugReturnsNotFound(t *testing.T) {
	fixture := newListFixture(t)

	if _, err := fixture.svc.Get(context.Background(), "does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(unknown) error = %v, want ErrNotFound", err)
	}
}

func TestGetNormalizesSlugBeforeStoreLookup(t *testing.T) {
	fixture := newListFixture(t)

	found, err := fixture.svc.Get(context.Background(), "  SETUP-CLAUDE-CODE  ")
	if err != nil {
		t.Fatalf("Get( SETUP-CLAUDE-CODE ) error = %v", err)
	}
	if found.Slug != "setup-claude-code" {
		t.Fatalf("slug = %q, want setup-claude-code", found.Slug)
	}
	if found.ID != fixture.p1.ID {
		t.Fatalf("id = %q, want %q", found.ID, fixture.p1.ID)
	}
}

// TestListRejectsNULInFilters proves a NUL byte in the query or the category
// filter is invalid input, so the Postgres store never sees a byte its text
// columns cannot hold.
func TestListRejectsNULInFilters(t *testing.T) {
	fixture := newListFixture(t)

	for _, filter := range []ListFilter{
		{Query: "a\x00b"},
		{Query: "\x00"},
		{CategorySlug: "\x00"},
		{CategorySlug: "alpha\x00"},
	} {
		if _, err := fixture.svc.List(context.Background(), filter); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("List(%+v) error = %v, want ErrInvalidInput", filter, err)
		}
	}
}

// TestGetRejectsNULSlug proves a NUL slug can only miss: no valid slug carries
// the byte, so the answer is the same ErrNotFound as any other unknown slug.
func TestGetRejectsNULSlug(t *testing.T) {
	fixture := newListFixture(t)

	if _, err := fixture.svc.Get(context.Background(), "\x00"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(NUL) error = %v, want ErrNotFound", err)
	}
}

// TestListHandlerNULFilterReturnsBadRequest proves the handler answers 400 for
// a NUL filter instead of letting the store fail the query with a 500.
func TestListHandlerNULFilterReturnsBadRequest(t *testing.T) {
	mux, _ := newTestMuxWithStore()

	for _, path := range []string{"/posts?q=%00", "/posts?category=%00"} {
		res := doJSON(t, mux, http.MethodGet, path, "", "")
		if res.Code != http.StatusBadRequest {
			t.Fatalf("GET %s status = %d, want %d; body = %s", path, res.Code, http.StatusBadRequest, res.Body.String())
		}
	}
}

// TestGetHandlerNULSlugReturnsNotFound proves the detail read answers 404 for a
// NUL slug, the same answer as any other slug that cannot exist.
func TestGetHandlerNULSlugReturnsNotFound(t *testing.T) {
	mux, _ := newTestMuxWithStore()

	res := doJSON(t, mux, http.MethodGet, "/posts/%00", "", "")
	if res.Code != http.StatusNotFound {
		t.Fatalf("GET /posts/%%00 status = %d, want %d; body = %s", res.Code, http.StatusNotFound, res.Body.String())
	}
}

func TestListHandlerReturnsPostsWithoutItems(t *testing.T) {
	mux, store := newTestMuxWithStore()
	svc := NewService(store)
	createCategory(t, svc, "alpha", "Alpha")
	createCategory(t, svc, "beta", "Beta")
	seedPost(t, svc, "setup-claude-code", "Setup Claude Code", []string{"alpha"}, 2)
	seedPost(t, svc, "claude-code-hooks", "Claude Code Hooks", []string{"alpha", "beta"}, 3)
	seedPost(t, svc, "postgres-notes", "Postgres Notes", []string{"beta"}, 4)

	res := doJSON(t, mux, http.MethodGet, "/posts", "", "")
	if res.Code != http.StatusOK {
		t.Fatalf("GET /posts status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	var listed []Post
	if err := json.Unmarshal(res.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode posts: %v", err)
	}
	if len(listed) != 3 {
		t.Fatalf("len(posts) = %d, want 3", len(listed))
	}
	for index, entry := range listed {
		if index > 0 && listed[index-1].CreatedAt.Before(entry.CreatedAt) {
			t.Fatalf("posts[%d].created_at = %v, want newest first after %v", index, entry.CreatedAt, listed[index-1].CreatedAt)
		}
	}

	// the wire shape omits items on every list element
	var raw []map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw posts: %v", err)
	}
	for _, entry := range raw {
		if _, ok := entry["items"]; ok {
			t.Fatalf("post %v carries an items key, want it omitted", entry["slug"])
		}
	}

	// category and query narrow the same list
	res = doJSON(t, mux, http.MethodGet, "/posts?category=beta&q=claude", "", "")
	if res.Code != http.StatusOK {
		t.Fatalf("GET /posts?category=beta&q=claude status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	var filtered []Post
	if err := json.Unmarshal(res.Body.Bytes(), &filtered); err != nil {
		t.Fatalf("decode filtered posts: %v", err)
	}
	if got := sortedSlugs(filtered); got != "claude-code-hooks" {
		t.Fatalf("filtered slugs = %q, want only claude-code-hooks", got)
	}

	// an unknown category is an empty array, not an error
	res = doJSON(t, mux, http.MethodGet, "/posts?category=nope", "", "")
	if res.Code != http.StatusOK {
		t.Fatalf("GET /posts?category=nope status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	if got := strings.TrimSpace(res.Body.String()); got != "[]" {
		t.Fatalf("unknown category body = %q, want []", got)
	}
}

func TestGetHandlerReturnsItemsInPositionOrder(t *testing.T) {
	mux, store := newTestMuxWithStore()
	svc := NewService(store)
	createCategory(t, svc, "alpha", "Alpha")

	created, err := svc.Create(context.Background(), CreatePostInput{
		Slug:          "setup-claude-code",
		Title:         "Setup Claude Code",
		AuthorID:      "user-1",
		CategorySlugs: []string{"alpha"},
		Items: []ItemInput{
			{Kind: KindMarkdown, BodyText: text("# Intro")},
			{Kind: KindLink, URL: text("https://ampcode.com/docs")},
			{Kind: KindFile, BodyText: text("# Notes\n\nInline file."), Filename: text("notes.md")},
		},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	res := doJSON(t, mux, http.MethodGet, "/posts/setup-claude-code", "", "")
	if res.Code != http.StatusOK {
		t.Fatalf("GET /posts/setup-claude-code status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	var found Post
	if err := json.Unmarshal(res.Body.Bytes(), &found); err != nil {
		t.Fatalf("decode post: %v", err)
	}
	if found.ID != created.ID {
		t.Fatalf("id = %q, want %q", found.ID, created.ID)
	}
	if got := slugsInOrder(found.Categories); got != "alpha" {
		t.Fatalf("category slugs = %q, want alpha", got)
	}
	if got := positionsInOrder(found.Items); got != "0,1,2" {
		t.Fatalf("item positions = %q, want 0,1,2", got)
	}
	for index, want := range []Kind{KindMarkdown, KindLink, KindFile} {
		if got := found.Items[index].Kind; got != want {
			t.Fatalf("items[%d].kind = %q, want %q", index, got, want)
		}
	}
	if got := textValue(t, "items[2].body_text", found.Items[2].BodyText); got != "# Notes\n\nInline file." {
		t.Fatalf("items[2].body_text = %q, want the inline file body", got)
	}
	if got := textValue(t, "items[2].filename", found.Items[2].Filename); got != "notes.md" {
		t.Fatalf("items[2].filename = %q, want notes.md", got)
	}
	if got := textValue(t, "items[1].url", found.Items[1].URL); got != "https://ampcode.com/docs" {
		t.Fatalf("items[1].url = %q, want the link", got)
	}
}

func TestGetHandlerUnknownSlugReturnsNotFound(t *testing.T) {
	mux, _ := newTestMuxWithStore()

	res := doJSON(t, mux, http.MethodGet, "/posts/does-not-exist", "", "")
	if res.Code != http.StatusNotFound {
		t.Fatalf("GET /posts/does-not-exist status = %d, want %d; body = %s", res.Code, http.StatusNotFound, res.Body.String())
	}
	if got := strings.TrimSpace(res.Body.String()); got != "post not found" {
		t.Fatalf("404 body = %q, want post not found", got)
	}
}

// TestEscapeLikePattern proves the ILIKE search text becomes literal: a
// backslash, a percent, and an underscore each gain one backslash, so the
// Postgres store matches the memory store.
func TestEscapeLikePattern(t *testing.T) {
	// input characters: 1 0 0 % _ x \ -> each metacharacter gains one backslash
	if got := escapeLikePattern(`100%_x\`); got != `100\%\_x\\` {
		t.Fatalf("escapeLikePattern(%q) = %q, want %q", `100%_x\`, got, `100\%\_x\\`)
	}

	// plain text stays untouched, and so does the empty query
	if got := escapeLikePattern("plain text"); got != "plain text" {
		t.Fatalf("escapeLikePattern(%q) = %q, want it unchanged", "plain text", got)
	}
	if got := escapeLikePattern(""); got != "" {
		t.Fatalf("escapeLikePattern(%q) = %q, want %q", "", got, "")
	}
}

// TestListQueryTreatsLikeMetacharactersLiterally proves the memory store reads
// %, _, and \ as plain characters, the same semantics escapeLikePattern gives
// the Postgres store.
func TestListQueryTreatsLikeMetacharactersLiterally(t *testing.T) {
	svc := NewService(NewMemoryStore())
	createCategory(t, svc, "alpha", "Alpha")
	seedPost(t, svc, "percent-title", "100% done", []string{"alpha"}, 1)
	seedPost(t, svc, "plain-title", "100x done", []string{"alpha"}, 1)

	posts, err := svc.List(context.Background(), ListFilter{Query: "100%"})
	if err != nil {
		t.Fatalf("List(100%%) error = %v", err)
	}
	if got := sortedSlugs(posts); got != "percent-title" {
		t.Fatalf("List(100%%) slugs = %q, want only percent-title", got)
	}
}

// TestListCategoriesTieBreaksOnID proves same-name categories come back in a
// stable (name, id) order, the same order the Postgres store returns.
func TestListCategoriesTieBreaksOnID(t *testing.T) {
	// Seed the store directly with ids in descending order, so only a real id
	// tie-break can produce the ascending result; generated ids are random and
	// could pass a name-only sort by luck.
	store := NewMemoryStore()
	store.categories = []Category{
		{ID: "ffffffff-ffff-4fff-8fff-ffffffffffff", Slug: "same-two", Name: "Same Name"},
		{ID: "00000000-0000-4000-8000-000000000000", Slug: "same-one", Name: "Same Name"},
		{ID: "7fffffff-ffff-4fff-8fff-ffffffffffff", Slug: "other", Name: "Other"},
	}
	svc := NewService(store)

	first, err := svc.ListCategories(context.Background())
	if err != nil {
		t.Fatalf("ListCategories() error = %v", err)
	}
	if len(first) != 3 {
		t.Fatalf("len(categories) = %d, want 3", len(first))
	}
	if got := namesInOrder(first); got != "Other,Same Name,Same Name" {
		t.Fatalf("category names = %q, want Other,Same Name,Same Name", got)
	}
	if first[1].ID != "00000000-0000-4000-8000-000000000000" || first[2].ID != "ffffffff-ffff-4fff-8fff-ffffffffffff" {
		t.Fatalf("tie-break ids = %q,%q, want ascending id order", first[1].ID, first[2].ID)
	}
	if first[1].Slug != "same-one" || first[2].Slug != "same-two" {
		t.Fatalf("tie-break slugs = %q,%q, want same-one,same-two", first[1].Slug, first[2].Slug)
	}

	// the order is stable across repeated reads
	second, err := svc.ListCategories(context.Background())
	if err != nil {
		t.Fatalf("second ListCategories() error = %v", err)
	}
	if got, want := slugsInOrder(second), slugsInOrder(first); got != want {
		t.Fatalf("second read slugs = %q, want the same order %q", got, want)
	}
}

// TestCreatePostCategoryTieBreaksOnSlug proves the categories embedded in a
// post come back in (name, slug) order, the order a Postgres detail read
// returns.
func TestCreatePostCategoryTieBreaksOnSlug(t *testing.T) {
	svc := NewService(NewMemoryStore())
	createCategory(t, svc, "same-two", "Same Name")
	createCategory(t, svc, "same-one", "Same Name")
	createCategory(t, svc, "alpha", "Alpha")

	created, err := svc.Create(context.Background(), CreatePostInput{
		Slug:          "tie-break-order",
		Title:         "Tie Break Order",
		AuthorID:      "user-1",
		CategorySlugs: []string{"same-two", "same-one", "alpha"},
		Items:         []ItemInput{{Kind: KindText, BodyText: text("body")}},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if got := slugsInOrder(created.Categories); got != "alpha,same-one,same-two" {
		t.Fatalf("category slugs = %q, want the name then slug order alpha,same-one,same-two", got)
	}

	stored, err := svc.Get(context.Background(), "tie-break-order")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got := slugsInOrder(stored.Categories); got != "alpha,same-one,same-two" {
		t.Fatalf("stored category slugs = %q, want alpha,same-one,same-two", got)
	}
}
