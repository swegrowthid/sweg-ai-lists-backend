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

// ListFilter narrows the catalog list. Category is a slug; Query is a
// case-insensitive substring on the tool name.
type ListFilter struct {
	Category string
	Query    string
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
