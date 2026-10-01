package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestMux mounts the tools handler on a fresh mux, the same way app.New does.
func newTestMux(svc *Service) *http.ServeMux {
	mux := http.NewServeMux()
	NewHandler(svc, nil).RegisterRoutes(mux)
	return mux
}

func TestToolsEndpointListsTheCatalog(t *testing.T) {
	svc, _ := newTestService(t, fixtureServer(t))
	if _, err := svc.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	mux := newTestMux(svc)

	res := httptest.NewRecorder()
	mux.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/tools", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	var list []Tool
	if err := json.Unmarshal(res.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(list) != 10 {
		t.Fatalf("len(list) = %d, want 10", len(list))
	}
}

func TestToolsEndpointFiltersByCategoryAndQuery(t *testing.T) {
	svc, _ := newTestService(t, fixtureServer(t))
	if _, err := svc.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	mux := newTestMux(svc)

	res := httptest.NewRecorder()
	mux.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/tools?category=coding-agents", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("category status = %d; body = %s", res.Code, res.Body.String())
	}
	var agents []Tool
	if err := json.Unmarshal(res.Body.Bytes(), &agents); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(agents) != 3 {
		t.Fatalf("len(agents) = %d, want 3", len(agents))
	}

	res = httptest.NewRecorder()
	mux.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/tools?q=warp", nil))
	var found []Tool
	if err := json.Unmarshal(res.Body.Bytes(), &found); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(found) != 1 || found[0].ID != "ADE-012" {
		t.Fatalf("q=warp = %+v, want only ADE-012", found)
	}

	res = httptest.NewRecorder()
	mux.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/tools?category=nope", nil))
	if res.Code != http.StatusBadRequest {
		t.Fatalf("unknown category status = %d, want %d", res.Code, http.StatusBadRequest)
	}
	if body := strings.TrimSpace(res.Body.String()); body != "unknown category" {
		t.Fatalf("unknown category body = %q", body)
	}

	res = httptest.NewRecorder()
	mux.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/tools?q=a%00b", nil))
	if res.Code != http.StatusBadRequest {
		t.Fatalf("NUL query status = %d, want %d", res.Code, http.StatusBadRequest)
	}
}

func TestToolsEndpointServesCategoriesAndDetail(t *testing.T) {
	svc, _ := newTestService(t, fixtureServer(t))
	if _, err := svc.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	mux := newTestMux(svc)

	res := httptest.NewRecorder()
	mux.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/tools/categories", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("categories status = %d; body = %s", res.Code, res.Body.String())
	}
	var cats []Category
	if err := json.Unmarshal(res.Body.Bytes(), &cats); err != nil {
		t.Fatalf("decode categories: %v", err)
	}
	if len(cats) != 3 || cats[0].Slug != "providers" || cats[0].Prefix != "P" || cats[0].Count != 4 {
		t.Fatalf("categories = %+v, want providers/P/4 first", cats)
	}

	// The literal route wins over the {id} wildcard.
	res = httptest.NewRecorder()
	mux.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/tools/CA-018", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("detail status = %d; body = %s", res.Code, res.Body.String())
	}
	var tool Tool
	if err := json.Unmarshal(res.Body.Bytes(), &tool); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if tool.Name != "Devin" || tool.Category != "coding-agents" {
		t.Fatalf("detail = %+v, want Devin in coding-agents", tool)
	}

	res = httptest.NewRecorder()
	mux.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/tools/XX-999", nil))
	if res.Code != http.StatusNotFound {
		t.Fatalf("unknown id status = %d, want %d", res.Code, http.StatusNotFound)
	}
}

func TestToolsEndpointEmptyStoreAnswersEmptyArray(t *testing.T) {
	mux := newTestMux(NewService(NewMemoryStore()))

	res := httptest.NewRecorder()
	mux.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/tools", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	if body := strings.TrimSpace(res.Body.String()); body != "[]" {
		t.Fatalf("body = %q, want an empty array", body)
	}

	res = httptest.NewRecorder()
	mux.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/tools/categories", nil))
	var cats []Category
	if err := json.Unmarshal(res.Body.Bytes(), &cats); err != nil {
		t.Fatalf("decode categories: %v", err)
	}
	// The mapping is static: it answers even before the first sync, count 0.
	if len(cats) != 3 || cats[0].Count != 0 {
		t.Fatalf("categories before sync = %+v, want 3 rows with count 0", cats)
	}
}
