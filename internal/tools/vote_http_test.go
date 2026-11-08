package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/swegrowthid/sweg-ai-lists-backend/internal/auth"
)

// newVoteMux mounts the handler the same way app.New does: reads behind
// OptionalAuth, writes behind RequireAuth. Tokens use a test-only secret;
// the user ids the tests vote with are plain strings, never real accounts.
func newVoteMux(t *testing.T, svc *Service) (*http.ServeMux, *auth.Tokens) {
	t.Helper()
	tokens := auth.NewTokens("test-secret-for-tools-vote-tests", time.Hour, time.Hour)
	mw := auth.NewMiddleware(tokens)
	mux := http.NewServeMux()
	NewHandler(svc, nil).RegisterRoutesWithAuth(mux, mw.RequireAuth, mw.OptionalAuth)
	return mux, tokens
}

// bearer issues an access token for the user id and returns the header value.
func bearer(t *testing.T, tokens *auth.Tokens, userID string) string {
	t.Helper()
	pair, _, err := tokens.Issue(userID, "voter")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	return "Bearer " + pair.AccessToken
}

func serveVote(t *testing.T, mux *http.ServeMux, method, target, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	if bearer != "" {
		req.Header.Set("Authorization", bearer)
	}
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	return res
}

func decodeVoteResult(t *testing.T, res *httptest.ResponseRecorder) VoteResult {
	t.Helper()
	var got VoteResult
	if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode vote result: %v; body = %s", err, res.Body.String())
	}
	return got
}

