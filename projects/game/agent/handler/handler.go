// Package handler implements the AgentServiceServer gRPC interface.
package handler

import (
	"context"
	"errors"
	"fmt"
	"io"

	"dominion/common/gopkg/logs"
	"dominion/common/gopkg/logs/event"
	"dominion/projects/game/agent/domain"

	game "dominion/projects/game"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
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

// CreateAgent creates an agent for a given session.
func (h *AgentHandler) CreateAgent(ctx context.Context, req *game.AgentCreateRequest) (*game.AgentStatus, error) {
	sessionID := req.GetSessionId()

	s, err := h.runtime.Create(ctx, sessionID)
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("agent create: %v", err))
	}

	logs.Info(ctx, "agent created",
		event.String(logFieldSessionID, sessionID),
	)

	return statusToProto(s), nil
}

// DeleteAgent deletes the agent for a given session.
func (h *AgentHandler) DeleteAgent(ctx context.Context, req *game.AgentDeleteRequest) (*emptypb.Empty, error) {
	sessionID := req.GetSessionId()

	if err := h.runtime.Delete(ctx, sessionID); err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("agent delete: %v", err))
	}

	logs.Info(ctx, "agent deleted",
		event.String(logFieldSessionID, sessionID),
	)

	return new(emptypb.Empty), nil
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

// Connect handles the bidirectional stream for agent communication.
// It reads AgentFrames from the gRPC stream and dispatches to the runtime.
//   - "status" frames reply with the current status from runtime.Status().
//   - all other frames are echoed back with type "echo".
//
// Returns nil on io.EOF (clean close) or the error from Recv/Send.
func (h *AgentHandler) Connect(stream game.AgentService_ConnectServer) error {
	for {
		frame, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		var resp *game.AgentFrame
		switch frame.Type {
		case "status":
			s, sErr := h.runtime.Status(stream.Context(), frame.SessionId)
			if sErr != nil {
				return sErr
			}
			resp = &game.AgentFrame{
				SessionId: frame.SessionId,
				Type:      "status",
				Payload:   []byte(s.Status),
			}
		default:
			resp = &game.AgentFrame{
				SessionId: frame.SessionId,
				Type:      "echo",
				Payload:   frame.Payload,
			}
		}

		if err := stream.Send(resp); err != nil {
			return err
		}
	}
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
