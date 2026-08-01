// Package domain defines the session domain model and repository contract.
package domain

import (
	"time"
)

// Session represents a game session domain entity, scoped to a template
// (spec 031-team-template-mode FR-002: templates/{template}/sessions/{session}).
type Session struct {
	// Template is the template path segment this session belongs to
	// (e.g. "saolei").
	Template string
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

// DefaultListSessionsPageSize is the default page size when listing sessions.
const DefaultListSessionsPageSize = 50

// MaxListSessionsPageSize is the maximum allowed page size when listing sessions.
const MaxListSessionsPageSize = 1000

// ListPageCursor represents the cursor for cursor-based pagination.
// The cursor encodes the last seen session's create time and session ID,
// enabling consistent pagination in a list ordered by create_time DESC, session_id DESC.
type ListPageCursor struct {
	// CreateTime is the create time of the last session in the current page.
	CreateTime time.Time
	// SessionID is the session ID of the last session in the current page.
	SessionID string
}
