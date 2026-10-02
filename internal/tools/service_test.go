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

// idsCSV renders row ids as one comma-joined string. Sorting assertions read
// better as a whole sequence than as a pile of index checks.
func idsCSV(rows []Tool) string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return strings.Join(ids, ",")
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
	if len(bySlug.Data) != 3 || bySlug.Data[0].ID != "CA-001" || bySlug.Meta.Total != 3 {
		t.Fatalf("by slug = %+v, want the 3 coding agents", bySlug)
	}

	byPrefix, err := svc.List(ctx, ListFilter{Category: "ADE"})
	if err != nil {
		t.Fatalf("List() by prefix error = %v", err)
	}
	if len(byPrefix.Data) != 3 || byPrefix.Data[0].ID != "ADE-001" || byPrefix.Meta.Total != 3 {
		t.Fatalf("by prefix = %+v, want the 3 ADE rows", byPrefix)
	}

	found, err := svc.List(ctx, ListFilter{Query: "cursor"})
	if err != nil {
		t.Fatalf("List() query error = %v", err)
	}
	if len(found.Data) != 1 || found.Data[0].ID != "ADE-001" || found.Meta.Total != 1 {
		t.Fatalf("query cursor = %+v, want only ADE-001 Cursor", found)
	}

	both, err := svc.List(ctx, ListFilter{Category: "providers", Query: "openai"})
	if err != nil {
		t.Fatalf("List() combined error = %v", err)
	}
	if len(both.Data) != 1 || both.Data[0].ID != "P-001" || both.Meta.Total != 1 {
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

// TestListSortsByNameAndUpdated pins the two sort keys. Name order is
// case-insensitive and every tie breaks by id ascending in both directions, so
// a page boundary can never repeat or drop a row.
func TestListSortsByNameAndUpdated(t *testing.T) {
	svc, _ := newTestService(t, fixtureServer(t))
	ctx := context.Background()
	if _, err := svc.Sync(ctx); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	// The fixtures hold Cursor, Devin, GitHub Copilot, Groq, HematToken, OmO,
	// OpenAgentic.id, OpenAI, Warp and Zuse. "OmO" lands before "OpenAI" because
	// "om" precedes "op".
	nameAsc := "ADE-001,CA-018,CA-001,P-005,P-153,CA-054,P-026,P-001,ADE-012,ADE-048"
	nameDesc := "ADE-048,ADE-012,P-001,P-026,CA-054,P-153,P-005,CA-001,CA-018,ADE-001"
	// Four rows share 2026-07-12 and two share 2026-09-24. The id tie-break keeps
	// each run in id order even when the date order is reversed.
	updatedAsc := "ADE-001,CA-001,P-001,P-005,ADE-012,CA-018,P-026,P-153,ADE-048,CA-054"
	updatedDesc := "ADE-048,CA-054,P-153,P-026,ADE-012,CA-018,ADE-001,CA-001,P-001,P-005"

	for _, tc := range []struct {
		name   string
		filter ListFilter
		want   string
	}{
		{"name asc", ListFilter{Sort: SortName, Order: OrderAsc}, nameAsc},
		{"name desc", ListFilter{Sort: SortName, Order: OrderDesc}, nameDesc},
		{"name without order", ListFilter{Sort: SortName}, nameAsc},
		{"updated asc", ListFilter{Sort: SortUpdated, Order: OrderAsc}, updatedAsc},
		{"updated desc", ListFilter{Sort: SortUpdated, Order: OrderDesc}, updatedDesc},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := svc.List(ctx, tc.filter)
			if err != nil {
				t.Fatalf("List(%+v) error = %v", tc.filter, err)
			}
			if joined := idsCSV(got.Data); joined != tc.want {
				t.Fatalf("List(%+v) ids = %s, want %s", tc.filter, joined, tc.want)
			}
		})
	}
}

