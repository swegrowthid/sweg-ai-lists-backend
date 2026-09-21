package user

import (
	"errors"
	"time"
)

// User is the domain entity. Keep it small; grow only on demand.
// PasswordHash never leaves the server (json:"-").
type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

var (
	// ErrNotFound signals a missing user. Services map it to 404.
	ErrNotFound = errors.New("user: not found")

	// ErrConflict signals that a username or email is already registered.
	ErrConflict = errors.New("user: username or email already registered")

	// ErrInvalidInput signals invalid registration data.
	ErrInvalidInput = errors.New("user: invalid registration input")

	// ErrInvalidCredentials signals a failed login. One error for every cause:
	// unknown identifier and wrong password stay indistinguishable.
	ErrInvalidCredentials = errors.New("user: invalid credentials")
)

// RegisterInput is the validated input for the registration use case.
type RegisterInput struct {
	Username string
	Email    string
	Password string
}

// LoginInput is the validated input for the authenticate use case.
// Identifier accepts a username or an email.
type LoginInput struct {
	Identifier string
	Password   string
}

// CreateInput contains the trusted values persisted for a new user.
type CreateInput struct {
	Username     string
	Email        string
	PasswordHash string
}
