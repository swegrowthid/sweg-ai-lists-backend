package post

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// querier is the subset shared by *sql.DB and *sql.Tx, so one read path serves
// plain reads and the create read-back inside its transaction.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// PostgresStore reads and writes posts via database/sql.
type PostgresStore struct {
	db *sql.DB
}

// NewPostgresStore wires the pool. Nil pool is a programmer bug.
func NewPostgresStore(db *sql.DB) *PostgresStore {
	if db == nil {
		panic("post: nil DB")
	}
	return &PostgresStore{db: db}
}

// CreateCategory implements Store with a parameterized insert.
func (s *PostgresStore) CreateCategory(ctx context.Context, input CreateCategoryInput) (Category, error) {
	var created Category
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO categories (slug, name)
		VALUES ($1, $2)
		RETURNING id, slug, name, created_at, updated_at
	`, input.Slug, input.Name).Scan(
		&created.ID,
		&created.Slug,
		&created.Name,
		&created.CreatedAt,
		&created.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return Category{}, ErrConflict
		}
		return Category{}, fmt.Errorf("post: create category: %w", err)
	}
	return created, nil
}

// ListCategories implements Store in name order.
func (s *PostgresStore) ListCategories(ctx context.Context) ([]Category, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, slug, name, created_at, updated_at
		FROM categories
		ORDER BY name, id
	`)
	if err != nil {
		return nil, fmt.Errorf("post: list categories query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	categories := []Category{}
	for rows.Next() {
		var category Category
		if err := rows.Scan(
			&category.ID,
			&category.Slug,
			&category.Name,
			&category.CreatedAt,
			&category.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("post: list categories scan: %w", err)
		}
		categories = append(categories, category)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("post: list categories rows: %w", err)
	}
	return categories, nil
}

// Create implements Store. One transaction writes the post, its category
// links, and its items, so a partial post never lands in the table.
func (s *PostgresStore) Create(ctx context.Context, input CreatePostInput) (Post, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Post{}, fmt.Errorf("post: begin create: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op once the commit succeeds

	postID, err := insertPost(ctx, tx, input)
	if err != nil {
		return Post{}, err
	}
	if err := linkCategories(ctx, tx, postID, input.CategorySlugs); err != nil {
		return Post{}, err
	}
	if err := insertItems(ctx, tx, postID, input.Items); err != nil {
		return Post{}, err
	}
	created, err := findBySlug(ctx, tx, input.Slug)
	if err != nil {
		return Post{}, err
	}
	if err := tx.Commit(); err != nil {
		return Post{}, fmt.Errorf("post: commit create: %w", err)
	}
	return created, nil
}

// List implements Store, newest first. Categories come from the join; items
// stay out on purpose, so list payloads stay small.
func (s *PostgresStore) List(ctx context.Context, filter ListFilter) ([]Post, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.slug, p.title, p.author_id, p.created_at, p.updated_at,
		       c.id, c.slug, c.name, c.created_at, c.updated_at
		FROM posts p
		JOIN post_categories pc ON pc.post_id = p.id
		JOIN categories c ON c.id = pc.category_id
		WHERE ($1 = '' OR EXISTS (
		         SELECT 1
		         FROM post_categories fpc
		         JOIN categories fc ON fc.id = fpc.category_id
		         WHERE fpc.post_id = p.id AND fc.slug = $1))
		  AND ($2 = '' OR p.title ILIKE '%' || $2 || '%')
		ORDER BY p.created_at DESC, p.id, c.name, c.slug
	`, filter.CategorySlug, escapeLikePattern(filter.Query))
	if err != nil {
		return nil, fmt.Errorf("post: list query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	posts, err := scanPosts(rows)
	if err != nil {
		return nil, err
	}
	return posts, nil
}

// FindBySlug implements Store with categories and items in display order.
func (s *PostgresStore) FindBySlug(ctx context.Context, slug string) (Post, error) {
	return findBySlug(ctx, s.db, slug)
}

// insertPost writes the post row and returns its generated id.
func insertPost(ctx context.Context, q querier, input CreatePostInput) (string, error) {
	var postID string
	err := q.QueryRowContext(ctx, `
		INSERT INTO posts (slug, title, author_id)
		VALUES ($1, $2, $3)
		RETURNING id
	`, input.Slug, input.Title, input.AuthorID).Scan(&postID)
	if err != nil {
		switch {
		case isUniqueViolation(err):
			return "", ErrConflict
		case isForeignKeyViolation(err):
			return "", ErrUnknownAuthor
		}
		return "", fmt.Errorf("post: insert post: %w", err)
	}
	return postID, nil
}

// linkCategories writes one row per slug. The insert selects from categories,
// so a missing slug inserts fewer rows than asked and the count check fails.
func linkCategories(ctx context.Context, q querier, postID string, slugs []string) error {
	result, err := q.ExecContext(ctx, `
		INSERT INTO post_categories (post_id, category_id)
		SELECT $1, id FROM categories WHERE slug = ANY($2)
	`, postID, slugs)
	if err != nil {
		return fmt.Errorf("post: link categories: %w", err)
	}
	linked, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("post: link categories rows: %w", err)
	}
	if int(linked) != len(slugs) {
		return ErrUnknownCategory
	}
	return nil
}

// insertItems writes every item in the caller's transaction. A nil payload
// pointer arrives as SQL NULL, so only the fields of the item's kind are set.
func insertItems(ctx context.Context, q querier, postID string, items []ItemInput) error {
	for _, item := range items {
		_, err := q.ExecContext(ctx, `
			INSERT INTO post_items (post_id, kind, position, body_text, url, filename, mime)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`,
			postID,
			item.Kind,
			item.Position,
			item.BodyText,
			item.URL,
			item.Filename,
			item.MIME,
		)
		if err != nil {
			return fmt.Errorf("post: insert item: %w", err)
		}
	}
	return nil
}

// findBySlug reads one post with its categories, then its items. A post with
// no matching row is ErrNotFound.
func findBySlug(ctx context.Context, q querier, slug string) (Post, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT p.id, p.slug, p.title, p.author_id, p.created_at, p.updated_at,
		       c.id, c.slug, c.name, c.created_at, c.updated_at
		FROM posts p
		JOIN post_categories pc ON pc.post_id = p.id
		JOIN categories c ON c.id = pc.category_id
		WHERE p.slug = $1
		ORDER BY c.name, c.slug
	`, slug)
	if err != nil {
		return Post{}, fmt.Errorf("post: find query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	posts, err := scanPosts(rows)
	if err != nil {
		return Post{}, err
	}
	if len(posts) == 0 {
		return Post{}, ErrNotFound
	}
	found := posts[0]
	items, err := findItems(ctx, q, found.ID)
	if err != nil {
		return Post{}, err
	}
	found.Items = items
	return found, nil
}

// findItems reads one post's items in display order.
func findItems(ctx context.Context, q querier, postID string) ([]Item, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT id, kind, position, body_text, url, filename, mime
		FROM post_items
		WHERE post_id = $1
		ORDER BY position
	`, postID)
	if err != nil {
		return nil, fmt.Errorf("post: find items query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := []Item{}
	for rows.Next() {
		var item Item
		if err := rows.Scan(
			&item.ID,
			&item.Kind,
			&item.Position,
			&item.BodyText,
			&item.URL,
			&item.Filename,
			&item.MIME,
		); err != nil {
			return nil, fmt.Errorf("post: find items scan: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("post: find items rows: %w", err)
	}
	return items, nil
}

// scanPosts groups the post JOIN category rows into posts, keeping the query's
// post order. Every post carries at least one category, so the join never
// drops a post.
func scanPosts(rows *sql.Rows) ([]Post, error) {
	posts := []Post{}
	indexByID := map[string]int{}
	for rows.Next() {
		var (
			post     Post
			category Category
		)
		if err := rows.Scan(
			&post.ID, &post.Slug, &post.Title, &post.AuthorID, &post.CreatedAt, &post.UpdatedAt,
			&category.ID, &category.Slug, &category.Name, &category.CreatedAt, &category.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("post: scan: %w", err)
		}
		index, seen := indexByID[post.ID]
		if !seen {
			index = len(posts)
			indexByID[post.ID] = index
			posts = append(posts, post)
		}
		posts[index].Categories = append(posts[index].Categories, category)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("post: rows: %w", err)
	}
	return posts, nil
}

// escapeLikePattern makes the search text literal: Postgres reads %, _, and
// backslash as pattern syntax in ILIKE, the memory store reads them as plain
// characters. Escaping here keeps both stores on the same semantics.
func escapeLikePattern(query string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
}

// isUniqueViolation reports SQLSTATE 23505, a unique index conflict.
func isUniqueViolation(err error) bool { return hasPgCode(err, "23505") }

// isForeignKeyViolation reports SQLSTATE 23503, a missing referenced row.
func isForeignKeyViolation(err error) bool { return hasPgCode(err, "23503") }

func hasPgCode(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code
}
