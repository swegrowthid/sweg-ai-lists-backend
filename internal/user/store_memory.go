package user

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/swegrowthid/sweg-ai-lists-backend/internal/platform/id"
)

// MemoryStore is the in-memory Store. Use for dev and tests.
type MemoryStore struct {
	mu    sync.RWMutex
	users []User
	// identities maps a provider to provider user id to local user id.
	identities map[string]map[string]string
}

// NewMemoryStore builds an empty store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{identities: map[string]map[string]string{}}
}

// Add inserts a user. Helper for seeds and tests, not an API yet.
func (m *MemoryStore) Add(u User) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.users = append(m.users, u)
}

// Create implements Store and mirrors the users and user_identities
// unique constraints. The user and its identity land together.
func (m *MemoryStore) Create(_ context.Context, input CreateInput) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, existing := range m.users {
		if existing.Username == input.Username || strings.EqualFold(existing.Email, input.Email) {
			return User{}, ErrConflict
		}
	}
	if input.Identity != nil {
		if _, taken := m.identities[input.Identity.Provider][input.Identity.ProviderUserID]; taken {
			return User{}, ErrLinkedElsewhere
		}
	}

	uuid, err := id.New()
	if err != nil {
		return User{}, fmt.Errorf("user: generate id: %w", err)
	}
	now := time.Now().UTC()
	created := User{
		ID:           uuid,
		Username:     input.Username,
		Email:        input.Email,
		PasswordHash: input.PasswordHash,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	m.users = append(m.users, created)
	if input.Identity != nil {
		m.link(created.ID, input.Identity.Provider, input.Identity.ProviderUserID)
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

// FindByIdentity implements Store.
func (m *MemoryStore) FindByIdentity(ctx context.Context, provider, providerUserID string) (User, error) {
	m.mu.RLock()
	userID, linked := m.identities[provider][providerUserID]
	m.mu.RUnlock()
	if !linked {
		return User{}, ErrNotFound
	}
	return m.FindByID(ctx, userID)
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

// LinkIdentity implements Store, mirroring the user_identities constraints:
// one account per provider per user, and one owner per account. The user row
// is not rewritten, so its UpdatedAt does not move.
func (m *MemoryStore) LinkIdentity(_ context.Context, userID, provider, providerUserID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, found := m.findUser(userID); !found {
		return ErrNotFound
	}
	if owner, taken := m.identities[provider][providerUserID]; taken {
		if owner == userID {
			return nil
		}
		return ErrLinkedElsewhere
	}
	for _, owner := range m.identities[provider] {
		if owner == userID {
			return ErrLinkedElsewhere
		}
	}
	m.link(userID, provider, providerUserID)
	return nil
}

// UnlinkIdentity implements Store with the Postgres store's guard: the row
// goes only while the user still holds a password or another identity.
// A missing link is ErrNotFound, a refused unlink is ErrLastCredential.
func (m *MemoryStore) UnlinkIdentity(_ context.Context, userID, provider string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	u, found := m.findUser(userID)
	if !found {
		return ErrNotFound
	}
	// UNIQUE (user_id, provider) holds at most one link per provider.
	var accountID string
	linked := false
	for id, owner := range m.identities[provider] {
		if owner == userID {
			accountID, linked = id, true
			break
		}
	}
	if !linked {
		return ErrNotFound
	}
	total := 0
	for _, owners := range m.identities {
		for _, owner := range owners {
			if owner == userID {
				total++
			}
		}
	}
	if u.PasswordHash == "" && total <= 1 {
		return ErrLastCredential
	}
	delete(m.identities[provider], accountID)
	return nil
}

// ListProfiles implements Store. Users keep the Postgres order:
// created_at, then id.
func (m *MemoryStore) ListProfiles(_ context.Context) ([]Profile, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	users := make([]User, len(m.users))
	copy(users, m.users)
	slices.SortStableFunc(users, func(a, b User) int {
		if byTime := a.CreatedAt.Compare(b.CreatedAt); byTime != 0 {
			return byTime
		}
		return strings.Compare(a.ID, b.ID)
	})

	profiles := make([]Profile, 0, len(users))
	for _, u := range users {
		profiles = append(profiles, m.profile(u))
	}
	return profiles, nil
}

// FindProfileByID implements Store.
func (m *MemoryStore) FindProfileByID(_ context.Context, id string) (Profile, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	u, found := m.findUser(id)
	if !found {
		return Profile{}, ErrNotFound
	}
	return m.profile(u), nil
}

// FindProfileByEmail implements Store. Email matches case-insensitively.
func (m *MemoryStore) FindProfileByEmail(_ context.Context, email string) (Profile, error) {
	email = strings.TrimSpace(email)
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, u := range m.users {
		if strings.EqualFold(u.Email, email) {
			return m.profile(u), nil
		}
	}
	return Profile{}, ErrNotFound
}

// findUser returns the stored user. Caller holds the lock.
func (m *MemoryStore) findUser(userID string) (User, bool) {
	for _, u := range m.users {
		if u.ID == userID {
			return u, true
		}
	}
	return User{}, false
}

// link records one external account. Caller holds the write lock.
func (m *MemoryStore) link(userID, provider, providerUserID string) {
	if m.identities[provider] == nil {
		m.identities[provider] = map[string]string{}
	}
	m.identities[provider][providerUserID] = userID
}

// profile attaches the sorted providers. Caller holds the lock.
// The password hash is cleared to match the Postgres profile reads.
func (m *MemoryStore) profile(u User) Profile {
	u.PasswordHash = ""
	providers := []string{}
	for provider, owners := range m.identities {
		for _, owner := range owners {
			if owner == u.ID {
				providers = append(providers, provider)
				break
			}
		}
	}
	slices.Sort(providers)
	return Profile{User: u, Providers: providers}
}
