package domain

import (
	"context"

	"dominion/common/gopkg/solver"
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
	// Pick selects an agent instance index for the session using a hash-based
	// strategy. Returns the instance index or an error if no instances are available.
	Pick(ctx context.Context, sessionID string, instances []*solver.StatefulInstance) (int, error)
}
