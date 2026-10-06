package wanted

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

// PostgresStore reads and writes the wanted list via database/sql.
type PostgresStore struct {
	db *sql.DB
}

// NewPostgresStore wires the pool. Nil pool is a programmer bug.
func NewPostgresStore(db *sql.DB) *PostgresStore {
	if db == nil {
		panic("wanted: nil DB")
	}
	return &PostgresStore{db: db}
}

// Add implements Store with one statement: a new post id is inserted, a known
// id inserts nothing. RowsAffected tells the two cases apart, so a duplicate
// POST stays idempotent without a read-modify-write race.
func (s *PostgresStore) Add(ctx context.Context, postID string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO wanted_posts (post_id)
		VALUES ($1)
		ON CONFLICT (post_id) DO NOTHING
	`, postID)
	if err != nil {
		if isForeignKeyViolation(err) {
			return false, ErrUnknownPost
		}
		return false, fmt.Errorf("wanted: add: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("wanted: add rows: %w", err)
	}
	return affected == 1, nil
}

// ListIDs implements Store, newest-wanted first, post id as the tie-break.
func (s *PostgresStore) ListIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT post_id
		FROM wanted_posts
		ORDER BY created_at DESC, post_id
	`)
	if err != nil {
		return nil, fmt.Errorf("wanted: list query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("wanted: list scan: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("wanted: list rows: %w", err)
	}
	return ids, nil
}

// Delete implements Store. No matching row is ErrNotFound.
func (s *PostgresStore) Delete(ctx context.Context, postID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM wanted_posts WHERE post_id = $1`, postID)
	if err != nil {
		return fmt.Errorf("wanted: delete: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("wanted: delete rows: %w", err)
	}
	if deleted == 0 {
		return ErrNotFound
	}
	return nil
}

// isForeignKeyViolation reports SQLSTATE 23503, a missing referenced row.
// Here it means the post row vanished between the service lookup and this
// insert.
func isForeignKeyViolation(err error) bool { return hasPgCode(err, "23503") }

func hasPgCode(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code
}
