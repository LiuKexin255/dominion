// Package service orchestrates the game session lifecycle.
package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"dominion/common/gopkg/logs"
	"dominion/common/gopkg/logs/event"
	"dominion/common/gopkg/otel"
	"dominion/projects/game/pkg/token"
	"dominion/projects/game/session/domain"
	"dominion/projects/game/session/runtime/gateway"

	"go.opentelemetry.io/otel/attribute"
)

const (
	sessionNamePrefix = "sessions/"

	spanCreate    = "session.create"
	spanReconnect = "session.reconnect"

	logFieldSessionID    = "session_id"
	logFieldGatewayID    = "gateway_id"
	logFieldNewGatewayID = "new_gateway_id"
	logFieldCount        = "count"
	logFieldError        = "error"
)

// SessionService orchestrates session lifecycle operations.
type SessionService struct {
	repo        domain.Repository
	tokenIssuer token.Issuer
	gatewayReg  gateway.Registry
}

// NewSessionService creates a SessionService.
func NewSessionService(repo domain.Repository, tokenIssuer token.Issuer, gatewayReg gateway.Registry) *SessionService {
	return &SessionService{
		repo:        repo,
		tokenIssuer: tokenIssuer,
		gatewayReg:  gatewayReg,
	}
}

// CreateSession creates, assigns, persists, and returns a new session.
func (s *SessionService) CreateSession(ctx context.Context, sessionType domain.SessionType, sessionID string) (*domain.Session, error) {
	ctx, span := otel.Tracer().Start(ctx, spanCreate)
	defer span.End()

	session, err := domain.NewSession(sessionType, sessionID)
	if err != nil {
		return nil, err
	}

	assignment, err := s.gatewayReg.PickRandom(ctx)
	if err != nil {
		return nil, normalizeGatewayError(err)
	}

	session.SetGatewayID(assignment.GatewayID)
	logs.Info(ctx, "gateway assigned", event.String(logFieldSessionID, session.Snapshot().ID), event.String(logFieldGatewayID, assignment.GatewayID))
	span.SetAttributes(attribute.String(logFieldSessionID, session.Snapshot().ID))
	span.SetAttributes(attribute.String(logFieldGatewayID, assignment.GatewayID))

	if err := s.enrichWithConnectURL(ctx, session); err != nil {
		return nil, err
	}
	if err := s.repo.Save(ctx, session); err != nil {
		return nil, err
	}
	logs.Info(ctx, "session saved", event.String(logFieldSessionID, session.Snapshot().ID))

	return session, nil
}

// GetSession loads a session by resource name.
func (s *SessionService) GetSession(ctx context.Context, name string) (*domain.Session, error) {
	session, err := s.repo.Get(ctx, name)
	if err != nil {
		return nil, err
	}

	logs.Info(ctx, "session loaded", event.String(logFieldSessionID, session.Snapshot().ID))

	if err := s.enrichWithConnectURL(ctx, session); err != nil {
		return nil, err
	}

	return session, nil
}

// ListSessions lists non-ended sessions with agent connect URLs when available.
func (s *SessionService) ListSessions(ctx context.Context) ([]*domain.Session, error) {
	sessions, err := s.repo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}

	logs.Info(ctx, "sessions listed", event.Int(logFieldCount, len(sessions)))

	for _, session := range sessions {
		if err := s.enrichWithConnectURL(ctx, session); err != nil {
			return nil, err
		}
	}

	return sessions, nil
}

// DeleteSession removes a session by resource name.
func (s *SessionService) DeleteSession(ctx context.Context, name string) error {
	err := s.repo.Delete(ctx, name)
	if err != nil {
		return err
	}

	logs.Info(ctx, "session deleted", event.String(logFieldSessionID, name))
	return nil
}

// ReconnectSession reassigns a gateway, issues a new token, and persists the updated session.
func (s *SessionService) ReconnectSession(ctx context.Context, name string) (*domain.Session, error) {
	ctx, span := otel.Tracer().Start(ctx, spanReconnect)
	defer span.End()

	session, err := s.repo.Get(ctx, name)
	if err != nil {
		return nil, err
	}

	current := session.Snapshot()
	assignment, err := s.gatewayReg.PickRandomExcluding(ctx, current.GatewayID)
	if err != nil {
		return nil, normalizeGatewayError(err)
	}

	session.SetGatewayID(assignment.GatewayID)
	logs.Info(ctx, "gateway reassigned", event.String(logFieldSessionID, session.Snapshot().ID), event.String(logFieldNewGatewayID, assignment.GatewayID))
	span.SetAttributes(attribute.String(logFieldSessionID, session.Snapshot().ID))
	span.SetAttributes(attribute.String(logFieldNewGatewayID, assignment.GatewayID))

	if err := session.MarkActive(); err != nil {
		return nil, err
	}

	if err := s.enrichWithConnectURL(ctx, session); err != nil {
		return nil, err
	}
	if err := s.repo.Save(ctx, session); err != nil {
		return nil, err
	}
	logs.Info(ctx, "session saved", event.String(logFieldSessionID, session.Snapshot().ID))

	return session, nil
}

func (s *SessionService) enrichWithConnectURL(ctx context.Context, session *domain.Session) error {
	snap := session.Snapshot()
	if snap.GatewayID == "" {
		return nil
	}
	host, err := s.gatewayReg.PublicHost(ctx, snap.GatewayID)
	if err != nil {
		return fmt.Errorf("lookup gateway host for %q: %w", snap.GatewayID, err)
	}

	tok, err := s.tokenIssuer.Issue(snap.ID, snap.GatewayID, snap.ReconnectGeneration)
	if err != nil {
		return fmt.Errorf("issue token for connect URL: %w", err)
	}

	connectURL := buildConnectURL(snap.ID, host, tok)
	session.SetAgentConnectURL(connectURL)
	return nil
}

func buildConnectURL(sessionID, publicHost, tok string) string {
	return fmt.Sprintf("wss://%s/v1/sessions/%s/game/connect?token=%s", publicHost, sessionID, url.QueryEscape(tok))
}

func normalizeGatewayError(err error) error {
	if errors.Is(err, gateway.ErrNoGatewayAvailable) {
		return domain.ErrNoGatewayAvailable
	}

	return err
}

func sessionName(id string) string {
	return sessionNamePrefix + id
}
