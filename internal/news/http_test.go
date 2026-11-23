package news

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTestMux mounts the news handler on a fresh mux, the same way app.New does.
func newTestMux(svc *Service) *http.ServeMux {
	mux := http.NewServeMux()
	NewHandler(svc, nil).RegisterRoutes(mux)
	return mux
}

func TestNewsEndpointListsNewestFirst(t *testing.T) {
	svc, _ := newTestService(t, fixtureServer(t))
	if _, err := svc.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	mux := newTestMux(svc)

	req := httptest.NewRequest(http.MethodGet, "/news", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	if got := res.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content-type = %q, want application/json", got)
	}
	var result ListResult
	if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(result.Data) != 4 {
		t.Fatalf("len(data) = %d, want 4", len(result.Data))
	}
	if result.Meta.Total != 4 || result.Meta.Page != 1 || result.Meta.PerPage != DefaultPerPage || result.Meta.TotalPages != 1 {
		t.Fatalf("meta = %+v, want one full page of 4", result.Meta)
	}
	entries := result.Data
	if entries[0].Title != "Pintar, Murah, atau Terlihat Kerjanya? — AI Tools Digest #35" {
		t.Fatalf("entries[0].Title = %q, want the newest digest", entries[0].Title)
	}
	if want := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC); !entries[0].PublishedAt.Equal(want) {
		t.Fatalf("entries[0].PublishedAt = %v, want %v", entries[0].PublishedAt, want)
	}
	if entries[0].Group != GroupDigest {
		t.Fatalf("entries[0].Group = %q, want %q", entries[0].Group, GroupDigest)
	}
}

func TestNewsEndpointEmptyStoreAnswersEmptyPage(t *testing.T) {
	mux := newTestMux(NewService(NewMemoryStore()))

	req := httptest.NewRequest(http.MethodGet, "/news", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	var result ListResult
	if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result.Data == nil || len(result.Data) != 0 {
		t.Fatalf("data = %+v, want an empty array", result.Data)
	}
	if result.Meta.Total != 0 {
		t.Fatalf("meta.total = %d, want 0", result.Meta.Total)
	}
}

// newCatalogService builds a service on the full default registry without
// fetching: rows are upserted straight into the store.
func newCatalogService(t *testing.T) *Service {
	t.Helper()
	store := NewMemoryStore()
	base := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	rows := []UpsertInput{
		{Title: "Digest one", URL: "https://a.com/1", Source: "zainfathoni.com", Summary: "s1", PublishedAt: base},
		{Title: "Digest two", URL: "https://a.com/2", Source: "zainfathoni.com", Summary: "s2", PublishedAt: base.Add(-time.Hour)},
		{Title: "Digest three", URL: "https://a.com/3", Source: "TLDR AI", Summary: "s3", PublishedAt: base.Add(-2 * time.Hour)},
		{Title: "Digest four", URL: "https://a.com/4", Source: "TLDR AI", Summary: "s4", PublishedAt: base.Add(-3 * time.Hour)},
		{Title: "OpenAI launches", URL: "https://a.com/5", Source: "OpenAI", Summary: "Lab news", PublishedAt: base.Add(-4 * time.Hour)},
	}
	for _, row := range rows {
		if _, err := store.Upsert(context.Background(), row); err != nil {
			t.Fatalf("Upsert(%q) error = %v", row.URL, err)
		}
	}
	return NewService(store)
}

func TestNewsEndpointFiltersAndPages(t *testing.T) {
	mux := newTestMux(newCatalogService(t))

	cases := []struct {
		path string
		want int
	}{
		{"/news?q=digest", 4},
		{"/news?q=openai", 1},
		{"/news?q=tidak-ada", 0},
		{"/news?source=OpenAI", 1},
		{"/news?source=openai", 1},
		{"/news?group=digest", 4},
		{"/news?group=lab", 1},
		{"/news?group=lab&source=OpenAI", 1},
		{"/news?group=lab&source=TechCrunch", 0},
		{"/news?source=Bukan%20Sumber", 0},
		{"/news?page=2&per_page=3", 2},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200; body = %s", tc.path, res.Code, res.Body.String())
		}
		var result ListResult
		if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
			t.Fatalf("GET %s decode: %v", tc.path, err)
		}
		if len(result.Data) != tc.want {
			t.Fatalf("GET %s len(data) = %d, want %d", tc.path, len(result.Data), tc.want)
		}
	}

	// Page 2 of 3 carries the paging metadata the pager needs.
	req := httptest.NewRequest(http.MethodGet, "/news?page=2&per_page=3", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	var result ListResult
	if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode page 2: %v", err)
	}
	if result.Meta.Total != 5 || result.Meta.TotalPages != 2 || result.Meta.Page != 2 || result.Meta.PerPage != 3 {
		t.Fatalf("meta = %+v, want total 5 over 2 pages", result.Meta)
	}
}

func TestNewsEndpointRejectsBadParams(t *testing.T) {
	mux := newTestMux(NewService(NewMemoryStore()))

	for _, path := range []string{
		"/news?page=abc",
		"/news?per_page=abc",
		"/news?page=-1",
		"/news?per_page=-5",
		"/news?per_page=101",
		"/news?group=tidak-ada",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		if res.Code != http.StatusBadRequest {
			t.Fatalf("GET %s status = %d, want 400; body = %s", path, res.Code, res.Body.String())
		}
	}
}

func TestNewsSourcesEndpointListsRegistryWithCounts(t *testing.T) {
	mux := newTestMux(newCatalogService(t))

	req := httptest.NewRequest(http.MethodGet, "/news/sources", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", res.Code, res.Body.String())
	}
	var sources []SourceInfo
	if err := json.Unmarshal(res.Body.Bytes(), &sources); err != nil {
		t.Fatalf("decode /news/sources: %v", err)
	}
	if len(sources) != len(defaultSources()) {
		t.Fatalf("len(sources) = %d, want %d", len(sources), len(defaultSources()))
	}
	byName := map[string]SourceInfo{}
	for _, info := range sources {
		byName[info.Name] = info
	}
	if got := byName["TLDR AI"]; got.Count != 2 || got.Group != GroupDigest {
		t.Fatalf("TLDR AI = %+v, want 2 digest rows", got)
	}
	if got := byName["zainfathoni.com"]; got.Count != 2 || got.Group != GroupDigest {
		t.Fatalf("zainfathoni.com = %+v, want 2 digest rows", got)
	}
	if got := byName["OpenAI"]; got.Count != 1 || got.Group != GroupLab {
		t.Fatalf("OpenAI = %+v, want 1 lab row", got)
	}
	if got := byName["TechCrunch"]; got.Count != 0 || got.Group != GroupPress {
		t.Fatalf("TechCrunch = %+v, want a registered source with zero rows", got)
	}
}

func TestNewsSourcesEndpointOnEmptyStore(t *testing.T) {
	mux := newTestMux(NewService(NewMemoryStore()))

	req := httptest.NewRequest(http.MethodGet, "/news/sources", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", res.Code, res.Body.String())
	}
	if body := strings.TrimSpace(res.Body.String()); !strings.HasPrefix(body, "[") {
		t.Fatalf("body = %q, want a JSON array", body)
	}
}
