package domain

import (
	"context"
)

// SessionRepository defines storage operations for Session entities.
type SessionRepository interface {
	// Get retrieves a session by its resource name.
	Get(ctx context.Context, name string) (*Session, error)
	// Create stores a new session and returns the created entity.
	Create(ctx context.Context, session *Session) (*Session, error)
	// Delete removes a session by its resource name.
	Delete(ctx context.Context, name string) error
}
