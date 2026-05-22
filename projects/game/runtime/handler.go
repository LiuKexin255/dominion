// Package runtime contains the game runtime gRPC service implementation.
package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"dominion/common/gopkg/logs"
	"dominion/common/gopkg/logs/event"
	"dominion/projects/game/runtime/domain"
	"dominion/projects/game/pkg/token"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// runtimeService defines the service methods the handler needs. Using an
// interface breaks the import cycle: runtime → service → runtime.
type runtimeService interface {
	GetSnapshot(ctx context.Context, sessionID string) (*domain.SnapshotRef, error)
	GetRuntime(ctx context.Context, sessionID string) (*domain.SessionRuntime, error)
	ConnectSession(ctx context.Context, pathSessionID, tokenStr string) (*domain.SessionRuntime, *token.Claims, error)
	ProcessHello(rt *domain.SessionRuntime, claims *token.Claims, role domain.ClientRole, connID string) ([]*domain.RoutedMessage, error)
	HandleAgentMessage(ctx context.Context, sessionID string, msg *domain.Message) ([]*domain.RoutedMessage, error)
	HandleWebMessage(ctx context.Context, sessionID string, connID string, msg *domain.Message) ([]*domain.RoutedMessage, error)
	DisconnectAgent(sessionID string)
	DisconnectWeb(sessionID, connID string)
	TouchSession(sessionID string) error
	AsyncMessages() <-chan *domain.RoutedMessage
	CreateGameRuntime(ctx context.Context, sessionID string, reconnectGeneration int64) (*domain.SessionRuntime, string, error)
	RefreshGameRuntime(ctx context.Context, sessionID string, oldToken string) (*domain.SessionRuntime, string, error)
}

// Handler implements GameGatewayServiceServer.
type Handler struct {
	UnimplementedGameRuntimeReaderServer

	svc      runtimeService
	verifier token.Verifier
}

// NewHandler creates a game runtime gRPC handler.
func NewHandler(svc runtimeService, verifier token.Verifier) *Handler {
	return &Handler{
		svc:      svc,
		verifier: verifier,
	}
}

// GameRuntimeHandler implements GameRuntimeManagerServer.
type GameRuntimeHandler struct {
	UnimplementedGameRuntimeManagerServer

	svc runtimeService
}

// NewRuntimeHandler creates a game runtime gRPC handler.
func NewRuntimeHandler(svc runtimeService) *GameRuntimeHandler {
	return &GameRuntimeHandler{
		svc: svc,
	}
}

// extractTokenFromContext extracts the session token from gRPC incoming
// metadata. The token is expected under the key "token" (lowercase), matching
// the grpc-gateway query-parameter convention.
func extractTokenFromContext(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "no metadata found in context")
	}
	vals := md.Get("token")
	if len(vals) == 0 {
		return "", status.Error(codes.Unauthenticated, "missing token in request")
	}
	return vals[0], nil
}

// verifyToken extracts the token from context, verifies it, and validates that
// the embedded claims match the requested session and have valid owner epoch
// and audience.
func (h *Handler) verifyToken(ctx context.Context, sessionID string) (*token.Claims, error) {
	tokenStr, err := extractTokenFromContext(ctx)
	if err != nil {
		return nil, err
	}

	claims, err := h.verifier.Verify(tokenStr)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid token")
	}

	if claims.SessionID != sessionID {
		return nil, status.Error(codes.PermissionDenied, "token session ID mismatch")
	}

	if err := claims.ValidateOwnerEpoch(); err != nil {
		return nil, status.Error(codes.PermissionDenied, "invalid token: missing owner epoch")
	}

	if err := claims.ValidateAudience(token.TokenAudienceInternal); err != nil {
		return nil, status.Error(codes.PermissionDenied, "invalid token: audience mismatch")
	}

	return claims, nil
}

// GetGameSnapshot returns the latest available snapshot for a session game
// runtime.
func (h *Handler) GetGameSnapshot(ctx context.Context, req *GetGameSnapshotRequest) (*GameSnapshot, error) {
	sessionID, err := parseResourceName(req.GetName(), "/game/snapshot")
	if err != nil {
		return nil, err
	}

	if _, err := h.verifyToken(ctx, sessionID); err != nil {
		return nil, err
	}

	snap, err := h.svc.GetSnapshot(ctx, sessionID)
	if err != nil {
		if errors.Is(err, domain.ErrSessionNotFound) {
			logs.Warn(ctx, "get game snapshot: session not found", event.String(logFieldSessionID, sessionID))
		} else {
			logs.Error(ctx, "get game snapshot failed", event.String(logFieldSessionID, sessionID), event.Err(err))
		}
		return nil, toStatusError(err)
	}

	logs.Info(ctx, "get game snapshot succeeded", event.String(logFieldSessionID, sessionID))
	return toProtoSnapshot(sessionID, snap), nil
}

