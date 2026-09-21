package auth

import (
	"context"
	"time"
)

// RefreshStore is the persistence port for refresh token lifecycle.
// It tracks jti rows so logout and rotation survive stateless JWTs.
type RefreshStore interface {
	// Save persists a newly issued refresh record.
	Save(ctx context.Context, rec RefreshRecord) error

	// Consume atomically marks jti revoked and returns its record.
	// ErrRefreshInvalid when missing or expired; ErrRefreshReused when
	// already revoked, with the record still carrying UserID.
	Consume(ctx context.Context, jti string, now time.Time) (RefreshRecord, error)

	// Revoke marks jti revoked. Unknown jti is not an error (idempotent logout).
	Revoke(ctx context.Context, jti string) error

	// RevokeAllForUser revokes every live refresh token of userID.
	RevokeAllForUser(ctx context.Context, userID string) error
}
