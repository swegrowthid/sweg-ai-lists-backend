package news

import (
	"context"
	"fmt"
	"sort"
	"strings"
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
		if existing.Title != input.Title || existing.Source != input.Source || existing.Summary != input.Summary || !existing.PublishedAt.Equal(input.PublishedAt) {
			existing.Title = input.Title
			existing.Source = input.Source
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
		Source:      input.Source,
		Summary:     input.Summary,
		PublishedAt: input.PublishedAt,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	m.entries = append(m.entries, created)
	return created, nil
}

// ListPage implements Store with the same filter, order, and paging rules as
// the Postgres store. PerPage <= 0 returns every match.
func (m *MemoryStore) ListPage(_ context.Context, filter StoreFilter) ([]Entry, int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	query := strings.ToLower(filter.Query)
	sourceSet := map[string]bool{}
	for _, name := range filter.Sources {
		sourceSet[name] = true
	}

	matched := []Entry{}
	for _, entry := range m.entries {
		source := entry.Source
		if source == "" {
			source = legacySource
		}
		if query != "" &&
			!strings.Contains(strings.ToLower(entry.Title), query) &&
			!strings.Contains(strings.ToLower(entry.Summary), query) &&
			!strings.Contains(strings.ToLower(source), query) {
			continue
		}
		if len(sourceSet) > 0 && !sourceSet[source] {
			continue
		}
		entry.Source = source
		matched = append(matched, entry)
	}
	sortEntries(matched)

	total := len(matched)
	if filter.PerPage <= 0 {
		return matched, total, nil
	}
	offset := 0
	if filter.Page > 1 {
		offset = (filter.Page - 1) * filter.PerPage
	}
	if offset >= total {
		return []Entry{}, total, nil
	}
	end := offset + filter.PerPage
	if end > total {
		end = total
	}
	return matched[offset:end], total, nil
}

// SourceCounts implements Store: one count per normalized source name.
func (m *MemoryStore) SourceCounts(_ context.Context) (map[string]int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	counts := map[string]int{}
	for _, entry := range m.entries {
		source := entry.Source
		if source == "" {
			source = legacySource
		}
		counts[source]++
	}
	return counts, nil
}

// sortEntries orders newest published first, id as the tie-break. Same
// order as the Postgres store.
func sortEntries(entries []Entry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if !entries[i].PublishedAt.Equal(entries[j].PublishedAt) {
			return entries[i].PublishedAt.After(entries[j].PublishedAt)
		}
		return entries[i].ID < entries[j].ID
	})
}
