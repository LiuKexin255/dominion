// Package handler implements the AgentServiceServer gRPC interface.
package handler

import (
	"context"
	"fmt"

	"dominion/common/gopkg/logs"
	"dominion/common/gopkg/logs/event"
	"dominion/projects/game/agent/domain"

	game "dominion/projects/game"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const logFieldSessionID = "session_id"

// AgentHandler implements AgentServiceServer for agent operations.
type AgentHandler struct {
	game.UnimplementedAgentServiceServer

	runtime domain.Runtime
}

// NewAgentHandler creates a new AgentHandler with the given runtime.
func NewAgentHandler(rt domain.Runtime) *AgentHandler {
	return &AgentHandler{
		runtime: rt,
	}
}

// InitAgent initializes the agent for a given session.
func (h *AgentHandler) InitAgent(ctx context.Context, req *game.InitAgentRequest) (*game.AgentStatus, error) {
	sessionID := req.GetSessionId()

	s, err := h.runtime.Init(ctx, sessionID)
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("agent init: %v", err))
	}

	logs.Info(ctx, "agent initialized",
		event.String(logFieldSessionID, sessionID),
	)

	return statusToProto(s), nil
}

// GetAgentStatus returns the current status of the agent in a session.
// Returns "unknown" status for sessions that have not been initialized.
func (h *AgentHandler) GetAgentStatus(ctx context.Context, req *game.GetAgentStatusRequest) (*game.AgentStatus, error) {
	sessionID := req.GetSessionId()

	s, err := h.runtime.Status(ctx, sessionID)
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("agent status: %v", err))
	}

	return statusToProto(s), nil
}

// Connect establishes a bidirectional streaming channel for agent communication.
// It delegates to the runtime's Connect method after adapting the gRPC stream.
func (h *AgentHandler) Connect(stream game.AgentService_ConnectServer) error {
	return h.runtime.Connect(&grpcAgentStream{stream: stream})
}

// grpcAgentStream adapts a gRPC AgentService_ConnectServer to the domain.AgentStream interface.
type grpcAgentStream struct {
	stream game.AgentService_ConnectServer
}

// Recv receives the next AgentFrame from the gRPC stream.
func (s *grpcAgentStream) Recv() (*game.AgentFrame, error) {
	return s.stream.Recv()
}

// Send sends an AgentFrame on the gRPC stream.
func (s *grpcAgentStream) Send(f *game.AgentFrame) error {
	return s.stream.Send(f)
}

// statusToProto converts a domain Status to a proto AgentStatus.
func statusToProto(s *domain.Status) *game.AgentStatus {
	if s == nil {
		return nil
	}

	p := &game.AgentStatus{
		SessionId: s.SessionId,
		Status:    s.Status,
	}
	if !s.CreateTime.IsZero() {
		p.CreateTime = timestamppb.New(s.CreateTime)
	}

	return p
}
