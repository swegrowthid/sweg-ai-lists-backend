package tools

import (
	"context"
	"sort"
	"strings"
	"sync"
)

// MemoryStore is the in-memory Store. The source markdown files are the
// database; this store only holds the parsed snapshot between syncs.
type MemoryStore struct {
	mu    sync.RWMutex
	tools []Tool
	index map[string]int
}

// NewMemoryStore builds an empty store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{index: map[string]int{}}
}

// ReplaceAll implements Store: one atomic swap of the whole snapshot.
func (m *MemoryStore) ReplaceAll(_ context.Context, tools []Tool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	index := make(map[string]int, len(tools))
	for i, tool := range tools {
		if _, dup := index[tool.ID]; !dup {
			index[tool.ID] = i
		}
	}
	m.tools = tools
	m.index = index
	return nil
}

// List implements Store. The snapshot keeps catalog order: category order
// first, then file order inside each category. A Sort on the filter reorders
// the match instead.
func (m *MemoryStore) List(_ context.Context, filter ListFilter) ([]Tool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]Tool, 0, len(m.tools))
	for _, tool := range m.tools {
		if filter.Category != "" && tool.Category != filter.Category {
			continue
		}
		if filter.Query != "" && !strings.Contains(strings.ToLower(tool.Name), filter.Query) {
			continue
		}
		out = append(out, tool)
	}
	sortTools(out, filter.Sort, filter.Order)
	return out, nil
}

// sortTools orders rows by the requested key, or leaves catalog order alone
// when the key is empty. Rows that tie on the key order by id ascending in both
// directions, so a page boundary never repeats or drops a row.
func sortTools(rows []Tool, by, order string) {
	if by == "" {
		return
	}
	desc := order == OrderDesc
	sort.SliceStable(rows, func(i, j int) bool {
		cmp := compareToolKey(rows[i], rows[j], by)
		if cmp == 0 {
			return rows[i].ID < rows[j].ID
		}
		if desc {
			return cmp > 0
		}
		return cmp < 0
	})
}

// compareToolKey orders two rows by the sort key alone. The updated column is
// a YYYY-MM-DD text, so a plain string compare sorts it chronologically; source
// text the parser could not turn into a date sorts along with it.
func compareToolKey(a, b Tool, by string) int {
	if by == SortUpdated {
		return strings.Compare(a.Updated, b.Updated)
	}
	return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
}

// Get implements Store. The id is already normalized by the service.
func (m *MemoryStore) Get(_ context.Context, id string) (Tool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	i, ok := m.index[id]
	if !ok {
		return Tool{}, ErrNotFound
	}
	return m.tools[i], nil
}
