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
	// Get retrieves an agent owner by session ID.
	Get(ctx context.Context, sessionID string) (*AgentOwner, error)
	// Delete removes an agent owner record by session ID.
	Delete(ctx context.Context, sessionID string) error
}

// OwnerPicker selects an agent instance for a given session.
type OwnerPicker interface {
	// Pick selects an agent connection for the session using a hash-based
	// strategy. Returns the connection or an error if no connections are available.
	Pick(ctx context.Context, sessionID string, conns []*agentclient.ConnRef) (*agentclient.ConnRef, error)
}
