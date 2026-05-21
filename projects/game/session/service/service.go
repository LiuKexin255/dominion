// Package service orchestrates the game session lifecycle.
package service

import (
	"context"
	"errors"
	"fmt"

	"dominion/common/gopkg/logs"
	"dominion/common/gopkg/logs/event"
	"dominion/common/gopkg/otel"
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
	repo          domain.Repository
	gatewayClient gateway.GatewayClient
}

// NewSessionService creates a SessionService.
func NewSessionService(repo domain.Repository, gatewayClient gateway.GatewayClient) *SessionService {
	return &SessionService{
		repo:          repo,
		gatewayClient: gatewayClient,
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

	result, err := s.gatewayClient.InitGameRuntime(ctx, session.ID(), 0)
	if err != nil {
		return nil, normalizeGatewayError(err)
	}

	session.SetToken(result.Token)
	session.SetGatewayID(result.OwnerGatewayID)
	logs.Info(ctx, "gateway assigned", event.String(logFieldSessionID, session.ID()), event.String(logFieldGatewayID, result.OwnerGatewayID))
	span.SetAttributes(attribute.String(logFieldSessionID, session.ID()))
	span.SetAttributes(attribute.String(logFieldGatewayID, result.OwnerGatewayID))

	if err := s.repo.Save(ctx, session); err != nil {
		return nil, err
	}
	logs.Info(ctx, "session saved", event.String(logFieldSessionID, session.ID()))

	return session, nil
}

// GetSession loads a session by resource name.
func (s *SessionService) GetSession(ctx context.Context, name string) (*domain.Session, error) {
	session, err := s.repo.Get(ctx, name)
	if err != nil {
		return nil, err
	}

	logs.Info(ctx, "session loaded", event.String(logFieldSessionID, session.ID()))

	return session, nil
}

// ListSessions lists non-ended sessions.
func (s *SessionService) ListSessions(ctx context.Context) ([]*domain.Session, error) {
	sessions, err := s.repo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}

	logs.Info(ctx, "sessions listed", event.Int(logFieldCount, len(sessions)))

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

// ReconnectSession tries RefreshGameRuntime first; if that fails, it falls back
// to InitGameRuntime with an incremented generation for a full rebuild.
func (s *SessionService) ReconnectSession(ctx context.Context, name string) (*domain.Session, error) {
	ctx, span := otel.Tracer().Start(ctx, spanReconnect)
	defer span.End()

	session, err := s.repo.Get(ctx, name)
	if err != nil {
		return nil, err
	}

	oldToken := session.Token()
	gen := session.ReconnectGeneration()

	result, err := s.gatewayClient.RefreshGameRuntime(ctx, session.ID(), oldToken)
	if err != nil {
		logs.Warn(ctx, "refresh game runtime failed, falling back to init", event.String(logFieldSessionID, session.ID()), event.Err(err))

		newGen := gen + 1
		initResult, initErr := s.gatewayClient.InitGameRuntime(ctx, session.ID(), newGen)
		if initErr != nil {
			return nil, normalizeGatewayError(initErr)
		}

		session.SetToken(initResult.Token)
		session.SetGatewayID(initResult.OwnerGatewayID)
		session.SetReconnectGeneration(newGen)
		logs.Info(ctx, "gateway re-initialized", event.String(logFieldSessionID, session.ID()), event.String(logFieldNewGatewayID, initResult.OwnerGatewayID))
		span.SetAttributes(attribute.String(logFieldSessionID, session.ID()))
		span.SetAttributes(attribute.String(logFieldNewGatewayID, initResult.OwnerGatewayID))
	} else {
		session.SetToken(result.Token)
		session.SetGatewayID(result.OwnerGatewayID)
		logs.Info(ctx, "gateway refreshed", event.String(logFieldSessionID, session.ID()), event.String(logFieldNewGatewayID, result.OwnerGatewayID))
		span.SetAttributes(attribute.String(logFieldSessionID, session.ID()))
		span.SetAttributes(attribute.String(logFieldNewGatewayID, result.OwnerGatewayID))
	}

	if err := session.MarkActive(); err != nil {
		return nil, err
	}

	if err := s.repo.Save(ctx, session); err != nil {
		return nil, err
	}
	logs.Info(ctx, "session saved", event.String(logFieldSessionID, session.ID()))

	return session, nil
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
