// Package domain defines the proxy domain model and interfaces.
package domain

import (
	"time"
)

// AgentOwner represents an agent ownership record in a game session.
type AgentOwner struct {
	// SessionID is the unique identifier of the session.
	SessionID string
	// OwnerIndex is the index of the owner within the session.
	OwnerIndex int
	// Owner is the identifier of the owner.
	Owner string
	// AgentProfileName is the profile name used to create this agent.
	AgentProfileName string
	// CreateTime is the timestamp when this ownership record was created.
	CreateTime time.Time
}
