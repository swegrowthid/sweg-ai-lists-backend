package user

import (
	"context"
	"errors"
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
	FindByUsernameOrEmail(ctx context.Context, identifier string) (User, error)
}

// dummyHash keeps logins with an unknown identifier on the bcrypt path,
// so response timing does not reveal whether the identifier exists.
var dummyHash = []byte("$2a$10$Wquyws8wr.DTZ.NQXw8x/ut.RZXl4geTcsSaK3UcSUyXcV3oAArQG")

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

// Authenticate verifies the identifier (username or email) and password.
// Both unknown user and wrong password return ErrInvalidCredentials.
func (s *Service) Authenticate(ctx context.Context, input LoginInput) (User, error) {
	identifier := strings.TrimSpace(input.Identifier)
	if identifier == "" || input.Password == "" {
		return User{}, ErrInvalidCredentials
	}

	u, err := s.store.FindByUsernameOrEmail(ctx, identifier)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(input.Password))
			return User{}, ErrInvalidCredentials
		}
		return User{}, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(input.Password)); err != nil {
		return User{}, ErrInvalidCredentials
	}
	return u, nil
}