// TestListGroupVocabulary pins every spelling of the group filter. Each one
// selects a single category; "all" and an empty filter select the catalog.
func TestListGroupVocabulary(t *testing.T) {
	svc, _ := newTestService(t, fixtureServer(t))
	ctx := context.Background()
	if _, err := svc.Sync(ctx); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	whole := "P-001,P-005,P-026,P-153,CA-001,CA-018,CA-054,ADE-001,ADE-012,ADE-048"
	for _, tc := range []struct {
		group string
		want  string
		total int
	}{
		{"", whole, 10},
		{"all", whole, 10},
		{"ALL", whole, 10},
		{"provider", "P-001,P-005,P-026,P-153", 4},
		{"providers", "P-001,P-005,P-026,P-153", 4},
		{"p", "P-001,P-005,P-026,P-153", 4},
		{"coding-agent", "CA-001,CA-018,CA-054", 3},
		{"coding_agent", "CA-001,CA-018,CA-054", 3},
		{"coding agent", "CA-001,CA-018,CA-054", 3},
		{"coding-agents", "CA-001,CA-018,CA-054", 3},
		{"ca", "CA-001,CA-018,CA-054", 3},
		{"ade", "ADE-001,ADE-012,ADE-048", 3},
		{"ADE", "ADE-001,ADE-012,ADE-048", 3},
	} {
		got, err := svc.List(ctx, ListFilter{Category: tc.group})
		if err != nil {
			t.Fatalf("List(group=%q) error = %v", tc.group, err)
		}
		if joined := idsCSV(got.Data); joined != tc.want {
			t.Fatalf("List(group=%q) ids = %s, want %s", tc.group, joined, tc.want)
		}
		if got.Meta.Total != tc.total {
			t.Fatalf("List(group=%q) total = %d, want %d", tc.group, got.Meta.Total, tc.total)
		}
	}
}

// TestListPagesTheCatalog pins the window the page parameters cut, plus the
// metadata that travels with it. A page past the end is empty but still reports
// the real total, so the client can tell "no more" from "nothing matched".
func TestListPagesTheCatalog(t *testing.T) {
	svc, _ := newTestService(t, fixtureServer(t))
	ctx := context.Background()
	if _, err := svc.Sync(ctx); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	page := func(n int) ListFilter {
		return ListFilter{Sort: SortName, Page: n, PerPage: 4}
	}

	first, err := svc.List(ctx, page(1))
	if err != nil {
		t.Fatalf("List(page=1) error = %v", err)
	}
	if ids := idsCSV(first.Data); ids != "ADE-001,CA-018,CA-001,P-005" {
		t.Fatalf("page 1 ids = %s, want the first 4 by name", ids)
	}
	if first.Meta != (PageMeta{Page: 1, PerPage: 4, Total: 10, TotalPages: 3}) {
		t.Fatalf("page 1 meta = %+v, want 1/4/10/3", first.Meta)
	}

	middle, err := svc.List(ctx, page(2))
	if err != nil {
		t.Fatalf("List(page=2) error = %v", err)
	}
	if ids := idsCSV(middle.Data); ids != "P-153,CA-054,P-026,P-001" {
		t.Fatalf("page 2 ids = %s, want the middle 4 by name", ids)
	}

	last, err := svc.List(ctx, page(3))
	if err != nil {
		t.Fatalf("List(page=3) error = %v", err)
	}
	if ids := idsCSV(last.Data); ids != "ADE-012,ADE-048" {
		t.Fatalf("page 3 ids = %s, want the last 2 by name", ids)
	}

	past, err := svc.List(ctx, page(9))
	if err != nil {
		t.Fatalf("List(page=9) error = %v", err)
	}
	if len(past.Data) != 0 {
		t.Fatalf("page 9 ids = %s, want no rows", idsCSV(past.Data))
	}
	if past.Meta != (PageMeta{Page: 9, PerPage: 4, Total: 10, TotalPages: 3}) {
		t.Fatalf("page 9 meta = %+v, want 9/4/10/3", past.Meta)
	}
}

// TestListRejectsBadSortAndPaging pins the values the catalog refuses, and
// confirms a lone order is dropped rather than rejected.
func TestListRejectsBadSortAndPaging(t *testing.T) {
	svc, _ := newTestService(t, fixtureServer(t))
	ctx := context.Background()
	if _, err := svc.Sync(ctx); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	for _, filter := range []ListFilter{
		{Sort: "price"},
		{Sort: SortName, Order: "sideways"},
		{Sort: SortUpdated, Order: "up"},
		{Page: -1},
		{Page: 1, PerPage: -5},
		{Page: 1, PerPage: MaxPerPage + 1},
	} {
		if _, err := svc.List(ctx, filter); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("List(%+v) error = %v, want ErrInvalidInput", filter, err)
		}
	}

	kept, err := svc.List(ctx, ListFilter{Order: OrderDesc, PerPage: 1})
	if err != nil {
		t.Fatalf("List() order only error = %v", err)
	}
	if kept.Meta != (PageMeta{Page: 1, PerPage: 1, Total: 10, TotalPages: 10}) {
		t.Fatalf("List() order only meta = %+v, want the requested page size", kept.Meta)
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
