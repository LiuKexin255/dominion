// Package service provides the proxy orchestration layer between the gRPC
// handler and the domain/runtime implementations.
package service

import (
	"context"
	"errors"
	"time"

	"dominion/common/gopkg/logs"
	"dominion/common/gopkg/logs/event"
	game "dominion/projects/game"
	"dominion/projects/game/pkg/bind"
	"dominion/projects/game/proxy/domain"
	"dominion/projects/game/proxy/runtime/agentclient"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ProxyService orchestrates proxy operations: owner resolution, agent client
// calls, and bidirectional stream binding.
type ProxyService struct {
	ownerStore  domain.OwnerStore
	ownerPicker domain.OwnerPicker
	manager     agentclient.Manager
	binder      bind.Binder
}

// NewProxyService creates a new ProxyService.
func NewProxyService(
	ownerStore domain.OwnerStore,
	ownerPicker domain.OwnerPicker,
	manager agentclient.Manager,
	binder bind.Binder,
) *ProxyService {
	return &ProxyService{
		ownerStore:  ownerStore,
		ownerPicker: ownerPicker,
		manager:     manager,
		binder:      binder,
	}
}

// resolveOwner resolves the owner for a session, creating one lazily when no
// owner record exists yet.
func (s *ProxyService) resolveOwner(ctx context.Context, sessionID string) (*domain.AgentOwner, error) {
	owner, err := s.ownerStore.Get(ctx, sessionID)
	if err == nil {
		return owner, nil
	}
	if !errors.Is(err, domain.ErrOwnerNotFound) {
		logs.Error(ctx, "resolve owner: store lookup failed",
			event.String("session_id", sessionID),
			event.Err(err),
		)
		return nil, mapDomainError(err)
	}

	// Lazy owner creation: pick a replica and persist the owner record.
	conns, err := s.manager.List(ctx)
	if err != nil {
		logs.Error(ctx, "resolve owner: list connections failed",
			event.String("session_id", sessionID),
			event.Err(err),
		)
		return nil, status.Errorf(codes.Internal, "list agent connections: %v", err)
	}

	pickedRef, err := s.ownerPicker.Pick(ctx, sessionID, conns)
	if err != nil {
		return nil, mapDomainError(err)
	}

	now := time.Now()
	owner = &domain.AgentOwner{
		SessionID:  sessionID,
		OwnerIndex: pickedRef.OwnerIndex,
		Owner:      pickedRef.Owner,
		CreateTime: now,
	}
	if err := s.ownerStore.Create(ctx, owner); err != nil {
		logs.Error(ctx, "resolve owner: create record failed",
			event.String("session_id", sessionID),
			event.Err(err),
		)
		return nil, mapDomainError(err)
	}

	logs.Info(ctx, "owner created lazily",
		event.String("session_id", sessionID),
		event.String("owner", pickedRef.Owner),
		event.Int("agent_index", pickedRef.OwnerIndex),
	)
	return owner, nil
}

// GetAgent returns the Agent resource for the given session.
func (s *ProxyService) GetAgent(ctx context.Context, sessionID string) (*game.Agent, error) {
	owner, err := s.resolveOwner(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	connRef, err := s.manager.Get(ctx, owner.OwnerIndex)
	if err != nil {
		logs.Error(ctx, "get agent: get connection failed",
			event.String("session_id", sessionID),
			event.Int("agent_index", owner.OwnerIndex),
			event.Err(err),
		)
		return nil, status.Errorf(codes.Internal, "get agent connection: %v", err)
	}
	client := agentclient.NewAgentClient(connRef.Conn)

	agent, err := client.GetAgent(ctx, &game.AgentGetRequest{SessionId: sessionID})
	if err != nil {
		logs.Error(ctx, "get agent: downstream call failed",
			event.String("session_id", sessionID),
			event.Err(err),
		)
		return nil, propagateAgentError(err, "get agent")
	}
	return agent, nil
}

// ListMessages lists messages for the given session.
func (s *ProxyService) ListMessages(ctx context.Context, sessionID string, req *game.ListMessagesRequest) (*game.ListMessagesResponse, error) {
	owner, err := s.resolveOwner(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	connRef, err := s.manager.Get(ctx, owner.OwnerIndex)
	if err != nil {
		logs.Error(ctx, "list messages: get connection failed",
			event.String("session_id", sessionID),
			event.Int("agent_index", owner.OwnerIndex),
			event.Err(err),
		)
		return nil, status.Errorf(codes.Internal, "get agent connection: %v", err)
	}
	client := agentclient.NewAgentClient(connRef.Conn)

	resp, err := client.ListMessages(ctx, req)
	if err != nil {
		logs.Error(ctx, "list messages: downstream call failed",
			event.String("session_id", sessionID),
			event.Err(err),
		)
		return nil, propagateAgentError(err, "list messages")
	}
	return resp, nil
}

// Connect establishes a bidirectional stream to the agent for the given session.
// The handler has already read and validated the first frame; it is injected
// into the proxy stream so the binder forwards it first.
func (s *ProxyService) Connect(
	ctx context.Context,
	sessionID string,
	firstFrame *game.AgentFrame,
	stream game.ProxyService_ConnectAgentServer,
) error {
	owner, err := s.resolveOwner(ctx, sessionID)
	if err != nil {
		return err
	}

	connRef, err := s.manager.Get(ctx, owner.OwnerIndex)
	if err != nil {
		logs.Error(ctx, "connect agent: get connection failed",
			event.String("session_id", sessionID),
			event.Int("agent_index", owner.OwnerIndex),
			event.Err(err),
		)
		return status.Errorf(codes.Internal, "get agent connection: %v", err)
	}
	client := agentclient.NewAgentClient(connRef.Conn)

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

	prefixed := bind.WithFirstFrame(stream, firstFrame)
	if err := s.binder.Bind(prefixed, agentStream); err != nil {
		logs.Error(ctx, "connect agent: bind failed",
			event.String("session_id", sessionID),
			event.Int("agent_index", owner.OwnerIndex),
			event.Err(err),
		)
		return err
	}
	return nil
}

// propagateAgentError returns a downstream gRPC status error unchanged,
// or wraps a non-status error as Internal so the proxy does not mask
// agent-level status codes.
func propagateAgentError(err error, msg string) error {
	if err == nil {
		return nil
	}
	if st, ok := status.FromError(err); ok {
		return st.Err()
	}
	return status.Errorf(codes.Internal, "%s: %v", msg, err)
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
