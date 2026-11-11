package post

import (
	"errors"
	"time"
)

// Category groups posts, for example "coding agent". The slug is the stable
// handle clients filter by. ParentSlug is nil for a top-level category and set
// to the parent's slug for a derivative: the tree is at most two levels deep.
type Category struct {
	ID         string    `json:"id"`
	Slug       string    `json:"slug"`
	Name       string    `json:"name"`
	ParentSlug *string   `json:"parent_slug"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Item is the content of a post. A post stores exactly one item at position 0;
// the slice shape stays so reads can grow back to multiple items later.
type Item struct {
	ID       string `json:"id"`
	Position int    `json:"position"`
	Value    string `json:"value"`
}

// Post is one published post with its categories. There is no draft state:
// every stored post is live. Items holds the single stored item on detail
// responses and stays empty on list responses.
type Post struct {
	ID       string `json:"id"`
	Slug     string `json:"slug"`
	Title    string `json:"title"`
	AuthorID string `json:"author_id"`
	// AuthorUsername is the author's display name. It is resolved from the
	// users table at read time, never copied onto the posts row.
	AuthorUsername string     `json:"author_username"`
	Categories     []Category `json:"categories"`
	Items          []Item     `json:"items,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

var (
	// ErrNotFound signals a missing post.
	ErrNotFound = errors.New("post: not found")

	// ErrConflict signals a slug that is already in use.
	ErrConflict = errors.New("post: slug already in use")

	// ErrInvalidInput signals input that breaks the slug, title, category,
	// or value contract.
	ErrInvalidInput = errors.New("post: invalid input")

	// ErrUnknownCategory signals a category slug that does not exist.
	ErrUnknownCategory = errors.New("post: unknown category")

	// ErrUnknownAuthor signals an author id with no user row. A live token for
	// a deleted user is the only way in.
	ErrUnknownAuthor = errors.New("post: unknown author")

	// ErrForbidden signals a write to a post the caller does not own.
	ErrForbidden = errors.New("post: forbidden")
)

// CreateCategoryInput is the validated input for the create-category use case.
// ParentSlug is empty for a top-level category. A non-empty ParentSlug must
// name an existing top-level category: the tree is only two levels deep.
type CreateCategoryInput struct {
	Slug       string
	Name       string
	ParentSlug string
}

// CreatePostInput is the validated input for the create-post use case.
// Slug stays empty when the client did not send one: the service generates it
// from the title. CategorySlug names any existing category, top-level or
// derivative, and DerivativeSlug optionally names a direct child of a
// top-level category. The service resolves the pair into CategorySlugs, the
// list the store links.
// Value keeps its exact bytes; trimming would change markdown content.
type CreatePostInput struct {
	Slug     string
	Title    string
	AuthorID string
	// AuthorUsername comes from the verified token. Only the memory store
	// keeps it: the Postgres store resolves the name from users at read time,
	// so a stored name can never drift from the account.
	AuthorUsername string
	CategorySlug   string
	DerivativeSlug string
	CategorySlugs  []string
	Value          string
}

// ListFilter narrows the list query. Empty fields mean "no filter".
type ListFilter struct {
	CategorySlug string
	Query        string
}
