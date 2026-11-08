package tools

import (
	"context"
	"sync"
)

// MemoryVoteStore is the in-memory VoteStore. Votes live only in the process;
// a restart drops them. Production uses PostgresVoteStore instead.
type MemoryVoteStore struct {
	mu sync.RWMutex
	// byTool maps a tool id to the set of user ids that voted for it.
	byTool map[string]map[string]struct{}
	// byUser maps a user id to the set of tool ids it voted for, so the
	// viewer flag in Summaries needs no scan over every tool.
	byUser map[string]map[string]struct{}
}

// NewMemoryVoteStore builds an empty store.
func NewMemoryVoteStore() *MemoryVoteStore {
	return &MemoryVoteStore{
		byTool: map[string]map[string]struct{}{},
		byUser: map[string]map[string]struct{}{},
	}
}

// Add implements VoteStore: the first vote records the pair, a repeat vote
// records nothing and reports created false.
func (m *MemoryVoteStore) Add(_ context.Context, userID, toolID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	users := m.byTool[toolID]
	if users == nil {
		users = map[string]struct{}{}
		m.byTool[toolID] = users
	}
	if _, dup := users[userID]; dup {
		return false, nil
	}
	users[userID] = struct{}{}

	tools := m.byUser[userID]
	if tools == nil {
		tools = map[string]struct{}{}
		m.byUser[userID] = tools
	}
	tools[toolID] = struct{}{}
	return true, nil
}

// Remove implements VoteStore: withdrawing a missing vote is a no-op that
// reports removed false, never an error.
func (m *MemoryVoteStore) Remove(_ context.Context, userID, toolID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	users := m.byTool[toolID]
	if _, ok := users[userID]; !ok {
		return false, nil
	}
	delete(users, userID)
	if len(users) == 0 {
		delete(m.byTool, toolID)
	}

	tools := m.byUser[userID]
	delete(tools, toolID)
	if len(tools) == 0 {
		delete(m.byUser, userID)
	}
	return true, nil
}

// Summaries implements VoteStore. Tool ids without votes stay absent from the
// map, mirroring the Postgres GROUP BY query; the service fills the zero
// default. An empty viewer id means an anonymous reader.
func (m *MemoryVoteStore) Summaries(_ context.Context, viewerID string, toolIDs []string) (map[string]VoteSummary, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make(map[string]VoteSummary, len(toolIDs))
	voted := m.byUser[viewerID]
	for _, id := range toolIDs {
		users := m.byTool[id]
		if len(users) == 0 {
			continue
		}
		s := VoteSummary{Votes: len(users)}
		if viewerID != "" {
			_, s.Voted = voted[id]
		}
		out[id] = s
	}
	return out, nil
}
