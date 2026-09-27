package post

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/swegrowthid/sweg-ai-lists-backend/internal/platform/id"
)

// MemoryStore is the in-memory Store. Use for dev and tests.
// It mirrors the Postgres store: same conflicts, same read shapes, same order.
// It does not model the users foreign key, so any non-empty author id is
// accepted here; Postgres rejects an unknown author with ErrUnknownAuthor.
type MemoryStore struct {
	mu         sync.RWMutex
	categories []Category
	posts      []Post
}

// NewMemoryStore builds an empty store.
func NewMemoryStore() *MemoryStore { return &MemoryStore{} }

// CreateCategory implements Store and mirrors the unique slug constraint and
// the parent lookup: a missing parent is ErrUnknownCategory.
func (m *MemoryStore) CreateCategory(_ context.Context, input CreateCategoryInput) (Category, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, existing := range m.categories {
		if existing.Slug == input.Slug {
			return Category{}, ErrConflict
		}
	}
	var parentSlug *string
	if input.ParentSlug != "" {
		if _, ok := m.categoryBySlug(input.ParentSlug); !ok {
			return Category{}, ErrUnknownCategory
		}
		value := input.ParentSlug
		parentSlug = &value
	}

	now := time.Now().UTC()
	categoryID, err := id.New()
	if err != nil {
		return Category{}, fmt.Errorf("post: generate category id: %w", err)
	}
	created := Category{
		ID:         categoryID,
		Slug:       input.Slug,
		Name:       input.Name,
		ParentSlug: parentSlug,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	m.categories = append(m.categories, created)
	return cloneCategory(created), nil
}

// ListCategories implements Store: parents first, then their children, each
// group in name order, id as the tie-break. Same order as the Postgres store.
func (m *MemoryStore) ListCategories(_ context.Context) ([]Category, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]Category, 0, len(m.categories))
	for _, category := range m.categories {
		out = append(out, cloneCategory(category))
	}
	sort.Slice(out, func(i, j int) bool {
		left, right := out[i], out[j]
		// Group a category with its own parent, by the parent's name and then its
		// id, so two parents that share a name never interleave their children.
		leftGroupName, leftGroupID := left.Name, left.ID
		if left.ParentSlug != nil {
			if parent, ok := m.categoryBySlug(*left.ParentSlug); ok {
				leftGroupName, leftGroupID = parent.Name, parent.ID
			}
		}
		rightGroupName, rightGroupID := right.Name, right.ID
		if right.ParentSlug != nil {
			if parent, ok := m.categoryBySlug(*right.ParentSlug); ok {
				rightGroupName, rightGroupID = parent.Name, parent.ID
			}
		}
		if leftGroupName != rightGroupName {
			return leftGroupName < rightGroupName
		}
		if leftGroupID != rightGroupID {
			return leftGroupID < rightGroupID
		}
		if (left.ParentSlug != nil) != (right.ParentSlug != nil) {
			return right.ParentSlug != nil
		}
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		return left.ID < right.ID
	})
	return out, nil
}

// FindCategoryBySlug implements Store. A missing slug is ErrUnknownCategory.
func (m *MemoryStore) FindCategoryBySlug(_ context.Context, slug string) (Category, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	category, ok := m.categoryBySlug(slug)
	if !ok {
		return Category{}, ErrUnknownCategory
	}
	return cloneCategory(category), nil
}

// categoryBySlug finds one category by its exact slug. Callers hold the lock.
func (m *MemoryStore) categoryBySlug(slug string) (Category, bool) {
	for _, category := range m.categories {
		if category.Slug == slug {
			return category, true
		}
	}
	return Category{}, false
}

