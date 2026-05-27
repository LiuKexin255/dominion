// Package handler implements the SessionServiceServer gRPC interface.
package handler

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"dominion/common/gopkg/logs"
	"dominion/common/gopkg/logs/event"
	gameconst "dominion/projects/game/pkg/gameconst"
	"dominion/projects/game/session/domain"

	game "dominion/projects/game"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	logFieldName      = gameconst.LogFieldName
	logFieldSessionID = gameconst.LogFieldSessionID
)

// SessionHandler implements SessionServiceServer for session CRUD operations.
type SessionHandler struct {
	game.UnimplementedSessionServiceServer

	sessionRepo domain.SessionRepository
	proxyClient game.ProxyServiceClient
}

// NewSessionHandler creates a new SessionHandler with the given repository and proxy client.
func NewSessionHandler(repo domain.SessionRepository, proxyClient game.ProxyServiceClient) *SessionHandler {
	return &SessionHandler{
		sessionRepo: repo,
		proxyClient: proxyClient,
	}
}

// CreateSession creates a new Session resource.
func (h *SessionHandler) CreateSession(ctx context.Context, req *game.CreateSessionRequest) (*game.Session, error) {
	sessionID := req.GetSessionId()

	s, err := h.sessionRepo.Create(ctx, &domain.Session{
		SessionID: sessionID,
	})
	if err != nil {
		return nil, toStatusError(err)
	}

	sessionName := gameconst.SessionName(s.SessionID)
	logs.Info(ctx, "session created",
		event.String(logFieldName, sessionName),
		event.String(logFieldSessionID, s.SessionID),
	)

	return sessionToProto(s), nil
}

// GetSession retrieves a Session by its resource name.
func (h *SessionHandler) GetSession(ctx context.Context, req *game.GetSessionRequest) (*game.Session, error) {
	sessionID := strings.TrimPrefix(req.GetName(), gameconst.SessionNamePrefix)
	s, err := h.sessionRepo.Get(ctx, sessionID)
	if err != nil {
		return nil, toStatusError(err)
	}

	return sessionToProto(s), nil
}

// DeleteSession deletes a Session by its resource name.
// Before deleting the session, it propagates the deletion to the proxy service
// to clean up the associated Agent resource. If the proxy returns NotFound,
// deletion continues (idempotent). Other proxy errors block session deletion.
func (h *SessionHandler) DeleteSession(ctx context.Context, req *game.DeleteSessionRequest) (*emptypb.Empty, error) {
	sessionID := strings.TrimPrefix(req.GetName(), gameconst.SessionNamePrefix)
	agentName := gameconst.AgentName(sessionID)

	_, err := h.proxyClient.DeleteAgent(ctx, &game.DeleteAgentRequest{Name: agentName})
	if err != nil && status.Code(err) != codes.NotFound {
		return nil, status.Errorf(codes.Internal, "delete agent failed: %v", err)
	}

	if err := h.sessionRepo.Delete(ctx, sessionID); err != nil {
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
		Name:      gameconst.SessionName(s.SessionID),
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
