package user

import (
	"context"
	"database/sql"
	"fmt"
)

// PostgresStore reads users from Postgres via database/sql.
type PostgresStore struct {
	db *sql.DB
}

// NewPostgresStore wires the pool. Nil pool is a programmer bug.
func NewPostgresStore(db *sql.DB) *PostgresStore {
	if db == nil {
		panic("user: nil DB")
	}
	return &PostgresStore{db: db}
}

// List implements Store. It never selects password_hash.
func (s *PostgresStore) List(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, username, email, created_at, updated_at FROM users ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("user: list query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	users := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, fmt.Errorf("user: list scan: %w", err)
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("user: list rows: %w", err)
	}
	return users, nil
}
