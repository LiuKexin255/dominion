// Package handler implements the ProxyService gRPC server interface.
package handler

import (
	"context"
	"fmt"
	"regexp"

	game "dominion/projects/game"
	"dominion/projects/game/proxy/domain"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	// agentPattern matches agent resource names of the form "sessions/{id}/agent".
	agentPattern = regexp.MustCompile(`^sessions/([^/]+)/agent$`)
	// sessionPattern matches session resource names of the form "sessions/{id}".
	sessionPattern = regexp.MustCompile(`^sessions/([^/]+)$`)
)

// ProxyHandler implements game.ProxyServiceServer.
type ProxyHandler struct {
	game.UnimplementedProxyServiceServer

	svc domain.ProxyService
}

// NewProxyHandler creates a new ProxyHandler.
func NewProxyHandler(svc domain.ProxyService) *ProxyHandler {
	return &ProxyHandler{svc: svc}
}

// GetAgent returns the Agent resource identified by name.
func (h *ProxyHandler) GetAgent(ctx context.Context, req *game.GetAgentRequest) (*game.Agent, error) {
	sessionID, err := extractSessionID(req.GetName(), agentPattern)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return h.svc.GetAgent(ctx, sessionID)
}

// ListMessages lists messages for an agent.
func (h *ProxyHandler) ListMessages(ctx context.Context, req *game.ListMessagesRequest) (*game.ListMessagesResponse, error) {
	sessionID, err := extractSessionID(req.GetParent(), sessionPattern)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return h.svc.ListMessages(ctx, sessionID, req)
}

// ConnectAgent establishes a bidirectional streaming channel for agent communication.
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

	return h.svc.Connect(ctx, sessionID, frame, stream)
}

// extractSessionID extracts a session ID from a resource name using the given pattern.
func extractSessionID(name string, pattern *regexp.Regexp) (string, error) {
	matches := pattern.FindStringSubmatch(name)
	if len(matches) != 2 {
		return "", fmt.Errorf("invalid resource name %q: expected %s", name, pattern)
	}
	return matches[1], nil
}
