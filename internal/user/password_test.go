package user

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func registerForPasswordTest(t *testing.T, svc *Service) User {
	t.Helper()
	created, err := svc.Register(context.Background(), RegisterInput{
		Username: "budi",
		Email:    "budi@example.com",
		Password: "old-password",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	return created
}

func TestUpdatePasswordSwapsHash(t *testing.T) {
	svc := NewService(NewMemoryStore())
	created := registerForPasswordTest(t, svc)
	ctx := context.Background()

	if err := svc.UpdatePassword(ctx, UpdatePasswordInput{
		UserID:          created.ID,
		CurrentPassword: "old-password",
		NewPassword:     "new-password",
	}); err != nil {
		t.Fatalf("UpdatePassword() error = %v", err)
	}

	if _, err := svc.Authenticate(ctx, LoginInput{Identifier: "budi", Password: "new-password"}); err != nil {
		t.Fatalf("Authenticate(new) error = %v", err)
	}
	if _, err := svc.Authenticate(ctx, LoginInput{Identifier: "budi", Password: "old-password"}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Authenticate(old) error = %v, want ErrInvalidCredentials", err)
	}

	stored, err := svc.store.FindByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("FindByID() error = %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(stored.PasswordHash), []byte("new-password")); err != nil {
		t.Fatalf("stored hash does not match new password: %v", err)
	}
}

func TestUpdatePasswordRejectsWrongCurrent(t *testing.T) {
	svc := NewService(NewMemoryStore())
	created := registerForPasswordTest(t, svc)

	err := svc.UpdatePassword(context.Background(), UpdatePasswordInput{
		UserID:          created.ID,
		CurrentPassword: "wrong-password",
		NewPassword:     "new-password",
	})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("UpdatePassword() error = %v, want ErrInvalidCredentials", err)
	}
}

func TestUpdatePasswordRejectsBadInput(t *testing.T) {
	svc := NewService(NewMemoryStore())
	created := registerForPasswordTest(t, svc)
	ctx := context.Background()

	for _, input := range []UpdatePasswordInput{
		{UserID: "", CurrentPassword: "old-password", NewPassword: "new-password"},
		{UserID: created.ID, CurrentPassword: "", NewPassword: "new-password"},
		{UserID: created.ID, CurrentPassword: "old-password", NewPassword: ""},
		{UserID: created.ID, CurrentPassword: "old-password", NewPassword: strings.Repeat("a", maxPasswordBytes+1)},
	} {
		if err := svc.UpdatePassword(ctx, input); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("UpdatePassword(%+v) error = %v, want ErrInvalidInput", input, err)
		}
	}
}

func TestUpdatePasswordMissingUser(t *testing.T) {
	svc := NewService(NewMemoryStore())
	err := svc.UpdatePassword(context.Background(), UpdatePasswordInput{
		UserID:          "00000000-0000-0000-0000-000000000000",
		CurrentPassword: "old-password",
		NewPassword:     "new-password",
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdatePassword() error = %v, want ErrNotFound", err)
	}
}

func newPasswordMux(userID string, onChanged func(ctx context.Context, id string) error) (*http.ServeMux, *Service) {
	svc := NewService(NewMemoryStore())
	handler := NewHandler(svc, nil)
	handler.SetAuth(
		func(context.Context) (string, bool) { return userID, userID != "" },
		onChanged,
	)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, nil)
	return mux, svc
}

func TestChangePasswordHandlerRevokesSessions(t *testing.T) {
	svc := NewService(NewMemoryStore())
	seeded, err := svc.Register(context.Background(), RegisterInput{
		Username: "budi",
		Email:    "budi@example.com",
		Password: "old-password",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	var revoked []string
	handler := NewHandler(svc, nil)
	handler.SetAuth(
		func(context.Context) (string, bool) { return seeded.ID, true },
		func(_ context.Context, id string) error {
			revoked = append(revoked, id)
			return nil
		},
	)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, nil)

	req := httptest.NewRequest(http.MethodPut, "/users/password", strings.NewReader(`{"current_password":"old-password","new_password":"new-password"}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body = %s", res.Code, http.StatusNoContent, res.Body.String())
	}
	if len(revoked) != 1 || revoked[0] != seeded.ID {
		t.Fatalf("revoked = %v, want [%s]", revoked, seeded.ID)
	}
	if _, err := svc.Authenticate(context.Background(), LoginInput{Identifier: "budi", Password: "new-password"}); err != nil {
		t.Fatalf("Authenticate(new) error = %v", err)
	}
}

func TestChangePasswordHandlerMapsErrors(t *testing.T) {
	cases := []struct {
		name       string
		userID     string
		body       string
		wantStatus int
		wantBody   string
	}{
		{"missing user", "some-id", `{"current_password":"x","new_password":"y"}`, http.StatusUnauthorized, "user not found"},
		{"bad json", "some-id", `{"current_password":}`, http.StatusBadRequest, "invalid request body"},
		{"empty new", "some-id", `{"current_password":"x","new_password":""}`, http.StatusBadRequest, "invalid current or new password"},
		{"missing principal", "", `{"current_password":"x","new_password":"y"}`, http.StatusUnauthorized, "missing bearer token"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := newPasswordMux(tc.userID, nil)
			req := httptest.NewRequest(http.MethodPut, "/users/password", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			res := httptest.NewRecorder()
			m.ServeHTTP(res, req)
			if res.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", res.Code, tc.wantStatus, res.Body.String())
			}
			if got := strings.TrimSpace(res.Body.String()); got != tc.wantBody {
				t.Fatalf("body = %q, want %q", got, tc.wantBody)
			}
		})
	}
}

func TestChangePasswordHandlerRejectsWrongCurrent(t *testing.T) {
	svc := NewService(NewMemoryStore())
	seeded, err := svc.Register(context.Background(), RegisterInput{
		Username: "budi",
		Email:    "budi@example.com",
		Password: "old-password",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	handler := NewHandler(svc, nil)
	handler.SetAuth(func(context.Context) (string, bool) { return seeded.ID, true }, nil)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, nil)

	req := httptest.NewRequest(http.MethodPut, "/users/password", strings.NewReader(`{"current_password":"wrong","new_password":"new-password"}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body = %s", res.Code, http.StatusUnauthorized, res.Body.String())
	}
	if got := strings.TrimSpace(res.Body.String()); got != "current password is incorrect" {
		t.Fatalf("body = %q, want wrong-password message", got)
	}
}
