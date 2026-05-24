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
	// Init initializes the agent for the given session and returns the
	// resulting status.
	Init(ctx context.Context, sessionID string) (*Status, error)
	// Status returns the current status of the agent for the given session.
	Status(ctx context.Context, sessionID string) (*Status, error)
	// Connect handles a bidirectional stream for agent communication.
	// The implementation processes incoming AgentFrames and sends responses.
	Connect(stream AgentStream) error
}
