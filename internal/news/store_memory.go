package news

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/swegrowthid/sweg-ai-lists-backend/internal/platform/id"
)

// MemoryStore is the in-memory Store. Use for dev and tests.
// It mirrors the Postgres store: same upsert key, same read order.
type MemoryStore struct {
	mu      sync.RWMutex
	entries []Entry
}

// NewMemoryStore builds an empty store.
func NewMemoryStore() *MemoryStore { return &MemoryStore{} }

// Upsert implements Store and mirrors the Postgres upsert: a new URL is
// inserted, a known URL updates only the fields that changed.
func (m *MemoryStore) Upsert(_ context.Context, input UpsertInput) (Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for index, existing := range m.entries {
		if existing.URL != input.URL {
			continue
		}
		if existing.Title != input.Title || existing.Summary != input.Summary || !existing.PublishedAt.Equal(input.PublishedAt) {
			existing.Title = input.Title
			existing.Summary = input.Summary
			existing.PublishedAt = input.PublishedAt
			existing.UpdatedAt = time.Now().UTC()
			m.entries[index] = existing
		}
		return existing, nil
	}

	now := time.Now().UTC()
	entryID, err := id.New()
	if err != nil {
		return Entry{}, fmt.Errorf("news: generate id: %w", err)
	}
	created := Entry{
		ID:          entryID,
		Title:       input.Title,
		URL:         input.URL,
		Summary:     input.Summary,
		PublishedAt: input.PublishedAt,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	m.entries = append(m.entries, created)
	return created, nil
}

// List implements Store, newest published first, id as the tie-break. Same
// order as the Postgres store.
func (m *MemoryStore) List(_ context.Context) ([]Entry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]Entry, len(m.entries))
	copy(out, m.entries)
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].PublishedAt.Equal(out[j].PublishedAt) {
			return out[i].PublishedAt.After(out[j].PublishedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}
