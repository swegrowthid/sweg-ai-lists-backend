package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/swegrowthid/sweg-ai-lists-backend/internal/user"
)

const testSupabaseKey = "sb_publishable_test"

// fakeSupabase answers GET /auth/v1/user from a token -> account table.
func fakeSupabase(t *testing.T, accounts map[string]supabaseAccount) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/v1/user" || r.Header.Get("apikey") != testSupabaseKey {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		account, ok := accounts[r.Header.Get("Authorization")]
		if !ok {
			http.Error(w, `{"msg":"invalid JWT"}`, http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(account)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newSupabaseMux(t *testing.T, store *user.MemoryStore, accounts map[string]supabaseAccount) *http.ServeMux {
	t.Helper()
	userSvc := user.NewService(store)
	tokens := NewTokens(testSecret, 24*time.Hour, 24*time.Hour)
	refresh := NewMemoryRefreshStore()
	mux := http.NewServeMux()
	userHandler := user.NewHandler(userSvc, nil)
	userHandler.SetAuth(
		func(ctx context.Context) (string, bool) {
			principal, ok := PrincipalFrom(ctx)
			if !ok {
				return "", false
			}
			return principal.UserID, true
		},
		refresh.RevokeAllForUser,
	)
	userHandler.RegisterRoutes(mux, NewMiddleware(tokens).RequireAuth)
	h := NewHandler(NewService(userSvc, tokens, refresh), nil)
	h.SetSupabase(NewSupabase(fakeSupabase(t, accounts).URL, testSupabaseKey, userSvc, tokens, refresh))
	h.RegisterRoutes(mux)
	return mux
}

func exchange(t *testing.T, mux *http.ServeMux, token string) *httptest.ResponseRecorder {
	t.Helper()
	return doJSON(t, mux, http.MethodPost, "/auth/supabase", `{"access_token":"`+token+`"}`, "")
}

func subjectOf(t *testing.T, res *httptest.ResponseRecorder) Claims {
	t.Helper()
	var pair Pair
	if err := json.Unmarshal(res.Body.Bytes(), &pair); err != nil {
		t.Fatalf("decode pair: %v", err)
	}
	claims, err := NewTokens(testSecret, time.Hour, time.Hour).ParseAccess(pair.AccessToken)
	if err != nil {
		t.Fatalf("parse access token: %v", err)
	}
	return claims
}

const (
	sidBudi  = "11111111-1111-1111-1111-111111111111"
	sidOther = "22222222-2222-2222-2222-222222222222"
)

func confirmed(id, email string) supabaseAccount {
	return supabaseAccount{ID: id, Email: email, EmailConfirmedAt: "2026-11-01T00:00:00Z"}
}

func TestSupabaseCreatesUserThenFindsTheSameOne(t *testing.T) {
	store := user.NewMemoryStore()
	mux := newSupabaseMux(t, store, map[string]supabaseAccount{
		"Bearer t1": confirmed(sidBudi, "Budi.Santoso@Example.com"),
	})

	first := exchange(t, mux, "t1")
	if first.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", first.Code, first.Body.String())
	}
	claims := subjectOf(t, first)
	if claims.Username != "budi.santoso" {
		t.Fatalf("username = %q, want budi.santoso", claims.Username)
	}

	again := subjectOf(t, exchange(t, mux, "t1"))
	if again.Subject != claims.Subject {
		t.Fatalf("second sign-in user = %s, want %s", again.Subject, claims.Subject)
	}

	// No password was set, so password login must not work for this account.
	res := doJSON(t, mux, http.MethodPost, "/auth/login", `{"identifier":"budi.santoso","password":""}`, "")
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("password login status = %d, want 401", res.Code)
	}
}

func TestSupabaseLinksExistingAccountByEmail(t *testing.T) {
	store := user.NewMemoryStore()
	mux := newSupabaseMux(t, store, map[string]supabaseAccount{
		"Bearer t1": confirmed(sidBudi, "budi@example.com"),
	})
	res := doJSON(t, mux, http.MethodPost, "/users/register",
		`{"username":"budi","email":"budi@example.com","password":"password"}`, "")
	if res.Code != http.StatusCreated {
		t.Fatalf("register status = %d", res.Code)
	}
	existing, err := store.FindByEmail(context.Background(), "budi@example.com")
	if err != nil {
		t.Fatal(err)
	}

	claims := subjectOf(t, exchange(t, mux, "t1"))
	if claims.Subject != existing.ID || claims.Username != "budi" {
		t.Fatalf("signed in as %s/%s, want the existing %s/budi", claims.Subject, claims.Username, existing.ID)
	}
	// The password keeps working after linking.
	if pair := login(t, mux, "budi", "password"); pair.AccessToken == "" {
		t.Fatal("password login stopped working after linking")
	}
}

func TestSupabaseExchangeMarksTheProfile(t *testing.T) {
	store := user.NewMemoryStore()
	mux := newSupabaseMux(t, store, map[string]supabaseAccount{
		"Bearer t1": confirmed(sidBudi, "budi@example.com"),
	})

	if res := exchange(t, mux, "t1"); res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", res.Code, res.Body.String())
	}

	profiles, err := store.ListProfiles(context.Background())
	if err != nil {
		t.Fatalf("ListProfiles() error = %v", err)
	}
	if len(profiles) != 1 {
		t.Fatalf("profiles = %d, want 1", len(profiles))
	}
	if len(profiles[0].Providers) != 1 || profiles[0].Providers[0] != user.ProviderSwegrowth {
		t.Fatalf("providers = %v, want [swegrowth]", profiles[0].Providers)
	}
}

