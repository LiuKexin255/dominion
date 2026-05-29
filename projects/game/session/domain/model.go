// Package domain defines the session domain model and repository contract.
package domain

import (
	"time"
)

// Session represents a game session domain entity.
type Session struct {
	// SessionID is the unique identifier for this session.
	SessionID string
	// CreateTime is the timestamp when this session was created.
	CreateTime time.Time
}

// ListSessionsResult is the result of listing sessions.
type ListSessionsResult struct {
	// Sessions is the list of sessions in the current page.
	Sessions []*Session
	// NextPageToken is the token to retrieve the next page of results.
	NextPageToken string
}
