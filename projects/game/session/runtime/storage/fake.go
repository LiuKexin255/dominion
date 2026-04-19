// Package storage provides an in-memory FakeStore for testing session operations.
package storage

import (
	"context"
	"sync"
	"time"

	"dominion/projects/game/session/domain"
)

// FakeStore is an in-memory implementation of domain.Repository for testing.
type FakeStore struct {
	mu       sync.RWMutex
	sessions map[string]*domain.Session // key = resource name sessions/{id}
	nowFunc  func() time.Time           // for testability, defaults to time.Now
}

// NewFakeStore creates a new FakeStore ready for use.
func NewFakeStore() *FakeStore {
	return &FakeStore{
		sessions: make(map[string]*domain.Session),
		nowFunc:  time.Now,
	}
}

// NewFakeStoreWithNow creates a new FakeStore with a configurable nowFunc for time-related testing.
func NewFakeStoreWithNow(nowFunc func() time.Time) *FakeStore {
	return &FakeStore{
		sessions: make(map[string]*domain.Session),
		nowFunc:  nowFunc,
	}
}

// Get retrieves a session by name. Returns ErrNotFound if the session does not exist.
func (s *FakeStore) Get(_ context.Context, name string) (*domain.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	snap, exists := s.sessions[name]
	if !exists {
		return nil, domain.ErrNotFound
	}

	// Return a copy via Rehydrate to prevent mutation of stored state.
	return domain.Rehydrate(snap.Snapshot())
}

// Save persists a session, creating or updating it as needed.
// Returns ErrAlreadyExists if a session with the same name but different ID already exists.
func (s *FakeStore) Save(_ context.Context, session *domain.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	snap := session.Snapshot()
	name := sessionName(snap.ID)

	if existing, exists := s.sessions[name]; exists {
		// Allow updating the same session (same ID).
		if existing.Snapshot().ID != snap.ID {
			return domain.ErrAlreadyExists
		}
	}

	// Store a copy to prevent mutation from outside.
	copy, err := domain.Rehydrate(snap)
	if err != nil {
		return err
	}

	s.sessions[name] = copy

	return nil
}

// Delete removes a session by name. Returns ErrNotFound if the session does not exist.
func (s *FakeStore) Delete(_ context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.sessions[name]; !exists {
		return domain.ErrNotFound
	}

	delete(s.sessions, name)

	return nil
}
