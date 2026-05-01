// Package service orchestrates the game session lifecycle.
package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"dominion/projects/game/pkg/token"
	"dominion/projects/game/session/domain"
	"dominion/projects/game/session/runtime/gateway"
)

const sessionNamePrefix = "sessions/"

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
	session, err := domain.NewSession(sessionType, sessionID)
	if err != nil {
		return nil, err
	}

	assignment, err := s.gatewayReg.PickRandom(ctx)
	if err != nil {
		return nil, normalizeGatewayError(err)
	}

	session.SetGatewayID(assignment.GatewayID)

	if err := s.enrichWithConnectURL(ctx, session); err != nil {
		return nil, err
	}
	if err := s.repo.Save(ctx, session); err != nil {
		return nil, err
	}

	return session, nil
}

// GetSession loads a session by resource name.
func (s *SessionService) GetSession(ctx context.Context, name string) (*domain.Session, error) {
	session, err := s.repo.Get(ctx, name)
	if err != nil {
		return nil, err
	}

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

	for _, session := range sessions {
		if err := s.enrichWithConnectURL(ctx, session); err != nil {
			return nil, err
		}
	}

	return sessions, nil
}

// DeleteSession removes a session by resource name.
func (s *SessionService) DeleteSession(ctx context.Context, name string) error {
	return s.repo.Delete(ctx, name)
}

// ReconnectSession reassigns a gateway, issues a new token, and persists the updated session.
func (s *SessionService) ReconnectSession(ctx context.Context, name string) (*domain.Session, error) {
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
	if err := session.MarkActive(); err != nil {
		return nil, err
	}

	if err := s.enrichWithConnectURL(ctx, session); err != nil {
		return nil, err
	}
	if err := s.repo.Save(ctx, session); err != nil {
		return nil, err
	}

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