// POST without a token answers 401; with a token the first vote answers 201
// and a repeat answers 200. Both DELETEs answer 200 with the latest count.
func TestToolsVoteEndpointsAuthAndIdempotency(t *testing.T) {
	svc := seededVoteService(t)
	mux, tokens := newVoteMux(t, svc)
	token := bearer(t, tokens, "user-1")

	res := serveVote(t, mux, http.MethodPost, "/tools/P-001/votes", "")
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("POST without token status = %d, want %d", res.Code, http.StatusUnauthorized)
	}
	res = serveVote(t, mux, http.MethodDelete, "/tools/P-001/votes", "")
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("DELETE without token status = %d, want %d", res.Code, http.StatusUnauthorized)
	}

	res = serveVote(t, mux, http.MethodPost, "/tools/P-001/votes", token)
	if res.Code != http.StatusCreated {
		t.Fatalf("first POST status = %d, want %d; body = %s", res.Code, http.StatusCreated, res.Body.String())
	}
	if got := decodeVoteResult(t, res); got != (VoteResult{ToolID: "P-001", Votes: 1, Voted: true}) {
		t.Fatalf("first POST body = %+v, want {P-001 1 true}", got)
	}

	res = serveVote(t, mux, http.MethodPost, "/tools/P-001/votes", token)
	if res.Code != http.StatusOK {
		t.Fatalf("second POST status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	if got := decodeVoteResult(t, res); got != (VoteResult{ToolID: "P-001", Votes: 1, Voted: true}) {
		t.Fatalf("second POST body = %+v, want {P-001 1 true}", got)
	}

	res = serveVote(t, mux, http.MethodDelete, "/tools/P-001/votes", token)
	if res.Code != http.StatusOK {
		t.Fatalf("first DELETE status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	if got := decodeVoteResult(t, res); got != (VoteResult{ToolID: "P-001", Votes: 0, Voted: false}) {
		t.Fatalf("first DELETE body = %+v, want {P-001 0 false}", got)
	}

	res = serveVote(t, mux, http.MethodDelete, "/tools/P-001/votes", token)
	if res.Code != http.StatusOK {
		t.Fatalf("second DELETE status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	if got := decodeVoteResult(t, res); got != (VoteResult{ToolID: "P-001", Votes: 0, Voted: false}) {
		t.Fatalf("second DELETE body = %+v, want {P-001 0 false}", got)
	}
}

// Voting for an unknown id answers 404 on both write endpoints.
func TestToolsVoteEndpointsUnknownTool(t *testing.T) {
	svc := seededVoteService(t)
	mux, tokens := newVoteMux(t, svc)
	token := bearer(t, tokens, "user-1")

	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		res := serveVote(t, mux, method, "/tools/XX-999/votes", token)
		if res.Code != http.StatusNotFound {
			t.Fatalf("%s unknown status = %d, want %d; body = %s", method, res.Code, http.StatusNotFound, res.Body.String())
		}
	}
}

// GET carries the count for everyone and voted only for the logged-in viewer.
func TestToolsVoteEndpointsGetCarriesVoted(t *testing.T) {
	svc := seededVoteService(t)
	mux, tokens := newVoteMux(t, svc)
	token := bearer(t, tokens, "user-1")

	if res := serveVote(t, mux, http.MethodPost, "/tools/P-001/votes", token); res.Code != http.StatusCreated {
		t.Fatalf("POST status = %d, want %d; body = %s", res.Code, http.StatusCreated, res.Body.String())
	}

	res := serveVote(t, mux, http.MethodGet, "/tools/P-001", token)
	if res.Code != http.StatusOK {
		t.Fatalf("GET with token status = %d; body = %s", res.Code, res.Body.String())
	}
	var authed Tool
	if err := json.Unmarshal(res.Body.Bytes(), &authed); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if authed.Votes != 1 || !authed.Voted {
		t.Fatalf("GET with token = %+v, want {Votes:1 Voted:true}", authed)
	}

	res = serveVote(t, mux, http.MethodGet, "/tools/P-001", "")
	if res.Code != http.StatusOK {
		t.Fatalf("GET anon status = %d; body = %s", res.Code, res.Body.String())
	}
	var anon Tool
	if err := json.Unmarshal(res.Body.Bytes(), &anon); err != nil {
		t.Fatalf("decode anon detail: %v", err)
	}
	if anon.Votes != 1 || anon.Voted {
		t.Fatalf("GET anon = %+v, want {Votes:1 Voted:false}", anon)
	}

	for _, tc := range []struct {
		bearer    string
		wantVoted bool
		name      string
	}{
		{token, true, "authed list"},
		{"", false, "anon list"},
	} {
		res := serveVote(t, mux, http.MethodGet, "/tools", tc.bearer)
		if res.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d; body = %s", tc.name, res.Code, res.Body.String())
		}
		var page ListResult
		if err := json.Unmarshal(res.Body.Bytes(), &page); err != nil {
			t.Fatalf("decode %s: %v", tc.name, err)
		}
		found := false
		for _, row := range page.Data {
			if row.ID != "P-001" {
				continue
			}
			found = true
			if row.Votes != 1 || row.Voted != tc.wantVoted {
				t.Fatalf("GET %s P-001 = %+v, want {Votes:1 Voted:%v}", tc.name, row, tc.wantVoted)
			}
		}
		if !found {
			t.Fatalf("GET %s misses P-001", tc.name)
		}
	}
}

// Two concurrent POSTs for one pair keep one row: exactly one answers 201 and
// the count stays 1. Each goroutine owns its recorder; only the mux is shared.
func TestToolsVoteEndpointsConcurrentPosts(t *testing.T) {
	svc := seededVoteService(t)
	mux, tokens := newVoteMux(t, svc)
	token := bearer(t, tokens, "user-1")

	const writers = 2
	codes := make([]int, writers)
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i] = serveVote(t, mux, http.MethodPost, "/tools/P-001/votes", token).Code
		}(i)
	}
	wg.Wait()

	created := 0
	for _, code := range codes {
		switch code {
		case http.StatusCreated:
			created++
		case http.StatusOK:
		default:
			t.Fatalf("POST status = %d, want 201 or 200", code)
		}
	}
	if created != 1 {
		t.Fatalf("201 count = %d, want exactly 1 (codes %v)", created, codes)
	}

	got, err := svc.GetFor(context.Background(), "P-001", "user-1")
	if err != nil {
		t.Fatalf("GetFor() error = %v", err)
	}
	if got.Votes != 1 || !got.Voted {
		t.Fatalf("GetFor() = %+v, want {Votes:1 Voted:true}", got)
	}
}
