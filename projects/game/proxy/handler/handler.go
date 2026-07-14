// Package handler implements the ProxyService gRPC server interface.
//
// The handler owns owner resolution, agent-client routing, and bidirectional
// stream binding directly. There is no separate service layer: Get/ListMessages/
// RefreshAgent require an existing owner (Connect is the only RPC that allocates
// one), which mirrors that an Agent is not independently creatable — it only
// comes into existence when a desktop connects.
package handler

import (
	"context"
	"errors"
	"time"

	"dominion/common/gopkg/logs"
	"dominion/common/gopkg/logs/event"
	game "dominion/projects/game"
	"dominion/projects/game/pkg/bind"
	gameconst "dominion/projects/game/pkg/gameconst"
	"dominion/projects/game/proxy/domain"
	"dominion/projects/game/proxy/runtime/agentclient"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// ProxyHandler implements game.ProxyServiceServer.
type ProxyHandler struct {
	game.UnimplementedProxyServiceServer

	ownerStore  domain.OwnerStore
	ownerPicker domain.OwnerPicker
	manager     agentclient.Manager
	binder      bind.Binder
}

// NewProxyHandler creates a new ProxyHandler.
func NewProxyHandler(
	ownerStore domain.OwnerStore,
	ownerPicker domain.OwnerPicker,
	manager agentclient.Manager,
	binder bind.Binder,
) *ProxyHandler {
	return &ProxyHandler{
		ownerStore:  ownerStore,
		ownerPicker: ownerPicker,
		manager:     manager,
		binder:      binder,
	}
}

// GetAgent returns the Agent resource identified by name. The owner must already
// exist; it is created only by ConnectAgent.
func (h *ProxyHandler) GetAgent(ctx context.Context, req *game.GetAgentRequest) (*game.Agent, error) {
	sessionID, err := gameconst.AgentSessionID(req.GetName())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	owner, err := h.lookupOwner(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	client, err := h.agentClient(ctx, owner)
	if err != nil {
		return nil, err
	}

	agent, err := client.GetAgent(ctx, &game.GetAgentRequest{Name: gameconst.AgentName(sessionID)})
	if err != nil {
		logs.Error(ctx, "get agent: downstream call failed",
			event.String("session_id", sessionID),
			event.Err(err),
		)
		return nil, propagateAgentError(err, "get agent")
	}
	return agent, nil
}

// ListMessages lists messages for the agent owning the session. The owner must
// already exist.
func (h *ProxyHandler) ListMessages(ctx context.Context, req *game.ListMessagesRequest) (*game.ListMessagesResponse, error) {
	sessionID, err := gameconst.SessionID(req.GetParent())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	owner, err := h.lookupOwner(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	client, err := h.agentClient(ctx, owner)
	if err != nil {
		return nil, err
	}

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

// ConnectAgent establishes a bidirectional streaming channel for agent
// communication. This is the only RPC that allocates an owner for a session:
// if no owner exists yet, one is picked and persisted before the stream is bound.
func (h *ProxyHandler) ConnectAgent(stream game.ProxyService_ConnectAgentServer) error {
	ctx := stream.Context()

	frame, err := stream.Recv()
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "failed to receive initial frame: %v", err)
	}

	sessionID := frame.GetSessionId()
	if sessionID == "" {
		return status.Error(codes.InvalidArgument, "session_id is required in the first frame")
	}

	owner, err := h.assignOwner(ctx, sessionID)
	if err != nil {
		return err
	}

	client, err := h.agentClient(ctx, owner)
	if err != nil {
		return err
	}

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

	prefixed := bind.WithFirstFrame(stream, frame)
	if err := h.binder.Bind(prefixed, agentStream); err != nil {
		logs.Error(ctx, "connect agent: bind failed",
			event.String("session_id", sessionID),
			event.Int("agent_index", owner.OwnerIndex),
			event.Err(err),
		)
		return err
	}
	return nil
}

// RefreshAgent forwards a RefreshAgent request to the agent owning the session.
// The owner must already exist (Connect must have run first).
func (h *ProxyHandler) RefreshAgent(ctx context.Context, req *game.RefreshAgentRequest) (*emptypb.Empty, error) {
	sessionID, err := gameconst.AgentSessionID(req.GetName())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	owner, err := h.lookupOwner(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	client, err := h.agentClient(ctx, owner)
	if err != nil {
		return nil, err
	}

	resp, err := client.RefreshAgent(ctx, &game.RefreshAgentRequest{Name: gameconst.AgentName(sessionID)})
	if err != nil {
		logs.Error(ctx, "refresh agent: downstream call failed",
			event.String("session_id", sessionID),
			event.Err(err),
		)
		return nil, propagateAgentError(err, "refresh agent")
	}
	return resp, nil
}

// lookupOwner returns the existing owner for a session or a mapped status error.
// It does NOT create an owner; only ConnectAgent allocates one.
func (h *ProxyHandler) lookupOwner(ctx context.Context, sessionID string) (*domain.AgentOwner, error) {
	owner, err := h.ownerStore.Get(ctx, sessionID)
	if err != nil {
		logs.Error(ctx, "owner lookup failed",
			event.String("session_id", sessionID),
			event.Err(err),
		)
		return nil, mapDomainError(err)
	}
	return owner, nil
}

// assignOwner returns the existing owner for a session, or picks and persists a
// new one when no owner exists yet. Used only by ConnectAgent.
func (h *ProxyHandler) assignOwner(ctx context.Context, sessionID string) (*domain.AgentOwner, error) {
	owner, err := h.ownerStore.Get(ctx, sessionID)
	if err == nil {
		return owner, nil
	}
	if !errors.Is(err, domain.ErrOwnerNotFound) {
		logs.Error(ctx, "assign owner: store lookup failed",
			event.String("session_id", sessionID),
			event.Err(err),
		)
		return nil, mapDomainError(err)
	}

	conns, err := h.manager.List(ctx)
	if err != nil {
		logs.Error(ctx, "assign owner: list connections failed",
			event.String("session_id", sessionID),
			event.Err(err),
		)
		return nil, status.Errorf(codes.Internal, "list agent connections: %v", err)
	}

	pickedRef, err := h.ownerPicker.Pick(ctx, sessionID, conns)
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
	if err := h.ownerStore.Create(ctx, owner); err != nil {
		logs.Error(ctx, "assign owner: create record failed",
			event.String("session_id", sessionID),
			event.Err(err),
		)
		return nil, mapDomainError(err)
	}

	logs.Info(ctx, "owner created on connect",
		event.String("session_id", sessionID),
		event.String("owner", pickedRef.Owner),
		event.Int("agent_index", pickedRef.OwnerIndex),
	)
	return owner, nil
}

// agentClient resolves the agent connection for an owner and wraps it as a client.
func (h *ProxyHandler) agentClient(ctx context.Context, owner *domain.AgentOwner) (agentclient.Client, error) {
	connRef, err := h.manager.Get(ctx, owner.OwnerIndex)
	if err != nil {
		logs.Error(ctx, "get agent connection failed",
			event.String("session_id", owner.SessionID),
			event.Int("agent_index", owner.OwnerIndex),
			event.Err(err),
		)
		return nil, status.Errorf(codes.Internal, "get agent connection: %v", err)
	}
	return agentclient.NewAgentClient(connRef.Conn), nil
}

// propagateAgentError returns a downstream gRPC status error unchanged, or wraps
// a non-status error as Internal so the proxy does not mask agent-level codes.
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
