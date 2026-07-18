// Package handler implements the SessionServiceServer gRPC interface.
package handler

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"dominion/common/gopkg/logs"
	"dominion/common/gopkg/logs/event"
	game "dominion/projects/game"
	gameconst "dominion/projects/game/pkg/gameconst"
	"dominion/projects/game/session/domain"

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
	idGenerator domain.IDGenerator
	proxyClient game.ProxyServiceClient
}

// NewSessionHandler creates a new SessionHandler with the given repository, ID generator, and proxy client.
func NewSessionHandler(repo domain.SessionRepository, idGenerator domain.IDGenerator, proxyClient game.ProxyServiceClient) *SessionHandler {
	return &SessionHandler{
		sessionRepo: repo,
		idGenerator: idGenerator,
		proxyClient: proxyClient,
	}
}

// CreateSession creates a new Session resource. A caller-supplied session_id
// (AIP-133) is used when present; otherwise the server generates one.
func (h *SessionHandler) CreateSession(ctx context.Context, req *game.CreateSessionRequest) (*game.Session, error) {
	sessionID := req.GetSessionId()
	if sessionID == "" {
		var err error
		sessionID, err = h.idGenerator.NewID(ctx)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "generate session id: %v", err)
		}
	}
	if strings.Contains(sessionID, "/") {
		return nil, status.Error(codes.InvalidArgument, "session_id must not contain '/'")
	}

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
	sessionID, err := gameconst.SessionID(req.GetName())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid session name: %v", err)
	}
	s, err := h.sessionRepo.Get(ctx, sessionID)
	if err != nil {
		return nil, toStatusError(err)
	}

	return sessionToProto(s), nil
}

// DeleteSession deletes a Session by its resource name.
func (h *SessionHandler) DeleteSession(ctx context.Context, req *game.DeleteSessionRequest) (*emptypb.Empty, error) {
	sessionID, err := gameconst.SessionID(req.GetName())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid session name: %v", err)
	}

	if err := h.sessionRepo.Delete(ctx, sessionID); err != nil {
		return nil, toStatusError(err)
	}

	logs.Info(ctx, "session deleted",
		event.String(logFieldName, req.GetName()),
	)

	return new(emptypb.Empty), nil
}

// ListSessions retrieves a paginated list of Session resources.
func (h *SessionHandler) ListSessions(ctx context.Context, req *game.ListSessionsRequest) (*game.ListSessionsResponse, error) {
	pageSize := int(req.GetPageSize())
	if pageSize <= 0 {
		pageSize = domain.DefaultListSessionsPageSize
	}
	if pageSize > domain.MaxListSessionsPageSize {
		return nil, status.Errorf(codes.InvalidArgument, "page_size exceeds maximum of %d", domain.MaxListSessionsPageSize)
	}

	var cursor *domain.ListPageCursor
	if token := req.GetPageToken(); token != "" {
		var err error
		cursor, err = domain.DecodePageToken(token)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid page token: %v", err)
		}
	}

	res, err := h.sessionRepo.List(ctx, pageSize, cursor)
	if err != nil {
		return nil, toStatusError(err)
	}

	return listSessionsResultToProto(res), nil
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

// listSessionsResultToProto converts a domain ListSessionsResult to a proto ListSessionsResponse.
func listSessionsResultToProto(res *domain.ListSessionsResult) *game.ListSessionsResponse {
	if res == nil {
		return new(game.ListSessionsResponse)
	}

	protos := make([]*game.Session, 0, len(res.Sessions))
	for _, s := range res.Sessions {
		protos = append(protos, sessionToProto(s))
	}

	return &game.ListSessionsResponse{
		Sessions:      protos,
		NextPageToken: res.NextPageToken,
	}
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
