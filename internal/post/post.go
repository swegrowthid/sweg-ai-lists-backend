package post

import (
	"errors"
	"time"
)

// Category groups posts, for example "coding agent". The slug is the stable
// handle clients filter by.
type Category struct {
	ID        string    `json:"id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Kind is the payload shape of one item. One item carries exactly one shape.
type Kind string

const (
	// KindMarkdown keeps markdown text inline in body_text.
	KindMarkdown Kind = "markdown"

	// KindText keeps plain text inline in body_text.
	KindText Kind = "text"

	// KindLink keeps one absolute URL in url.
	KindLink Kind = "link"

	// KindFile keeps a small file inline: body_text plus filename and mime.
	KindFile Kind = "file"
)

// Item is one ordered block of a post. Only the fields of its Kind are set;
// the rest stay nil, so JSON never shows a field that carries no meaning.
type Item struct {
	ID       string  `json:"id"`
	Kind     Kind    `json:"kind"`
	Position int     `json:"position"`
	BodyText *string `json:"body_text,omitempty"`
	URL      *string `json:"url,omitempty"`
	Filename *string `json:"filename,omitempty"`
	MIME     *string `json:"mime,omitempty"`
}

// Post is one published post with its categories. There is no draft state:
// every stored post is live. Items stay empty on list responses and are
// filled by the detail read only.
type Post struct {
	ID         string     `json:"id"`
	Slug       string     `json:"slug"`
	Title      string     `json:"title"`
	AuthorID   string     `json:"author_id"`
	Categories []Category `json:"categories"`
	Items      []Item     `json:"items,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

var (
	// ErrNotFound signals a missing post.
	ErrNotFound = errors.New("post: not found")

	// ErrConflict signals a slug that is already in use.
	ErrConflict = errors.New("post: slug already in use")

	// ErrInvalidInput signals input that breaks the slug, title, category,
	// or item-kind contract.
	ErrInvalidInput = errors.New("post: invalid input")

	// ErrUnknownCategory signals a category slug that does not exist.
	ErrUnknownCategory = errors.New("post: unknown category")

	// ErrUnknownAuthor signals an author id with no user row. A live token for
	// a deleted user is the only way in.
	ErrUnknownAuthor = errors.New("post: unknown author")
)

// CreateCategoryInput is the validated input for the create-category use case.
type CreateCategoryInput struct {
	Slug string
	Name string
}

// CreatePostInput is the validated input for the create-post use case.
// Item positions are already assigned from the request order.
type CreatePostInput struct {
	Slug          string
	Title         string
	AuthorID      string
	CategorySlugs []string
	Items         []ItemInput
}

// ItemInput is one item as the client sent it. Each payload field is a
// pointer: nil means the key was absent, which is how the service spots a
// field that does not belong to the item's kind. Only body_text keeps its
// exact bytes; trimming it would change markdown and inline file content.
type ItemInput struct {
	Kind     Kind
	Position int
	BodyText *string
	URL      *string
	Filename *string
	MIME     *string
}

// ListFilter narrows the list query. Empty fields mean "no filter".
type ListFilter struct {
	CategorySlug string
	Query        string
}
