package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// PostgresMagicStore persists magic links in Postgres via database/sql.
type PostgresMagicStore struct {
	db *sql.DB
}

// NewPostgresMagicStore wires the pool. Nil pool is a programmer bug.
func NewPostgresMagicStore(db *sql.DB) *PostgresMagicStore {
	if db == nil {
		panic("auth: nil DB")
	}
	return &PostgresMagicStore{db: db}
}

// Save implements MagicStore. After inserting the new link, it opportunistically
// deletes rows that were used or expired more than 24 hours ago.
func (s *PostgresMagicStore) Save(ctx context.Context, rec MagicLinkRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO magic_links (token_hash, user_id, email, expires_at)
		VALUES ($1, $2, lower($3), $4)
	`, rec.TokenHash, rec.UserID, strings.TrimSpace(rec.Email), rec.ExpiresAt)
	if err != nil {
		return fmt.Errorf("auth: save magic link: %w", err)
	}

	_, _ = s.db.ExecContext(ctx, `
		DELETE FROM magic_links
		WHERE (used_at IS NOT NULL AND used_at < now() - interval '24 hours')
		   OR expires_at < now() - interval '24 hours'
	`)
	return nil
}

// Consume implements MagicStore with a single conditional UPDATE (compare-and-swap)
// so two concurrent requests for the same token cannot both succeed.
func (s *PostgresMagicStore) Consume(ctx context.Context, tokenHash string, now time.Time) (MagicLinkRecord, error) {
	var rec MagicLinkRecord
	var usedAt time.Time
	err := s.db.QueryRowContext(ctx, `
		UPDATE magic_links
		SET used_at = $2
		WHERE token_hash = $1 AND used_at IS NULL AND expires_at > $2
		RETURNING token_hash, user_id, email, expires_at, used_at, created_at
	`, tokenHash, now.UTC()).Scan(
		&rec.TokenHash,
		&rec.UserID,
		&rec.Email,
		&rec.ExpiresAt,
		&usedAt,
		&rec.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return MagicLinkRecord{}, ErrMagicLinkInvalid
		}
		return MagicLinkRecord{}, fmt.Errorf("auth: consume magic link: %w", err)
	}
	rec.UsedAt = &usedAt
	return rec, nil
}

// CountRecent implements MagicStore.
func (s *PostgresMagicStore) CountRecent(ctx context.Context, email string, since time.Time) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM magic_links
		WHERE email = lower($1) AND created_at > $2
	`, strings.TrimSpace(email), since.UTC()).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("auth: count recent magic links: %w", err)
	}
	return count, nil
}
