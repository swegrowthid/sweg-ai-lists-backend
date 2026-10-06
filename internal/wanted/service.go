package wanted

import (
	"context"
	"errors"
	"strings"

	"github.com/swegrowthid/sweg-ai-lists-backend/internal/post"
)

// Store is the persistence port. Service depends on it, never on SQL.
type Store interface {
	// Add stores one post id. It reports true for a new row and false when
	// the post was already wanted. A missing post row is ErrUnknownPost.
	Add(ctx context.Context, postID string) (bool, error)

	// ListIDs returns every wanted post id, newest-wanted first.
	ListIDs(ctx context.Context) ([]string, error)

	// Delete drops one post id. An absent id is ErrNotFound.
	Delete(ctx context.Context, postID string) error
}

// postSource is the narrow post port the wanted list needs: resolve one slug
// and read the catalog. *post.Service satisfies it.
type postSource interface {
	Get(ctx context.Context, slug string) (post.Post, error)
	List(ctx context.Context, filter post.ListFilter) ([]post.Post, error)
}

// Service owns the wanted use-cases. No HTTP, no SQL here.
type Service struct {
	store Store
	posts postSource
}

// NewService wires the store to the post source. Nil deps are programmer
// bugs, so panic early.
func NewService(store Store, posts postSource) *Service {
	if store == nil {
		panic("wanted: nil Store")
	}
	if posts == nil {
		panic("wanted: nil post source")
	}
	return &Service{store: store, posts: posts}
}

// Add marks one post slug as wanted and returns the post in list shape (no
// items, like GET /posts), plus whether the row is new. A duplicate slug
// answers the stored post with added=false, so the handler can reply 200
// instead of 201.
func (s *Service) Add(ctx context.Context, input AddInput) (post.Post, bool, error) {
	slug := strings.TrimSpace(input.Slug)
	if slug == "" || strings.IndexByte(slug, 0) >= 0 {
		return post.Post{}, false, ErrInvalidInput
	}
	found, err := s.posts.Get(ctx, slug)
	if err != nil {
		if errors.Is(err, post.ErrNotFound) {
			return post.Post{}, false, ErrUnknownPost
		}
		return post.Post{}, false, err
	}
	added, err := s.store.Add(ctx, found.ID)
	if err != nil {
		return post.Post{}, false, err
	}
	found.Items = nil
	return found, added, nil
}

// List returns every wanted post in list shape (no items), newest-wanted
// first. A wanted id whose post row is gone is skipped: Postgres removes it
// via ON DELETE CASCADE, the memory store cannot.
func (s *Service) List(ctx context.Context) ([]post.Post, error) {
	ids, err := s.store.ListIDs(ctx)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []post.Post{}, nil
	}
	all, err := s.posts.List(ctx, post.ListFilter{})
	if err != nil {
		return nil, err
	}
	byID := make(map[string]post.Post, len(all))
	for _, candidate := range all {
		byID[candidate.ID] = candidate
	}
	out := make([]post.Post, 0, len(ids))
	for _, id := range ids {
		if resolved, ok := byID[id]; ok {
			out = append(out, resolved)
		}
	}
	return out, nil
}

// Delete drops one post slug from the wanted list. An unknown slug is
// ErrUnknownPost, a known post that is not wanted is ErrNotFound.
func (s *Service) Delete(ctx context.Context, slug string) error {
	trimmed := strings.TrimSpace(slug)
	if trimmed == "" || strings.IndexByte(trimmed, 0) >= 0 {
		return ErrInvalidInput
	}
	found, err := s.posts.Get(ctx, trimmed)
	if err != nil {
		if errors.Is(err, post.ErrNotFound) {
			return ErrUnknownPost
		}
		return err
	}
	return s.store.Delete(ctx, found.ID)
}
