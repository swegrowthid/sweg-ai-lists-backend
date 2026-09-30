// Package news serves the community news list: one row per AI Tools Digest
// post from the zainfathoni.com blog. A scheduler syncs the list once at
// startup and then every day at midnight server time.
package news

import "time"

// Entry is one news row: a source blog post whose title matched the filter.
// PublishedAt is the post date from the source, midnight UTC: the source
// carries a calendar day, never a time of day.
type Entry struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	URL         string    `json:"url"`
	Summary     string    `json:"summary"`
	PublishedAt time.Time `json:"published_at"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// UpsertInput is one entry as the source listing carries it. The store keys
// on URL and fills in id, created_at, and updated_at.
type UpsertInput struct {
	Title       string
	URL         string
	Summary     string
	PublishedAt time.Time
}
