package tools

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureServer serves the three trimmed real source files at /providers.md,
// /codingagents.md, and /ade.md, the same paths the raw GitHub base uses.
// The whole fetch path runs: request, body read, table parse, replace.
func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	for _, cat := range categories {
		name := cat.file
		mux.HandleFunc("/"+name, func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, filepath.Join("testdata", name))
		})
	}
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

// newTestService points the service at one test server and a memory store.
func newTestService(t *testing.T, server *httptest.Server) (*Service, *MemoryStore) {
	t.Helper()
	store := NewMemoryStore()
	svc := newServiceWithSource(store, source{rawBase: server.URL, client: server.Client()})
	return svc, store
}

// listTools reads the store, failing the test on any error.
func listTools(t *testing.T, store *MemoryStore) []Tool {
	t.Helper()
	list, err := store.List(context.Background(), ListFilter{})
	if err != nil {
		t.Fatalf("store.List() error = %v", err)
	}
	return list
}

func TestNewServiceFillsSourceDefaults(t *testing.T) {
	svc := NewService(NewMemoryStore())
	if svc.source.rawBase != defaultRawBase {
		t.Fatalf("rawBase = %q, want %q", svc.source.rawBase, defaultRawBase)
	}
	if svc.source.pageBase != defaultPageBase {
		t.Fatalf("pageBase = %q, want %q", svc.source.pageBase, defaultPageBase)
	}
	if svc.source.client == nil || svc.source.client.Timeout != fetchTimeout {
		t.Fatal("client = nil or missing the fetch timeout")
	}
}

func TestSyncStoresAllCategoriesInOrder(t *testing.T) {
	svc, store := newTestService(t, fixtureServer(t))

	stored, err := svc.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if stored != 10 {
		t.Fatalf("Sync() stored = %d, want 10", stored)
	}

	list := listTools(t, store)
	if len(list) != 10 {
		t.Fatalf("len(list) = %d, want 10", len(list))
	}
	// Catalog order: providers first, then coding agents, then ADE.
	if list[0].ID != "P-001" || list[4].ID != "CA-001" || list[9].ID != "ADE-048" {
		t.Fatalf("order = %q %q %q, want P-001 first, CA-001 then ADE-048 last",
			list[0].ID, list[4].ID, list[9].ID)
	}
	for _, tool := range list {
		if tool.Category == "" || tool.Name == "" || tool.Status == "" {
			t.Fatalf("tool %q misses mapped fields: %+v", tool.ID, tool)
		}
	}
}

func TestSyncTwiceReplacesSnapshot(t *testing.T) {
	svc, store := newTestService(t, fixtureServer(t))
	ctx := context.Background()

	if _, err := svc.Sync(ctx); err != nil {
		t.Fatalf("first Sync() error = %v", err)
	}
	if _, err := svc.Sync(ctx); err != nil {
		t.Fatalf("second Sync() error = %v", err)
	}
	if list := listTools(t, store); len(list) != 10 {
		t.Fatalf("len(list) = %d after the second sync, want 10", len(list))
	}
}

func TestSyncFailureKeepsTheLastSnapshot(t *testing.T) {
	server := fixtureServer(t)
	svc, store := newTestService(t, server)
	ctx := context.Background()

	if _, err := svc.Sync(ctx); err != nil {
		t.Fatalf("first Sync() error = %v", err)
	}

	// The source starts failing: ade.md answers 500. The whole run aborts and
	// the snapshot from the good run keeps serving.
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "ade.md") {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		http.ServeFile(w, r, filepath.Join("testdata", filepath.Base(r.URL.Path)))
	}))
	t.Cleanup(broken.Close)
	svc.source.rawBase = broken.URL

	if _, err := svc.Sync(ctx); err == nil {
		t.Fatal("Sync() error = nil, want a failure")
	}
	if list := listTools(t, store); len(list) != 10 {
		t.Fatalf("len(list) = %d after a failed sync, want the 10 old rows", len(list))
	}
}

