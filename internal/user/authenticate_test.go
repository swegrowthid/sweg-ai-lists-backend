package user

import (
	"context"
	"errors"
	"testing"
)

func TestAuthenticateAcceptsUsernameOrEmail(t *testing.T) {
	svc := NewService(NewMemoryStore())
	_, err := svc.Register(context.Background(), RegisterInput{
		Username: "budi",
		Email:    "budi@example.com",
		Password: "password",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	for _, identifier := range []string{"budi", "budi@example.com", "BUDI@EXAMPLE.COM"} {
		u, err := svc.Authenticate(context.Background(), LoginInput{
			Identifier: identifier,
			Password:   "password",
		})
		if err != nil {
			t.Fatalf("Authenticate(%q) error = %v", identifier, err)
		}
		if u.Username != "budi" {
			t.Fatalf("Authenticate(%q) username = %q, want budi", identifier, u.Username)
		}
	}
}

func TestAuthenticateErrorsAreIndistinguishable(t *testing.T) {
	svc := NewService(NewMemoryStore())
	_, err := svc.Register(context.Background(), RegisterInput{
		Username: "budi",
		Email:    "budi@example.com",
		Password: "password",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	for _, input := range []LoginInput{
		{Identifier: "nobody", Password: "password"},
		{Identifier: "budi", Password: "wrong-password"},
		{Identifier: "", Password: "password"},
		{Identifier: "budi", Password: ""},
	} {
		if _, err := svc.Authenticate(context.Background(), input); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("Authenticate(%+v) error = %v, want ErrInvalidCredentials", input, err)
		}
	}
}
