package user

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestRegisterHashesPasswordAndNormalizesEmail(t *testing.T) {
	store := NewMemoryStore()
	svc := NewService(store)

	created, err := svc.Register(context.Background(), RegisterInput{
		Username: " budi ",
		Email:    "BUDI@Example.COM ",
		Password: "correct horse battery staple",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if created.Email != "budi@example.com" {
		t.Fatalf("email = %q, want lowercase email", created.Email)
	}
	if created.PasswordHash == "" || created.PasswordHash == "correct horse battery staple" {
		t.Fatal("password was not stored as a hash")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(created.PasswordHash), []byte("correct horse battery staple")); err != nil {
		t.Fatalf("stored password hash does not match password: %v", err)
	}
}

func TestRegisterRejectsDuplicateUsernameOrEmail(t *testing.T) {
	store := NewMemoryStore()
	svc := NewService(store)

	_, err := svc.Register(context.Background(), RegisterInput{
		Username: "budi",
		Email:    "budi@example.com",
		Password: "password",
	})
	if err != nil {
		t.Fatalf("first Register() error = %v", err)
	}

	for _, input := range []RegisterInput{
		{Username: "budi", Email: "other@example.com", Password: "password"},
		{Username: "other", Email: "BUDI@EXAMPLE.COM", Password: "password"},
	} {
		if _, err := svc.Register(context.Background(), input); !errors.Is(err, ErrConflict) {
			t.Fatalf("Register() error = %v, want ErrConflict", err)
		}
	}
}

func TestRegisterHandlerDoesNotExposePassword(t *testing.T) {
	handler := NewHandler(NewService(NewMemoryStore()), nil)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/users/register", strings.NewReader(`{"username":"budi","email":"BUDI@Example.COM","password":"password"}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", res.Code, http.StatusCreated, res.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := response["password"]; ok {
		t.Fatal("response exposed password")
	}
	if _, ok := response["password_hash"]; ok {
		t.Fatal("response exposed password_hash")
	}
	if response["email"] != "budi@example.com" {
		t.Fatalf("email = %v, want lowercase email", response["email"])
	}
}

func TestRegisterHandlerReturnsGenericDuplicateMessage(t *testing.T) {
	handler := NewHandler(NewService(NewMemoryStore()), nil)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	register := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/users/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		return res
	}

	first := register(`{"username":"budi","email":"budi@example.com","password":"password"}`)
	if first.Code != http.StatusCreated {
		t.Fatalf("first status = %d, want %d", first.Code, http.StatusCreated)
	}

	duplicate := register(`{"username":"budi","email":"other@example.com","password":"password"}`)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate status = %d, want %d", duplicate.Code, http.StatusConflict)
	}
	if got := strings.TrimSpace(duplicate.Body.String()); got != "username or email already exists" {
		t.Fatalf("duplicate message = %q, want generic message", got)
	}
}

func TestRegisterRejectsPasswordOverBcryptLimit(t *testing.T) {
	svc := NewService(NewMemoryStore())
	_, err := svc.Register(context.Background(), RegisterInput{
		Username: "budi",
		Email:    "budi@example.com",
		Password: strings.Repeat("a", maxPasswordBytes+1),
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Register() error = %v, want ErrInvalidInput", err)
	}
}
