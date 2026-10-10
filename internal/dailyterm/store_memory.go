package dailyterm

import (
	"context"
	"sort"
	"sync"
)

// MemoryStore is the in-memory Store. The source markdown files are the
// database; this store only holds the parsed snapshot between syncs.
type MemoryStore struct {
	mu    sync.RWMutex
	terms []Term
	index map[string]int
}

// NewMemoryStore builds an empty store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{index: map[string]int{}}
}

// ReplaceAll implements Store: one atomic swap of the whole snapshot.
func (m *MemoryStore) ReplaceAll(_ context.Context, terms []Term) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	index := make(map[string]int, len(terms))
	for i, term := range terms {
		// A duplicate id would make Get ambiguous. Upstream titles are unique
		// inside every month file, so this only guards a future collision.
		if _, dup := index[term.ID]; !dup {
			index[term.ID] = i
		}
	}
	m.terms = terms
	m.index = index
	return nil
}

// List implements Store. The snapshot keeps month order, then source order
// inside a month. A monthKey narrows the result to that month.
func (m *MemoryStore) List(_ context.Context, monthKey string) ([]Term, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]Term, 0, len(m.terms))
	for _, term := range m.terms {
		if monthKey != "" && term.MonthKey != monthKey {
			continue
		}
		out = append(out, term)
	}
	// Months sort newest first because that is the order a client wants to walk
	// the journal in. The snapshot itself is stored old to new.
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].MonthKey > out[j].MonthKey
	})
	return out, nil
}

// Get implements Store. The id is already normalized by the service.
func (m *MemoryStore) Get(_ context.Context, id string) (Term, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	i, ok := m.index[id]
	if !ok {
		return Term{}, ErrNotFound
	}
	return m.terms[i], nil
}
