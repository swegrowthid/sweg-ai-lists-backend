package user

import (
	"errors"
	"time"
)

// ProviderSwegrowth is the external identity provider for swegrowth.id.
const ProviderSwegrowth = "swegrowth"

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

// Profile is a user plus the external providers linked to it.
// Providers is an empty slice, never nil, on every profile read.
type Profile struct {
	User
	Providers []string `json:"providers"`
}

var (
	// ErrNotFound signals a missing user. Services map it to 404.
	ErrNotFound = errors.New("user: not found")

	// ErrConflict signals that a username or email is already registered.
	ErrConflict = errors.New("user: username or email already registered")

	// ErrInvalidInput signals invalid registration data.
	ErrInvalidInput = errors.New("user: invalid registration input")

	// ErrLinkedElsewhere signals that the email's local account is already
	// linked to a different swegrowth.id account.
	ErrLinkedElsewhere = errors.New("user: email linked to another swegrowth account")

	// ErrSwegrowthMember signals that the email already belongs to a
	// swegrowth.id member, so local registration is closed.
	ErrSwegrowthMember = errors.New("user: email registered with swegrowth")

	// ErrInvalidCredentials signals a failed login. One error for every cause:
	// unknown identifier and wrong password stay indistinguishable.
	ErrInvalidCredentials = errors.New("user: invalid credentials")

	// ErrLastCredential signals that unlinking would remove the last way to
	// sign in: the user has no password and no other identity left.
	ErrLastCredential = errors.New("user: cannot unlink the last sign-in method")
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

// UpdatePasswordInput is the validated input for the password change use case.
// UserID comes from the verified access token, never from the request body.
type UpdatePasswordInput struct {
	UserID          string
	CurrentPassword string
	NewPassword     string
}

// Identity is an external account linked to a local user.
type Identity struct {
	Provider       string
	ProviderUserID string
}

// CreateInput contains the trusted values persisted for a new user.
type CreateInput struct {
	Username     string
	Email        string
	PasswordHash string
	// Identity links the new user to an external account. Nil for none.
	Identity *Identity
}

// SupabaseIdentity is a swegrowth.id account as Supabase vouches for it.
// The caller has already checked the email is confirmed.
type SupabaseIdentity struct {
	ID    string
	Email string
}
