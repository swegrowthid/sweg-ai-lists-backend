package app

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/swegrowthid/sweg-ai-lists-backend/internal/auth"
)

// The vote routes ride the production graph: writes need a token, and with an
// empty snapshot an authenticated vote answers 404 for the unknown tool id.
func TestAppToolsVoteRoutesAreRegistered(t *testing.T) {
	handler := newTestApp(t).Handler()

	res := doJSON(t, handler, http.MethodPost, "/tools/P-001/votes", "", "")
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("POST without token status = %d, want %d; body = %s", res.Code, http.StatusUnauthorized, res.Body.String())
	}
	res = doJSON(t, handler, http.MethodDelete, "/tools/P-001/votes", "", "")
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("DELETE without token status = %d, want %d; body = %s", res.Code, http.StatusUnauthorized, res.Body.String())
	}

	res = doJSON(t, handler, http.MethodPost, "/users/register",
		`{"username":"toolvoter","email":"toolvoter@example.com","password":"password"}`, "")
	if res.Code != http.StatusCreated {
		t.Fatalf("register status = %d, want %d; body = %s", res.Code, http.StatusCreated, res.Body.String())
	}
	res = doJSON(t, handler, http.MethodPost, "/auth/login",
		`{"identifier":"toolvoter","password":"password"}`, "")
	if res.Code != http.StatusOK {
		t.Fatalf("login status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	var pair auth.Pair
	if err := json.Unmarshal(res.Body.Bytes(), &pair); err != nil {
		t.Fatalf("decode login response: %v", err)
	}

	res = doJSON(t, handler, http.MethodPost, "/tools/P-001/votes", "", pair.AccessToken)
	if res.Code != http.StatusNotFound {
		t.Fatalf("POST with token on empty snapshot status = %d, want %d; body = %s",
			res.Code, http.StatusNotFound, res.Body.String())
	}
	res = doJSON(t, handler, http.MethodDelete, "/tools/P-001/votes", "", pair.AccessToken)
	if res.Code != http.StatusNotFound {
		t.Fatalf("DELETE with token on empty snapshot status = %d, want %d; body = %s",
			res.Code, http.StatusNotFound, res.Body.String())
	}
}
