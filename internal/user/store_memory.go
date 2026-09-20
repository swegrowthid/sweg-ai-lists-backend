package user

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"
	"sync"
	"time"
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
	uuid, err := newUUID()
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

func newUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}

// List implements Store. Returns a copy so callers cannot mutate state.
func (m *MemoryStore) List(_ context.Context) ([]User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]User, len(m.users))
	copy(out, m.users)
	return out, nil
}
