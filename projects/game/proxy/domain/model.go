// Package domain defines the proxy domain model and interfaces.
package domain

import (
	"time"
)

// AgentOwner represents an agent ownership record in a game session.
type AgentOwner struct {
	// TemplateID is part of the owner's composite key (templateID, sessionID):
	// a session is identified by the resource pattern
	// templates/{template}/sessions/{session}
	// (projects/game/game.proto), so the same session ID under different
	// templates is a distinct session and must not share an owner.
	TemplateID string
	// SessionID is the unique identifier of the session.
	SessionID string
	// OwnerIndex is the index of the owner within the session.
	OwnerIndex int
	// Owner is the identifier of the owner.
	Owner string
	// CreateTime is the timestamp when this ownership record was created.
	CreateTime time.Time
}
