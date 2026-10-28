package httpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The frontend calls PUT /users/password and DELETE endpoints cross-origin.
// A preflight that omits either method makes the browser block the request.
func TestCORSPreflightAllowsWriteMethods(t *testing.T) {
	const origin = "https://ai-sweg.my.id"
	handler := CORS(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("preflight reached the next handler")
	}), []string{origin})

	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		req := httptest.NewRequest(http.MethodOptions, "/users/password", nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", method)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("%s preflight status = %d, want %d", method, rec.Code, http.StatusNoContent)
		}
		allowed := rec.Header().Get("Access-Control-Allow-Methods")
		listed := false
		for _, tok := range strings.Split(allowed, ",") {
			if strings.TrimSpace(tok) == method {
				listed = true
				break
			}
		}
		if !listed {
			t.Fatalf("%s missing from Access-Control-Allow-Methods %q", method, allowed)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != origin {
			t.Fatalf("%s preflight origin = %q, want %q", method, got, origin)
		}
	}
}
