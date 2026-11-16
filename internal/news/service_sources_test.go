package news

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
)

// twoFeedServer serves an RSS feed at /rss.xml and an Atom feed at /atom.xml.
func twoFeedServer(t *testing.T, rssBody, atomBody string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /rss.xml", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(rssBody))
	})
	mux.HandleFunc("GET /atom.xml", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(atomBody))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func TestSyncStoresEverySourceWithItsName(t *testing.T) {
	rss := rssFixture(`<item><title>RSS post</title><link>/rss-post</link>` + pubDate + `<description>From RSS</description></item>`)
	atom := atomFixture(`<entry><title>Atom post</title><link href="/atom-post"/><updated>2026-01-05T00:00:00Z</updated><summary>From Atom</summary></entry>`)
	server := twoFeedServer(t, rss, atom)

	store := NewMemoryStore()
	svc := newServiceWithSource(store, []source{
		{name: "Test RSS", kind: kindFeed, listingURL: server.URL + "/rss.xml", client: server.Client()},
		{name: "Test Atom", kind: kindFeed, listingURL: server.URL + "/atom.xml", client: server.Client()},
	})

	stored, err := svc.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if stored != 2 {
		t.Fatalf("Sync() stored = %d, want 2", stored)
	}

	entries := listEntries(t, store)
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	bySource := map[string]string{}
	for _, entry := range entries {
		bySource[entry.Source] = entry.Title
	}
	if bySource["Test RSS"] != "RSS post" || bySource["Test Atom"] != "Atom post" {
		t.Fatalf("bySource = %v, want one row per source name", bySource)
	}
}

func TestSyncIsolatesAFailingSource(t *testing.T) {
	rss := rssFixture(`<item><title>RSS post</title><link>/rss-post</link>` + pubDate + `<description>From RSS</description></item>`)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /rss.xml", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(rss))
	})
	mux.HandleFunc("GET /atom.xml", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	store := NewMemoryStore()
	svc := newServiceWithSource(store, []source{
		{name: "Test RSS", kind: kindFeed, listingURL: server.URL + "/rss.xml", client: server.Client()},
		{name: "Test Broken", kind: kindFeed, listingURL: server.URL + "/atom.xml", client: server.Client()},
	})

	stored, err := svc.Sync(context.Background())
	if err == nil {
		t.Fatal("Sync() error = nil, want the joined failure")
	}
	if !strings.Contains(err.Error(), "news: source Test Broken:") {
		t.Fatalf("Sync() error %q does not name the broken source", err)
	}
	if stored != 1 {
		t.Fatalf("Sync() stored = %d, want 1", stored)
	}
	entries := listEntries(t, store)
	if len(entries) != 1 || entries[0].Source != "Test RSS" {
		t.Fatalf("entries = %+v, want the healthy source fully stored", entries)
	}
}

func TestSyncIsolatesAnUnparseableSource(t *testing.T) {
	server := twoFeedServer(t, `<html>not a feed</html>`, `<html>not a feed</html>`)
	store := NewMemoryStore()
	svc := newServiceWithSource(store, []source{
		{name: "Broken A", kind: kindFeed, listingURL: server.URL + "/rss.xml", client: server.Client()},
		{name: "Broken B", kind: kindFeed, listingURL: server.URL + "/atom.xml", client: server.Client()},
	})

	stored, err := svc.Sync(context.Background())
	if err == nil {
		t.Fatal("Sync() error = nil, want the joined failure")
	}
	if !strings.Contains(err.Error(), "Broken A") || !strings.Contains(err.Error(), "Broken B") {
		t.Fatalf("Sync() error %q does not name both sources", err)
	}
	if stored != 0 {
		t.Fatalf("Sync() stored = %d, want 0", stored)
	}
	if entries := listEntries(t, store); len(entries) != 0 {
		t.Fatalf("store kept %d entries", len(entries))
	}
	if joined := errors.Join(nil); joined != nil {
		t.Fatal("errors.Join(nil) is not nil")
	}
}

func TestSyncCapsAFeedToItsNewestWindow(t *testing.T) {
	items := make([]string, 0, 120)
	for index := 0; index < 120; index++ {
		when := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, index)
		id := strconv.Itoa(index)
		items = append(items, `<item><title>post `+id+`</title><link>/post-`+id+`</link><pubDate>`+when.Format(time.RFC1123Z)+`</pubDate></item>`)
	}
	// The empty Atom feed parses to zero entries, so the count below is the
	// RSS feed's capped contribution alone.
	server := twoFeedServer(t, rssFixture(items...), atomFixture())

	store := NewMemoryStore()
	svc := newServiceWithSource(store, []source{
		{name: "Test RSS", kind: kindFeed, listingURL: server.URL + "/rss.xml", client: server.Client()},
		{name: "Test Atom", kind: kindFeed, listingURL: server.URL + "/atom.xml", client: server.Client()},
	})

	stored, err := svc.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if stored != maxFeedItems {
		t.Fatalf("Sync() stored = %d, want the cap %d", stored, maxFeedItems)
	}

	entries := listEntries(t, store)
	if len(entries) != maxFeedItems {
		t.Fatalf("len(entries) = %d, want the cap %d", len(entries), maxFeedItems)
	}
	// List is newest-first: the window kept posts 20..119.
	if entries[0].Title != "post 119" {
		t.Fatalf("entries[0] = %q, want the newest item post 119", entries[0].Title)
	}
	if entries[maxFeedItems-1].Title != "post 20" {
		t.Fatalf("entries[%d] = %q, want the oldest kept item post 20", maxFeedItems-1, entries[maxFeedItems-1].Title)
	}
	for _, entry := range entries {
		if entry.Title == "post 0" {
			t.Fatal("the oldest item survived the cap")
		}
	}
}

func TestNewsJSONCarriesSource(t *testing.T) {
	rss := rssFixture(`<item><title>RSS post</title><link>/rss-post</link>` + pubDate + `<description>From RSS</description></item>`)
	atom := atomFixture(`<entry><title>Atom post</title><link href="/atom-post"/><updated>2026-01-05T00:00:00Z</updated><summary>From Atom</summary></entry>`)
	server := twoFeedServer(t, rss, atom)

	store := NewMemoryStore()
	svc := newServiceWithSource(store, []source{
		{name: "Test RSS", kind: kindFeed, listingURL: server.URL + "/rss.xml", client: server.Client()},
		{name: "Test Atom", kind: kindFeed, listingURL: server.URL + "/atom.xml", client: server.Client()},
	})
	if _, err := svc.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	recorder := httptest.NewRecorder()
	NewHandler(svc, nil).list(recorder, httptest.NewRequest(http.MethodGet, "/news", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /news status = %d, want 200", recorder.Code)
	}

	var rows []map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode /news: %v", err)
	}
	sources := map[string]bool{}
	for _, row := range rows {
		name, _ := row["source"].(string)
		sources[name] = true
	}
	if !sources["Test RSS"] || !sources["Test Atom"] {
		t.Fatalf("rows carry %v, want both source names", sources)
	}
}
