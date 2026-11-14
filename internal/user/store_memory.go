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
	// supabase maps a swegrowth.id account to the local user id.
	supabase map[string]string
}

// NewMemoryStore builds an empty store.
func NewMemoryStore() *MemoryStore { return &MemoryStore{supabase: map[string]string{}} }

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
	if _, taken := m.supabase[input.SupabaseID]; input.SupabaseID != "" && taken {
		return User{}, ErrConflict
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
	if input.SupabaseID != "" {
		m.supabase[input.SupabaseID] = created.ID
	}
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

// FindByEmail implements Store. Email matches case-insensitively.
func (m *MemoryStore) FindByEmail(_ context.Context, email string) (User, error) {
	email = strings.TrimSpace(email)
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, u := range m.users {
		if strings.EqualFold(u.Email, email) {
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

// FindByID implements Store.
func (m *MemoryStore) FindByID(_ context.Context, id string) (User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, u := range m.users {
		if u.ID == id {
			return u, nil
		}
	}
	return User{}, ErrNotFound
}

// UpdatePasswordHash implements Store. It also bumps UpdatedAt.
func (m *MemoryStore) UpdatePasswordHash(_ context.Context, id, passwordHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, u := range m.users {
		if u.ID == id {
			m.users[i].PasswordHash = passwordHash
			m.users[i].UpdatedAt = time.Now().UTC()
			return nil
		}
	}
	return ErrNotFound
}

// FindBySupabaseID implements Store.
func (m *MemoryStore) FindBySupabaseID(ctx context.Context, supabaseID string) (User, error) {
	m.mu.RLock()
	userID, ok := m.supabase[supabaseID]
	m.mu.RUnlock()
	if !ok {
		return User{}, ErrNotFound
	}
	return m.FindByID(ctx, userID)
}

// LinkSupabaseID implements Store, mirroring the unique supabase_id column.
func (m *MemoryStore) LinkSupabaseID(_ context.Context, id, supabaseID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if owner, taken := m.supabase[supabaseID]; taken {
		if owner == id {
			return nil
		}
		return ErrLinkedElsewhere
	}
	for linked, owner := range m.supabase {
		if owner == id && linked != supabaseID {
			return ErrLinkedElsewhere
		}
	}
	m.supabase[supabaseID] = id
	return nil
}