func TestListFiltersCategoryAndQuery(t *testing.T) {
	svc, _ := newTestService(t, fixtureServer(t))
	ctx := context.Background()
	if _, err := svc.Sync(ctx); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	bySlug, err := svc.List(ctx, ListFilter{Category: "coding-agents"})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(bySlug) != 3 || bySlug[0].ID != "CA-001" {
		t.Fatalf("by slug = %+v, want the 3 coding agents", bySlug)
	}

	byPrefix, err := svc.List(ctx, ListFilter{Category: "ADE"})
	if err != nil {
		t.Fatalf("List() by prefix error = %v", err)
	}
	if len(byPrefix) != 3 || byPrefix[0].ID != "ADE-001" {
		t.Fatalf("by prefix = %+v, want the 3 ADE rows", byPrefix)
	}

	found, err := svc.List(ctx, ListFilter{Query: "cursor"})
	if err != nil {
		t.Fatalf("List() query error = %v", err)
	}
	if len(found) != 1 || found[0].ID != "ADE-001" {
		t.Fatalf("query cursor = %+v, want only ADE-001 Cursor", found)
	}

	both, err := svc.List(ctx, ListFilter{Category: "providers", Query: "openai"})
	if err != nil {
		t.Fatalf("List() combined error = %v", err)
	}
	if len(both) != 1 || both[0].ID != "P-001" {
		t.Fatalf("combined filter = %+v, want only P-001", both)
	}
}

func TestListRejectsBadFilters(t *testing.T) {
	svc, _ := newTestService(t, fixtureServer(t))
	ctx := context.Background()

	if _, err := svc.List(ctx, ListFilter{Category: "nope"}); !errors.Is(err, ErrUnknownCategory) {
		t.Fatalf("List() unknown category error = %v, want ErrUnknownCategory", err)
	}
	if _, err := svc.List(ctx, ListFilter{Query: "a\x00b"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("List() NUL query error = %v, want ErrInvalidInput", err)
	}
	if _, err := svc.List(ctx, ListFilter{Category: "p\x00roviders"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("List() NUL category error = %v, want ErrInvalidInput", err)
	}
}

func TestGetFindsToolCaseInsensitive(t *testing.T) {
	svc, _ := newTestService(t, fixtureServer(t))
	ctx := context.Background()
	if _, err := svc.Sync(ctx); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	tool, err := svc.Get(ctx, "ca-018")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if tool.ID != "CA-018" || tool.Name != "Devin" {
		t.Fatalf("Get() = %+v, want CA-018 Devin", tool)
	}

	if _, err := svc.Get(ctx, "XX-999"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() unknown id error = %v, want ErrNotFound", err)
	}
	if _, err := svc.Get(ctx, " \x00 "); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() NUL id error = %v, want ErrNotFound", err)
	}
}

func TestCategoriesReportsTheMapping(t *testing.T) {
	svc, _ := newTestService(t, fixtureServer(t))
	ctx := context.Background()
	if _, err := svc.Sync(ctx); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	list, err := svc.Categories(ctx)
	if err != nil {
		t.Fatalf("Categories() error = %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("len(categories) = %d, want 3", len(list))
	}
	want := []struct {
		slug, name, prefix, file string
		count                    int
	}{
		{"providers", "Providers", "P", "providers.md", 4},
		{"coding-agents", "Coding Agents", "CA", "codingagents.md", 3},
		{"ade", "AI Dev Environment", "ADE", "ade.md", 3},
	}
	for i, w := range want {
		got := list[i]
		if got.Slug != w.slug || got.Name != w.name || got.Prefix != w.prefix || got.SourceFile != w.file || got.Count != w.count {
			t.Fatalf("categories[%d] = %+v, want slug=%q name=%q prefix=%q file=%q count=%d",
				i, got, w.slug, w.name, w.prefix, w.file, w.count)
		}
		if got.SourceURL != defaultPageBase+"/"+w.file {
			t.Fatalf("categories[%d].SourceURL = %q, want the GitHub page for %q", i, got.SourceURL, w.file)
		}
	}
}
