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

// grpcServerStream adapts a gRPC proxy ConnectAgent server stream to AgentFrameStream.
type grpcServerStream struct {
	stream game.ProxyService_ConnectAgentServer
}

func (s *grpcServerStream) Recv() (*game.AgentFrame, error) {
	return s.stream.Recv()
}

func (s *grpcServerStream) Send(frame *game.AgentFrame) error {
	return s.stream.Send(frame)
}

// grpcClientStream adapts a gRPC agent Connect client stream to AgentFrameStream.
type grpcClientStream struct {
	stream game.AgentService_ConnectClient
}

func (s *grpcClientStream) Recv() (*game.AgentFrame, error) {
	return s.stream.Recv()
}

func (s *grpcClientStream) Send(frame *game.AgentFrame) error {
	return s.stream.Send(frame)
}

// prefixedGatewayStream wraps an AgentFrameStream and returns a pre-read first
// frame on the first Recv() call, then delegates to the inner stream.
type prefixedGatewayStream struct {
	inner      bind.AgentFrameStream
	firstFrame *game.AgentFrame
	sentFirst  bool
}

func (s *prefixedGatewayStream) Recv() (*game.AgentFrame, error) {
	if !s.sentFirst {
		s.sentFirst = true
		return s.firstFrame, nil
	}
	return s.inner.Recv()
}

func (s *prefixedGatewayStream) Send(frame *game.AgentFrame) error {
	return s.inner.Send(frame)
}

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

	// Get the cached client for the owner instance.
	client, err := c.manager.Get(ctx, owner.OwnerIndex)
	if err != nil {
		logs.Error(ctx, "connect agent: get client failed",
			event.String("session_id", sessionID),
			event.Int("agent_index", owner.OwnerIndex),
			event.Err(err),
		)
		return status.Errorf(codes.Internal, "get agent client: %v", err)
	}

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

	// Wrap streams as AgentFrameStream adapters.
	gatewayAdapter := &grpcServerStream{stream: stream}
	agentAdapter := &grpcClientStream{stream: agentStream}

	// The first frame was already read from the gateway — inject it via the
	// prefixed stream so the Binder forwards it to the agent first.
	prefixStream := &prefixedGatewayStream{
		inner:      gatewayAdapter,
		firstFrame: frame,
	}

	return c.binder.Bind(ctx, prefixStream, agentAdapter)
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
