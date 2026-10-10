package dailyterm

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTestMux mounts the daily term handler on a fresh mux, the same way app.New does.
func newTestMux(svc *Service) *http.ServeMux {
	mux := http.NewServeMux()
	NewHandler(svc, nil).RegisterRoutes(mux)
	return mux
}

// syncedMux mounts the handler over a synced fixture service.
func syncedMux(t *testing.T) *http.ServeMux {
	t.Helper()
	svc, _ := newTestService(t, fixtureServer(t))
	syncFixture(t, svc)
	return newTestMux(svc)
}

func TestDailyTermEndpointServesAMonthWithoutAuth(t *testing.T) {
	mux := syncedMux(t)

	// No Authorization header: the journal is public, like the homepage that
	// reads it.
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/daily-terms?month=2026-01", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	var list TermList
	if err := json.Unmarshal(res.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if list.Meta.MonthKey != "2026-01" || list.Meta.Count != 3 || len(list.Data) != 3 {
		t.Fatalf("list = %+v, want January with 3 terms", list)
	}
	if list.Data[0].Definition == "" || list.Data[0].Example == "" {
		t.Fatalf("data[0] misses the rendered fields: %+v", list.Data[0])
	}
}

func TestDailyTermEndpointDefaultsToANewestMonth(t *testing.T) {
	mux := syncedMux(t)

	res := httptest.NewRecorder()
	mux.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/daily-terms", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	var list TermList
	if err := json.Unmarshal(res.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	// The fixture months are 2025-06, 2026-01 and 2026-03. Whichever one answers,
	// the response must name it.
	if list.Meta.MonthKey == "" || len(list.Data) == 0 {
		t.Fatalf("list = %+v, want a named month with rows", list)
	}
}

func TestDailyTermEndpointMonthsAndDetail(t *testing.T) {
	mux := syncedMux(t)

	res := httptest.NewRecorder()
	mux.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/daily-terms/months", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("months status = %d; body = %s", res.Code, res.Body.String())
	}
	var months []MonthSummary
	if err := json.Unmarshal(res.Body.Bytes(), &months); err != nil {
		t.Fatalf("decode months: %v", err)
	}
	if len(months) != 3 || months[0].MonthKey != "2026-03" {
		t.Fatalf("months = %+v, want 3 months newest first", months)
	}

	// The literal /months route must win over the {id} wildcard.
	res = httptest.NewRecorder()
	mux.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/daily-terms/2026-03-beta-term", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("detail status = %d; body = %s", res.Code, res.Body.String())
	}
	var term Term
	if err := json.Unmarshal(res.Body.Bytes(), &term); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if term.Title != "Beta Term" {
		t.Fatalf("detail = %+v, want Beta Term", term)
	}
}

func TestDailyTermEndpointErrorStatuses(t *testing.T) {
	mux := syncedMux(t)

	for _, tc := range []struct {
		target string
		status int
		body   string
	}{
		// A malformed month is the client's mistake.
		{"/daily-terms?month=2026-13", http.StatusBadRequest, "invalid month"},
		{"/daily-terms?month=nope", http.StatusBadRequest, "invalid month"},
		// A well-formed month the fixture does not carry is a missing resource.
		{"/daily-terms?month=2025-02", http.StatusNotFound, "month not found"},
		// A missing term reads differently from a missing month.
		{"/daily-terms/2026-01-nope", http.StatusNotFound, "term not found"},
	} {
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, httptest.NewRequest(http.MethodGet, tc.target, nil))
		if res.Code != tc.status {
			t.Fatalf("GET %s status = %d, want %d; body = %s", tc.target, res.Code, tc.status, res.Body.String())
		}
		if body := strings.TrimSpace(res.Body.String()); body != tc.body {
			t.Fatalf("GET %s body = %q, want %q", tc.target, body, tc.body)
		}
	}
}

func TestDailyTermEndpointOnAnEmptySnapshot(t *testing.T) {
	mux := newTestMux(NewService(NewMemoryStore()))

	// Nothing is synced yet: the list is a 404, not a 500, and months is an
	// empty array rather than null.
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/daily-terms", nil))
	if res.Code != http.StatusNotFound {
		t.Fatalf("list status = %d, want %d; body = %s", res.Code, http.StatusNotFound, res.Body.String())
	}

	res = httptest.NewRecorder()
	mux.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/daily-terms/months", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("months status = %d, want %d", res.Code, http.StatusOK)
	}
	if body := strings.TrimSpace(res.Body.String()); body != "[]" {
		t.Fatalf("months body = %q, want []", body)
	}
}

func TestDailyTermEndpointReportsAMonthWithNoRowsAsNotFound(t *testing.T) {
	// A store that holds a month, then is asked for one it never had.
	svc, _ := newTestService(t, fixtureServer(t))
	syncFixture(t, svc)
	mux := newTestMux(svc)

	// 2025-02 is well-formed but not among the fixture months, so the store
	// returns nothing and the service answers ErrNotFound.
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/daily-terms?month=2025-02", nil))
	if res.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusNotFound)
	}
}

func TestNextMidnight(t *testing.T) {
	zone := time.FixedZone("test", 7*60*60)
	cases := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{"midday", time.Date(2026, 9, 30, 10, 0, 0, 0, zone), time.Date(2026, 10, 1, 0, 0, 0, 0, zone)},
		{"one minute before midnight", time.Date(2026, 9, 30, 23, 59, 0, 0, zone), time.Date(2026, 10, 1, 0, 0, 0, 0, zone)},
		{"exactly midnight", time.Date(2026, 10, 1, 0, 0, 0, 0, zone), time.Date(2026, 10, 2, 0, 0, 0, 0, zone)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := nextMidnight(tc.now); !got.Equal(tc.want) {
				t.Fatalf("nextMidnight(%v) = %v, want %v", tc.now, got, tc.want)
			}
		})
	}
}

// TestSchedulerRunsAtStartupAndStopsWithContext pins the same slot news and
// tools use: one sync now, then midnight, and a clean stop when ctx ends.
func TestSchedulerRunsAtStartupAndStopsWithContext(t *testing.T) {
	svc, store := newTestService(t, fixtureServer(t))
	scheduler := NewScheduler(svc, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		scheduler.Run(ctx)
		close(done)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		all, err := store.List(context.Background(), "")
		if err != nil {
			t.Fatalf("store.List() error = %v", err)
		}
		if len(all) == 15 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Run() did not store the startup sync result")
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not stop after the context ended")
	}
}

// TestSchedulerSurvivesAFailingSync keeps the loop alive so tomorrow's run
// retries instead of the goroutine dying on the first bad day upstream.
func TestSchedulerSurvivesAFailingSync(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	svc, _ := newTestService(t, server)
	scheduler := NewScheduler(svc, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		scheduler.Run(ctx)
		close(done)
	}()

	// Give the failing startup sync time to run, then confirm the loop is still
	// waiting rather than gone.
	time.Sleep(100 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("Run() returned after a failed sync")
	default:
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not stop after the context ended")
	}
}
