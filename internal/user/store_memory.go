package user

import (
	"context"
	"sync"
)

// MemoryStore is the in-memory Store. Use for dev and tests.
type MemoryStore struct {
	mu    sync.RWMutex
	users []User
}

// NewMemoryStore builds an empty store.
func NewMemoryStore() *MemoryStore { return &MemoryStore{} }

// Add inserts a user. Helper for seeds and tests, not an API yet.
func (m *MemoryStore) Add(u User) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.users = append(m.users, u)
}

// List implements Store. Returns a copy so callers cannot mutate state.
func (m *MemoryStore) List(_ context.Context) ([]User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]User, len(m.users))
	copy(out, m.users)
	return out, nil
}
