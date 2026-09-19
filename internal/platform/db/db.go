package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// Pinger is anything readiness can ping. Stores adapt to it.
// Pool implements it, so pass the pool directly.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Pool wraps sql.DB so it matches Pinger.
// Keep one type for ping, query, and close.
type Pool struct {
	*sql.DB
}

// Ping implements Pinger via PingContext.
func (p *Pool) Ping(ctx context.Context) error { return p.DB.PingContext(ctx) }

// NoopPinger always reports healthy. Use only in tests.
type NoopPinger struct{}

// Ping implements Pinger.
func (NoopPinger) Ping(context.Context) error { return nil }

// Open builds a Postgres pool and verifies it with one ping.
// Fail-fast: return error when DSN empty or ping fails.
func Open(ctx context.Context, dsn string) (*Pool, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, errors.New("db: DB_URL is empty")
	}
	raw, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("db: open pool: %w", err)
	}
	raw.SetMaxOpenConns(10)
	raw.SetMaxIdleConns(5)
	raw.SetConnMaxLifetime(30 * time.Minute)
	raw.SetConnMaxIdleTime(5 * time.Minute)

	pool := &Pool{DB: raw}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		_ = pool.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}
	return pool, nil
}
