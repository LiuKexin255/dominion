// Package service provides the proxy orchestration layer between the gRPC
// handler and the domain/runtime implementations.
package service

import (
	"errors"

	"dominion/common/gopkg/logs"
	"dominion/common/gopkg/logs/event"
	game "dominion/projects/game"
	"dominion/projects/game/pkg/bind"
	"dominion/projects/game/proxy/domain"
	"dominion/projects/game/proxy/runtime/agentclient"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// connectAgenter implements domain.ConnectAgenter.
type connectAgenter struct {
	ownerStore domain.OwnerStore
	manager    agentclient.Manager
	binder     bind.Binder
}

// NewConnectAgenter creates a new ConnectAgenter implementation.
func NewConnectAgenter(
	ownerStore domain.OwnerStore,
	manager agentclient.Manager,
	binder bind.Binder,
) domain.ConnectAgenter {
	return &connectAgenter{
		ownerStore: ownerStore,
		manager:    manager,
		binder:     binder,
	}
}

// Connect handles a ConnectAgent gRPC stream by reading the first frame,
// resolving ownership, and establishing bidirectional forwarding via the Binder.
func (c *connectAgenter) Connect(stream game.ProxyService_ConnectAgentServer) error {
	ctx := stream.Context()

	// Receive the initial frame to identify the session.
	frame, err := stream.Recv()
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "failed to receive initial frame: %v", err)
	}

	sessionID := frame.GetSessionId()
	if sessionID == "" {
		return status.Error(codes.InvalidArgument, "session_id is required in the first frame")
	}

	// Look up the owner for this session.
	owner, err := c.ownerStore.Get(ctx, sessionID)
	if err != nil {
		return mapDomainError(err)
	}

	// Get the cached connection for the owner instance and create a client.
	connRef, err := c.manager.Get(ctx, owner.OwnerIndex)
	if err != nil {
		logs.Error(ctx, "connect agent: get connection failed",
			event.String("session_id", sessionID),
			event.Int("agent_index", owner.OwnerIndex),
			event.Err(err),
		)
		return status.Errorf(codes.Internal, "get agent connection: %v", err)
	}
	client := agentclient.NewAgentClient(connRef.Conn)

	// Establish a bidirectional stream to the agent.
	agentStream, err := client.Connect(ctx)
	if err != nil {
		logs.Error(ctx, "connect agent: open stream failed",
			event.String("session_id", sessionID),
			event.Err(err),
		)
		return status.Errorf(codes.Internal, "connect to agent: %v", err)
	}

	logs.Info(ctx, "agent stream connected",
		event.String("session_id", sessionID),
		event.Int("agent_index", owner.OwnerIndex),
	)

	// The first frame was already read from the gateway — inject it via
	// WithFirstFrame so the Binder forwards it to the agent first.
	prefixed := bind.WithFirstFrame(stream, frame)

	return c.binder.Bind(prefixed, agentStream)
}

// mapDomainError converts domain errors to gRPC status errors.
func mapDomainError(err error) error {
	switch {
	case errors.Is(err, domain.ErrOwnerNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, domain.ErrOwnerAlreadyExists):
		return status.Error(codes.AlreadyExists, err.Error())
	case errors.Is(err, domain.ErrNoAgentInstances):
		return status.Error(codes.Unavailable, err.Error())
	default:
		return err
	}
}
