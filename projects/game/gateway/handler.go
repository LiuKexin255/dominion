// Package gateway contains the game gateway gRPC service implementation.
package gateway

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"dominion/projects/game/gateway/domain"
	"dominion/projects/game/pkg/token"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// gatewayService defines the service methods the handler needs. Using an
// interface breaks the import cycle: gateway → service → gateway.
type gatewayService interface {
	GetSnapshot(ctx context.Context, sessionID string) (*domain.SnapshotRef, error)
	GetRuntime(ctx context.Context, sessionID string) (*domain.SessionRuntime, error)
	ConnectSession(ctx context.Context, pathSessionID, tokenStr string) (*domain.SessionRuntime, *token.Claims, error)
	ProcessHello(rt *domain.SessionRuntime, claims *token.Claims, role domain.ClientRole, connID string) ([]*domain.RoutedMessage, error)
	HandleAgentMessage(ctx context.Context, sessionID string, msg *domain.Message) ([]*domain.RoutedMessage, error)
	HandleWebMessage(ctx context.Context, sessionID string, connID string, msg *domain.Message) ([]*domain.RoutedMessage, error)
	DisconnectAgent(sessionID string)
	DisconnectWeb(sessionID, connID string)
}

// Handler implements GameGatewayServiceServer.
type Handler struct {
	UnimplementedGameGatewayServiceServer

	svc gatewayService
}

// NewHandler creates a game gateway gRPC handler.
func NewHandler(svc gatewayService) *Handler {
	return &Handler{
		svc: svc,
	}
}

// GetGameSnapshot returns the latest available snapshot for a session game
// runtime.
func (h *Handler) GetGameSnapshot(ctx context.Context, req *GetGameSnapshotRequest) (*GameSnapshot, error) {
	sessionID, err := parseResourceName(req.GetName(), "/game/snapshot")
	if err != nil {
		return nil, err
	}

	snap, err := h.svc.GetSnapshot(ctx, sessionID)
	if err != nil {
		return nil, toStatusError(err)
	}

	return toProtoSnapshot(sessionID, snap), nil
}

// GetGameRuntime returns the current in-memory runtime summary for a session
// on the gateway instance.
func (h *Handler) GetGameRuntime(ctx context.Context, req *GetGameRuntimeRequest) (*GameRuntime, error) {
	sessionID, err := parseResourceName(req.GetName(), "/game/runtime")
	if err != nil {
		return nil, err
	}

	rt, err := h.svc.GetRuntime(ctx, sessionID)
	if err != nil {
		return nil, toStatusError(err)
	}

	return toProtoRuntime(rt), nil
}

// parseResourceName validates that name has format "sessions/{id}{suffix}" and
// returns the session ID.
func parseResourceName(name, suffix string) (string, error) {
	if name == "" {
		return "", status.Error(codes.InvalidArgument, "name must not be empty")
	}

	expectedPrefix := "sessions/"
	if !strings.HasPrefix(name, expectedPrefix) {
		return "", status.Error(codes.InvalidArgument, fmt.Sprintf("name must have format sessions/{id}%s", suffix))
	}

	rest := strings.TrimPrefix(name, expectedPrefix)
	if !strings.HasSuffix(rest, suffix) {
		return "", status.Error(codes.InvalidArgument, fmt.Sprintf("name must have format sessions/{id}%s", suffix))
	}

	id := strings.TrimSuffix(rest, suffix)
	if id == "" {
		return "", status.Error(codes.InvalidArgument, fmt.Sprintf("name must have format sessions/{id}%s", suffix))
	}

	return id, nil
}

// toProtoSnapshot converts a domain SnapshotRef to a proto GameSnapshot.
func toProtoSnapshot(sessionID string, ref *domain.SnapshotRef) *GameSnapshot {
	if ref == nil {
		return &GameSnapshot{
			Name:    "sessions/" + sessionID + "/game/snapshot",
			Session: "sessions/" + sessionID,
		}
	}

	return &GameSnapshot{
		Name:        "sessions/" + sessionID + "/game/snapshot",
		Session:     "sessions/" + sessionID,
		MimeType:    ref.MimeType,
		Image:       ref.Data,
		Cached:      ref.Cached,
		CaptureTime: timestamppb.New(ref.CaptureTime),
	}
}

// toProtoRuntime converts a domain SessionRuntime to a proto GameRuntime.
func toProtoRuntime(rt *domain.SessionRuntime) *GameRuntime {
	return &GameRuntime{
		Name:                "sessions/" + rt.SessionID + "/game/runtime",
		Session:             "sessions/" + rt.SessionID,
		GatewayId:           rt.GatewayID,
		AgentConnected:      rt.AgentConn != nil,
		WebConnectionCount:  int32(len(rt.WebConns)),
		StreamStatus:        toProtoStreamState(rt.StreamState),
		LastMediaTime:       toProtoTimestampPtr(rt.LastMediaTime),
		LastSnapshotTime:    toProtoTimestampPtr(rt.LastSnapshotTime),
		InflightOperation:   toProtoOperation(rt.InflightOp),
		LastError:           rt.LastError,
		ReconnectGeneration: rt.ReconnectGeneration,
	}
}

// toProtoStreamState converts a domain StreamState to a proto GameStreamStatus.
func toProtoStreamState(s domain.StreamState) GameStreamStatus {
	switch s {
	case domain.StreamStateActive:
		return GameStreamStatus_GAME_STREAM_STATUS_ACTIVE
	case domain.StreamStatePaused:
		return GameStreamStatus_GAME_STREAM_STATUS_PAUSED
	case domain.StreamStateUnavailable:
		return GameStreamStatus_GAME_STREAM_STATUS_UNAVAILABLE
	default:
		return GameStreamStatus_GAME_STREAM_STATUS_UNSPECIFIED
	}
}

// toProtoOperation converts a domain InflightOperation to a proto GameOperation.
func toProtoOperation(op *domain.InflightOperation) *GameOperation {
	if op == nil {
		return nil
	}

	return &GameOperation{
		OperationId:   op.OperationID,
		Kind:          toProtoOperationKind(op.Kind),
		FlashSnapshot: op.FlashSnapshot,
		CreateTime:    timestamppb.New(op.CreateTime),
	}
}

// toProtoOperationKind converts a domain OperationKind to a proto
// GameControlOperationKind.
func toProtoOperationKind(k domain.OperationKind) GameControlOperationKind {
	switch k {
	case domain.OperationKindMouseClick:
		return GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_CLICK
	case domain.OperationKindMouseDoubleClick:
		return GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_DOUBLE_CLICK
	case domain.OperationKindMouseDrag:
		return GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_DRAG
	case domain.OperationKindMouseHover:
		return GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_HOVER
	case domain.OperationKindMouseHold:
		return GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_HOLD
	default:
		return GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_UNSPECIFIED
	}
}

// toStatusError maps domain errors to gRPC status errors.
func toStatusError(err error) error {
	switch {
	case errors.Is(err, domain.ErrSessionNotFound):
		return status.Error(codes.NotFound, err.Error())
	default:
		return status.Error(codes.Internal, fmt.Sprintf("gateway handler: %v", err))
	}
}

// toProtoTimestampPtr converts a time.Time to a *timestamppb.Timestamp,
// returning nil for the zero value.
func toProtoTimestampPtr(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}

	return timestamppb.New(t)
}
