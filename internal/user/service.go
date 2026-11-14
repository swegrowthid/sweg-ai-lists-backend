package user

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
	FindByEmail(ctx context.Context, email string) (User, error)
	FindByID(ctx context.Context, id string) (User, error)
	UpdatePasswordHash(ctx context.Context, id, passwordHash string) error
	FindBySupabaseID(ctx context.Context, supabaseID string) (User, error)
	// LinkSupabaseID sets the user's swegrowth.id account when none is set.
	// A different one already set returns ErrLinkedElsewhere.
	LinkSupabaseID(ctx context.Context, id, supabaseID string) error
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

// FindByEmail looks up a user by email (case-insensitive).
// Missing users return ErrNotFound.
func (s *Service) FindByEmail(ctx context.Context, email string) (User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || !strings.Contains(email, "@") {
		return User{}, ErrNotFound
	}
	return s.store.FindByEmail(ctx, email)
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

// UpdatePassword verifies the current password and stores the new hash.
// A wrong current password returns ErrInvalidCredentials; a missing user
// returns ErrNotFound. Callers revoke sessions after a successful change.
func (s *Service) UpdatePassword(ctx context.Context, input UpdatePasswordInput) error {
	if input.UserID == "" || input.CurrentPassword == "" || input.NewPassword == "" || len([]byte(input.NewPassword)) > maxPasswordBytes {
		return ErrInvalidInput
	}

	u, err := s.store.FindByID(ctx, input.UserID)
	if err != nil {
		return err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(input.CurrentPassword)); err != nil {
		return ErrInvalidCredentials
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(input.NewPassword), bcryptCost)
	if err != nil {
		return fmt.Errorf("user: hash password: %w", err)
	}
	if err := s.store.UpdatePasswordHash(ctx, input.UserID, string(newHash)); err != nil {
		return err
	}
	return nil
}

// SignInSupabase returns the local user for a swegrowth.id account. In order:
// the user already linked to it; else the user with the same email, now
// linked, so their posts stay theirs; else a new user with no password.
func (s *Service) SignInSupabase(ctx context.Context, identity SupabaseIdentity) (User, error) {
	identity.Email = strings.ToLower(strings.TrimSpace(identity.Email))
	if identity.ID == "" || !strings.Contains(identity.Email, "@") {
		return User{}, ErrInvalidInput
	}

	u, err := s.store.FindBySupabaseID(ctx, identity.ID)
	if err == nil {
		return u, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return User{}, err
	}

	u, err = s.store.FindByEmail(ctx, identity.Email)
	if err == nil {
		if err := s.store.LinkSupabaseID(ctx, u.ID, identity.ID); err != nil {
			return User{}, err
		}
		return u, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return User{}, err
	}

	// A taken username is the only conflict left, so try the next candidate.
	for _, username := range usernameCandidates(identity.Email) {
		created, err := s.store.Create(ctx, CreateInput{
			Username:   username,
			Email:      identity.Email,
			SupabaseID: identity.ID,
		})
		if errors.Is(err, ErrConflict) {
			continue
		}
		return created, err
	}
	return User{}, ErrConflict
}

// usernameCandidates derives usernames from the email's local part:
// "budi.s@x" gives budi.s, budi.s-2 … budi.s-9, then one random suffix.
func usernameCandidates(email string) []string {
	local, _, _ := strings.Cut(email, "@")
	var b strings.Builder
	for _, r := range strings.ToLower(local) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	base := strings.Trim(b.String(), ".-_")
	if len(base) > 30 {
		base = base[:30]
	}
	if len(base) < 3 {
		base = "member"
	}
	out := []string{base}
	for i := 2; i <= 9; i++ {
		out = append(out, fmt.Sprintf("%s-%d", base, i))
	}
	suffix := make([]byte, 3)
	if _, err := rand.Read(suffix); err == nil {
		out = append(out, base+"-"+hex.EncodeToString(suffix))
	}
	return out
}
