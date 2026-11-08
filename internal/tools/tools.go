// Package tools serves the AI tools catalog from the swegrowthid/tools-ai-swe-growth
// repository. The three markdown files there are the database: a scheduler
// fetches and parses them once at startup and then every day at midnight
// server time, and keeps the result as an in-memory snapshot. No SQL table
// mirrors the catalog; the markdown stays the single source of truth. Usage
// votes live apart in the tool_votes SQL table and join the snapshot only at
// response time.
package tools

import (
	"context"
	"errors"
)

// Tool is one catalog row. Common fields exist on every row; the per-category
// fields are nil or empty when the source table does not carry the column.
// Votes and Voted are view-only: the parser leaves them zero and the service
// fills them from the VoteStore before responding, so the snapshot never
// carries vote state.
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
	Votes        int      `json:"votes"`
	Voted        bool     `json:"voted"`
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
	// SortVotes orders by the vote count. The service sorts after it
	// enriches the rows, because the snapshot store holds no votes.
	SortVotes = "votes"
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
// or a category slug; Query is a case-insensitive substring over name, website
// label, status, minimum spend, and the ADE feature list.
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

// VoteSummary carries the vote state of one tool for one viewer.
type VoteSummary struct {
	// Votes is how many users voted for the tool.
	Votes int
	// Voted is whether the viewer voted for the tool.
	Voted bool
}

// VoteResult is the POST/DELETE /tools/{id}/votes body: the latest count and
// the viewer status after the operation.
type VoteResult struct {
	ToolID string `json:"tool_id"`
	Votes  int    `json:"votes"`
	Voted  bool   `json:"voted"`
}

// VoteStore is the vote persistence port. Service code depends on it, never on
// a concrete store. Add and Remove are idempotent: repeating them changes
// nothing and reports created or removed false.
type VoteStore interface {
	// Add records one usage vote. Created is false for a duplicate vote.
	Add(ctx context.Context, userID, toolID string) (created bool, err error)

	// Remove withdraws one usage vote. Removed is false when no vote exists.
	Remove(ctx context.Context, userID, toolID string) (removed bool, err error)

	// Summaries returns the vote count and the viewer status per tool id.
	// Tool ids without votes are absent from the map; an empty viewer id
	// means an anonymous reader with Voted false everywhere.
	Summaries(ctx context.Context, viewerID string, toolIDs []string) (map[string]VoteSummary, error)
}

// Sentinel errors for the E channel. Handlers map them to status codes.
var (
	// ErrNotFound: no tool carries the requested id.
	ErrNotFound = errors.New("tools: not found")
	// ErrUnknownCategory: the category filter names no known category.
	ErrUnknownCategory = errors.New("tools: unknown category")
	// ErrInvalidInput: a filter carries a byte the catalog cannot hold.
	ErrInvalidInput = errors.New("tools: invalid input")
	// ErrUnknownUser: the voter id names no known user.
	ErrUnknownUser = errors.New("tools: unknown user")
)
