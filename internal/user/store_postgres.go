package user

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

const (
	pgUniqueViolation           = "23505"
	pgForeignKeyViolation       = "23503"
	pgInvalidTextRepresentation = "22P02"

	constraintUsersUsername    = "users_username_key"
	constraintUsersEmail       = "users_email_key"
	constraintIdentityPK       = "user_identities_pkey"
	constraintIdentityUserProv = "user_identities_user_provider_key"
)

// querier is the subset of *sql.DB and *sql.Tx the user queries need,
// so Create shares one insert path inside and outside a transaction.
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

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

// Create implements Store. An external identity needs both rows, so the
// user and the identity insert share one transaction.
func (s *PostgresStore) Create(ctx context.Context, input CreateInput) (User, error) {
	if input.Identity == nil {
		return insertUser(ctx, s.db, input)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, fmt.Errorf("user: create begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	created, err := insertUser(ctx, tx, input)
	if err != nil {
		return User{}, err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO user_identities (user_id, provider, provider_user_id)
		VALUES ($1, $2, $3)
	`, created.ID, input.Identity.Provider, input.Identity.ProviderUserID)
	if err != nil {
		return User{}, mapConstraintError(err, "user: create identity")
	}

	if err := tx.Commit(); err != nil {
		return User{}, fmt.Errorf("user: create commit: %w", err)
	}
	return created, nil
}

// insertUser writes one users row and maps its unique constraints.
func insertUser(ctx context.Context, q querier, input CreateInput) (User, error) {
	var created User
	err := q.QueryRowContext(ctx, `
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
		return User{}, mapConstraintError(err, "user: create")
	}
	return created, nil
}

// invalidTextRepresentation reports a Postgres 22P02, e.g. a non-UUID value
// compared with a UUID column. Callers treat it as a missing row.
func invalidTextRepresentation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgInvalidTextRepresentation
}

// mapConstraintError turns a Postgres constraint violation into the matching
// domain error. Every other failure is wrapped with prefix.
func mapConstraintError(err error, prefix string) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return fmt.Errorf("%s: %w", prefix, err)
	}
	if pgErr.Code != pgUniqueViolation {
		return fmt.Errorf("%s: %w", prefix, err)
	}
	switch pgErr.ConstraintName {
	case constraintIdentityPK, constraintIdentityUserProv:
		return ErrLinkedElsewhere
	case constraintUsersUsername, constraintUsersEmail:
		return ErrConflict
	default:
		return fmt.Errorf("%s: unmapped constraint %q: %w", prefix, pgErr.ConstraintName, err)
	}
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

// FindByEmail implements Store. Email matches lowercase-folded.
func (s *PostgresStore) FindByEmail(ctx context.Context, email string) (User, error) {
	var u User
	err := s.db.QueryRowContext(ctx, `
		SELECT id, username, email, password_hash, created_at, updated_at
		FROM users
		WHERE email = lower($1)
	`, email).Scan(
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
		return User{}, fmt.Errorf("user: find by email: %w", err)
	}
	return u, nil
}

// FindByID implements Store.
func (s *PostgresStore) FindByID(ctx context.Context, id string) (User, error) {
	var u User
	err := s.db.QueryRowContext(ctx, `
		SELECT id, username, email, password_hash, created_at, updated_at
		FROM users
		WHERE id = $1
	`, id).Scan(
		&u.ID,
		&u.Username,
		&u.Email,
		&u.PasswordHash,
		&u.CreatedAt,
		&u.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || invalidTextRepresentation(err) {
			return User{}, ErrNotFound
		}
		return User{}, fmt.Errorf("user: find by id: %w", err)
	}
	return u, nil
}

