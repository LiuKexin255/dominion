// Package domain defines the core interfaces and types for the proxy service.
package domain

import (
	"context"

	"dominion/projects/game/proxy/runtime/agentclient"
)

// OwnerStore defines storage operations for AgentOwner entities.
type OwnerStore interface {
	// Create stores a new agent owner record and returns the created entity.
	Create(ctx context.Context, owner *AgentOwner) error
	// Get retrieves an agent owner by its (templateID, sessionID) composite
	// key. A session is identified by the resource pattern
	// templates/{template}/sessions/{session}, so the template ID is part of
	// the key.
	Get(ctx context.Context, templateID, sessionID string) (*AgentOwner, error)
	// Delete removes an agent owner record by its (templateID, sessionID)
	// composite key.
	Delete(ctx context.Context, templateID, sessionID string) error
}

// OwnerPicker selects an agent instance for a given session.
type OwnerPicker interface {
	// Pick selects an agent connection for the session using a hash-based
	// strategy. Returns the connection or an error if no connections are available.
	Pick(ctx context.Context, sessionID string, conns []*agentclient.ConnRef) (*agentclient.ConnRef, error)
}
