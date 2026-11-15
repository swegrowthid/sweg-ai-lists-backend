package user

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	identityBudi  = "11111111-1111-1111-1111-111111111111"
	identityOther = "22222222-2222-2222-2222-222222222222"
)

func hasProvider(providers []string, want string) bool {
	for _, provider := range providers {
		if provider == want {
			return true
		}
	}
	return false
}

func TestLocalAccountThenSwegrowthKeepsBothLogins(t *testing.T) {
	store := NewMemoryStore()
	svc := NewService(store)
	ctx := context.Background()

	local, err := svc.Register(ctx, RegisterInput{
		Username: "budi",
		Email:    "budi@example.com",
		Password: "password",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	linked, err := svc.SignInSupabase(ctx, SupabaseIdentity{ID: identityBudi, Email: "BUDI@example.com"})
	if err != nil {
		t.Fatalf("SignInSupabase() error = %v", err)
	}
	if linked.ID != local.ID {
		t.Fatalf("linked user = %s, want local user %s", linked.ID, local.ID)
	}

	if _, err := svc.Authenticate(ctx, LoginInput{Identifier: "budi", Password: "password"}); err != nil {
		t.Fatalf("password login after linking error = %v", err)
	}

	profile, err := svc.Profile(ctx, local.ID)
	if err != nil {
		t.Fatalf("Profile() error = %v", err)
	}
	if !hasProvider(profile.Providers, ProviderSwegrowth) {
		t.Fatalf("providers = %v, want swegrowth", profile.Providers)
	}
}

func TestRegisterRejectsSwegrowthMemberEmail(t *testing.T) {
	store := NewMemoryStore()
	svc := NewService(store)
	ctx := context.Background()

	if _, err := svc.SignInSupabase(ctx, SupabaseIdentity{ID: identityBudi, Email: "member@example.com"}); err != nil {
		t.Fatalf("SignInSupabase() error = %v", err)
	}

	_, err := svc.Register(ctx, RegisterInput{
		Username: "member",
		Email:    "MEMBER@example.com",
		Password: "password",
	})
	if !errors.Is(err, ErrSwegrowthMember) {
		t.Fatalf("Register() error = %v, want ErrSwegrowthMember", err)
	}
}

func TestRegisterLocalDuplicateStaysGenericConflict(t *testing.T) {
	store := NewMemoryStore()
	svc := NewService(store)
	ctx := context.Background()

	if _, err := svc.Register(ctx, RegisterInput{
		Username: "budi",
		Email:    "budi@example.com",
		Password: "password",
	}); err != nil {
		t.Fatalf("first Register() error = %v", err)
	}

	_, err := svc.Register(ctx, RegisterInput{
		Username: "other",
		Email:    "budi@example.com",
		Password: "password",
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("Register() error = %v, want ErrConflict", err)
	}
	if errors.Is(err, ErrSwegrowthMember) {
		t.Fatal("local duplicate was reported as a swegrowth member")
	}
}

func TestTwoSwegrowthAccountsStaySeparate(t *testing.T) {
	store := NewMemoryStore()
	svc := NewService(store)
	ctx := context.Background()

	first, err := svc.SignInSupabase(ctx, SupabaseIdentity{ID: identityBudi, Email: "budi@example.com"})
	if err != nil {
		t.Fatalf("first SignInSupabase() error = %v", err)
	}
	second, err := svc.SignInSupabase(ctx, SupabaseIdentity{ID: identityOther, Email: "other@example.com"})
	if err != nil {
		t.Fatalf("second SignInSupabase() error = %v", err)
	}
	if first.ID == second.ID {
		t.Fatal("two swegrowth accounts resolved to one local user")
	}

	profiles, err := svc.ListProfiles(ctx)
	if err != nil {
		t.Fatalf("ListProfiles() error = %v", err)
	}
	if len(profiles) != 2 {
		t.Fatalf("profiles = %d, want 2", len(profiles))
	}
	for _, profile := range profiles {
		if !hasProvider(profile.Providers, ProviderSwegrowth) {
			t.Fatalf("profile %s providers = %v, want swegrowth", profile.ID, profile.Providers)
		}
	}
}

func TestSecondSwegrowthIdentityForSameEmailRejected(t *testing.T) {
	store := NewMemoryStore()
	svc := NewService(store)
	ctx := context.Background()

	if _, err := svc.SignInSupabase(ctx, SupabaseIdentity{ID: identityBudi, Email: "member@example.com"}); err != nil {
		t.Fatalf("first SignInSupabase() error = %v", err)
	}

	_, err := svc.SignInSupabase(ctx, SupabaseIdentity{ID: identityOther, Email: "member@example.com"})
	if !errors.Is(err, ErrLinkedElsewhere) {
		t.Fatalf("second SignInSupabase() error = %v, want ErrLinkedElsewhere", err)
	}
}

func TestProfileForLocalUserHasEmptyProviders(t *testing.T) {
	store := NewMemoryStore()
	svc := NewService(store)
	ctx := context.Background()

	local, err := svc.Register(ctx, RegisterInput{
		Username: "budi",
		Email:    "budi@example.com",
		Password: "password",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	profile, err := svc.Profile(ctx, local.ID)
	if err != nil {
		t.Fatalf("Profile() error = %v", err)
	}
	if profile.Providers == nil {
		t.Fatal("providers = nil, want empty slice")
	}
	if len(profile.Providers) != 0 {
		t.Fatalf("providers = %v, want empty", profile.Providers)
	}
}

type testPrincipalKey struct{}

func newIdentityMux(t *testing.T, store *MemoryStore) (*http.ServeMux, *Service) {
	t.Helper()
	svc := NewService(store)
	handler := NewHandler(svc, nil)
	handler.SetAuth(func(ctx context.Context) (string, bool) {
		userID, _ := ctx.Value(testPrincipalKey{}).(string)
		return userID, userID != ""
	}, nil)
	protect := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := r.Header.Get("X-Test-User")
			ctx := context.WithValue(r.Context(), testPrincipalKey{}, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, protect)
	return mux, svc
}

func doJSONRequest(t *testing.T, mux *http.ServeMux, method, path, body, userID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if userID != "" {
		req.Header.Set("X-Test-User", userID)
	}
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	return res
}

func TestRegisterHandlerRejectsSwegrowthMemberWithGateMessage(t *testing.T) {
	store := NewMemoryStore()
	mux, svc := newIdentityMux(t, store)

	if _, err := svc.SignInSupabase(context.Background(), SupabaseIdentity{ID: identityBudi, Email: "member@example.com"}); err != nil {
		t.Fatalf("SignInSupabase() error = %v", err)
	}

	res := doJSONRequest(t, mux, http.MethodPost, "/users/register",
		`{"username":"member","email":"member@example.com","password":"password"}`, "")
	if res.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body = %s", res.Code, res.Body.String())
	}
	if got := strings.TrimSpace(res.Body.String()); got != "this email is registered with swegrowth.id; sign in with swegrowth.id" {
		t.Fatalf("message = %q, want gate message", got)
	}
}

func TestMeHandlerReturnsProviders(t *testing.T) {
	store := NewMemoryStore()
	mux, svc := newIdentityMux(t, store)

	u, err := svc.SignInSupabase(context.Background(), SupabaseIdentity{ID: identityBudi, Email: "member@example.com"})
	if err != nil {
		t.Fatalf("SignInSupabase() error = %v", err)
	}

	res := doJSONRequest(t, mux, http.MethodGet, "/users/me", "", u.ID)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", res.Code, res.Body.String())
	}
	var got Profile
	if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.ID != u.ID {
		t.Fatalf("id = %q, want %q", got.ID, u.ID)
	}
	if len(got.Providers) != 1 || got.Providers[0] != ProviderSwegrowth {
		t.Fatalf("providers = %v, want [swegrowth]", got.Providers)
	}
	if strings.Contains(res.Body.String(), identityBudi) {
		t.Fatal("response exposed provider_user_id")
	}
}

func TestMeHandlerWithoutPrincipalReturns401(t *testing.T) {
	store := NewMemoryStore()
	mux, _ := newIdentityMux(t, store)

	res := doJSONRequest(t, mux, http.MethodGet, "/users/me", "", "")
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", res.Code)
	}
}

func TestListHandlerReturnsEmptyProvidersForLocalUser(t *testing.T) {
	store := NewMemoryStore()
	mux, svc := newIdentityMux(t, store)

	local, err := svc.Register(context.Background(), RegisterInput{
		Username: "localuser",
		Email:    "local@example.com",
		Password: "password",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	res := doJSONRequest(t, mux, http.MethodGet, "/users", "", local.ID)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", res.Code, res.Body.String())
	}
	var got []Profile
	if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("profiles = %d, want 1", len(got))
	}
	if got[0].Providers == nil {
		t.Fatal("providers = null, want []")
	}
	if len(got[0].Providers) != 0 {
		t.Fatalf("providers = %v, want []", got[0].Providers)
	}
}

func TestListHandlerReturnsProviders(t *testing.T) {
	store := NewMemoryStore()
	mux, svc := newIdentityMux(t, store)

	first, err := svc.SignInSupabase(context.Background(), SupabaseIdentity{ID: identityBudi, Email: "budi@example.com"})
	if err != nil {
		t.Fatalf("first SignInSupabase() error = %v", err)
	}
	if _, err := svc.SignInSupabase(context.Background(), SupabaseIdentity{ID: identityOther, Email: "other@example.com"}); err != nil {
		t.Fatalf("second SignInSupabase() error = %v", err)
	}

	res := doJSONRequest(t, mux, http.MethodGet, "/users", "", first.ID)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", res.Code, res.Body.String())
	}
	var got []Profile
	if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("profiles = %d, want 2", len(got))
	}
	for _, profile := range got {
		if !hasProvider(profile.Providers, ProviderSwegrowth) {
			t.Fatalf("profile %s providers = %v, want swegrowth", profile.ID, profile.Providers)
		}
	}
	if got[0].ID == got[1].ID {
		t.Fatalf("duplicate profile id %s", got[0].ID)
	}
}

