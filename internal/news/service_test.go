package news

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fixtureServer serves the trimmed real listing page, so the whole fetch path
// runs: request, body read, parse, filter, upsert.
func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	page := fixtureHTML(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(page)
	}))
	t.Cleanup(server.Close)
	return server
}

// newTestService points the service at one test server and a memory store.
func newTestService(t *testing.T, server *httptest.Server) (*Service, *MemoryStore) {
	t.Helper()
	store := NewMemoryStore()
	svc := newServiceWithSource(store, source{listingURL: server.URL + "/blog", client: server.Client()})
	return svc, store
}

// listEntries reads the store, failing the test on any error.
func listEntries(t *testing.T, store *MemoryStore) []Entry {
	t.Helper()
	entries, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	return entries
}

func TestNewServiceFillsSourceDefaults(t *testing.T) {
	svc := NewService(NewMemoryStore())
	if svc.source.listingURL != defaultListingURL {
		t.Fatalf("listingURL = %q, want %q", svc.source.listingURL, defaultListingURL)
	}
	if svc.source.filter != defaultFilter {
		t.Fatalf("filter = %q, want %q", svc.source.filter, defaultFilter)
	}
	if svc.source.client == nil || svc.source.client.Timeout != fetchTimeout {
		t.Fatal("client = nil or missing the fetch timeout")
	}
}

func TestSyncStoresFilteredEntriesNewestFirst(t *testing.T) {
	svc, store := newTestService(t, fixtureServer(t))

	stored, err := svc.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if stored != 4 {
		t.Fatalf("Sync() stored = %d, want 4", stored)
	}

	entries := listEntries(t, store)
	if len(entries) != 4 {
		t.Fatalf("len(entries) = %d, want 4", len(entries))
	}
	if entries[0].Title != "Pintar, Murah, atau Terlihat Kerjanya? — AI Tools Digest #35" {
		t.Fatalf("entries[0].Title = %q, want the newest digest", entries[0].Title)
	}
	if want := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC); !entries[0].PublishedAt.Equal(want) {
		t.Fatalf("entries[0].PublishedAt = %v, want %v", entries[0].PublishedAt, want)
	}
	if entries[3].Title != "AI Tools Digest: Week 2 - Model Tier Wars & The Great Price Negotiation" {
		t.Fatalf("entries[3].Title = %q, want the oldest digest", entries[3].Title)
	}
	for _, entry := range entries {
		if entry.ID == "" || entry.CreatedAt.IsZero() || entry.UpdatedAt.IsZero() {
			t.Fatalf("entry %q misses id or timestamps: %+v", entry.URL, entry)
		}
	}
}

func TestSyncTwiceKeepsOneCopyPerURL(t *testing.T) {
	svc, store := newTestService(t, fixtureServer(t))
	ctx := context.Background()

	if _, err := svc.Sync(ctx); err != nil {
		t.Fatalf("first Sync() error = %v", err)
	}
	first := listEntries(t, store)
	// Let the clock move, so an accidental updated_at bump would show.
	time.Sleep(2 * time.Millisecond)

	if _, err := svc.Sync(ctx); err != nil {
		t.Fatalf("second Sync() error = %v", err)
	}
	second := listEntries(t, store)

	if len(second) != len(first) {
		t.Fatalf("len(entries) = %d after the second sync, want %d", len(second), len(first))
	}
	for index := range first {
		if second[index].ID != first[index].ID {
			t.Fatalf("entries[%d].ID = %q, want %q", index, second[index].ID, first[index].ID)
		}
		if !second[index].CreatedAt.Equal(first[index].CreatedAt) {
			t.Fatalf("entries[%d].CreatedAt moved on an unchanged sync", index)
		}
		if !second[index].UpdatedAt.Equal(first[index].UpdatedAt) {
			t.Fatalf("entries[%d].UpdatedAt moved on an unchanged sync", index)
		}
	}
}

// switchPage serves HTML that a test swaps between requests.
type switchPage struct {
	mu   sync.Mutex
	body string
}

func (p *switchPage) set(body string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.body = body
}

func (p *switchPage) get() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.body
}

// newSwitchService serves a page a test can edit between syncs.
func newSwitchService(t *testing.T, body string) (*Service, *MemoryStore, *switchPage) {
	t.Helper()
	page := &switchPage{body: body}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, page.get())
	}))
	t.Cleanup(server.Close)
	store := NewMemoryStore()
	svc := newServiceWithSource(store, source{listingURL: server.URL + "/blog", client: server.Client()})
	return svc, store, page
}

func TestSyncUpdatesChangedFieldsOnly(t *testing.T) {
	svc, store, page := newSwitchService(t, string(fixtureHTML(t)))
	ctx := context.Background()

	if _, err := svc.Sync(ctx); err != nil {
		t.Fatalf("first Sync() error = %v", err)
	}
	before := listEntries(t, store)

	// The source edits one card: that row changes, the rest stay put.
	edited := strings.Replace(string(fixtureHTML(t)), "AI Tools Digest #35", "AI Tools Digest #35 (revisi)", 1)
	page.set(edited)
	time.Sleep(2 * time.Millisecond)

	if _, err := svc.Sync(ctx); err != nil {
		t.Fatalf("second Sync() error = %v", err)
	}
	after := listEntries(t, store)

	if len(after) != len(before) {
		t.Fatalf("len(entries) = %d after the edit, want %d", len(after), len(before))
	}
	for index := range before {
		if after[index].URL != before[index].URL {
			t.Fatalf("entries[%d].URL = %q, want %q", index, after[index].URL, before[index].URL)
		}
		if strings.HasSuffix(after[index].URL, "/blog/ai-tools-swe-growth-sep-20-sep-27-2026") {
			if after[index].Title != "Pintar, Murah, atau Terlihat Kerjanya? — AI Tools Digest #35 (revisi)" {
				t.Fatalf("edited title = %q, want the revised title", after[index].Title)
			}
			if !after[index].CreatedAt.Equal(before[index].CreatedAt) {
				t.Fatal("edited entry lost its created_at")
			}
			if !after[index].UpdatedAt.After(before[index].UpdatedAt) {
				t.Fatal("edited entry kept its old updated_at")
			}
			continue
		}
		if !after[index].UpdatedAt.Equal(before[index].UpdatedAt) {
			t.Fatalf("untouched entry %q moved updated_at", after[index].URL)
		}
	}
}

func TestSyncFailsWhenSourceIsDown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	svc, store := newTestService(t, server)

	if _, err := svc.Sync(context.Background()); err == nil {
		t.Fatal("Sync() error = nil, want a failure")
	}
	if entries := listEntries(t, store); len(entries) != 0 {
		t.Fatalf("store kept %d entries after a failed sync", len(entries))
	}
}
