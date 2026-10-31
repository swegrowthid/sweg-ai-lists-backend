package auth

import (
	"context"
	"crypto/subtle"
	"strings"
	"sync"
	"time"
)

// MemoryMagicStore is the in-memory MagicStore. Use for dev and tests.
// It preserves the same observable contract and atomic single-use guarantee as Postgres.
type MemoryMagicStore struct {
	mu      sync.Mutex
	records map[string]MagicLinkRecord
}

// NewMemoryMagicStore builds an empty store.
func NewMemoryMagicStore() *MemoryMagicStore {
	return &MemoryMagicStore{records: map[string]MagicLinkRecord{}}
}

// Save implements MagicStore. It supersedes any still-active link for the same
// email so only the newest request stays usable, and prunes rows older than 24
// hours past use or expiry.
func (m *MemoryMagicStore) Save(_ context.Context, rec MagicLinkRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = now
	}
	rec.Email = strings.ToLower(strings.TrimSpace(rec.Email))

	for hash, existing := range m.records {
		if strings.EqualFold(existing.Email, rec.Email) && existing.UsedAt == nil {
			existing.UsedAt = &now
			m.records[hash] = existing
		}
	}

	cutoff := rec.CreatedAt.Add(-magicLinkStaleAge)
	for hash, existing := range m.records {
		if (existing.UsedAt != nil && existing.UsedAt.Before(cutoff)) || existing.ExpiresAt.Before(cutoff) {
			delete(m.records, hash)
		}
	}

	m.records[rec.TokenHash] = rec
	return nil
}

// Consume implements MagicStore. Under the mutex, only the first caller on an
// unused, unexpired token wins; all others receive ErrMagicLinkInvalid.
func (m *MemoryMagicStore) Consume(_ context.Context, tokenHash string, now time.Time) (MagicLinkRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	rec, ok := m.records[tokenHash]
	if !ok {
		return MagicLinkRecord{}, ErrMagicLinkInvalid
	}
	if rec.UsedAt != nil {
		return MagicLinkRecord{}, ErrMagicLinkInvalid
	}
	if !rec.ExpiresAt.After(now) {
		return MagicLinkRecord{}, ErrMagicLinkInvalid
	}

	used := now.UTC()
	rec.UsedAt = &used
	m.records[tokenHash] = rec
	return rec, nil
}

// ConsumeCode implements MagicStore. The code hash is compared in constant
// time; each miss increments CodeAttempts and the row locks at
// magicCodeMaxAttempts.
func (m *MemoryMagicStore) ConsumeCode(_ context.Context, email, codeHash string, now time.Time) (MagicLinkRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var rec MagicLinkRecord
	found := false
	for _, existing := range m.records {
		if !strings.EqualFold(existing.Email, email) || existing.UsedAt != nil || !existing.ExpiresAt.After(now) {
			continue
		}
		if !found || existing.CreatedAt.After(rec.CreatedAt) {
			rec = existing
			found = true
		}
	}
	if !found {
		return MagicLinkRecord{}, ErrMagicLinkInvalid
	}
	if rec.CodeAttempts >= magicCodeMaxAttempts {
		return MagicLinkRecord{}, ErrMagicLinkInvalid
	}
	if subtle.ConstantTimeCompare([]byte(rec.CodeHash), []byte(codeHash)) != 1 {
		rec.CodeAttempts++
		m.records[rec.TokenHash] = rec
		return MagicLinkRecord{}, ErrMagicLinkInvalid
	}

	used := now.UTC()
	rec.UsedAt = &used
	m.records[rec.TokenHash] = rec
	return rec, nil
}

// CountRecent implements MagicStore.
func (m *MemoryMagicStore) CountRecent(_ context.Context, email string, since time.Time) (int, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	m.mu.Lock()
	defer m.mu.Unlock()

	count := 0
	for _, rec := range m.records {
		if strings.EqualFold(rec.Email, email) && rec.CreatedAt.After(since) {
			count++
		}
	}
	return count, nil
}
