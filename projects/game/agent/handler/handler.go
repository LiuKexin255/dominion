// Package handler implements the AgentServiceServer gRPC interface.
package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

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

// CreateAgent creates an agent for a given session.
func (h *AgentHandler) CreateAgent(ctx context.Context, req *game.AgentCreateRequest) (*game.AgentStatus, error) {
	sessionID := req.GetSessionId()

	s, err := h.runtime.Create(ctx, sessionID)
	if err != nil {
		return nil, grpcStatus.Error(codes.Internal, fmt.Sprintf("agent create: %v", err))
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
		return nil, grpcStatus.Error(codes.Internal, fmt.Sprintf("agent delete: %v", err))
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
//     runtime.ReceiveScreenshot, and returns an AgentAckFrame.
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
		var resp *game.AgentFrame

		switch p := frame.GetPayload().(type) {
		case *game.AgentFrame_Status:
			resp, err = h.handleStatus(ctx, sessionID)
		case *game.AgentFrame_Echo:
			resp = h.handleEcho(p.Echo, sessionID)
		case *game.AgentFrame_Screenshot:
			resp, err = h.handleScreenshot(ctx, p.Screenshot, sessionID)
		default:
			logs.Warn(ctx, "skipping frame with empty or unknown payload",
				event.String(logFieldSessionID, sessionID),
			)
			continue
		}

		if err != nil {
			return err
		}

		if err := stream.Send(resp); err != nil {
			return err
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

// handleScreenshot converts a proto screenshot frame to a domain input,
// passes it to the runtime, and returns an ack frame.
func (h *AgentHandler) handleScreenshot(ctx context.Context, f *game.AgentScreenshotFrame, sessionID string) (*game.AgentFrame, error) {
	input := screenshotFrameToInput(f, sessionID)
	receipt, err := h.runtime.ReceiveScreenshot(ctx, input)
	if err != nil {
		return nil, err
	}
	return receiptToAckFrame(receipt, sessionID), nil
}

// screenshotFrameToInput converts a proto AgentScreenshotFrame to a domain
// ScreenshotInput. It maps the ImageEncoding enum to the "PNG" string
// expected by the domain layer and handles nil CaptureTime gracefully.
func screenshotFrameToInput(f *game.AgentScreenshotFrame, sessionID string) *domain.ScreenshotInput {
	if f == nil {
		return nil
	}

	encoding := ""
	if f.GetEncoding() == game.ImageEncoding_IMAGE_ENCODING_PNG {
		encoding = "PNG"
	}

	var captureTime time.Time
	if ct := f.GetCaptureTime(); ct != nil {
		captureTime = ct.AsTime()
	}

	return &domain.ScreenshotInput{
		SessionId:   sessionID,
		CaptureId:   f.GetCaptureId(),
		Encoding:    encoding,
		Data:        f.GetData(),
		WidthPx:     f.GetWidthPx(),
		HeightPx:    f.GetHeightPx(),
		ScaleFactor: f.GetScaleFactor(),
		WindowTitle: f.GetWindowTitle(),
		CaptureTime: captureTime,
	}
}

// receiptToAckFrame converts a domain ScreenshotReceipt to a proto AgentFrame
// containing an AgentAckFrame. The AckFrameId echoes the original CaptureId
// so the sender can correlate the acknowledgment.
func receiptToAckFrame(receipt *domain.ScreenshotReceipt, sessionID string) *game.AgentFrame {
	if receipt == nil {
		return nil
	}
	return &game.AgentFrame{
		SessionId: sessionID,
		Payload: &game.AgentFrame_Ack{
			Ack: &game.AgentAckFrame{
				AckFrameId: receipt.AckFrameId,
				Message:    receipt.Message,
			},
		},
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
