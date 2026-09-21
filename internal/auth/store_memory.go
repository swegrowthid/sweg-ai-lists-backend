package auth

import (
	"context"
	"sync"
	"time"
)

// MemoryRefreshStore is the in-memory RefreshStore. Use for dev and tests.
// It keeps the same observable contract as the Postgres store.
type MemoryRefreshStore struct {
	mu      sync.Mutex
	records map[string]RefreshRecord
}

// NewMemoryRefreshStore builds an empty store.
func NewMemoryRefreshStore() *MemoryRefreshStore {
	return &MemoryRefreshStore{records: map[string]RefreshRecord{}}
}

// Save implements RefreshStore.
func (m *MemoryRefreshStore) Save(_ context.Context, rec RefreshRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	}
	m.records[rec.JTI] = rec
	return nil
}

// Consume implements RefreshStore with the same contract as Postgres.
func (m *MemoryRefreshStore) Consume(_ context.Context, jti string, now time.Time) (RefreshRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.records[jti]
	if !ok {
		return RefreshRecord{}, ErrRefreshInvalid
	}
	if rec.RevokedAt != nil {
		return rec, ErrRefreshReused
	}
	if !rec.ExpiresAt.After(now) {
		return rec, ErrRefreshInvalid
	}
	revoked := now.UTC()
	rec.RevokedAt = &revoked
	m.records[jti] = rec
	rec.RevokedAt = nil
	return rec, nil
}

// Revoke implements RefreshStore.
func (m *MemoryRefreshStore) Revoke(_ context.Context, jti string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.records[jti]
	if !ok || rec.RevokedAt != nil {
		return nil
	}
	now := time.Now().UTC()
	rec.RevokedAt = &now
	m.records[jti] = rec
	return nil
}

// RevokeAllForUser implements RefreshStore.
func (m *MemoryRefreshStore) RevokeAllForUser(_ context.Context, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	for jti, rec := range m.records {
		if rec.UserID == userID && rec.RevokedAt == nil {
			rec.RevokedAt = &now
			m.records[jti] = rec
		}
	}
	return nil
}
