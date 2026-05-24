// Package domain defines the session domain model and repository contract.
package domain

import (
	"time"
)

// Session represents a game session domain entity.
type Session struct {
	// Name is the resource name of the session, e.g. "sessions/abc123".
	Name string
	// SessionID is the unique identifier for this session.
	SessionID string
	// CreateTime is the timestamp when this session was created.
	CreateTime time.Time
}
