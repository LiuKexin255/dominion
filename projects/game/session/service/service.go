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
func (s *SessionService) CreateSession(ctx context.Context, sessionType domain.SessionType, sessionID string) (*domain.Session, string, error) {
	session, err := domain.NewSession(sessionType, sessionID)
	if err != nil {
		return nil, "", err
	}

	assignment, err := s.gatewayReg.PickRandom(ctx)
	if err != nil {
		return nil, "", normalizeGatewayError(err)
	}

	session.SetGatewayID(assignment.GatewayID)

	snapshot := session.Snapshot()
	tok, err := s.tokenIssuer.Issue(snapshot.ID, assignment.GatewayID, snapshot.ReconnectGeneration)
	if err != nil {
		return nil, "", err
	}

	connectURL := buildConnectURL(snapshot.ID, assignment.PublicHost, tok)
	if err := s.repo.Save(ctx, session); err != nil {
		return nil, "", err
	}

	return session, connectURL, nil
}

// GetSession loads a session by resource name.
func (s *SessionService) GetSession(ctx context.Context, name string) (*domain.Session, error) {
	return s.repo.Get(ctx, name)
}

// DeleteSession removes a session by resource name.
func (s *SessionService) DeleteSession(ctx context.Context, name string) error {
	return s.repo.Delete(ctx, name)
}

// ReconnectSession reassigns a gateway, issues a new token, and persists the updated session.
func (s *SessionService) ReconnectSession(ctx context.Context, name string) (*domain.Session, string, error) {
	session, err := s.repo.Get(ctx, name)
	if err != nil {
		return nil, "", err
	}

	current := session.Snapshot()
	assignment, err := s.gatewayReg.PickRandomExcluding(ctx, current.GatewayID)
	if err != nil {
		return nil, "", normalizeGatewayError(err)
	}

	session.SetGatewayID(assignment.GatewayID)
	if err := session.MarkActive(); err != nil {
		return nil, "", err
	}

	snapshot := session.Snapshot()
	tok, err := s.tokenIssuer.Issue(snapshot.ID, assignment.GatewayID, snapshot.ReconnectGeneration)
	if err != nil {
		return nil, "", err
	}

	connectURL := buildConnectURL(snapshot.ID, assignment.PublicHost, tok)
	if err := s.repo.Save(ctx, session); err != nil {
		return nil, "", err
	}

	return session, connectURL, nil
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
