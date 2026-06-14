// Package handler implements the ProxyService gRPC server interface.
package handler

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"dominion/common/gopkg/logs"
	"dominion/common/gopkg/logs/event"
	game "dominion/projects/game"
	"dominion/projects/game/proxy/domain"
	"dominion/projects/game/proxy/runtime/agentclient"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

var (
	// parentPattern matches session resource names of the form "sessions/{id}".
	parentPattern = regexp.MustCompile(`^sessions/([^/]+)$`)
	// agentPattern matches agent resource names of the form "sessions/{id}/agent".
	agentPattern = regexp.MustCompile(`^sessions/([^/]+)/agent$`)
)

// ProxyHandler implements game.ProxyServiceServer.
type ProxyHandler struct {
	game.UnimplementedProxyServiceServer

	ownerStore     domain.OwnerStore
	ownerPicker    domain.OwnerPicker
	manager        agentclient.Manager
	connectAgenter domain.ConnectAgenter
}

// NewProxyHandler creates a new ProxyHandler.
func NewProxyHandler(
	ownerStore domain.OwnerStore,
	ownerPicker domain.OwnerPicker,
	manager agentclient.Manager,
	connectAgenter domain.ConnectAgenter,
) *ProxyHandler {
	return &ProxyHandler{
		ownerStore:     ownerStore,
		ownerPicker:    ownerPicker,
		manager:        manager,
		connectAgenter: connectAgenter,
	}
}

// CreateAgent creates an Agent resource under the specified parent Session.
func (h *ProxyHandler) CreateAgent(ctx context.Context, req *game.CreateAgentRequest) (*game.Agent, error) {
	sessionID, err := extractSessionID(req.GetParent(), parentPattern)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	// List available agent connections.
	conns, err := h.manager.List(ctx)
	if err != nil {
		logs.Error(ctx, "list agent connections failed", event.Err(err))
		return nil, status.Errorf(codes.Internal, "list agent connections: %v", err)
	}

	// Pick an owner instance for this session.
	pickedRef, err := h.ownerPicker.Pick(ctx, sessionID, conns)
	if err != nil {
		return nil, mapDomainError(err)
	}

	// Get the cached agent connection and create a client from it.
	connRef, err := h.manager.Get(ctx, pickedRef.OwnerIndex)
	if err != nil {
		logs.Error(ctx, "get agent connection failed", event.Int("agent_index", pickedRef.OwnerIndex), event.Err(err))
		return nil, status.Errorf(codes.Internal, "get agent connection: %v", err)
	}
	client := agentclient.NewAgentClient(connRef.Conn)

	agentProfileName := req.GetAgent().GetAgentProfileName()

	agent, err := client.CreateAgent(ctx, &game.AgentCreateRequest{SessionId: sessionID, AgentProfileName: agentProfileName})
	if err != nil {
		logs.Error(ctx, "create agent failed", event.String("session_id", sessionID), event.Int("agent_index", pickedRef.OwnerIndex), event.Err(err))
		return nil, propagateAgentError(err, "create agent")
	}

	now := time.Now()
	owner := &domain.AgentOwner{
		SessionID:  sessionID,
		OwnerIndex: pickedRef.OwnerIndex,
		Owner:      pickedRef.Owner,
		CreateTime: now,
	}
	if err := h.ownerStore.Create(ctx, owner); err != nil {
		logs.Error(ctx, "create owner record failed", event.String("session_id", sessionID), event.Err(err))
		return nil, mapDomainError(err)
	}

	logs.Info(ctx, "agent created",
		event.String("session_id", sessionID),
		event.String("owner", pickedRef.Owner),
		event.Int("agent_index", pickedRef.OwnerIndex),
	)

	return agent, nil
}

// GetAgent returns the Agent resource identified by name.
func (h *ProxyHandler) GetAgent(ctx context.Context, req *game.GetAgentRequest) (*game.Agent, error) {
	sessionID, err := extractSessionID(req.GetName(), agentPattern)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	owner, err := h.ownerStore.Get(ctx, sessionID)
	if err != nil {
		return nil, mapDomainError(err)
	}

	// Verify the agent is alive by querying its status.
	connRef, err := h.manager.Get(ctx, owner.OwnerIndex)
	if err != nil {
		logs.Error(ctx, "get agent connection failed", event.Int("agent_index", owner.OwnerIndex), event.Err(err))
		return nil, status.Errorf(codes.Internal, "get agent connection: %v", err)
	}
	client := agentclient.NewAgentClient(connRef.Conn)

	agent, err := client.GetAgent(ctx, &game.AgentGetRequest{SessionId: sessionID})
	if err != nil {
		logs.Error(ctx, "get agent failed", event.String("session_id", sessionID), event.Err(err))
		return nil, propagateAgentError(err, "get agent")
	}

	return agent, nil
}

// DeleteAgent deletes the Agent resource identified by name.
func (h *ProxyHandler) DeleteAgent(ctx context.Context, req *game.DeleteAgentRequest) (*emptypb.Empty, error) {
	sessionID, err := extractSessionID(req.GetName(), agentPattern)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	owner, err := h.ownerStore.Get(ctx, sessionID)
	if err != nil {
		if errors.Is(err, domain.ErrOwnerNotFound) {
			return new(emptypb.Empty), nil
		}
		return nil, mapDomainError(err)
	}

	connRef, err := h.manager.Get(ctx, owner.OwnerIndex)
	if err != nil {
		logs.Error(ctx, "delete agent: get connection failed",
			event.String("session_id", sessionID),
			event.Int("agent_index", owner.OwnerIndex),
			event.Err(err),
		)
		return nil, status.Errorf(codes.Internal, "get agent connection: %v", err)
	}
	client := agentclient.NewAgentClient(connRef.Conn)

	if _, err := client.DeleteAgent(ctx, &game.AgentDeleteRequest{SessionId: sessionID}); err != nil {
		if status.Code(err) == codes.NotFound {
			// Agent on the instance is already gone — continue to delete owner record.
			logs.Info(ctx, "agent already deleted on instance",
				event.String("session_id", sessionID),
				event.Int("agent_index", owner.OwnerIndex),
			)
		} else {
			logs.Error(ctx, "delete agent failed",
				event.String("session_id", sessionID),
				event.Int("agent_index", owner.OwnerIndex),
				event.Err(err),
			)
			return nil, propagateAgentError(err, "delete agent")
		}
	}

	if err := h.ownerStore.Delete(ctx, sessionID); err != nil {
		return nil, mapDomainError(err)
	}

	logs.Info(ctx, "agent deleted", event.String("session_id", sessionID))
	return new(emptypb.Empty), nil
}

// ConnectAgent establishes a bidirectional streaming channel for agent communication.
func (h *ProxyHandler) ConnectAgent(stream game.ProxyService_ConnectAgentServer) error {
	return h.connectAgenter.Connect(stream)
}

// extractSessionID extracts a session ID from a resource name using the given pattern.
func extractSessionID(name string, pattern *regexp.Regexp) (string, error) {
	matches := pattern.FindStringSubmatch(name)
	if len(matches) != 2 {
		return "", fmt.Errorf("invalid resource name %q: expected %s", name, pattern)
	}
	return matches[1], nil
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
