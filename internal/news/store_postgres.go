package news

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
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
		INSERT INTO news (title, url, source, summary, published_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (url) DO UPDATE SET
			title = EXCLUDED.title,
			source = EXCLUDED.source,
			summary = EXCLUDED.summary,
			published_at = EXCLUDED.published_at
		WHERE (news.title, news.source, news.summary, news.published_at)
		      IS DISTINCT FROM (EXCLUDED.title, EXCLUDED.source, EXCLUDED.summary, EXCLUDED.published_at)
		RETURNING id, title, url, source, summary, published_at, created_at, updated_at
	`, input.Title, input.URL, input.Source, input.Summary, input.PublishedAt).Scan(
		&stored.ID,
		&stored.Title,
		&stored.URL,
		&stored.Source,
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
		SELECT id, title, url, source, summary, published_at, created_at, updated_at
		FROM news
		WHERE url = $1
	`, entryURL).Scan(
		&stored.ID,
		&stored.Title,
		&stored.URL,
		&stored.Source,
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

// sourceExpr is the normalized source name: rows written before the source
// column existed carry "" and belong to the zainfathoni.com listing.
const sourceExpr = `COALESCE(NULLIF(source, ''), 'zainfathoni.com')`

// ListPage implements Store: a filtered, ordered, paginated read plus the
// total match count. PerPage <= 0 drops the LIMIT clause entirely.
func (s *PostgresStore) ListPage(ctx context.Context, filter StoreFilter) ([]Entry, int, error) {
	where, args := newsWhere(filter)

	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM news WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("news: count query: %w", err)
	}

	pageArgs := append([]any{}, args...)
	limit := ""
	if filter.PerPage > 0 {
		offset := 0
		if filter.Page > 1 {
			offset = (filter.Page - 1) * filter.PerPage
		}
		pageArgs = append(pageArgs, filter.PerPage, offset)
		limit = fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(pageArgs)-1, len(pageArgs))
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, title, url, `+sourceExpr+` AS source, summary, published_at, created_at, updated_at
		FROM news
		WHERE `+where+`
		ORDER BY published_at DESC, id
	`+limit, pageArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("news: list query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	entries := []Entry{}
	for rows.Next() {
		var stored Entry
		if err := rows.Scan(
			&stored.ID,
			&stored.Title,
			&stored.URL,
			&stored.Source,
			&stored.Summary,
			&stored.PublishedAt,
			&stored.CreatedAt,
			&stored.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("news: list scan: %w", err)
		}
		normalizeTimes(&stored)
		entries = append(entries, stored)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("news: list rows: %w", err)
	}
	return entries, total, nil
}

// SourceCounts implements Store: one grouped count per normalized source.
func (s *PostgresStore) SourceCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+sourceExpr+`, count(*)
		FROM news
		GROUP BY 1
	`)
	if err != nil {
		return nil, fmt.Errorf("news: source counts query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	counts := map[string]int{}
	for rows.Next() {
		var name string
		var count int
		if err := rows.Scan(&name, &count); err != nil {
			return nil, fmt.Errorf("news: source counts scan: %w", err)
		}
		counts[name] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("news: source counts rows: %w", err)
	}
	return counts, nil
}

// newsWhere builds the shared WHERE clause for ListPage's count and page
// queries. An empty filter matches every row.
func newsWhere(filter StoreFilter) (string, []any) {
	conds := []string{}
	args := []any{}
	if filter.Query != "" {
		args = append(args, escapeLikePattern(filter.Query))
		n := len(args)
		conds = append(conds, fmt.Sprintf(
			"(title ILIKE '%%' || $%d || '%%' OR summary ILIKE '%%' || $%d || '%%' OR %s ILIKE '%%' || $%d || '%%')",
			n, n, sourceExpr, n))
	}
	if len(filter.Sources) > 0 {
		args = append(args, filter.Sources)
		conds = append(conds, fmt.Sprintf("%s = ANY($%d::text[])", sourceExpr, len(args)))
	}
	if len(conds) == 0 {
		return "TRUE", args
	}
	return strings.Join(conds, " AND "), args
}

// escapeLikePattern makes the search text literal: Postgres reads %, _, and
// backslash as pattern syntax in ILIKE.
func escapeLikePattern(query string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
}

// normalizeTimes moves every scanned timestamp to UTC, so the API contract
// never depends on the database session time zone. The memory store already
// works in UTC.
func normalizeTimes(entry *Entry) {
	entry.PublishedAt = entry.PublishedAt.UTC()
	entry.CreatedAt = entry.CreatedAt.UTC()
	entry.UpdatedAt = entry.UpdatedAt.UTC()
}
