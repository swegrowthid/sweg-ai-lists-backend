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
	var entries []Entry
	if err := json.Unmarshal(res.Body.Bytes(), &entries); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(entries) != 4 {
		t.Fatalf("len(entries) = %d, want 4", len(entries))
	}
	if entries[0].Title != "Pintar, Murah, atau Terlihat Kerjanya? — AI Tools Digest #35" {
		t.Fatalf("entries[0].Title = %q, want the newest digest", entries[0].Title)
	}
	if want := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC); !entries[0].PublishedAt.Equal(want) {
		t.Fatalf("entries[0].PublishedAt = %v, want %v", entries[0].PublishedAt, want)
	}
}

func TestNewsEndpointEmptyStoreAnswersEmptyArray(t *testing.T) {
	mux := newTestMux(NewService(NewMemoryStore()))

	req := httptest.NewRequest(http.MethodGet, "/news", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	if body := strings.TrimSpace(res.Body.String()); body != "[]" {
		t.Fatalf("body = %q, want an empty array", body)
	}
}
