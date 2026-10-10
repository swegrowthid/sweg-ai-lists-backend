package dailyterm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// treeJSON is the shape the source tree endpoint answers with. The fixture
// server builds it from the month files it serves.
func treeJSON(paths []string) string {
	entries := make([]map[string]string, 0, len(paths))
	for _, path := range paths {
		entries = append(entries, map[string]string{"path": path})
	}
	body, _ := json.Marshal(map[string]any{"truncated": false, "tree": entries})
	return string(body)
}

// fixturePaths are the month files the fixture server publishes. It mirrors the
// irregular upstream set: a short month name next to a long one, a gap, and a
// term file for the following year.
var fixturePaths = []string{
	"README.md",
	"2025/june-term.md",
	"2026/jan-term.md",
	"2026/mar-term.md",
}

// fixtureServer serves the source tree listing and each month file, the same way
// the raw GitHub base and the GitHub tree endpoint do.
func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/tree", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(treeJSON(fixturePaths)))
	})
	// Each month path maps to a testdata file: 2026/jan-term.md -> jan-term.md.
	files := map[string]string{
		"/2025/june-term.md": "june-term.md",
		"/2026/jan-term.md":  "jan-term.md",
		"/2026/mar-term.md":  "quirks.md",
	}
	for path, name := range files {
		file := name
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, filepath.Join("testdata", file))
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
	svc := newServiceWithSource(store, source{
		rawBase: server.URL,
		treeURL: server.URL + "/tree",
		client:  server.Client(),
	})
	return svc, store
}

// syncFixture syncs the fixture source, failing the test on any error.
func syncFixture(t *testing.T, svc *Service) int {
	t.Helper()
	stored, err := svc.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	return stored
}

func TestNewServiceFillsSourceDefaults(t *testing.T) {
	svc := NewService(NewMemoryStore())
	if svc.source.rawBase != defaultRawBase {
		t.Fatalf("rawBase = %q, want %q", svc.source.rawBase, defaultRawBase)
	}
	if svc.source.treeURL != defaultTreeURL {
		t.Fatalf("treeURL = %q, want %q", svc.source.treeURL, defaultTreeURL)
	}
	if svc.source.pageBase != defaultPageBase {
		t.Fatalf("pageBase = %q, want %q", svc.source.pageBase, defaultPageBase)
	}
	if svc.source.client == nil || svc.source.client.Timeout != fetchTimeout {
		t.Fatal("client = nil or missing the fetch timeout")
	}
}

func TestNewServicePanicsOnNilStore(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewService(nil) did not panic")
		}
	}()
	NewService(nil)
}

func TestSyncStoresEveryMonthItDiscovers(t *testing.T) {
	svc, store := newTestService(t, fixtureServer(t))
	// june 2025 = 3, january 2026 = 3, march 2026 (quirks) = 9.
	if stored := syncFixture(t, svc); stored != 15 {
		t.Fatalf("Sync() stored = %d, want 15", stored)
	}

	all, err := store.List(context.Background(), "")
	if err != nil {
		t.Fatalf("store.List() error = %v", err)
	}
	if len(all) != 15 {
		t.Fatalf("len(all) = %d, want 15", len(all))
	}
	// Months come back newest first.
	if all[0].MonthKey != "2026-03" || all[len(all)-1].MonthKey != "2025-06" {
		t.Fatalf("month order = %q .. %q, want 2026-03 first", all[0].MonthKey, all[len(all)-1].MonthKey)
	}
}

func TestSyncIsIdempotentAndReplacesTheSnapshot(t *testing.T) {
	svc, _ := newTestService(t, fixtureServer(t))
	syncFixture(t, svc)
	if stored := syncFixture(t, svc); stored != 15 {
		t.Fatalf("second Sync() stored = %d, want 15: the snapshot replaces, it does not append", stored)
	}
}

func TestSyncSkipsUnknownMonthTokensAndOtherPaths(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/tree" {
			// A file whose month token upstream never used, a file outside a year
			// directory, and a real month.
			_, _ = w.Write([]byte(treeJSON([]string{"notes.md", "2026/vend-term.md", "2026/jan-term.md"})))
			return
		}
		http.ServeFile(w, r, filepath.Join("testdata", "jan-term.md"))
	}))
	t.Cleanup(server.Close)
	svc, _ := newTestService(t, server)

	if stored := syncFixture(t, svc); stored != 3 {
		t.Fatalf("stored = %d, want only the 3 terms of the one known month", stored)
	}
}

