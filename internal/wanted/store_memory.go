package wanted

import (
	"context"
	"sort"
	"sync"
	"time"
)

// MemoryStore is the in-memory Store. Use for dev and tests.
// It mirrors the Postgres store: idempotent add, newest-first list order,
// and ErrNotFound on deleting an absent id.
type MemoryStore struct {
	mu      sync.RWMutex
	addedAt map[string]time.Time
}

// NewMemoryStore builds an empty store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{addedAt: map[string]time.Time{}}
}

// Add implements Store: true for a new id, false for a duplicate.
func (m *MemoryStore) Add(_ context.Context, postID string) (bool, error) {
	if postID == "" {
		return false, ErrUnknownPost
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, seen := m.addedAt[postID]; seen {
		return false, nil
	}
	m.addedAt[postID] = time.Now().UTC()
	return true, nil
}

// ListIDs implements Store, newest-wanted first, post id as the tie-break.
func (m *MemoryStore) ListIDs(_ context.Context) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := make([]string, 0, len(m.addedAt))
	for id := range m.addedAt {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		left, right := m.addedAt[ids[i]], m.addedAt[ids[j]]
		if !left.Equal(right) {
			return left.After(right)
		}
		return ids[i] < ids[j]
	})
	return ids, nil
}

// Delete implements Store. An absent id is ErrNotFound.
func (m *MemoryStore) Delete(_ context.Context, postID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, seen := m.addedAt[postID]; !seen {
		return ErrNotFound
	}
	delete(m.addedAt, postID)
	return nil
}
