// Package domain defines the core types and interfaces for the agent service.
package domain

import (
	"context"

	game "dominion/projects/game"
)

// AgentStream is the bidirectional stream interface for agent communication.
// Implementations wrap gRPC or WebSocket streams for exchanging AgentFrames.
type AgentStream interface {
	// Recv receives the next AgentFrame from the stream.
	// Returns io.EOF when the stream is closed by the peer.
	Recv() (*game.AgentFrame, error)
	// Send sends an AgentFrame on the stream.
	Send(*game.AgentFrame) error
}

// Runtime defines the interface for agent runtime operations.
// Implementations provide the actual in-memory or persistent behavior.
type Runtime interface {
	// Create creates the agent for the given session and returns the
	// resulting status.
	Create(ctx context.Context, sessionID string) (*Status, error)
	// Delete removes the agent for the given session.
	// It is idempotent: deleting a non-existent session returns nil.
	Delete(ctx context.Context, sessionID string) error
	// Status returns the current status of the agent for the given session.
	Status(ctx context.Context, sessionID string) (*Status, error)
	// Connect handles a bidirectional stream for agent communication.
	// The implementation processes incoming AgentFrames and sends responses.
	Connect(stream AgentStream) error
}
