package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"dominion/projects/game/session/domain"
	"dominion/projects/game/session/runtime/gateway"
)

func TestSessionServiceCreateSession(t *testing.T) {
	tests := []struct {
		name           string
		sessionType    domain.SessionType
		sessionID      string
		pickedGateway  string
		pickErr        error
		issuedToken    string
		issueErr       error
		wantErr        error
		wantSessionID  string
		wantGatewayID  string
		wantURL        string
		wantTokenCall  issueCall
		wantIDNonEmpty bool
	}{
		{
			name:          "happy path with provided session id",
			sessionType:   domain.TypeSaolei,
			sessionID:     "session-123",
			pickedGateway: "gateway-a",
			issuedToken:   "token-abc",
			wantSessionID: "session-123",
			wantGatewayID: "gateway-a",
			wantURL:       "wss://gateway-a.gateway.example/connect?token=token-abc",
			wantTokenCall: issueCall{sessionID: "session-123", gatewayID: "gateway-a", reconnectGeneration: 0},
		},
		{
			name:           "generates session id when empty",
			sessionType:    domain.TypeSaolei,
			pickedGateway:  "gateway-b",
			issuedToken:    "token-generated",
			wantGatewayID:  "gateway-b",
			wantURL:        "wss://gateway-b.gateway.example/connect?token=token-generated",
			wantIDNonEmpty: true,
		},
		{
			name:        "no gateway available",
			sessionType: domain.TypeSaolei,
			pickErr:     gateway.ErrNoGatewayAvailable,
			wantErr:     domain.ErrNoGatewayAvailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeRepository()
			issuer := &stubIssuer{token: tt.issuedToken, err: tt.issueErr}
			registry := &stubRegistry{pickRandomGateway: tt.pickedGateway, pickRandomErr: tt.pickErr}

			svc := NewSessionService(repo, issuer, registry, "gateway.example")

			session, url, err := svc.CreateSession(context.Background(), tt.sessionType, tt.sessionID)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("CreateSession() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}

			snapshot := session.Snapshot()
			if tt.wantSessionID != "" && snapshot.ID != tt.wantSessionID {
				t.Fatalf("CreateSession() session ID = %q, want %q", snapshot.ID, tt.wantSessionID)
			}
			if tt.wantIDNonEmpty && snapshot.ID == "" {
				t.Fatal("CreateSession() generated empty session ID")
			}
			if snapshot.GatewayID != tt.wantGatewayID {
				t.Fatalf("CreateSession() gateway ID = %q, want %q", snapshot.GatewayID, tt.wantGatewayID)
			}
			if url != tt.wantURL {
				t.Fatalf("CreateSession() URL = %q, want %q", url, tt.wantURL)
			}

			if len(issuer.calls) != 1 {
				t.Fatalf("Issue() calls = %d, want 1", len(issuer.calls))
			}
			if tt.wantTokenCall.sessionID != "" && issuer.calls[0] != tt.wantTokenCall {
				t.Fatalf("Issue() call = %+v, want %+v", issuer.calls[0], tt.wantTokenCall)
			}
			if tt.wantIDNonEmpty {
				if issuer.calls[0].sessionID != snapshot.ID {
					t.Fatalf("Issue() session ID = %q, want %q", issuer.calls[0].sessionID, snapshot.ID)
				}
				if !strings.HasPrefix(repo.lastSavedName(), sessionNamePrefix) {
					t.Fatalf("saved name = %q, want %q prefix", repo.lastSavedName(), sessionNamePrefix)
				}
			}
		})
	}
}

