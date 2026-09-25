package user

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/swegrowthid/sweg-ai-lists-backend/internal/platform/id"
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

// Create implements Store and mirrors the users table's unique constraints.
func (m *MemoryStore) Create(_ context.Context, input CreateInput) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, existing := range m.users {
		if existing.Username == input.Username || strings.EqualFold(existing.Email, input.Email) {
			return User{}, ErrConflict
		}
	}

	now := time.Now().UTC()
	uuid, err := id.New()
	if err != nil {
		return User{}, fmt.Errorf("user: generate id: %w", err)
	}
	created := User{
		ID:           uuid,
		Username:     input.Username,
		Email:        input.Email,
		PasswordHash: input.PasswordHash,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	m.users = append(m.users, created)
	return created, nil
}

// FindByUsernameOrEmail implements Store. Username matches exactly,
// email case-insensitive, mirroring the Postgres store.
func (m *MemoryStore) FindByUsernameOrEmail(_ context.Context, identifier string) (User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, u := range m.users {
		if u.Username == identifier || strings.EqualFold(u.Email, identifier) {
			return u, nil
		}
	}
	return User{}, ErrNotFound
}

// List implements Store. Returns a copy so callers cannot mutate state.
func (m *MemoryStore) List(_ context.Context) ([]User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]User, len(m.users))
	copy(out, m.users)
	return out, nil
}
