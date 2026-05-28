package domain

import (
	"context"
)

// SessionRepository defines storage operations for Session entities.
type SessionRepository interface {
	// Get retrieves a session by its session ID.
	Get(ctx context.Context, sessionID string) (*Session, error)
	// Create stores a new session and returns the created entity.
	Create(ctx context.Context, session *Session) (*Session, error)
	// Delete removes a session by its session ID.
	Delete(ctx context.Context, sessionID string) error
}
