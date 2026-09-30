package news

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// PostgresStore reads and writes news via database/sql.
type PostgresStore struct {
	db *sql.DB
}

// NewPostgresStore wires the pool. Nil pool is a programmer bug.
func NewPostgresStore(db *sql.DB) *PostgresStore {
	if db == nil {
		panic("news: nil DB")
	}
	return &PostgresStore{db: db}
}

// Upsert implements Store with one statement: a new URL is inserted, a known
// URL updates only when a stored field changed, so updated_at keeps meaning
// "last time the source changed". An unchanged row is read back instead.
func (s *PostgresStore) Upsert(ctx context.Context, input UpsertInput) (Entry, error) {
	var stored Entry
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO news (title, url, summary, published_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (url) DO UPDATE SET
			title = EXCLUDED.title,
			summary = EXCLUDED.summary,
			published_at = EXCLUDED.published_at
		WHERE (news.title, news.summary, news.published_at)
		      IS DISTINCT FROM (EXCLUDED.title, EXCLUDED.summary, EXCLUDED.published_at)
		RETURNING id, title, url, summary, published_at, created_at, updated_at
	`, input.Title, input.URL, input.Summary, input.PublishedAt).Scan(
		&stored.ID,
		&stored.Title,
		&stored.URL,
		&stored.Summary,
		&stored.PublishedAt,
		&stored.CreatedAt,
		&stored.UpdatedAt,
	)
	if err == nil {
		normalizeTimes(&stored)
		return stored, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Entry{}, fmt.Errorf("news: upsert: %w", err)
	}
	return s.findByURL(ctx, input.URL)
}

// findByURL reads one row by its URL. A missing row here means the row
// vanished between the upsert and the read-back: a surprising failure.
func (s *PostgresStore) findByURL(ctx context.Context, entryURL string) (Entry, error) {
	var stored Entry
	err := s.db.QueryRowContext(ctx, `
		SELECT id, title, url, summary, published_at, created_at, updated_at
		FROM news
		WHERE url = $1
	`, entryURL).Scan(
		&stored.ID,
		&stored.Title,
		&stored.URL,
		&stored.Summary,
		&stored.PublishedAt,
		&stored.CreatedAt,
		&stored.UpdatedAt,
	)
	if err != nil {
		return Entry{}, fmt.Errorf("news: read back: %w", err)
	}
	normalizeTimes(&stored)
	return stored, nil
}

// List implements Store, newest published first, id as the tie-break.
func (s *PostgresStore) List(ctx context.Context) ([]Entry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, title, url, summary, published_at, created_at, updated_at
		FROM news
		ORDER BY published_at DESC, id
	`)
	if err != nil {
		return nil, fmt.Errorf("news: list query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	entries := []Entry{}
	for rows.Next() {
		var stored Entry
		if err := rows.Scan(
			&stored.ID,
			&stored.Title,
			&stored.URL,
			&stored.Summary,
			&stored.PublishedAt,
			&stored.CreatedAt,
			&stored.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("news: list scan: %w", err)
		}
		normalizeTimes(&stored)
		entries = append(entries, stored)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("news: list rows: %w", err)
	}
	return entries, nil
}

// normalizeTimes moves every scanned timestamp to UTC, so the API contract
// never depends on the database session time zone. The memory store already
// works in UTC.
func normalizeTimes(entry *Entry) {
	entry.PublishedAt = entry.PublishedAt.UTC()
	entry.CreatedAt = entry.CreatedAt.UTC()
	entry.UpdatedAt = entry.UpdatedAt.UTC()
}
