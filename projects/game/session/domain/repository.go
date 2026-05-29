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
	// List retrieves a page of sessions using cursor-based pagination.
	// pageSize controls the maximum number of results; pageToken is the _id cursor.
	List(ctx context.Context, pageSize int, pageToken string) (*ListSessionsResult, error)
}
