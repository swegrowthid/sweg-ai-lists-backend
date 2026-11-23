// Package news serves the community news list: AI news entries ingested from
// several configured sources (RSS/Atom feeds plus one HTML listing). A
// scheduler syncs the list once at startup and then every 6 hours.
package news

import (
	"errors"
	"time"
)

// Entry is one news row from a configured source. PublishedAt is the source
// post date. Source carries the source's display name; Group is derived from
// the source registry and never stored.
type Entry struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	URL         string    `json:"url"`
	Source      string    `json:"source"`
	Group       string    `json:"group"`
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

// Lens groups for the news catalog. A reader picks one lens to narrow the
// feed: labs announce models, press covers industry moves, research surfaces
// papers, digest aggregates curated roundups.
const (
	GroupLab      = "lab"
	GroupPress    = "press"
	GroupResearch = "research"
	GroupDigest   = "digest"
)

// Paging bounds for ListFilter.PerPage.
const (
	// DefaultPerPage is the page size when the client sends none.
	DefaultPerPage = 30
	// MaxPerPage is the largest page size the catalog will serve.
	MaxPerPage = 100
)

// ListFilter narrows and shapes the news list. Query is a case-insensitive
// substring over title, summary, and source. Group is one of the lens slugs;
// Source is a source display name. Page is 1-based and PerPage is capped at
// MaxPerPage; both default when left at zero.
type ListFilter struct {
	Query   string
	Group   string
	Source  string
	Page    int
	PerPage int
}

// PageMeta describes the page a list response carries.
type PageMeta struct {
	// Page is the 1-based page number being served.
	Page int `json:"page"`
	// PerPage is the page size the server applied.
	PerPage int `json:"per_page"`
	// Total is how many rows match the filter, across every page.
	Total int `json:"total"`
	// TotalPages is how many pages Total makes at PerPage.
	TotalPages int `json:"total_pages"`
}

// ListResult is the GET /news body: one page of rows plus paging metadata.
type ListResult struct {
	Data []Entry  `json:"data"`
	Meta PageMeta `json:"meta"`
}

// SourceInfo describes one configured source and how many stored rows it
// contributed. Group is the lens it belongs to.
type SourceInfo struct {
	Name  string `json:"name"`
	Group string `json:"group"`
	Count int    `json:"count"`
}

// StoreFilter is the persistence-level form of ListFilter: the service has
// already resolved the group into concrete source names.
type StoreFilter struct {
	Query   string
	Sources []string
	Page    int
	PerPage int
}

// Sentinel errors for the E channel. Handlers map them to status codes.
var (
	// ErrInvalidInput: a filter carries a byte the catalog cannot hold.
	ErrInvalidInput = errors.New("news: invalid input")
	// ErrUnknownGroup: the group filter names no known lens.
	ErrUnknownGroup = errors.New("news: unknown group")
)
