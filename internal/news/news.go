// Package news serves the community news list: AI news entries ingested from
// several configured sources (RSS/Atom feeds plus one HTML listing). A
// scheduler syncs the list once at startup and then every 6 hours.
package news

import "time"

// Entry is one news row from a configured source. PublishedAt is the source
// post date. Source carries the source's display name; legacy rows have "".
type Entry struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	URL         string    `json:"url"`
	Source      string    `json:"source"`
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
	Source      string
	Summary     string
	PublishedAt time.Time
}
