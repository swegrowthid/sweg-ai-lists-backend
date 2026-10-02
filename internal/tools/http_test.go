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
	var list ListResult
	if err := json.Unmarshal(res.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(list.Data) != 10 || list.Meta.Total != 10 {
		t.Fatalf("list = %+v, want the whole catalog of 10", list)
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
	var agents ListResult
	if err := json.Unmarshal(res.Body.Bytes(), &agents); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(agents.Data) != 3 || agents.Meta.Total != 3 {
		t.Fatalf("agents = %+v, want the 3 coding agents", agents)
	}

	res = httptest.NewRecorder()
	mux.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/tools?q=warp", nil))
	var found ListResult
	if err := json.Unmarshal(res.Body.Bytes(), &found); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(found.Data) != 1 || found.Data[0].ID != "ADE-012" {
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

func TestToolsEndpointEmptyStoreAnswersEmptyPage(t *testing.T) {
	mux := newTestMux(NewService(NewMemoryStore()))

	res := httptest.NewRecorder()
	mux.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/tools", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	var page ListResult
	if err := json.Unmarshal(res.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	// The envelope survives an empty catalog: data is [] and never null, and
	// the meta block still reports the page size the server applied.
	if page.Data == nil {
		t.Fatal("data = nil, want an empty non-nil slice")
	}
	if page.Meta != (PageMeta{Page: 1, PerPage: DefaultPerPage}) {
		t.Fatalf("meta = %+v, want page 1 of %d with no rows", page.Meta, DefaultPerPage)
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

// TestToolsEndpointSortsGroupsAndPages drives the whole query surface over HTTP:
// the group vocabulary, both sort keys, the page window, and the meta block that
// tells the client how much is left.
func TestToolsEndpointSortsGroupsAndPages(t *testing.T) {
	svc, _ := newTestService(t, fixtureServer(t))
	if _, err := svc.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	mux := newTestMux(svc)

	get := func(target string) ListResult {
		t.Helper()
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, httptest.NewRequest(http.MethodGet, target, nil))
		if res.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d; body = %s", target, res.Code, res.Body.String())
		}
		var page ListResult
		if err := json.Unmarshal(res.Body.Bytes(), &page); err != nil {
			t.Fatalf("GET %s decode: %v", target, err)
		}
		return page
	}

	// The group names the UI sends, next to the older category spelling.
	for _, tc := range []struct {
		target, want string
		total        int
	}{
		{"/tools?group=all&sort=name", "ADE-001,CA-018,CA-001,P-005,P-153,CA-054,P-026,P-001,ADE-012,ADE-048", 10},
		{"/tools?group=provider&sort=name", "P-005,P-153,P-026,P-001", 4},
		{"/tools?group=coding-agent&sort=name", "CA-018,CA-001,CA-054", 3},
		{"/tools?group=ade&sort=name", "ADE-001,ADE-012,ADE-048", 3},
		{"/tools?category=ca&sort=name", "CA-018,CA-001,CA-054", 3},
	} {
		got := get(tc.target)
		if ids := idsCSV(got.Data); ids != tc.want {
			t.Fatalf("GET %s ids = %s, want %s", tc.target, ids, tc.want)
		}
		if got.Meta.Total != tc.total {
			t.Fatalf("GET %s total = %d, want %d", tc.target, got.Meta.Total, tc.total)
		}
	}

	first := get("/tools?sort=name&per_page=4&page=1")
	if ids := idsCSV(first.Data); ids != "ADE-001,CA-018,CA-001,P-005" {
		t.Fatalf("page 1 ids = %s, want the first 4 by name", ids)
	}
	if first.Meta != (PageMeta{Page: 1, PerPage: 4, Total: 10, TotalPages: 3}) {
		t.Fatalf("page 1 meta = %+v, want 1/4/10/3", first.Meta)
	}

	past := get("/tools?sort=name&per_page=4&page=99")
	if len(past.Data) != 0 {
		t.Fatalf("page 99 ids = %s, want no rows", idsCSV(past.Data))
	}
	if past.Meta != (PageMeta{Page: 99, PerPage: 4, Total: 10, TotalPages: 3}) {
		t.Fatalf("page 99 meta = %+v, want the real total on an empty page", past.Meta)
	}

	// Time added, newest first.
	newest := get("/tools?sort=updated&order=desc&per_page=3")
	if ids := idsCSV(newest.Data); ids != "ADE-048,CA-054,P-153" {
		t.Fatalf("newest first ids = %s, want ADE-048,CA-054,P-153", ids)
	}
}

// TestToolsEndpointRejectsBadListParams pins the 400s the list endpoint returns
// instead of silently ignoring a parameter it cannot honour.
func TestToolsEndpointRejectsBadListParams(t *testing.T) {
	svc, _ := newTestService(t, fixtureServer(t))
	if _, err := svc.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	mux := newTestMux(svc)

	for _, target := range []string{
		"/tools?group=nope",
		"/tools?sort=price",
		"/tools?sort=name&order=sideways",
		"/tools?page=0",
		"/tools?page=abc",
		"/tools?per_page=101",
		"/tools?per_page=-1",
	} {
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, httptest.NewRequest(http.MethodGet, target, nil))
		if res.Code != http.StatusBadRequest {
			t.Fatalf("GET %s status = %d, want %d; body = %s", target, res.Code, http.StatusBadRequest, res.Body.String())
		}
	}
}