// GetGameRuntime returns the current in-memory runtime summary for a session
// on the gateway instance.
func (h *Handler) GetGameRuntime(ctx context.Context, req *GetGameRuntimeRequest) (*GameRuntime, error) {
	sessionID, err := parseResourceName(req.GetName(), "/game/runtime")
	if err != nil {
		return nil, err
	}

	if _, err := h.verifyToken(ctx, sessionID); err != nil {
		return nil, err
	}

	rt, err := h.svc.GetRuntime(ctx, sessionID)
	if err != nil {
		if errors.Is(err, domain.ErrSessionNotFound) {
			logs.Warn(ctx, "get game runtime: session not found", event.String(logFieldSessionID, sessionID))
		} else {
			logs.Error(ctx, "get game runtime failed", event.String(logFieldSessionID, sessionID), event.Err(err))
		}
		return nil, toStatusError(err)
	}

	logs.Info(ctx, "get game runtime succeeded", event.String(logFieldSessionID, sessionID))
	return toProtoRuntime(rt), nil
}

// CreateGameRuntime creates an in-memory game runtime on the receiving gateway
// instance for the session and returns an ownership token bound to the
// reconnect generation.
func (h *GameRuntimeHandler) CreateGameRuntime(ctx context.Context, req *CreateGameRuntimeRequest) (*CreateGameRuntimeResponse, error) {
	sessionID, err := parseSessionIDFromParent(req.GetParent())
	if err != nil {
		return nil, err
	}

	rt, tokenStr, err := h.svc.CreateGameRuntime(ctx, sessionID, req.GetReconnectGeneration())
	if err != nil {
		logs.Error(ctx, "create game runtime failed",
			event.String(logFieldSessionID, sessionID),
			event.Err(err),
		)
		return nil, toStatusError(err)
	}

	logs.Info(ctx, "create game runtime succeeded",
		event.String(logFieldSessionID, sessionID),
	)
	return &CreateGameRuntimeResponse{
		OwnerRuntimeId: rt.OwnerRuntimeID,
		OwnerEpoch:     rt.OwnerEpoch,
		Token:          tokenStr,
		ExpiresAt:      toProtoTimestampPtr(rt.LastTrafficTime),
	}, nil
}

// RefreshGameRuntime refreshes an existing runtime token before expiry and
// returns a new token with an updated expiry window.
func (h *GameRuntimeHandler) RefreshGameRuntime(ctx context.Context, req *RefreshGameRuntimeRequest) (*RefreshGameRuntimeResponse, error) {
	sessionID, err := parseResourceName(req.GetName(), "/game/runtime")
	if err != nil {
		return nil, err
	}

	rt, tokenStr, err := h.svc.RefreshGameRuntime(ctx, sessionID, req.GetOldToken())
	if err != nil {
		logs.Error(ctx, "refresh game runtime failed",
			event.String(logFieldSessionID, sessionID),
			event.Err(err),
		)
		return nil, toStatusError(err)
	}

	logs.Info(ctx, "refresh game runtime succeeded",
		event.String(logFieldSessionID, sessionID),
	)
	return &RefreshGameRuntimeResponse{
		OwnerRuntimeId:      rt.OwnerRuntimeID,
		OwnerEpoch:          rt.OwnerEpoch,
		ReconnectGeneration: rt.ReconnectGeneration,
		Token:               tokenStr,
		ExpiresAt:           toProtoTimestampPtr(rt.LastTrafficTime),
	}, nil
}

// parseSessionIDFromParent extracts the session ID from a parent string with
// format "sessions/{id}".
func parseSessionIDFromParent(parent string) (string, error) {
	if parent == "" {
		return "", status.Error(codes.InvalidArgument, "parent must not be empty")
	}

	const prefix = "sessions/"
	if !strings.HasPrefix(parent, prefix) {
		return "", status.Error(codes.InvalidArgument, "parent must have format sessions/{id}")
	}

	id := strings.TrimPrefix(parent, prefix)
	if id == "" {
		return "", status.Error(codes.InvalidArgument, "parent must have format sessions/{id}")
	}

	return id, nil
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
	name := "sessions/" + sessionID + "/game/snapshot"
	session := "sessions/" + sessionID
	if ref == nil {
		return &GameSnapshot{
			Name:    name,
			Session: session,
		}
	}

	return &GameSnapshot{
		Name:        name,
		Session:     session,
		SnapshotId:  fmt.Sprintf("%s-%d", sessionID, ref.CaptureTime.UnixMilli()),
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
		RuntimeId:           rt.RuntimeID,
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
		Kind:          ProtoOperationKind(op.Kind),
		FlashSnapshot: op.FlashSnapshot,
		CreateTime:    timestamppb.New(op.CreateTime),
	}
}

// toStatusError maps domain errors to gRPC status errors. Errors that already
// carry a gRPC status code are returned unchanged.
func toStatusError(err error) error {
	if _, ok := status.FromError(err); ok {
		return err
	}

	switch {
	case errors.Is(err, domain.ErrSessionNotFound):
		return status.Error(codes.NotFound, err.Error())
	default:
		return status.Error(codes.Internal, fmt.Sprintf("runtime handler: %v", err))
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