func TestSyncFailsWithoutMonthsSoTheOldSnapshotSurvives(t *testing.T) {
	server := fixtureServer(t)
	svc, store := newTestService(t, server)
	syncFixture(t, svc)

	// The tree answers, but names no month file: upstream renamed or moved them.
	// The sync must fail rather than empty the journal.
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(treeJSON([]string{"README.md"})))
	}))
	t.Cleanup(empty.Close)
	svc.source.treeURL = empty.URL + "/tree"

	if _, err := svc.Sync(context.Background()); err == nil {
		t.Fatal("Sync() error = nil, want a failure when no month is listed")
	}
	all, _ := store.List(context.Background(), "")
	if len(all) != 15 {
		t.Fatalf("len(all) = %d after a failed sync, want the 15 old terms", len(all))
	}
}

func TestSyncFailureKeepsTheLastSnapshot(t *testing.T) {
	server := fixtureServer(t)
	svc, store := newTestService(t, server)
	syncFixture(t, svc)

	// One month starts failing. The whole run aborts, so a month is never
	// half-written.
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/tree" {
			_, _ = w.Write([]byte(treeJSON(fixturePaths)))
			return
		}
		if strings.HasSuffix(r.URL.Path, "jan-term.md") {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		http.ServeFile(w, r, filepath.Join("testdata", "june-term.md"))
	}))
	t.Cleanup(broken.Close)
	svc.source.rawBase = broken.URL
	svc.source.treeURL = broken.URL + "/tree"

	if _, err := svc.Sync(context.Background()); err == nil {
		t.Fatal("Sync() error = nil, want a failure")
	}
	all, _ := store.List(context.Background(), "")
	if len(all) != 15 {
		t.Fatalf("len(all) = %d after a failed sync, want the 15 old terms", len(all))
	}
}

