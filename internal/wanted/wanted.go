// Package wanted serves the public wanted list: one row per post the
// community marked as wanted. Reads and writes are public, no login needed.
// A duplicate POST is idempotent: it answers the stored post again instead
// of failing.
package wanted

import "errors"

var (
	// ErrInvalidInput signals a blank slug or one carrying a NUL byte.
	ErrInvalidInput = errors.New("wanted: invalid input")

	// ErrUnknownPost signals a slug with no post row behind it.
	ErrUnknownPost = errors.New("wanted: post not found")

	// ErrNotFound signals a post slug that is not on the wanted list.
	ErrNotFound = errors.New("wanted: entry not found")
)

// AddInput is the validated input for the add use case: one post slug.
type AddInput struct {
	Slug string
}