func TestSessionServiceGetSession(t *testing.T) {
	t.Run("returns session", func(t *testing.T) {
		seed, err := domain.NewSession(domain.TypeSaolei, "session-1")
		if err != nil {
			t.Fatalf("NewSession() error = %v", err)
		}
		seed.SetGatewayID("gateway-a")
		if err := seed.MarkActive(); err != nil {
			t.Fatalf("MarkActive() error = %v", err)
		}

		repo := newFakeRepository(seed)
		svc := NewSessionService(repo, &stubIssuer{}, &stubRegistry{}, "gateway.example")

		session, err := svc.GetSession(context.Background(), sessionName("session-1"))
		if err != nil {
			t.Fatalf("GetSession() error = %v", err)
		}

		if session.Snapshot().ID != "session-1" {
			t.Fatalf("GetSession() ID = %q, want %q", session.Snapshot().ID, "session-1")
		}
	})

	t.Run("returns not found", func(t *testing.T) {
		repo := newFakeRepository()
		svc := NewSessionService(repo, &stubIssuer{}, &stubRegistry{}, "gateway.example")

		_, err := svc.GetSession(context.Background(), sessionName("missing"))
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("GetSession() error = %v, want %v", err, domain.ErrNotFound)
		}
	})
}

func TestSessionServiceDeleteSession(t *testing.T) {
	t.Run("deletes session", func(t *testing.T) {
		seed, err := domain.NewSession(domain.TypeSaolei, "session-1")
		if err != nil {
			t.Fatalf("NewSession() error = %v", err)
		}
		seed.SetGatewayID("gateway-a")

		repo := newFakeRepository(seed)
		svc := NewSessionService(repo, &stubIssuer{}, &stubRegistry{}, "gateway.example")

		if err := svc.DeleteSession(context.Background(), sessionName("session-1")); err != nil {
			t.Fatalf("DeleteSession() error = %v", err)
		}

		if !repo.deleted[sessionName("session-1")] {
			t.Fatal("DeleteSession() did not delete session")
		}
		if repo.lastSaved != nil {
			t.Fatal("DeleteSession() unexpectedly saved session before delete")
		}
	})

	t.Run("returns not found", func(t *testing.T) {
		repo := newFakeRepository()
		svc := NewSessionService(repo, &stubIssuer{}, &stubRegistry{}, "gateway.example")

		err := svc.DeleteSession(context.Background(), sessionName("missing"))
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("DeleteSession() error = %v, want %v", err, domain.ErrNotFound)
		}
	})
}

func TestSessionServiceReconnectSession(t *testing.T) {
	tests := []struct {
		name          string
		seedGatewayID string
		pickedGateway string
		pickErr       error
		issuedToken   string
		wantErr       error
		wantGatewayID string
		wantURL       string
	}{
		{
			name:          "reassigns to different gateway",
			seedGatewayID: "gateway-a",
			pickedGateway: "gateway-b",
			issuedToken:   "token-next",
			wantGatewayID: "gateway-b",
			wantURL:       "wss://gateway-b.gateway.example/connect?token=token-next",
		},
		{
			name:          "falls back to same gateway when single gateway available",
			seedGatewayID: "gateway-a",
			pickedGateway: "gateway-a",
			issuedToken:   "token-same",
			wantGatewayID: "gateway-a",
			wantURL:       "wss://gateway-a.gateway.example/connect?token=token-same",
		},
		{
			name:    "no gateway available",
			pickErr: gateway.ErrNoGatewayAvailable,
			wantErr: domain.ErrNoGatewayAvailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seed, err := domain.NewSession(domain.TypeSaolei, "session-1")
			if err != nil {
				t.Fatalf("NewSession() error = %v", err)
			}
			seed.SetGatewayID(tt.seedGatewayID)
			if err := seed.MarkActive(); err != nil {
				t.Fatalf("MarkActive() error = %v", err)
			}
			if err := seed.MarkDisconnected(); err != nil {
				t.Fatalf("MarkDisconnected() error = %v", err)
			}

			repo := newFakeRepository(seed)
			issuer := &stubIssuer{token: tt.issuedToken}
			registry := &stubRegistry{pickExcludingGateway: tt.pickedGateway, pickExcludingErr: tt.pickErr}
			svc := NewSessionService(repo, issuer, registry, "gateway.example")

			session, url, err := svc.ReconnectSession(context.Background(), sessionName("session-1"))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ReconnectSession() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}

			snapshot := session.Snapshot()
			if snapshot.Status != domain.StatusActive {
				t.Fatalf("ReconnectSession() status = %v, want %v", snapshot.Status, domain.StatusActive)
			}
			if snapshot.GatewayID != tt.wantGatewayID {
				t.Fatalf("ReconnectSession() gateway ID = %q, want %q", snapshot.GatewayID, tt.wantGatewayID)
			}
			if snapshot.ReconnectGeneration != 1 {
				t.Fatalf("ReconnectSession() generation = %d, want 1", snapshot.ReconnectGeneration)
			}
			if url != tt.wantURL {
				t.Fatalf("ReconnectSession() URL = %q, want %q", url, tt.wantURL)
			}
			if len(issuer.calls) != 1 {
				t.Fatalf("Issue() calls = %d, want 1", len(issuer.calls))
			}
			wantCall := issueCall{sessionID: "session-1", gatewayID: tt.wantGatewayID, reconnectGeneration: 1}
			if issuer.calls[0] != wantCall {
				t.Fatalf("Issue() call = %+v, want %+v", issuer.calls[0], wantCall)
			}
		})
	}

	t.Run("returns not found", func(t *testing.T) {
		repo := newFakeRepository()
		svc := NewSessionService(repo, &stubIssuer{}, &stubRegistry{}, "gateway.example")

		_, _, err := svc.ReconnectSession(context.Background(), sessionName("missing"))
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("ReconnectSession() error = %v, want %v", err, domain.ErrNotFound)
		}
	})
}

