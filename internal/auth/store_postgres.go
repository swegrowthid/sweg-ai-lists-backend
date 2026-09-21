package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// PostgresRefreshStore tracks refresh tokens in Postgres via database/sql.
type PostgresRefreshStore struct {
	db *sql.DB
}

// NewPostgresRefreshStore wires the pool. Nil pool is a programmer bug.
func NewPostgresRefreshStore(db *sql.DB) *PostgresRefreshStore {
	if db == nil {
		panic("auth: nil DB")
	}
	return &PostgresRefreshStore{db: db}
}

// Save implements RefreshStore.
func (s *PostgresRefreshStore) Save(ctx context.Context, rec RefreshRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO refresh_tokens (jti, user_id, expires_at)
		VALUES ($1, $2, $3)
	`, rec.JTI, rec.UserID, rec.ExpiresAt)
	if err != nil {
		return fmt.Errorf("auth: save refresh token: %w", err)
	}
	return nil
}

// Consume implements RefreshStore. One UPDATE rotates the token atomically;
// a second SELECT only runs to classify the failure (reused vs expired vs unknown).
func (s *PostgresRefreshStore) Consume(ctx context.Context, jti string, now time.Time) (RefreshRecord, error) {
	var rec RefreshRecord
	err := s.db.QueryRowContext(ctx, `
		UPDATE refresh_tokens
		SET revoked_at = $2
		WHERE jti = $1 AND revoked_at IS NULL AND expires_at > $2
		RETURNING jti, user_id, expires_at, created_at
	`, jti, now).Scan(&rec.JTI, &rec.UserID, &rec.ExpiresAt, &rec.CreatedAt)
	if err == nil {
		return rec, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return RefreshRecord{}, fmt.Errorf("auth: consume refresh token: %w", err)
	}

	var revokedAt *time.Time
	err = s.db.QueryRowContext(ctx, `
		SELECT jti, user_id, expires_at, revoked_at, created_at
		FROM refresh_tokens WHERE jti = $1
	`, jti).Scan(&rec.JTI, &rec.UserID, &rec.ExpiresAt, &revokedAt, &rec.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RefreshRecord{}, ErrRefreshInvalid
		}
		return RefreshRecord{}, fmt.Errorf("auth: lookup refresh token: %w", err)
	}
	rec.RevokedAt = revokedAt
	if revokedAt != nil {
		return rec, ErrRefreshReused
	}
	return rec, ErrRefreshInvalid
}

// Revoke implements RefreshStore.
func (s *PostgresRefreshStore) Revoke(ctx context.Context, jti string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE refresh_tokens SET revoked_at = now() WHERE jti = $1 AND revoked_at IS NULL
	`, jti)
	if err != nil {
		return fmt.Errorf("auth: revoke refresh token: %w", err)
	}
	return nil
}

// RevokeAllForUser implements RefreshStore.
func (s *PostgresRefreshStore) RevokeAllForUser(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE refresh_tokens SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL
	`, userID)
	if err != nil {
		return fmt.Errorf("auth: revoke user tokens: %w", err)
	}
	return nil
}
