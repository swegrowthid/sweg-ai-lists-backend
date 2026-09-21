package auth

import (
	"context"
	"errors"
	"time"

	"github.com/swegrowthid/sweg-ai-lists-backend/internal/user"
)

// Authenticator is the user port auth needs. user.Service satisfies it.
type Authenticator interface {
	Authenticate(ctx context.Context, input user.LoginInput) (user.User, error)
}

// Service owns auth use-cases: login, refresh rotation, logout.
type Service struct {
	users   Authenticator
	tokens  *Tokens
	refresh RefreshStore
}

// NewService wires dependencies. Nil deps are programmer bugs, panic early.
func NewService(users Authenticator, tokens *Tokens, refresh RefreshStore) *Service {
	if users == nil || tokens == nil || refresh == nil {
		panic("auth: nil dependency")
	}
	return &Service{users: users, tokens: tokens, refresh: refresh}
}

// Login verifies credentials and returns a signed token pair.
func (s *Service) Login(ctx context.Context, input user.LoginInput) (Pair, error) {
	u, err := s.users.Authenticate(ctx, input)
	if err != nil {
		return Pair{}, err
	}
	return s.issueAndSave(ctx, u.ID, u.Username)
}

// Refresh rotates a refresh token: consume the old jti, issue a new pair.
// A revoked token kills the user's whole token family (reuse detection).
func (s *Service) Refresh(ctx context.Context, refreshToken string) (Pair, error) {
	claims, err := s.tokens.ParseRefresh(refreshToken)
	if err != nil {
		return Pair{}, err
	}
	rec, err := s.refresh.Consume(ctx, claims.ID, time.Now())
	if errors.Is(err, ErrRefreshReused) {
		_ = s.refresh.RevokeAllForUser(ctx, rec.UserID)
		return Pair{}, err
	}
	if err != nil {
		return Pair{}, err
	}
	return s.issueAndSave(ctx, claims.Subject, claims.Username)
}

// Logout revokes one refresh token. Unknown tokens stay a no-op.
func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	claims, err := s.tokens.ParseRefresh(refreshToken)
	if err != nil {
		return err
	}
	return s.refresh.Revoke(ctx, claims.ID)
}

func (s *Service) issueAndSave(ctx context.Context, userID, username string) (Pair, error) {
	pair, rec, err := s.tokens.Issue(userID, username)
	if err != nil {
		return Pair{}, err
	}
	if err := s.refresh.Save(ctx, rec); err != nil {
		return Pair{}, err
	}
	return pair, nil
}