type issueCall struct {
	sessionID           string
	gatewayID           string
	reconnectGeneration int64
}

type stubIssuer struct {
	token string
	err   error
	calls []issueCall
}

func (s *stubIssuer) Issue(sessionID, gatewayID string, reconnectGeneration int64) (string, error) {
	s.calls = append(s.calls, issueCall{
		sessionID:           sessionID,
		gatewayID:           gatewayID,
		reconnectGeneration: reconnectGeneration,
	})
	if s.err != nil {
		return "", s.err
	}
	return s.token, nil
}

type stubRegistry struct {
	pickRandomGateway    string
	pickRandomErr        error
	pickExcludingGateway string
	pickExcludingErr     error
}

func (s *stubRegistry) PickRandom() (string, error) {
	if s.pickRandomErr != nil {
		return "", s.pickRandomErr
	}
	return s.pickRandomGateway, nil
}

func (s *stubRegistry) PickRandomExcluding(_ string) (string, error) {
	if s.pickExcludingErr != nil {
		return "", s.pickExcludingErr
	}
	return s.pickExcludingGateway, nil
}

type fakeRepository struct {
	mu        sync.RWMutex
	sessions  map[string]*domain.Session
	deleted   map[string]bool
	lastSaved *domain.Session
	getErr    error
	saveErr   error
	deleteErr error
}

func newFakeRepository(seed ...*domain.Session) *fakeRepository {
	repo := &fakeRepository{
		sessions: make(map[string]*domain.Session, len(seed)),
		deleted:  make(map[string]bool),
	}
	for _, session := range seed {
		if session == nil {
			continue
		}
		repo.sessions[sessionName(session.Snapshot().ID)] = mustRehydrate(session.Snapshot())
	}
	return repo
}

func (r *fakeRepository) Get(_ context.Context, name string) (*domain.Session, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	session, ok := r.sessions[name]
	if !ok {
		return nil, domain.ErrNotFound
	}

	return mustRehydrate(session.Snapshot()), nil
}

func (r *fakeRepository) Save(_ context.Context, session *domain.Session) error {
	if r.saveErr != nil {
		return r.saveErr
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	cloned := mustRehydrate(session.Snapshot())
	r.sessions[sessionName(cloned.Snapshot().ID)] = cloned
	r.lastSaved = mustRehydrate(cloned.Snapshot())
	return nil
}

func (r *fakeRepository) Delete(_ context.Context, name string) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.sessions[name]; !ok {
		return domain.ErrNotFound
	}

	delete(r.sessions, name)
	r.deleted[name] = true
	return nil
}

func (r *fakeRepository) lastSavedName() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.lastSaved == nil {
		return ""
	}
	return sessionName(r.lastSaved.Snapshot().ID)
}

func mustRehydrate(snapshot domain.SessionSnapshot) *domain.Session {
	session, err := domain.Rehydrate(snapshot)
	if err != nil {
		panic(err)
	}
	return session
}
