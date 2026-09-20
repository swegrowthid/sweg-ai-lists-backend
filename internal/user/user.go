package user

import (
	"errors"
	"time"
)

// User is the domain entity. Keep it small; grow only on demand.
// PasswordHash never leaves the server (json:"-").
type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// ErrNotFound signals a missing user. Services map it to 404.
var ErrNotFound = errors.New("user: not found")