// FindByIdentity implements Store. It returns the user linked to the
// external account.
func (s *PostgresStore) FindByIdentity(ctx context.Context, provider, providerUserID string) (User, error) {
	var u User
	err := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.email, u.password_hash, u.created_at, u.updated_at
		FROM users u
		JOIN user_identities i ON i.user_id = u.id
		WHERE i.provider = $1 AND i.provider_user_id = $2
	`, provider, providerUserID).Scan(
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
		return User{}, fmt.Errorf("user: find by identity: %w", err)
	}
	return u, nil
}

// UpdatePasswordHash implements Store. Zero rows means the user is gone.
func (s *PostgresStore) UpdatePasswordHash(ctx context.Context, id, passwordHash string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE users
		SET password_hash = $2, updated_at = now()
		WHERE id = $1
	`, id, passwordHash)
	if err != nil {
		if invalidTextRepresentation(err) {
			return ErrNotFound
		}
		return fmt.Errorf("user: update password: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("user: update password rows: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// LinkIdentity implements Store. Re-linking the same account is a no-op;
// a second account for the same provider or an account owned by another
// user returns ErrLinkedElsewhere. It does not touch users.updated_at: the
// users row does not change, and linked_at records the link.
func (s *PostgresStore) LinkIdentity(ctx context.Context, userID, provider, providerUserID string) error {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO user_identities (user_id, provider, provider_user_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (provider, provider_user_id) DO NOTHING
	`, userID, provider, providerUserID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch {
			case pgErr.Code == pgUniqueViolation:
				return ErrLinkedElsewhere
			case pgErr.Code == pgForeignKeyViolation:
				return ErrNotFound
			}
		}
		return fmt.Errorf("user: link identity: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("user: link identity rows: %w", err)
	}
	if affected == 1 {
		return nil
	}

	// The account already has a row: it is this user's, or someone else's.
	var owner string
	err = s.db.QueryRowContext(ctx, `
		SELECT user_id FROM user_identities
		WHERE provider = $1 AND provider_user_id = $2
	`, provider, providerUserID).Scan(&owner)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrLinkedElsewhere
		}
		return fmt.Errorf("user: link identity owner: %w", err)
	}
	if owner == userID {
		return nil
	}
	return ErrLinkedElsewhere
}

// UnlinkIdentity implements Store. One guarded statement removes the row
// only while the user still holds a password or another identity, so two
// racing unlinks can never strand the account. Zero affected rows are
// ambiguous: the link was absent, or the guard rejected it. One extra read
// tells them apart for the 404 vs 409 mapping.
func (s *PostgresStore) UnlinkIdentity(ctx context.Context, userID, provider string) error {
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM user_identities
		WHERE user_id = $1 AND provider = $2
		AND (
			EXISTS (SELECT 1 FROM users u WHERE u.id = $1 AND u.password_hash <> '')
			OR (SELECT count(*) FROM user_identities x WHERE x.user_id = $1) >= 2
		)
	`, userID, provider)
	if err != nil {
		if invalidTextRepresentation(err) {
			return ErrNotFound
		}
		return fmt.Errorf("user: unlink identity: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("user: unlink identity rows: %w", err)
	}
	if affected == 1 {
		return nil
	}

	var count int
	err = s.db.QueryRowContext(ctx, `
		SELECT count(*) FROM user_identities
		WHERE user_id = $1 AND provider = $2
	`, userID, provider).Scan(&count)
	if err != nil {
		return fmt.Errorf("user: unlink identity probe: %w", err)
	}
	if count == 0 {
		return ErrNotFound
	}
	return ErrLastCredential
}

// ListProfiles implements Store. One query feeds the whole list; rows are
// grouped by user in Go to avoid array scanning.
func (s *PostgresStore) ListProfiles(ctx context.Context) ([]Profile, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT u.id, u.username, u.email, u.created_at, u.updated_at, i.provider
		FROM users u
		LEFT JOIN user_identities i ON i.user_id = u.id
		ORDER BY u.created_at, u.id, i.provider
	`)
	if err != nil {
		return nil, fmt.Errorf("user: list profiles query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	profiles := []Profile{}
	position := map[string]int{}
	for rows.Next() {
		var (
			row      Profile
			provider sql.NullString
		)
		if err := rows.Scan(&row.ID, &row.Username, &row.Email, &row.CreatedAt, &row.UpdatedAt, &provider); err != nil {
			return nil, fmt.Errorf("user: list profiles scan: %w", err)
		}
		if at, seen := position[row.ID]; seen {
			if provider.Valid {
				profiles[at].Providers = append(profiles[at].Providers, provider.String)
			}
			continue
		}
		row.Providers = []string{}
		if provider.Valid {
			row.Providers = append(row.Providers, provider.String)
		}
		position[row.ID] = len(profiles)
		profiles = append(profiles, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("user: list profiles rows: %w", err)
	}
	return profiles, nil
}

// FindProfileByID implements Store. The password hash is never selected.
func (s *PostgresStore) FindProfileByID(ctx context.Context, id string) (Profile, error) {
	return s.findProfile(ctx, `
		SELECT id, username, email, created_at, updated_at
		FROM users
		WHERE id = $1
	`, id)
}

// FindProfileByEmail implements Store. Email matches lowercase-folded.
func (s *PostgresStore) FindProfileByEmail(ctx context.Context, email string) (Profile, error) {
	return s.findProfile(ctx, `
		SELECT id, username, email, created_at, updated_at
		FROM users
		WHERE email = lower($1)
	`, email)
}

// findProfile reads one user and then its providers, in two queries.
func (s *PostgresStore) findProfile(ctx context.Context, query, value string) (Profile, error) {
	var p Profile
	err := s.db.QueryRowContext(ctx, query, value).Scan(
		&p.ID,
		&p.Username,
		&p.Email,
		&p.CreatedAt,
		&p.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || invalidTextRepresentation(err) {
			return Profile{}, ErrNotFound
		}
		return Profile{}, fmt.Errorf("user: find profile: %w", err)
	}

	providers, err := s.providers(ctx, p.ID)
	if err != nil {
		return Profile{}, err
	}
	p.Providers = providers
	return p, nil
}

// providers lists the sorted providers linked to one user.
func (s *PostgresStore) providers(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT provider FROM user_identities
		WHERE user_id = $1
		ORDER BY provider
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("user: list providers query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	providers := []string{}
	for rows.Next() {
		var provider string
		if err := rows.Scan(&provider); err != nil {
			return nil, fmt.Errorf("user: list providers scan: %w", err)
		}
		providers = append(providers, provider)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("user: list providers rows: %w", err)
	}
	return providers, nil
}