// Create implements Store and mirrors the unique slug constraint, the category
// foreign key, and the Postgres read-back shape.
func (m *MemoryStore) Create(_ context.Context, input CreatePostInput) (Post, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, existing := range m.posts {
		if existing.Slug == input.Slug {
			return Post{}, ErrConflict
		}
	}
	categories, err := m.categoriesBySlug(input.CategorySlugs)
	if err != nil {
		return Post{}, err
	}

	now := time.Now().UTC()
	postID, err := id.New()
	if err != nil {
		return Post{}, fmt.Errorf("post: generate post id: %w", err)
	}
	items := make([]Item, 0, len(input.Items))
	for _, item := range input.Items {
		itemID, err := id.New()
		if err != nil {
			return Post{}, fmt.Errorf("post: generate item id: %w", err)
		}
		items = append(items, Item{
			ID:       itemID,
			Kind:     item.Kind,
			Position: item.Position,
			BodyText: cloneText(item.BodyText),
			URL:      cloneText(item.URL),
			Filename: cloneText(item.Filename),
			MIME:     cloneText(item.MIME),
		})
	}

	created := Post{
		ID:         postID,
		Slug:       input.Slug,
		Title:      input.Title,
		AuthorID:   input.AuthorID,
		Categories: categories,
		Items:      items,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	m.posts = append(m.posts, created)
	return clonePost(created, true), nil
}

// List implements Store, newest first, without items: the same shape the
// Postgres store returns.
func (m *MemoryStore) List(_ context.Context, filter ListFilter) ([]Post, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]Post, 0, len(m.posts))
	for _, candidate := range m.posts {
		if !matchesFilter(candidate, filter) {
			continue
		}
		out = append(out, clonePost(candidate, false))
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// FindBySlug implements Store with categories and items in display order.
func (m *MemoryStore) FindBySlug(_ context.Context, slug string) (Post, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, candidate := range m.posts {
		if candidate.Slug == slug {
			return clonePost(candidate, true), nil
		}
	}
	return Post{}, ErrNotFound
}

// categoriesBySlug resolves every slug, or fails on the first unknown one.
// The result puts the top-level category first, then its derivative, then name
// and slug as tie-breaks - the same order the Postgres store returns.
// Callers hold the write lock.
func (m *MemoryStore) categoriesBySlug(slugs []string) ([]Category, error) {
	resolved := make([]Category, 0, len(slugs))
	for _, slug := range slugs {
		found := false
		for _, category := range m.categories {
			if category.Slug == slug {
				resolved = append(resolved, cloneCategory(category))
				found = true
				break
			}
		}
		if !found {
			return nil, ErrUnknownCategory
		}
	}
	sort.Slice(resolved, func(i, j int) bool {
		if (resolved[i].ParentSlug != nil) != (resolved[j].ParentSlug != nil) {
			return resolved[j].ParentSlug != nil
		}
		if resolved[i].Name != resolved[j].Name {
			return resolved[i].Name < resolved[j].Name
		}
		return resolved[i].Slug < resolved[j].Slug
	})
	return resolved, nil
}

// matchesFilter mirrors the Postgres WHERE clause: an optional category slug
// and an optional case-insensitive title search.
func matchesFilter(candidate Post, filter ListFilter) bool {
	if filter.CategorySlug != "" {
		matched := false
		for _, category := range candidate.Categories {
			if category.Slug == filter.CategorySlug {
				matched = true
				break
			}
			if category.ParentSlug != nil && *category.ParentSlug == filter.CategorySlug {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if filter.Query != "" && !strings.Contains(strings.ToLower(candidate.Title), strings.ToLower(filter.Query)) {
		return false
	}
	return true
}

// clonePost copies a post so callers cannot mutate store state. Items are
// dropped when withItems is false.
func clonePost(source Post, withItems bool) Post {
	out := source
	out.Categories = make([]Category, len(source.Categories))
	for index, category := range source.Categories {
		out.Categories[index] = cloneCategory(category)
	}
	if !withItems {
		out.Items = nil
		return out
	}
	out.Items = make([]Item, len(source.Items))
	for index, item := range source.Items {
		out.Items[index] = cloneItem(item)
	}
	return out
}

// cloneCategory copies a category and its parent pointer, so a caller can never
// write into stored state through a shared pointer.
func cloneCategory(source Category) Category {
	out := source
	out.ParentSlug = cloneText(source.ParentSlug)
	return out
}

// cloneItem copies an item and every payload pointer, so a caller can never
// write into stored state through a shared pointer.
func cloneItem(source Item) Item {
	out := source
	out.BodyText = cloneText(source.BodyText)
	out.URL = cloneText(source.URL)
	out.Filename = cloneText(source.Filename)
	out.MIME = cloneText(source.MIME)
	return out
}

// cloneText copies the pointed-to text, or returns nil for an absent field.
func cloneText(value *string) *string {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