func TestSupabaseRefusesEmailLinkedToAnotherAccount(t *testing.T) {
	store := user.NewMemoryStore()
	mux := newSupabaseMux(t, store, map[string]supabaseAccount{
		"Bearer t1": confirmed(sidBudi, "budi@example.com"),
		"Bearer t2": confirmed(sidOther, "budi@example.com"),
	})
	if res := exchange(t, mux, "t1"); res.Code != http.StatusOK {
		t.Fatalf("first status = %d", res.Code)
	}
	res := exchange(t, mux, "t2")
	if res.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body = %s", res.Code, res.Body.String())
	}
}

func TestSupabaseRejectsUnconfirmedEmailAndBadTokens(t *testing.T) {
	store := user.NewMemoryStore()
	mux := newSupabaseMux(t, store, map[string]supabaseAccount{
		"Bearer unconfirmed": {ID: sidBudi, Email: "budi@example.com"},
	})
	if res := exchange(t, mux, "unconfirmed"); res.Code != http.StatusForbidden {
		t.Fatalf("unconfirmed status = %d, want 403", res.Code)
	}
	if res := exchange(t, mux, "garbage"); res.Code != http.StatusUnauthorized {
		t.Fatalf("garbage status = %d, want 401", res.Code)
	}
	if res := exchange(t, mux, ""); res.Code != http.StatusUnauthorized {
		t.Fatalf("empty status = %d, want 401", res.Code)
	}
	if _, err := store.FindByEmail(context.Background(), "budi@example.com"); err == nil {
		t.Fatal("a rejected sign-in created a user")
	}
}

func TestSupabaseOffAnswers503(t *testing.T) {
	res := doJSON(t, newTestMux(), http.MethodPost, "/auth/supabase", `{"access_token":"x"}`, "")
	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", res.Code)
	}
}

func TestUsernameCandidatesStayUsable(t *testing.T) {
	store := user.NewMemoryStore()
	svc := user.NewService(store)
	ctx := context.Background()
	// Two different people whose emails share a local part get distinct names.
	a, err := svc.SignInSupabase(ctx, user.SupabaseIdentity{ID: sidBudi, Email: "x@one.com"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.SignInSupabase(ctx, user.SupabaseIdentity{ID: sidOther, Email: "x@two.com"})
	if err != nil {
		t.Fatal(err)
	}
	if a.Username != "member" || b.Username != "member-2" {
		t.Fatalf("usernames = %q, %q; want member, member-2", a.Username, b.Username)
	}
}

func TestSupabaseFirstSignInGainsAPasswordAndCanUnlink(t *testing.T) {
	store := user.NewMemoryStore()
	mux := newSupabaseMux(t, store, map[string]supabaseAccount{
		"Bearer t1": confirmed(sidBudi, "budi@example.com"),
		"Bearer t2": confirmed(sidOther, "newmember@example.com"),
	})

	// The first OAuth sign-in creates a local user without a password.
	first := exchange(t, mux, "t1")
	if first.Code != http.StatusOK {
		t.Fatalf("exchange status = %d, want 200; body = %s", first.Code, first.Body.String())
	}
	var pair Pair
	if err := json.Unmarshal(first.Body.Bytes(), &pair); err != nil {
		t.Fatalf("decode pair: %v", err)
	}
	created := subjectOf(t, first)

	profile := doJSON(t, mux, http.MethodGet, "/users/me", "", pair.AccessToken)
	if profile.Code != http.StatusOK {
		t.Fatalf("profile status = %d, want 200; body = %s", profile.Code, profile.Body.String())
	}
	if !strings.Contains(profile.Body.String(), `"providers":["swegrowth"]`) {
		t.Fatalf("profile missing providers: %s", profile.Body.String())
	}

	// The first password needs no current one.
	set := doJSON(t, mux, http.MethodPut, "/users/password", `{"new_password":"first-password"}`, pair.AccessToken)
	if set.Code != http.StatusNoContent {
		t.Fatalf("set password status = %d, want 204; body = %s", set.Code, set.Body.String())
	}
	if login(t, mux, "budi@example.com", "first-password").AccessToken == "" {
		t.Fatal("password login did not work after the first set")
	}

	// With a password in place, unlinking keeps the account reachable.
	unlink := doJSON(t, mux, http.MethodDelete, "/users/me/identities/swegrowth", "", pair.AccessToken)
	if unlink.Code != http.StatusNoContent {
		t.Fatalf("unlink status = %d, want 204; body = %s", unlink.Code, unlink.Body.String())
	}

	// Signing in again links the same user back.
	again := exchange(t, mux, "t1")
	if again.Code != http.StatusOK {
		t.Fatalf("re-exchange status = %d, want 200; body = %s", again.Code, again.Body.String())
	}
	if relinked := subjectOf(t, again); relinked.Subject != created.Subject {
		t.Fatalf("re-exchange user = %s, want the same %s", relinked.Subject, created.Subject)
	}
	if login(t, mux, "budi@example.com", "first-password").AccessToken == "" {
		t.Fatal("password login stopped after re-linking")
	}

	// A fresh passwordless account still cannot drop its only credential.
	second := exchange(t, mux, "t2")
	if second.Code != http.StatusOK {
		t.Fatalf("second exchange status = %d, want 200", second.Code)
	}
	var secondPair Pair
	if err := json.Unmarshal(second.Body.Bytes(), &secondPair); err != nil {
		t.Fatalf("decode second pair: %v", err)
	}
	res := doJSON(t, mux, http.MethodDelete, "/users/me/identities/swegrowth", "", secondPair.AccessToken)
	if res.Code != http.StatusConflict {
		t.Fatalf("second unlink status = %d, want 409; body = %s", res.Code, res.Body.String())
	}
}