func TestSignInSupabaseSameIdentityReturnsSameUser(t *testing.T) {
	store := NewMemoryStore()
	svc := NewService(store)
	ctx := context.Background()

	first, err := svc.SignInSupabase(ctx, SupabaseIdentity{ID: identityBudi, Email: "budi@example.com"})
	if err != nil {
		t.Fatalf("first SignInSupabase() error = %v", err)
	}
	second, err := svc.SignInSupabase(ctx, SupabaseIdentity{ID: identityBudi, Email: "budi@example.com"})
	if err != nil {
		t.Fatalf("second SignInSupabase() error = %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("second sign in user = %s, want %s", second.ID, first.ID)
	}

	profiles, err := svc.ListProfiles(ctx)
	if err != nil {
		t.Fatalf("ListProfiles() error = %v", err)
	}
	if len(profiles) != 1 {
		t.Fatalf("profiles = %d, want 1", len(profiles))
	}
}

// conflictingCreateStore makes the first Create report a conflict after the
// row was written. This reproduces the loser of two concurrent first sign-ins.
type conflictingCreateStore struct {
	*MemoryStore
	conflicted bool
}

func (s *conflictingCreateStore) Create(ctx context.Context, input CreateInput) (User, error) {
	created, err := s.MemoryStore.Create(ctx, input)
	if err != nil {
		return User{}, err
	}
	if !s.conflicted {
		s.conflicted = true
		return User{}, ErrConflict
	}
	return created, nil
}

func TestSignInSupabaseResolvesConcurrentCreate(t *testing.T) {
	store := &conflictingCreateStore{MemoryStore: NewMemoryStore()}
	svc := NewService(store)
	ctx := context.Background()

	got, err := svc.SignInSupabase(ctx, SupabaseIdentity{ID: identityBudi, Email: "budi@example.com"})
	if err != nil {
		t.Fatalf("SignInSupabase() error = %v", err)
	}
	if got.Email != "budi@example.com" {
		t.Fatalf("email = %q, want budi@example.com", got.Email)
	}
	linked, err := store.FindByIdentity(ctx, ProviderSwegrowth, identityBudi)
	if err != nil {
		t.Fatalf("FindByIdentity() error = %v", err)
	}
	if linked.ID != got.ID {
		t.Fatalf("linked user = %s, want %s", linked.ID, got.ID)
	}
}

func TestRegisterRejectsEmailWithOtherProvider(t *testing.T) {
	store := NewMemoryStore()
	svc := NewService(store)
	ctx := context.Background()

	if _, err := store.Create(ctx, CreateInput{
		Username:     "othermember",
		Email:        "other@example.com",
		PasswordHash: "x",
		Identity:     &Identity{Provider: "github", ProviderUserID: "123"},
	}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	_, err := svc.Register(ctx, RegisterInput{
		Username: "other",
		Email:    "other@example.com",
		Password: "password",
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("Register() error = %v, want ErrConflict", err)
	}
	if errors.Is(err, ErrSwegrowthMember) {
		t.Fatal("non-swegrowth provider was reported as a swegrowth member")
	}
}

func TestListHandlerDoesNotExposePasswordOrIdentity(t *testing.T) {
	store := NewMemoryStore()
	mux, svc := newIdentityMux(t, store)
	ctx := context.Background()

	if _, err := svc.Register(ctx, RegisterInput{
		Username: "budi",
		Email:    "budi@example.com",
		Password: "password",
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	member, err := svc.SignInSupabase(ctx, SupabaseIdentity{ID: identityBudi, Email: "budi@example.com"})
	if err != nil {
		t.Fatalf("SignInSupabase() error = %v", err)
	}

	res := doJSONRequest(t, mux, http.MethodGet, "/users", "", member.ID)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", res.Code, res.Body.String())
	}
	body := res.Body.String()
	for _, leaked := range []string{"password_hash", "provider_user_id", identityBudi} {
		if strings.Contains(body, leaked) {
			t.Fatalf("response exposed %q: %s", leaked, body)
		}
	}
	if !strings.Contains(body, `"providers":["swegrowth"]`) {
		t.Fatalf("response missing providers: %s", body)
	}
}

func TestMeHandlerForMissingUserReturns401(t *testing.T) {
	store := NewMemoryStore()
	mux, _ := newIdentityMux(t, store)

	res := doJSONRequest(t, mux, http.MethodGet, "/users/me", "", "00000000-0000-0000-0000-000000000000")
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body = %s", res.Code, res.Body.String())
	}
}
