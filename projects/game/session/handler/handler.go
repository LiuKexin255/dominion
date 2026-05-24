// Package handler implements the SessionServiceServer gRPC interface.
package handler

import (
	"context"
	"errors"
	"fmt"

	"dominion/common/gopkg/logs"
	"dominion/common/gopkg/logs/event"
	"dominion/projects/game/session/domain"

	game "dominion/projects/game"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	sessionNamePrefix = "sessions/"

	logFieldName      = "name"
	logFieldSessionID = "session_id"
)

// SessionHandler implements SessionServiceServer for session CRUD operations.
type SessionHandler struct {
	game.UnimplementedSessionServiceServer

	sessionRepo domain.SessionRepository
}

// NewSessionHandler creates a new SessionHandler with the given repository.
func NewSessionHandler(repo domain.SessionRepository) *SessionHandler {
	return &SessionHandler{
		sessionRepo: repo,
	}
}

// CreateSession creates a new Session resource.
func (h *SessionHandler) CreateSession(ctx context.Context, req *game.CreateSessionRequest) (*game.Session, error) {
	sessionID := req.GetSessionId()
	name := sessionNamePrefix + sessionID

	s, err := h.sessionRepo.Create(ctx, &domain.Session{
		Name:      name,
		SessionID: sessionID,
	})
	if err != nil {
		return nil, toStatusError(err)
	}

	logs.Info(ctx, "session created",
		event.String(logFieldName, s.Name),
		event.String(logFieldSessionID, s.SessionID),
	)

	return sessionToProto(s), nil
}

// GetSession retrieves a Session by its resource name.
func (h *SessionHandler) GetSession(ctx context.Context, req *game.GetSessionRequest) (*game.Session, error) {
	s, err := h.sessionRepo.Get(ctx, req.GetName())
	if err != nil {
		return nil, toStatusError(err)
	}

	return sessionToProto(s), nil
}

// DeleteSession deletes a Session by its resource name.
func (h *SessionHandler) DeleteSession(ctx context.Context, req *game.DeleteSessionRequest) (*emptypb.Empty, error) {
	if err := h.sessionRepo.Delete(ctx, req.GetName()); err != nil {
		return nil, toStatusError(err)
	}

	logs.Info(ctx, "session deleted",
		event.String(logFieldName, req.GetName()),
	)

	return new(emptypb.Empty), nil
}

// sessionToProto converts a domain Session to a proto Session.
func sessionToProto(s *domain.Session) *game.Session {
	if s == nil {
		return nil
	}

	p := &game.Session{
		Name:      s.Name,
		SessionId: s.SessionID,
	}
	if !s.CreateTime.IsZero() {
		p.CreateTime = timestamppb.New(s.CreateTime)
	}

	return p
}

// toStatusError maps domain errors to gRPC status errors.
func toStatusError(err error) error {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, domain.ErrAlreadyExists):
		return status.Error(codes.AlreadyExists, err.Error())
	default:
		return status.Error(codes.Internal, fmt.Sprintf("session handler: %v", err))
	}
}
