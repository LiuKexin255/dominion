// Package domain defines the core interfaces and types for the proxy service.
package domain

import (
	"context"

	game "dominion/projects/game"
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
	// Pick selects an agent client for the session using a hash-based
	// strategy. Returns the client or an error if no clients are available.
	Pick(ctx context.Context, sessionID string, clients []agentclient.ClientRef) (agentclient.ClientRef, error)
}

// ConnectAgenter handles agent connection streams by reading the first frame,
// resolving ownership, and establishing bidirectional forwarding.
type ConnectAgenter interface {
	// Connect handles a ConnectAgent gRPC stream.
	Connect(stream game.ProxyService_ConnectAgentServer) error
}
