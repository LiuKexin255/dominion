// Package session contains the game session gRPC service implementation.
package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"dominion/common/gopkg/logs"
	"dominion/common/gopkg/logs/event"
	"dominion/projects/game/session/domain"
	"dominion/projects/game/session/service"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	sessionResourcePrefix = "sessions/"

	logFieldSessionID   = "session_id"
	logFieldSessionType = "session_type"
	logFieldCount       = "count"
	logFieldError       = "error"
)

// parseSessionName validates that name has format "sessions/{id}" and returns the ID.
func parseSessionName(name string) (string, error) {
	if !strings.HasPrefix(name, sessionResourcePrefix) {
		return "", status.Error(codes.InvalidArgument, "name must have format sessions/{id}")
	}

	id := strings.TrimPrefix(name, sessionResourcePrefix)
	if id == "" {
		return "", status.Error(codes.InvalidArgument, "name must have format sessions/{id}")
	}

	return id, nil
}

// Handler implements SessionServiceServer.
type Handler struct {
	UnimplementedSessionServiceServer

	svc *service.SessionService
}

// NewHandler creates a session gRPC handler.
func NewHandler(svc *service.SessionService) *Handler {
	return &Handler{
		svc: svc,
	}
}

// GetSession returns the latest persisted Session resource.
func (h *Handler) GetSession(ctx context.Context, req *GetSessionRequest) (*Session, error) {
	sessionID, err := parseSessionName(req.GetName())
	if err != nil {
		return nil, err
	}

	session, err := h.svc.GetSession(ctx, req.GetName())
	if err != nil {
		logs.Error(ctx, "get session failed", event.String(logFieldSessionID, sessionID), event.Err(err))
		return nil, toStatusError(err)
	}

	logs.Info(ctx, "get session succeeded", event.String(logFieldSessionID, sessionID))
	return toProtoSession(session), nil
}

// CreateSession creates a new Session and returns the agent connection URL.
func (h *Handler) CreateSession(ctx context.Context, req *CreateSessionRequest) (*CreateSessionResponse, error) {
	sessionType, err := toDomainSessionType(req.GetType())
	if err != nil {
		return nil, err
	}

	session, err := h.svc.CreateSession(ctx, sessionType, req.GetSessionId())
	if err != nil {
		logs.Error(ctx, "create session failed", event.String(logFieldSessionType, req.GetType().String()), event.Err(err))
		return nil, toStatusError(err)
	}

	logs.Info(ctx, "create session succeeded", event.String(logFieldSessionID, session.ID()), event.String(logFieldSessionType, req.GetType().String()))
	return &CreateSessionResponse{
		Session: toProtoSession(session),
	}, nil
}

// DeleteSession ends a Session and removes it from the control plane.
func (h *Handler) DeleteSession(ctx context.Context, req *DeleteSessionRequest) (*emptypb.Empty, error) {
	sessionID, err := parseSessionName(req.GetName())
	if err != nil {
		return nil, err
	}

	if err := h.svc.DeleteSession(ctx, req.GetName()); err != nil {
		logs.Error(ctx, "delete session failed", event.String(logFieldSessionID, sessionID), event.Err(err))
		return nil, toStatusError(err)
	}

	logs.Info(ctx, "delete session succeeded", event.String(logFieldSessionID, sessionID))
	return new(emptypb.Empty), nil
}

// ReconnectSession reallocates a gateway for an existing Session.
func (h *Handler) ReconnectSession(ctx context.Context, req *ReconnectSessionRequest) (*ReconnectSessionResponse, error) {
	sessionID, err := parseSessionName(req.GetName())
	if err != nil {
		return nil, err
	}

	session, err := h.svc.ReconnectSession(ctx, req.GetName())
	if err != nil {
		logs.Error(ctx, "reconnect session failed", event.String(logFieldSessionID, sessionID), event.Err(err))
		return nil, toStatusError(err)
	}

	logs.Info(ctx, "reconnect session succeeded", event.String(logFieldSessionID, sessionID))
	return &ReconnectSessionResponse{
		Session: toProtoSession(session),
	}, nil
}

