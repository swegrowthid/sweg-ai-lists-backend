package user

import (
	"context"
	"errors"
)

// ErrPostgresUnwired marks the postgres store as not connected yet.
// Wire a real *sql.DB here when migrations land. Until then, app uses memory.
var ErrPostgresUnwired = errors.New("user: postgres store not wired")

// PostgresStore is the future SQL implementation. Stub only.
type PostgresStore struct{}

// NewPostgresStore builds the stub. Keep the constructor so wiring stays stable.
func NewPostgresStore() *PostgresStore { return &PostgresStore{} }

// List implements Store. Always fails until SQL lands.
func (*PostgresStore) List(context.Context) ([]User, error) {
	return nil, ErrPostgresUnwired
}
