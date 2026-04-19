// Package domain contains the game session domain model and repository contract.
package domain

import "context"

// Repository defines persistence operations for sessions.
type Repository interface {
	// Get retrieves a session by name.
	Get(ctx context.Context, name string) (*Session, error)

	// Save persists a session, creating or updating it as needed.
	Save(ctx context.Context, session *Session) error

	// Delete removes a session by name.
	Delete(ctx context.Context, name string) error
}