// ListSessions returns all non-ended sessions.
func (h *Handler) ListSessions(ctx context.Context, req *ListSessionsRequest) (*ListSessionsResponse, error) {
	sessions, err := h.svc.ListSessions(ctx)
	if err != nil {
		logs.Error(ctx, "list sessions failed", event.Err(err))
		return nil, toStatusError(err)
	}

	logs.Info(ctx, "list sessions succeeded", event.Int(logFieldCount, len(sessions)))
	protos := make([]*Session, 0, len(sessions))
	for _, s := range sessions {
		protos = append(protos, toProtoSession(s))
	}

	return &ListSessionsResponse{Sessions: protos}, nil
}

// toProtoSession converts a domain Session to a proto Session.
func toProtoSession(session *domain.Session) *Session {
	if session == nil {
		return nil
	}

	return &Session{
		Name:                sessionResourcePrefix + session.ID(),
		Type:                toProtoSessionType(session.Type()),
		Status:              toProtoSessionStatus(session.Status()),
		GatewayId:           session.GatewayID(),
		CreateTime:          timestamppb.New(session.CreatedAt()),
		UpdateTime:          timestamppb.New(session.UpdatedAt()),
		EndTime:             toProtoTimestampPtr(session.EndedAt()),
		ReconnectGeneration: session.ReconnectGeneration(),
		LastError:           session.LastError(),
		Token:               session.Token(),
	}
}

// toProtoSessionType converts a domain SessionType to a proto SessionType.
func toProtoSessionType(t domain.SessionType) SessionType {
	switch t {
	case domain.TypeSaolei:
		return SessionType_SESSION_TYPE_SAOLEI
	default:
		return SessionType_SESSION_TYPE_UNSPECIFIED
	}
}

// toProtoSessionStatus converts a domain SessionStatus to a proto SessionStatus.
func toProtoSessionStatus(s domain.SessionStatus) SessionStatus {
	switch s {
	case domain.StatusPending:
		return SessionStatus_SESSION_STATUS_PENDING
	case domain.StatusActive:
		return SessionStatus_SESSION_STATUS_ACTIVE
	case domain.StatusDisconnected:
		return SessionStatus_SESSION_STATUS_DISCONNECTED
	case domain.StatusEnded:
		return SessionStatus_SESSION_STATUS_ENDED
	case domain.StatusFailed:
		return SessionStatus_SESSION_STATUS_FAILED
	default:
		return SessionStatus_SESSION_STATUS_UNSPECIFIED
	}
}

// toDomainSessionType converts a proto SessionType to a domain SessionType.
func toDomainSessionType(t SessionType) (domain.SessionType, error) {
	switch t {
	case SessionType_SESSION_TYPE_SAOLEI:
		return domain.TypeSaolei, nil
	default:
		return domain.SessionTypeUnspecified, status.Error(codes.InvalidArgument, "invalid session type")
	}
}

// toProtoTimestampPtr converts a *time.Time to a *timestamppb.Timestamp.
func toProtoTimestampPtr(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}

	return timestamppb.New(*t)
}

// toStatusError maps domain errors to gRPC status errors.
func toStatusError(err error) error {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, domain.ErrAlreadyExists):
		return status.Error(codes.AlreadyExists, err.Error())
	case errors.Is(err, domain.ErrInvalidState):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, domain.ErrInvalidType):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, domain.ErrNoGatewayAvailable):
		return status.Error(codes.Internal, err.Error())
	case errors.Is(err, domain.ErrSessionEnded):
		return status.Error(codes.FailedPrecondition, err.Error())
	default:
		return status.Error(codes.Internal, fmt.Sprintf("session handler: %v", err))
	}
}
