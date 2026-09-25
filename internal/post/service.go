package post

import (
	"context"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	maxSlugLen      = 100
	maxTitleLen     = 200
	maxNameLen      = 100
	maxCategories   = 8
	maxItems        = 20
	maxBodyTextLen  = 64 << 10 // 64 KiB: inline .md files stay small on purpose
	defaultFileMIME = "text/markdown"
)

// slugPattern keeps slugs URL-safe: lowercase words joined by single dashes.
var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// Store is the persistence port. Service depends on it, never on SQL.
type Store interface {
	CreateCategory(ctx context.Context, input CreateCategoryInput) (Category, error)
	ListCategories(ctx context.Context) ([]Category, error)
	Create(ctx context.Context, input CreatePostInput) (Post, error)
	List(ctx context.Context, filter ListFilter) ([]Post, error)
	FindBySlug(ctx context.Context, slug string) (Post, error)
}

// Service owns post and category use-cases. No HTTP, no SQL here.
type Service struct {
	store Store
}

// NewService wires the store. Nil store is a programmer bug, so panic early.
func NewService(store Store) *Service {
	if store == nil {
		panic("post: nil Store")
	}
	return &Service{store: store}
}

// ListCategories returns every category in name order.
func (s *Service) ListCategories(ctx context.Context) ([]Category, error) {
	return s.store.ListCategories(ctx)
}

// CreateCategory validates and persists one category.
func (s *Service) CreateCategory(ctx context.Context, input CreateCategoryInput) (Category, error) {
	slug, err := normalizeSlug(input.Slug)
	if err != nil {
		return Category{}, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || utf8.RuneCountInString(name) > maxNameLen || hasNUL(name) {
		return Category{}, ErrInvalidInput
	}
	return s.store.CreateCategory(ctx, CreateCategoryInput{Slug: slug, Name: name})
}

// List returns posts newest first, filtered by category slug and title query.
func (s *Service) List(ctx context.Context, filter ListFilter) ([]Post, error) {
	category := strings.ToLower(strings.TrimSpace(filter.CategorySlug))
	query := strings.TrimSpace(filter.Query)
	// No Postgres text column holds a NUL byte, so reject it here instead of
	// letting the store fail the query with an encoding error.
	if hasNUL(category) || hasNUL(query) {
		return nil, ErrInvalidInput
	}
	return s.store.List(ctx, ListFilter{CategorySlug: category, Query: query})
}

// Get returns one post with its categories and its items in display order.
func (s *Service) Get(ctx context.Context, slug string) (Post, error) {
	normalized := strings.ToLower(strings.TrimSpace(slug))
	// A valid slug never carries a NUL byte, so the lookup can only miss.
	if hasNUL(normalized) {
		return Post{}, ErrNotFound
	}
	return s.store.FindBySlug(ctx, normalized)
}

// Create validates one post, assigns item positions from the request order,
// and persists the post with its categories and items.
func (s *Service) Create(ctx context.Context, input CreatePostInput) (Post, error) {
	slug, err := normalizeSlug(input.Slug)
	if err != nil {
		return Post{}, err
	}
	title := strings.TrimSpace(input.Title)
	if title == "" || utf8.RuneCountInString(title) > maxTitleLen || hasNUL(title) {
		return Post{}, ErrInvalidInput
	}
	if strings.TrimSpace(input.AuthorID) == "" {
		return Post{}, ErrInvalidInput
	}
	categorySlugs, err := normalizeCategorySlugs(input.CategorySlugs)
	if err != nil {
		return Post{}, err
	}
	items, err := normalizeItems(input.Items)
	if err != nil {
		return Post{}, err
	}
	return s.store.Create(ctx, CreatePostInput{
		Slug:          slug,
		Title:         title,
		AuthorID:      input.AuthorID,
		CategorySlugs: categorySlugs,
		Items:         items,
	})
}

// normalizeSlug lowercases, trims, and checks the URL-safe shape.
func normalizeSlug(raw string) (string, error) {
	slug := strings.ToLower(strings.TrimSpace(raw))
	if slug == "" || len(slug) > maxSlugLen || !slugPattern.MatchString(slug) {
		return "", ErrInvalidInput
	}
	return slug, nil
}

// normalizeCategorySlugs validates every slug and drops repeats, keeping the
// request order.
func normalizeCategorySlugs(raw []string) ([]string, error) {
	if len(raw) == 0 || len(raw) > maxCategories {
		return nil, ErrInvalidInput
	}
	seen := make(map[string]struct{}, len(raw))
	slugs := make([]string, 0, len(raw))
	for _, value := range raw {
		slug, err := normalizeSlug(value)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[slug]; ok {
			continue
		}
		seen[slug] = struct{}{}
		slugs = append(slugs, slug)
	}
	return slugs, nil
}

// normalizeItems validates every item and sets position from the array order,
// so clients never send positions and gaps cannot appear.
func normalizeItems(raw []ItemInput) ([]ItemInput, error) {
	if len(raw) == 0 || len(raw) > maxItems {
		return nil, ErrInvalidInput
	}
	items := make([]ItemInput, 0, len(raw))
	for index, item := range raw {
		normalized, err := normalizeItem(item)
		if err != nil {
			return nil, err
		}
		normalized.Position = index
		items = append(items, normalized)
	}
	return items, nil
}

// normalizeItem enforces one payload shape per kind. A field that does not
// belong to the kind is rejected as soon as the client sends it, even when it
// is empty, so no data is dropped silently. body_text keeps its exact bytes.
func normalizeItem(item ItemInput) (ItemInput, error) {
	normalized := ItemInput{Kind: item.Kind}
	switch item.Kind {
	case KindMarkdown, KindText:
		if !isFilled(item.BodyText) || item.URL != nil || item.Filename != nil || item.MIME != nil {
			return ItemInput{}, ErrInvalidInput
		}
		normalized.BodyText = item.BodyText
	case KindLink:
		if !isFilled(item.URL) || item.BodyText != nil || item.Filename != nil || item.MIME != nil {
			return ItemInput{}, ErrInvalidInput
		}
		link := strings.TrimSpace(*item.URL)
		if !isAbsoluteHTTPURL(link) {
			return ItemInput{}, ErrInvalidInput
		}
		normalized.URL = &link
	case KindFile:
		if !isFilled(item.BodyText) || !isFilled(item.Filename) || item.URL != nil {
			return ItemInput{}, ErrInvalidInput
		}
		filename := strings.TrimSpace(*item.Filename)
		mime := defaultFileMIME
		if isFilled(item.MIME) {
			mime = strings.TrimSpace(*item.MIME)
		}
		normalized.BodyText = item.BodyText
		normalized.Filename = &filename
		normalized.MIME = &mime
	default:
		return ItemInput{}, ErrInvalidInput
	}

	if body := normalized.BodyText; body != nil && len(*body) > maxBodyTextLen {
		return ItemInput{}, ErrInvalidInput
	}
	for _, value := range []*string{normalized.BodyText, normalized.URL, normalized.Filename, normalized.MIME} {
		if value != nil && hasNUL(*value) {
			return ItemInput{}, ErrInvalidInput
		}
	}
	return normalized, nil
}

// isFilled reports whether the client sent the key with non-blank text.
func isFilled(value *string) bool {
	return value != nil && strings.TrimSpace(*value) != ""
}

// hasNUL reports the one byte Postgres text columns cannot store.
func hasNUL(value string) bool { return strings.IndexByte(value, 0) >= 0 }

// isAbsoluteHTTPURL keeps links clickable and blocks javascript: style values.
func isAbsoluteHTTPURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}
