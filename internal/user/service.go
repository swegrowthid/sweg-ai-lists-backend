package user

import "context"

// Store is the persistence port. Service depends on it, never on SQL.
type Store interface {
	List(ctx context.Context) ([]User, error)
}

// Service owns user use-cases. No HTTP, no SQL here.
type Service struct {
	store Store
}

// NewService wires the store. Nil store is a programmer bug, so panic early.
func NewService(store Store) *Service {
	if store == nil {
		panic("user: nil Store")
	}
	return &Service{store: store}
}

// List returns all users. Errors pass through untouched (E channel).
func (s *Service) List(ctx context.Context) ([]User, error) {
	return s.store.List(ctx)
}
