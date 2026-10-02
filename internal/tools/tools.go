// Package tools serves the AI tools catalog from the swegrowthid/tools-ai-swe-growth
// repository. The three markdown files there are the database: a scheduler
// fetches and parses them once at startup and then every day at midnight
// server time, and keeps the result as an in-memory snapshot. No SQL table
// mirrors the catalog; the markdown stays the single source of truth.
package tools

import "errors"

// Tool is one catalog row. Common fields exist on every row; the per-category
// fields are nil or empty when the source table does not carry the column.
type Tool struct {
	ID           string   `json:"id"`
	Category     string   `json:"category"`
	Name         string   `json:"name"`
	Website      string   `json:"website"`
	WebsiteLabel string   `json:"website_label"`
	Status       string   `json:"status"`
	Updated      string   `json:"updated"`
	TopUp        *bool    `json:"top_up,omitempty"`
	Subscribe    *bool    `json:"subscribe,omitempty"`
	MinSpend     string   `json:"min_spend,omitempty"`
	AIFeatures   []string `json:"ai_features,omitempty"`
}

// Category maps one source markdown file to one catalog category. Count is
// the number of rows in the last successful sync.
type Category struct {
	Slug       string `json:"slug"`
	Name       string `json:"name"`
	Prefix     string `json:"prefix"`
	SourceFile string `json:"source_file"`
	SourceURL  string `json:"source_url"`
	Count      int    `json:"count"`
}

// Sort keys accepted by ListFilter.Sort. An empty key keeps the source
// catalog order instead of reordering the snapshot.
const (
	// SortName orders by the tool name, case-insensitive.
	SortName = "name"
	// SortUpdated orders by the source updated date, YYYY-MM-DD.
	SortUpdated = "updated"
)

// Sort directions accepted by ListFilter.Order.
const (
	// OrderAsc is oldest/smallest first.
	OrderAsc = "asc"
	// OrderDesc is newest/largest first.
	OrderDesc = "desc"
)

// Paging bounds for ListFilter.PerPage.
const (
	// DefaultPerPage is the page size when the client sends none.
	DefaultPerPage = 20
	// MaxPerPage is the largest page size the catalog will serve.
	MaxPerPage = 100
)

// ListFilter narrows and shapes the catalog list. Category holds a group name
// or a category slug; Query is a case-insensitive substring on the tool name.
// Sort and Order are empty for source catalog order, otherwise SortName or
// SortUpdated paired with OrderAsc or OrderDesc. Page is 1-based and PerPage is
// capped at MaxPerPage; both default when left at zero.
type ListFilter struct {
	Category string
	Query    string
	Sort     string
	Order    string
	Page     int
	PerPage  int
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

// ListResult is the GET /tools body: one page of rows plus paging metadata.
// The envelope is always present, even on an empty page, so a client can read
// the total without guessing whether there is more to fetch.
type ListResult struct {
	Data []Tool   `json:"data"`
	Meta PageMeta `json:"meta"`
}

// Sentinel errors for the E channel. Handlers map them to status codes.
var (
	// ErrNotFound: no tool carries the requested id.
	ErrNotFound = errors.New("tools: not found")
	// ErrUnknownCategory: the category filter names no known category.
	ErrUnknownCategory = errors.New("tools: unknown category")
	// ErrInvalidInput: a filter carries a byte the catalog cannot hold.
	ErrInvalidInput = errors.New("tools: invalid input")
)
