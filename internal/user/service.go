package user

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

const (
	bcryptCost       = bcrypt.DefaultCost
	maxPasswordBytes = 72
)

// Store is the persistence port. Service depends on it, never on SQL.
type Store interface {
	List(ctx context.Context) ([]User, error)
	Create(ctx context.Context, input CreateInput) (User, error)
}

// Service owns user use-cases. No HTTP, no SQL here.
type Service struct {
	store Store
}

// NewService wires the store. Nil store is a programmer bug, so panic early.
func NewService(store Store) *Service {
	if store == nil {
		panic("user: nil Store")
	}
	return &Service{store: store}
}

// List returns all users. Errors pass through untouched (E channel).
func (s *Service) List(ctx context.Context) ([]User, error) {
	return s.store.List(ctx)
}

// Register validates, hashes, and persists a new user.
func (s *Service) Register(ctx context.Context, input RegisterInput) (User, error) {
	input.Username = strings.TrimSpace(input.Username)
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))

	if input.Username == "" || utf8.RuneCountInString(input.Username) < 3 || input.Email == "" || !strings.Contains(input.Email, "@") || input.Password == "" || len([]byte(input.Password)) > maxPasswordBytes {
		return User{}, ErrInvalidInput
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcryptCost)
	if err != nil {
		return User{}, fmt.Errorf("user: hash password: %w", err)
	}

	created, err := s.store.Create(ctx, CreateInput{
		Username:     input.Username,
		Email:        input.Email,
		PasswordHash: string(passwordHash),
	})
	if err != nil {
		return User{}, err
	}
	return created, nil
}
