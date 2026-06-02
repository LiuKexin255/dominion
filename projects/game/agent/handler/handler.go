// Package handler implements the AgentServiceServer gRPC interface.
package handler

import (
	"context"
	"errors"
	"fmt"
	"io"

	"dominion/common/gopkg/logs"
	"dominion/common/gopkg/logs/event"
	game "dominion/projects/game"
	"dominion/projects/game/agent/domain"

	"google.golang.org/grpc/codes"
	grpcStatus "google.golang.org/grpc/status"
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

// CreateAgent creates an agent for a given session using the specified profile.
func (h *AgentHandler) CreateAgent(ctx context.Context, req *game.AgentCreateRequest) (*game.AgentStatus, error) {
	sessionID := req.GetSessionId()
	profileName := req.GetAgentProfileName()

	s, err := h.runtime.CreateWithProfile(ctx, sessionID, profileName)
	if err != nil {
		return nil, grpcStatus.Error(codes.Internal, fmt.Sprintf("agent create: %v", err))
	}

	logs.Info(ctx, "agent created",
		event.String(logFieldSessionID, sessionID),
	)

	return statusToProto(s), nil
}

// DeleteAgent deletes the agent for a given session.
// The runtime Delete is idempotent; deleting a non-existent session succeeds.
func (h *AgentHandler) DeleteAgent(ctx context.Context, req *game.AgentDeleteRequest) (*emptypb.Empty, error) {
	sessionID := req.GetSessionId()

	if err := h.runtime.Delete(ctx, sessionID); err != nil {
		return nil, grpcStatus.Error(codes.Internal, fmt.Sprintf("agent delete: %v", err))
	}

	logs.Info(ctx, "agent deleted",
		event.String(logFieldSessionID, sessionID),
	)

	return new(emptypb.Empty), nil
}

// GetAgentStatus returns the current status of the agent in a session.
func (h *AgentHandler) GetAgentStatus(ctx context.Context, req *game.GetAgentStatusRequest) (*game.AgentStatus, error) {
	sessionID := req.GetSessionId()

	s, err := h.runtime.Status(ctx, sessionID)
	if err != nil {
		return nil, grpcStatus.Error(codes.Internal, fmt.Sprintf("agent status: %v", err))
	}

	return statusToProto(s), nil
}

// Connect handles the bidirectional stream for agent communication.
// It reads AgentFrames from the gRPC stream and dispatches based on the
// oneof payload field:
//   - status: queries runtime.Status and returns an AgentStatusFrame.
//   - echo: echoes back the data in an AgentEchoFrame.
//   - screenshot: converts to domain.ScreenshotInput, calls
//     runtime.ReceiveScreenshot, and converts returned domain.Frames to proto.
//   - operation_result: converts to domain.OperationResult, calls
//     runtime.ReceiveOperationResult.
//   - empty payload: skipped gracefully with a warning log.
//
// Returns nil on io.EOF (clean close) or the error from Recv/Send.
func (h *AgentHandler) Connect(stream game.AgentService_ConnectServer) error {
	ctx := stream.Context()

	for {
		frame, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		sessionID := frame.GetSessionId()

		switch p := frame.GetPayload().(type) {
		case *game.AgentFrame_Status:
			resp, err := h.handleStatus(ctx, sessionID)
			if err != nil {
				return err
			}
			if err := stream.Send(resp); err != nil {
				return err
			}

		case *game.AgentFrame_Echo:
			resp := h.handleEcho(p.Echo, sessionID)
			if err := stream.Send(resp); err != nil {
				return err
			}

		case *game.AgentFrame_Screenshot:
			frames, err := h.handleScreenshot(ctx, p.Screenshot, sessionID)
			if err != nil {
				return err
			}
			for _, resp := range frames {
				if err := stream.Send(resp); err != nil {
					return err
				}
			}

		case *game.AgentFrame_OperationResult:
			frames, err := h.handleOperationResult(ctx, p.OperationResult, sessionID, frame.GetInvokeId(), frame.GetSequence())
			if err != nil {
				return err
			}
			for _, resp := range frames {
				if err := stream.Send(resp); err != nil {
					return err
				}
			}

		default:
			logs.Warn(ctx, "skipping frame with empty or unknown payload",
				event.String(logFieldSessionID, sessionID),
			)
		}
	}
}

// handleStatus queries the runtime for the current status and returns
// an AgentFrame containing an AgentStatusFrame.
func (h *AgentHandler) handleStatus(ctx context.Context, sessionID string) (*game.AgentFrame, error) {
	s, err := h.runtime.Status(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return &game.AgentFrame{
		SessionId: sessionID,
		Payload: &game.AgentFrame_Status{
			Status: &game.AgentStatusFrame{Status: s.Status},
		},
	}, nil
}

// handleEcho echoes back the data from the incoming echo frame.
func (h *AgentHandler) handleEcho(echo *game.AgentEchoFrame, sessionID string) *game.AgentFrame {
	var data []byte
	if echo != nil {
		data = echo.GetData()
	}
	return &game.AgentFrame{
		SessionId: sessionID,
		Payload: &game.AgentFrame_Echo{
			Echo: &game.AgentEchoFrame{Data: data},
		},
	}
}

// handleScreenshot converts a proto screenshot frame to domain input,
// passes it to the runtime, and converts the returned domain frames to
// proto frames.
func (h *AgentHandler) handleScreenshot(ctx context.Context, f *game.AgentScreenshotFrame, sessionID string) ([]*game.AgentFrame, error) {
	input := screenshotFrameToInput(f, sessionID)
	domainFrames, err := h.runtime.ReceiveScreenshot(ctx, sessionID, input)
	if err != nil {
		return nil, err
	}
	protoFrames := make([]*game.AgentFrame, 0, len(domainFrames))
	for _, df := range domainFrames {
		pf := convertFrameToProto(df, sessionID)
		protoFrames = append(protoFrames, pf)
	}
	return protoFrames, nil
}

// handleOperationResult converts a proto operation result frame to domain,
// and passes it to the runtime.
func (h *AgentHandler) handleOperationResult(ctx context.Context, f *game.AgentOperationResultFrame, sessionID, invokeID string, sequence int64) ([]*game.AgentFrame, error) {
	if f == nil {
		return nil, nil
	}
	result := &domain.OperationResult{
		OperationID: f.GetOperationId(),
		InvokeID:    invokeID,
		Sequence:    sequence,
		Status:      int32(f.GetStatus()),
		Message:     f.GetMessage(),
	}
	domainFrames, err := h.runtime.ReceiveOperationResult(ctx, sessionID, result)
	if err != nil {
		return nil, err
	}
	protoFrames := make([]*game.AgentFrame, 0, len(domainFrames))
	for _, df := range domainFrames {
		pf := convertFrameToProto(df, sessionID)
		protoFrames = append(protoFrames, pf)
	}
	return protoFrames, nil
}

// screenshotFrameToInput converts a proto AgentScreenshotFrame to a domain
// ScreenshotInput.
func screenshotFrameToInput(f *game.AgentScreenshotFrame, sessionID string) *domain.ScreenshotInput {
	if f == nil {
		return nil
	}
	return &domain.ScreenshotInput{
		SessionId: sessionID,
		CaptureId: f.GetCaptureId(),
		Data:      f.GetData(),
		WidthPx:   f.GetWidthPx(),
		HeightPx:  f.GetHeightPx(),
	}
}

// convertFrameToProto converts a domain Frame to a proto AgentFrame.
func convertFrameToProto(df *domain.Frame, sessionID string) *game.AgentFrame {
	if df == nil {
		return nil
	}

	frame := &game.AgentFrame{
		SessionId: sessionID,
	}

	switch df.Type {
	case domain.FrameTypeText:
		frame.Payload = &game.AgentFrame_Text{
			Text: &game.AgentTextFrame{Content: df.Content},
		}
	case domain.FrameTypeThinking:
		frame.Payload = &game.AgentFrame_Thinking{
			Thinking: &game.AgentThinkingFrame{Content: df.Content},
		}
	case domain.FrameTypeOperation:
		opFrame := &game.AgentOperationFrame{
			OperationId:  df.OperationID,
			ScreenshotId: df.ScreenshotID,
			Sequence:     df.OperationSeq,
		}
		if df.IsMouse {
			opFrame.Operation = &game.AgentOperationFrame_Mouse{
				Mouse: &game.AgentMouseOperation{
					Button:    game.AgentMouseButton(df.Button),
					ClickType: game.AgentMouseClickType(df.ClickType),
					XPx:       df.XPx,
					YPx:       df.YPx,
				},
			}
		} else {
			opFrame.Operation = &game.AgentOperationFrame_Keyboard{
				Keyboard: &game.AgentKeyboardOperation{KeyCodes: df.KeyCodes},
			}
		}
		frame.Payload = &game.AgentFrame_Operation{Operation: opFrame}
	case domain.FrameTypeWarn:
		frame.Payload = &game.AgentFrame_Warn{
			Warn: &game.AgentWarnFrame{
				Message: df.WarnMessage,
				Code:    df.WarnCode,
			},
		}
	}

	return frame
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
