package user

import "errors"

// User is the domain entity. Keep it small; grow only on demand.
type User struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ErrNotFound signals a missing user. Services map it to 404.
var ErrNotFound = errors.New("user: not found")
