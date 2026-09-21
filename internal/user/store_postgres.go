package user

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
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

// Create implements Store with a parameterized insert.
func (s *PostgresStore) Create(ctx context.Context, input CreateInput) (User, error) {
	var created User
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO users (username, email, password_hash)
		VALUES ($1, $2, $3)
		RETURNING id, username, email, password_hash, created_at, updated_at
	`, input.Username, input.Email, input.PasswordHash).Scan(
		&created.ID,
		&created.Username,
		&created.Email,
		&created.PasswordHash,
		&created.CreatedAt,
		&created.UpdatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return User{}, ErrConflict
		}
		return User{}, fmt.Errorf("user: create: %w", err)
	}
	return created, nil
}

// FindByUsernameOrEmail implements Store. Username matches exactly,
// email matches lowercase-folded (emails are stored lowercase).
func (s *PostgresStore) FindByUsernameOrEmail(ctx context.Context, identifier string) (User, error) {
	var u User
	err := s.db.QueryRowContext(ctx, `
		SELECT id, username, email, password_hash, created_at, updated_at
		FROM users
		WHERE username = $1 OR email = lower($2)
	`, identifier, identifier).Scan(
		&u.ID,
		&u.Username,
		&u.Email,
		&u.PasswordHash,
		&u.CreatedAt,
		&u.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrNotFound
		}
		return User{}, fmt.Errorf("user: find by identifier: %w", err)
	}
	return u, nil
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
