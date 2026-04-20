// Package session contains the game session gRPC service implementation.
package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"dominion/projects/game/session/domain"
	"dominion/projects/game/session/service"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const sessionResourcePrefix = "sessions/"

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
	if _, err := parseSessionName(req.GetName()); err != nil {
		return nil, err
	}

	session, err := h.svc.GetSession(ctx, req.GetName())
	if err != nil {
		return nil, toStatusError(err)
	}

	return toProtoSession(session), nil
}

// CreateSession creates a new Session and returns the agent connection URL.
func (h *Handler) CreateSession(ctx context.Context, req *CreateSessionRequest) (*CreateSessionResponse, error) {
	sessionType, err := toDomainSessionType(req.GetType())
	if err != nil {
		return nil, err
	}

	session, connectURL, err := h.svc.CreateSession(ctx, sessionType, req.GetSessionId())
	if err != nil {
		return nil, toStatusError(err)
	}

	return &CreateSessionResponse{
		Session:         toProtoSession(session),
		AgentConnectUrl: connectURL,
	}, nil
}

// DeleteSession ends a Session and removes it from the control plane.
func (h *Handler) DeleteSession(ctx context.Context, req *DeleteSessionRequest) (*emptypb.Empty, error) {
	if _, err := parseSessionName(req.GetName()); err != nil {
		return nil, err
	}

	if err := h.svc.DeleteSession(ctx, req.GetName()); err != nil {
		return nil, toStatusError(err)
	}

	return new(emptypb.Empty), nil
}

// ReconnectSession reallocates a gateway for an existing Session.
func (h *Handler) ReconnectSession(ctx context.Context, req *ReconnectSessionRequest) (*ReconnectSessionResponse, error) {
	if _, err := parseSessionName(req.GetName()); err != nil {
		return nil, err
	}

	session, connectURL, err := h.svc.ReconnectSession(ctx, req.GetName())
	if err != nil {
		return nil, toStatusError(err)
	}

	return &ReconnectSessionResponse{
		Session:         toProtoSession(session),
		AgentConnectUrl: connectURL,
	}, nil
}

// toProtoSession converts a domain Session to a proto Session.
func toProtoSession(session *domain.Session) *Session {
	if session == nil {
		return nil
	}

	snapshot := session.Snapshot()
	return &Session{
		Name:                sessionResourcePrefix + snapshot.ID,
		Type:                toProtoSessionType(snapshot.Type),
		Status:              toProtoSessionStatus(snapshot.Status),
		GatewayId:           snapshot.GatewayID,
		CreateTime:          timestamppb.New(snapshot.CreatedAt),
		UpdateTime:          timestamppb.New(snapshot.UpdatedAt),
		EndTime:             toProtoTimestampPtr(snapshot.EndedAt),
		ReconnectGeneration: snapshot.ReconnectGeneration,
		LastError:           snapshot.LastError,
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
