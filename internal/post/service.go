package post

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	maxSlugLen      = 100
	maxTitleLen     = 200
	maxNameLen      = 100
	maxItems        = 20
	maxBodyTextLen  = 64 << 10 // 64 KiB: inline .md files stay small on purpose
	defaultFileMIME = "text/markdown"

	// maxGeneratedSlugLen caps a slug derived from a title, leaving room for
	// the -2, -3, ... collision suffixes under maxSlugLen.
	maxGeneratedSlugLen = 90

	// maxSlugAttempts bounds the base, base-2, base-3, ... retry loop for a
	// generated slug.
	maxSlugAttempts = 100
)

// slugPattern keeps slugs URL-safe: lowercase words joined by single dashes.
var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// nonSlugRun matches every run of characters outside [a-z0-9]; each run
// becomes one dash when a slug is generated from a title.
var nonSlugRun = regexp.MustCompile(`[^a-z0-9]+`)

// Store is the persistence port. Service depends on it, never on SQL.
type Store interface {
	CreateCategory(ctx context.Context, input CreateCategoryInput) (Category, error)
	ListCategories(ctx context.Context) ([]Category, error)
	FindCategoryBySlug(ctx context.Context, slug string) (Category, error)
	Create(ctx context.Context, input CreatePostInput) (Post, error)
	List(ctx context.Context, filter ListFilter) ([]Post, error)
	FindBySlug(ctx context.Context, slug string) (Post, error)
	Delete(ctx context.Context, slug string) error
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

// ListCategories returns every category parents first, then their children,
// each group in name order.
func (s *Service) ListCategories(ctx context.Context) ([]Category, error) {
	return s.store.ListCategories(ctx)
}

// CreateCategory validates and persists one category. A non-empty ParentSlug
// makes the new category a derivative: the parent must exist and must itself be
// top-level, so the tree never grows past two levels.
func (s *Service) CreateCategory(ctx context.Context, input CreateCategoryInput) (Category, error) {
	slug, err := normalizeSlug(input.Slug)
	if err != nil {
		return Category{}, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || utf8.RuneCountInString(name) > maxNameLen || hasNUL(name) {
		return Category{}, ErrInvalidInput
	}
	parentSlug := strings.ToLower(strings.TrimSpace(input.ParentSlug))
	if parentSlug != "" {
		if _, err := normalizeSlug(parentSlug); err != nil {
			return Category{}, err
		}
		parent, err := s.store.FindCategoryBySlug(ctx, parentSlug)
		if err != nil {
			return Category{}, err
		}
		if parent.ParentSlug != nil {
			// A derivative cannot own derivatives.
			return Category{}, ErrInvalidInput
		}
	}
	return s.store.CreateCategory(ctx, CreateCategoryInput{Slug: slug, Name: name, ParentSlug: parentSlug})
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
// and persists the post with its categories and items. An explicit slug keeps
// the strict check: a conflict fails fast. An absent or blank slug is
// generated from the title and retried with -2, -3, ... suffixes until the
// write lands.
func (s *Service) Create(ctx context.Context, input CreatePostInput) (Post, error) {
	title := strings.TrimSpace(input.Title)
	if title == "" || utf8.RuneCountInString(title) > maxTitleLen || hasNUL(title) {
		return Post{}, ErrInvalidInput
	}
	if strings.TrimSpace(input.AuthorID) == "" {
		return Post{}, ErrInvalidInput
	}
	categorySlugs, err := s.resolveCategoryChoice(ctx, input.CategorySlug, input.DerivativeSlug)
	if err != nil {
		return Post{}, err
	}
	items, err := normalizeItems(input.Items)
	if err != nil {
		return Post{}, err
	}
	create := CreatePostInput{
		Title:         title,
		AuthorID:      input.AuthorID,
		CategorySlugs: categorySlugs,
		Items:         items,
	}

	if strings.TrimSpace(input.Slug) != "" {
		slug, err := normalizeSlug(input.Slug)
		if err != nil {
			return Post{}, err
		}
		create.Slug = slug
		return s.store.Create(ctx, create)
	}
	return s.createWithGeneratedSlug(ctx, create)
}

// Delete removes one post. Only the author may delete: a missing post is
// ErrNotFound, another author's post ErrForbidden.
func (s *Service) Delete(ctx context.Context, slug, authorID string) error {
	found, err := s.Get(ctx, slug)
	if err != nil {
		return err
	}
	if found.AuthorID != authorID {
		return ErrForbidden
	}
	return s.store.Delete(ctx, found.Slug)
}

// createWithGeneratedSlug derives the slug from the title and retries on a
// conflict with the -2, -3, ... suffixes, bounded at maxSlugAttempts tries.
// A pathological run that collides every time ends in ErrConflict.
func (s *Service) createWithGeneratedSlug(ctx context.Context, create CreatePostInput) (Post, error) {
	base := slugify(create.Title)
	for attempt := 1; attempt <= maxSlugAttempts; attempt++ {
		create.Slug = base
		if attempt > 1 {
			create.Slug = fmt.Sprintf("%s-%d", base, attempt)
		}
		created, err := s.store.Create(ctx, create)
		if err == nil {
			return created, nil
		}
		if !errors.Is(err, ErrConflict) {
			return Post{}, err
		}
	}
	return Post{}, ErrConflict
}

// resolveCategoryChoice turns the client's category plus optional derivative into
// the category slugs to link. The category may name a top-level slug or a
// derivative: a derivative links its parent too, and the derivative field may
// then only repeat the same slug. With a top-level category the derivative
// must be one of its direct children.
func (s *Service) resolveCategoryChoice(ctx context.Context, rawCategory, rawDerivative string) ([]string, error) {
	categorySlug, err := normalizeSlug(rawCategory)
	if err != nil {
		return nil, err
	}
	category, err := s.store.FindCategoryBySlug(ctx, categorySlug)
	if err != nil {
		return nil, err
	}
	derivativeSlug := strings.ToLower(strings.TrimSpace(rawDerivative))

	if category.ParentSlug != nil {
		// The category is itself a derivative: link it with its parent.
		if derivativeSlug != "" && derivativeSlug != categorySlug {
			return nil, ErrInvalidInput
		}
		return []string{*category.ParentSlug, categorySlug}, nil
	}

	if derivativeSlug == "" {
		return []string{categorySlug}, nil
	}
	if _, err := normalizeSlug(derivativeSlug); err != nil {
		return nil, err
	}
	derivative, err := s.store.FindCategoryBySlug(ctx, derivativeSlug)
	if err != nil {
		return nil, err
	}
	if derivative.ParentSlug == nil || *derivative.ParentSlug != categorySlug {
		return nil, ErrInvalidInput
	}
	return []string{categorySlug, derivativeSlug}, nil
}

// slugify derives a URL-safe slug from a title: lowercase, each run of
// characters outside [a-z0-9] becomes one dash, edge dashes are dropped, and
// the result is capped at maxGeneratedSlugLen. A title with no usable
// characters falls back to "post".
func slugify(title string) string {
	slug := strings.Trim(nonSlugRun.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if len(slug) > maxGeneratedSlugLen {
		slug = strings.TrimRight(slug[:maxGeneratedSlugLen], "-")
	}
	if slug == "" {
		return "post"
	}
	return slug
}

// normalizeSlug lowercases, trims, and checks the URL-safe shape.
func normalizeSlug(raw string) (string, error) {
	slug := strings.ToLower(strings.TrimSpace(raw))
	if slug == "" || len(slug) > maxSlugLen || !slugPattern.MatchString(slug) {
		return "", ErrInvalidInput
	}
	return slug, nil
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
