package tools

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

// PostgresVoteStore reads and writes usage votes via database/sql.
type PostgresVoteStore struct {
	db *sql.DB
}

// NewPostgresVoteStore wires the pool. Nil pool is a programmer bug.
func NewPostgresVoteStore(db *sql.DB) *PostgresVoteStore {
	if db == nil {
		panic("tools: nil DB")
	}
	return &PostgresVoteStore{db: db}
}

// Add implements VoteStore with one statement: a first vote inserts the row,
// a duplicate vote inserts nothing. RowsAffected tells the two cases apart,
// so a repeat POST stays idempotent without a read-modify-write race. A user
// id with no users row fails the foreign key and maps to ErrUnknownUser.
func (s *PostgresVoteStore) Add(ctx context.Context, userID, toolID string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO tool_votes (tool_id, user_id)
		VALUES ($1, $2)
		ON CONFLICT (tool_id, user_id) DO NOTHING
	`, toolID, userID)
	if err != nil {
		if isVoteForeignKeyViolation(err) {
			return false, ErrUnknownUser
		}
		return false, fmt.Errorf("tools: add vote: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("tools: add vote rows: %w", err)
	}
	return affected == 1, nil
}

// Remove implements VoteStore. A missing row deletes nothing and reports
// removed false, so a repeat DELETE stays idempotent.
func (s *PostgresVoteStore) Remove(ctx context.Context, userID, toolID string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM tool_votes
		WHERE tool_id = $1 AND user_id = $2
	`, toolID, userID)
	if err != nil {
		return false, fmt.Errorf("tools: remove vote: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("tools: remove vote rows: %w", err)
	}
	return affected == 1, nil
}

// Summaries implements VoteStore: one grouped count per tool id plus the
// viewer flag. Tool ids without votes have no row and stay absent from the
// map; the service fills the zero default. An empty tool id list skips the
// query. An empty viewer id means an anonymous reader: NULLIF turns the
// compare to NULL so every voted flag comes back false.
func (s *PostgresVoteStore) Summaries(ctx context.Context, viewerID string, toolIDs []string) (map[string]VoteSummary, error) {
	if len(toolIDs) == 0 {
		return map[string]VoteSummary{}, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT
		  tool_id,
		  count(*)::int AS votes,
		  coalesce(bool_or(user_id = NULLIF($2::text, '')::uuid), false) AS voted
		FROM tool_votes
		WHERE tool_id = ANY($1::text[])
		GROUP BY tool_id
	`, toolIDs, viewerID)
	if err != nil {
		return nil, fmt.Errorf("tools: vote summaries query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make(map[string]VoteSummary, len(toolIDs))
	for rows.Next() {
		var id string
		var summary VoteSummary
		if err := rows.Scan(&id, &summary.Votes, &summary.Voted); err != nil {
			return nil, fmt.Errorf("tools: vote summaries scan: %w", err)
		}
		out[id] = summary
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tools: vote summaries rows: %w", err)
	}
	return out, nil
}

// isVoteForeignKeyViolation reports SQLSTATE 23503, a missing referenced row.
// Here it means the voter id names no users row.
func isVoteForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}