func TestSyncRejectsATruncatedTree(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"truncated":true,"tree":[{"path":"2026/jan-term.md"}]}`))
	}))
	t.Cleanup(server.Close)
	svc, _ := newTestService(t, server)

	if _, err := svc.Sync(context.Background()); err == nil {
		t.Fatal("Sync() error = nil, want a failure on a truncated tree")
	}
}

func TestListServesOneMonth(t *testing.T) {
	svc, _ := newTestService(t, fixtureServer(t))
	syncFixture(t, svc)

	list, err := svc.List(context.Background(), "2026-01")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if list.Meta.MonthKey != "2026-01" || list.Meta.Name != "January" || list.Meta.Count != 3 {
		t.Fatalf("meta = %+v, want January with 3 terms", list.Meta)
	}
	if len(list.Data) != 3 {
		t.Fatalf("len(data) = %d, want 3", len(list.Data))
	}
	if list.Data[0].Title != "Principle of Least Surprise" || list.Data[0].Day != 1 {
		t.Fatalf("data[0] = %+v", list.Data[0])
	}
	if list.Meta.SourceFile != "2026/jan-term.md" {
		t.Fatalf("meta source = %q", list.Meta.SourceFile)
	}

	// A month with exactly one short file.
	june, err := svc.List(context.Background(), "2025-06")
	if err != nil {
		t.Fatalf("List(2025-06) error = %v", err)
	}
	if june.Meta.Name != "June" || june.Meta.Count != 3 {
		t.Fatalf("june meta = %+v", june.Meta)
	}
}

func TestListRejectsAMalformedMonth(t *testing.T) {
	svc, _ := newTestService(t, fixtureServer(t))
	syncFixture(t, svc)
	for _, raw := range []string{"2026", "2026-13", "2026-00", "26-01", "nope", "2026-1"} {
		if _, err := svc.List(context.Background(), raw); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("List(%q) error = %v, want ErrInvalidInput", raw, err)
		}
	}
	if _, err := svc.List(context.Background(), "2026-01\x00"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("List() NUL month error, want ErrInvalidInput")
	}
}

func TestListReportsAMonthWithNoFileAsNotFound(t *testing.T) {
	svc, _ := newTestService(t, fixtureServer(t))
	syncFixture(t, svc)

	// Well-formed, but outside the three fixture months (2025-06, 2026-01,
	// 2026-03). This is not a 400: the request was valid, the month was simply
	// never synced.
	if _, err := svc.List(context.Background(), "2025-02"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("List(2025-02) error = %v, want ErrNotFound", err)
	}
}

func TestListDefaultsToTheNewestMonthWhenTheCurrentOneIsEmpty(t *testing.T) {
	svc, _ := newTestService(t, fixtureServer(t))
	syncFixture(t, svc)
	// Pin the clock to a month the fixture does not carry, the way the first days
	// of a new month behave.
	svc.now = func() time.Time { return time.Date(2026, time.December, 3, 9, 0, 0, 0, time.UTC) }

	list, err := svc.List(context.Background(), "")
	if err != nil {
		t.Fatalf("List(\"\") error = %v", err)
	}
	if list.Meta.MonthKey != "2026-03" {
		t.Fatalf("meta = %+v, want the newest available month 2026-03", list.Meta)
	}
}

func TestListServesTheCurrentMonthWhenItExists(t *testing.T) {
	svc, _ := newTestService(t, fixtureServer(t))
	syncFixture(t, svc)
	svc.now = func() time.Time { return time.Date(2026, time.January, 4, 9, 0, 0, 0, time.UTC) }

	list, err := svc.List(context.Background(), "")
	if err != nil {
		t.Fatalf("List(\"\") error = %v", err)
	}
	if list.Meta.MonthKey != "2026-01" {
		t.Fatalf("meta = %+v, want the current month 2026-01", list.Meta)
	}
}

func TestListOnAnEmptySnapshotIsNotFound(t *testing.T) {
	svc := NewService(NewMemoryStore())
	if _, err := svc.List(context.Background(), ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("List() error = %v, want ErrNotFound before the first sync", err)
	}
}

func TestMonthsReportsEverySyncedMonthNewestFirst(t *testing.T) {
	svc, _ := newTestService(t, fixtureServer(t))
	syncFixture(t, svc)

	months, err := svc.Months(context.Background())
	if err != nil {
		t.Fatalf("Months() error = %v", err)
	}
	if len(months) != 3 {
		t.Fatalf("len(months) = %d, want 3: %+v", len(months), months)
	}
	want := []struct {
		key, name string
		count     int
	}{
		{"2026-03", "March", 9},
		{"2026-01", "January", 3},
		{"2025-06", "June", 3},
	}
	for i, w := range want {
		got := months[i]
		if got.MonthKey != w.key || got.Name != w.name || got.Count != w.count {
			t.Fatalf("months[%d] = %+v, want %s %s count %d", i, got, w.key, w.name, w.count)
		}
		if got.SourceFile == "" || got.SourceURL == "" {
			t.Fatalf("months[%d] misses provenance: %+v", i, got)
		}
	}
}

func TestMonthsIsEmptyBeforeTheFirstSync(t *testing.T) {
	svc := NewService(NewMemoryStore())
	months, err := svc.Months(context.Background())
	if err != nil {
		t.Fatalf("Months() error = %v", err)
	}
	if len(months) != 0 {
		t.Fatalf("months = %+v, want none before the first sync", months)
	}
}

func TestGetFindsATermById(t *testing.T) {
	svc, _ := newTestService(t, fixtureServer(t))
	syncFixture(t, svc)

	term, err := svc.Get(context.Background(), "2026-01-sieve-algorithm-or-sieve-of-eratosthenes")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if term.Title != "Sieve Algorithm or Sieve of Eratosthenes" || term.Day != 2 {
		t.Fatalf("Get() = %+v", term)
	}

	// The id resolves whatever its case, because a client may have hand-typed it.
	upper, err := svc.Get(context.Background(), "2026-01-SIEVE-ALGORITHM-OR-SIEVE-OF-ERATOSTHENES")
	if err != nil || upper.Title != term.Title {
		t.Fatalf("Get(upper) = %+v / %v", upper, err)
	}

	if _, err := svc.Get(context.Background(), "2026-01-nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(unknown) error = %v, want ErrNotFound", err)
	}
	if _, err := svc.Get(context.Background(), "  "); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(blank) error = %v, want ErrNotFound", err)
	}
	if _, err := svc.Get(context.Background(), "x\x00y"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(NUL) error = %v, want ErrNotFound", err)
	}
}

func TestStoreKeepsTheFirstOfADuplicateId(t *testing.T) {
	store := NewMemoryStore()
	first := Term{ID: "2026-01-dup", Title: "First", MonthKey: "2026-01"}
	second := Term{ID: "2026-01-dup", Title: "Second", MonthKey: "2026-01"}
	if err := store.ReplaceAll(context.Background(), []Term{first, second}); err != nil {
		t.Fatalf("ReplaceAll() error = %v", err)
	}
	got, err := store.Get(context.Background(), "2026-01-dup")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Title != "First" {
		t.Fatalf("Get() = %+v, want the first row so the id stays unambiguous", got)
	}
}
