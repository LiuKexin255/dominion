package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"dominion/projects/game/session/domain"
	"dominion/projects/game/session/runtime/gateway"
)

const testPublicHost = "gateway-0-game.liukexin.com"

func TestSessionServiceCreateSession(t *testing.T) {
	tests := []struct {
		name           string
		sessionType    domain.SessionType
		sessionID      string
		pickedGateway  string
		pickedHost     string
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
			pickedHost:    testPublicHost,
			issuedToken:   "token-abc",
			wantSessionID: "session-123",
			wantGatewayID: "gateway-a",
			wantURL:       "wss://" + testPublicHost + "/v1/sessions/session-123/game/connect?token=token-abc",
			wantTokenCall: issueCall{sessionID: "session-123", gatewayID: "gateway-a", reconnectGeneration: 0},
		},
		{
			name:           "generates session id when empty",
			sessionType:    domain.TypeSaolei,
			pickedGateway:  "gateway-b",
			pickedHost:     "gateway-1-game.liukexin.com",
			issuedToken:    "token-generated",
			wantGatewayID:  "gateway-b",
			wantURL:        "wss://gateway-1-game.liukexin.com/v1/sessions/%s/game/connect?token=token-generated",
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
			registry := &stubRegistry{
				pickRandomAssignment: &gateway.Assignment{
					GatewayID:  tt.pickedGateway,
					PublicHost: tt.pickedHost,
				},
				pickRandomErr: tt.pickErr,
				publicHostMap: map[string]string{
					"gateway-a": testPublicHost,
					"gateway-b": "gateway-1-game.liukexin.com",
				},
			}

			svc := NewSessionService(repo, issuer, registry)

			// when
			session, err := svc.CreateSession(context.Background(), tt.sessionType, tt.sessionID)

			// then
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
			wantURL := tt.wantURL
			if tt.wantIDNonEmpty {
				wantURL = fmt.Sprintf(tt.wantURL, snapshot.ID)
			}
			if snapshot.AgentConnectURL != wantURL {
				t.Fatalf("CreateSession() agent connect URL = %q, want %q", snapshot.AgentConnectURL, wantURL)
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
		if err := seed.MarkActive(); err != nil {
			t.Fatalf("MarkActive() error = %v", err)
		}

		repo := newFakeRepository(seed)
		svc := NewSessionService(repo, &stubIssuer{}, &stubRegistry{})

		session, err := svc.GetSession(context.Background(), sessionName("session-1"))
		if err != nil {
			t.Fatalf("GetSession() error = %v", err)
		}

		if session.Snapshot().ID != "session-1" {
			t.Fatalf("GetSession() ID = %q, want %q", session.Snapshot().ID, "session-1")
		}
		if session.Snapshot().AgentConnectURL != "" {
			t.Fatalf("GetSession() agent connect URL = %q, want empty", session.Snapshot().AgentConnectURL)
		}
	})

	t.Run("enriches session with connect URL when host is set", func(t *testing.T) {
		seed, err := domain.NewSession(domain.TypeSaolei, "session-1")
		if err != nil {
			t.Fatalf("NewSession() error = %v", err)
		}
		seed.SetGatewayID("gateway-a")
		if err := seed.MarkActive(); err != nil {
			t.Fatalf("MarkActive() error = %v", err)
		}

		repo := newFakeRepository(seed)
		issuer := &stubIssuer{token: "token-abc"}
		svc := NewSessionService(repo, issuer, &stubRegistry{publicHostMap: map[string]string{"gateway-a": testPublicHost}})

		session, err := svc.GetSession(context.Background(), sessionName("session-1"))
		if err != nil {
			t.Fatalf("GetSession() error = %v", err)
		}

		wantURL := "wss://" + testPublicHost + "/v1/sessions/session-1/game/connect?token=token-abc"
		if session.Snapshot().AgentConnectURL != wantURL {
			t.Fatalf("GetSession() agent connect URL = %q, want %q", session.Snapshot().AgentConnectURL, wantURL)
		}
		wantCall := issueCall{sessionID: "session-1", gatewayID: "gateway-a", reconnectGeneration: 0}
		if len(issuer.calls) != 1 {
			t.Fatalf("Issue() calls = %d, want 1", len(issuer.calls))
		}
		if issuer.calls[0] != wantCall {
			t.Fatalf("Issue() call = %+v, want %+v", issuer.calls[0], wantCall)
		}
	})

	t.Run("returns not found", func(t *testing.T) {
		repo := newFakeRepository()
		svc := NewSessionService(repo, &stubIssuer{}, &stubRegistry{})

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
		svc := NewSessionService(repo, &stubIssuer{}, &stubRegistry{})

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
		svc := NewSessionService(repo, &stubIssuer{}, &stubRegistry{})

		err := svc.DeleteSession(context.Background(), sessionName("missing"))
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("DeleteSession() error = %v, want %v", err, domain.ErrNotFound)
		}
	})
}

func TestSessionServiceReconnectSession(t *testing.T) {
	tests := []struct {
		name                string
		seedGatewayID       string
		pickedGateway       string
		pickedHost          string
		pickErr             error
		issuedToken         string
		wantErr             error
		wantGatewayID       string
		wantAgentConnectURL string
	}{
		{
			name:                "reassigns to different gateway",
			seedGatewayID:       "gateway-a",
			pickedGateway:       "gateway-b",
			pickedHost:          "gateway-1-game.liukexin.com",
			issuedToken:         "token-next",
			wantGatewayID:       "gateway-b",
			wantAgentConnectURL: "wss://gateway-1-game.liukexin.com/v1/sessions/session-1/game/connect?token=token-next",
		},
		{
			name:                "falls back to same gateway when single gateway available",
			seedGatewayID:       "gateway-a",
			pickedGateway:       "gateway-a",
			pickedHost:          testPublicHost,
			issuedToken:         "token-same",
			wantGatewayID:       "gateway-a",
			wantAgentConnectURL: "wss://" + testPublicHost + "/v1/sessions/session-1/game/connect?token=token-same",
		},
		{
			name:    "no gateway available",
			pickErr: gateway.ErrNoGatewayAvailable,
			wantErr: domain.ErrNoGatewayAvailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
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
			registry := &stubRegistry{
				pickExcludingAssignment: &gateway.Assignment{
					GatewayID:  tt.pickedGateway,
					PublicHost: tt.pickedHost,
				},
				pickExcludingErr: tt.pickErr,
				publicHostMap: map[string]string{
					"gateway-a": testPublicHost,
					"gateway-b": "gateway-1-game.liukexin.com",
				},
			}
			svc := NewSessionService(repo, issuer, registry)

			// when
			session, err := svc.ReconnectSession(context.Background(), sessionName("session-1"))

			// then
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
			if snapshot.AgentConnectURL != tt.wantAgentConnectURL {
				t.Fatalf("ReconnectSession() agent connect URL = %q, want %q", snapshot.AgentConnectURL, tt.wantAgentConnectURL)
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
		svc := NewSessionService(repo, &stubIssuer{}, &stubRegistry{})

		_, err := svc.ReconnectSession(context.Background(), sessionName("missing"))
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("ReconnectSession() error = %v, want %v", err, domain.ErrNotFound)
		}
	})
}

func TestSessionService_ListSessions(t *testing.T) {
	t.Run("returns non-ended sessions with connect URLs", func(t *testing.T) {
		active, err := domain.NewSession(domain.TypeSaolei, "session-active")
		if err != nil {
			t.Fatalf("NewSession() error = %v", err)
		}
		active.SetGatewayID("gateway-a")
		if err := active.MarkActive(); err != nil {
			t.Fatalf("MarkActive() error = %v", err)
		}

		disconnected, err := domain.NewSession(domain.TypeSaolei, "session-disconnected")
		if err != nil {
			t.Fatalf("NewSession() error = %v", err)
		}
		disconnected.SetGatewayID("gateway-b")
		if err := disconnected.MarkActive(); err != nil {
			t.Fatalf("MarkActive() error = %v", err)
		}
		if err := disconnected.MarkDisconnected(); err != nil {
			t.Fatalf("MarkDisconnected() error = %v", err)
		}

		ended, err := domain.NewSession(domain.TypeSaolei, "session-ended")
		if err != nil {
			t.Fatalf("NewSession() error = %v", err)
		}
		ended.SetGatewayID("gateway-c")
		if err := ended.MarkEnded(); err != nil {
			t.Fatalf("MarkEnded() error = %v", err)
		}

		repo := newFakeRepository(active, disconnected, ended)
		issuer := &stubIssuer{token: "token-list"}
		svc := NewSessionService(repo, issuer, &stubRegistry{publicHostMap: map[string]string{
			"gateway-a": testPublicHost,
			"gateway-b": "gateway-1-game.liukexin.com",
			"gateway-c": "gateway-2-game.liukexin.com",
		}})

		sessions, err := svc.ListSessions(context.Background())
		if err != nil {
			t.Fatalf("ListSessions() error = %v", err)
		}

		if len(sessions) != 2 {
			t.Fatalf("ListSessions() count = %d, want 2", len(sessions))
		}
		gotURLs := map[string]string{}
		for _, session := range sessions {
			snapshot := session.Snapshot()
			gotURLs[snapshot.ID] = snapshot.AgentConnectURL
		}
		wantActiveURL := "wss://" + testPublicHost + "/v1/sessions/session-active/game/connect?token=token-list"
		if gotURLs["session-active"] != wantActiveURL {
			t.Fatalf("ListSessions() active URL = %q, want %q", gotURLs["session-active"], wantActiveURL)
		}
		wantDisconnectedURL := "wss://gateway-1-game.liukexin.com/v1/sessions/session-disconnected/game/connect?token=token-list"
		if gotURLs["session-disconnected"] != wantDisconnectedURL {
			t.Fatalf("ListSessions() disconnected URL = %q, want %q", gotURLs["session-disconnected"], wantDisconnectedURL)
		}
		if _, ok := gotURLs["session-ended"]; ok {
			t.Fatal("ListSessions() returned ended session")
		}
		if len(issuer.calls) != 2 {
			t.Fatalf("Issue() calls = %d, want 2", len(issuer.calls))
		}
	})

	t.Run("returns nil for empty store", func(t *testing.T) {
		repo := newFakeRepository()
		svc := NewSessionService(repo, &stubIssuer{}, &stubRegistry{})

		sessions, err := svc.ListSessions(context.Background())
		if err != nil {
			t.Fatalf("ListSessions() error = %v", err)
		}
		if sessions != nil {
			t.Fatalf("ListSessions() sessions = %#v, want nil", sessions)
		}
	})

	t.Run("skips session without gateway", func(t *testing.T) {
		seed, err := domain.NewSession(domain.TypeSaolei, "session-pending")
		if err != nil {
			t.Fatalf("NewSession() error = %v", err)
		}

		repo := newFakeRepository(seed)
		issuer := &stubIssuer{token: "token-list"}
		svc := NewSessionService(repo, issuer, &stubRegistry{})

		sessions, err := svc.ListSessions(context.Background())
		if err != nil {
			t.Fatalf("ListSessions() error = %v", err)
		}
		if len(sessions) != 1 {
			t.Fatalf("ListSessions() count = %d, want 1", len(sessions))
		}
		if sessions[0].Snapshot().AgentConnectURL != "" {
			t.Fatalf("ListSessions() agent connect URL = %q, want empty", sessions[0].Snapshot().AgentConnectURL)
		}
		if len(issuer.calls) != 0 {
			t.Fatalf("Issue() calls = %d, want 0", len(issuer.calls))
		}
	})

	t.Run("returns error when list fails", func(t *testing.T) {
		listErr := errors.New("list failed")
		repo := newFakeRepository()
		repo.listErr = listErr
		svc := NewSessionService(repo, &stubIssuer{}, &stubRegistry{})

		_, err := svc.ListSessions(context.Background())
		if !errors.Is(err, listErr) {
			t.Fatalf("ListSessions() error = %v, want %v", err, listErr)
		}
	})
}

func Test_enrichWithConnectURL(t *testing.T) {
	t.Run("session with no gateway keeps empty connect URL", func(t *testing.T) {
		session, err := domain.NewSession(domain.TypeSaolei, "session-pending")
		if err != nil {
			t.Fatalf("NewSession() error = %v", err)
		}
		issuer := &stubIssuer{token: "token-abc"}
		svc := NewSessionService(newFakeRepository(), issuer, &stubRegistry{})

		if err := svc.enrichWithConnectURL(context.Background(), session); err != nil {
			t.Fatalf("enrichWithConnectURL() error = %v", err)
		}

		if session.Snapshot().AgentConnectURL != "" {
			t.Fatalf("enrichWithConnectURL() agent connect URL = %q, want empty", session.Snapshot().AgentConnectURL)
		}
		if len(issuer.calls) != 0 {
			t.Fatalf("Issue() calls = %d, want 0", len(issuer.calls))
		}
	})

	t.Run("session with gateway returns token issue error", func(t *testing.T) {
		session, err := domain.NewSession(domain.TypeSaolei, "session-active")
		if err != nil {
			t.Fatalf("NewSession() error = %v", err)
		}
		session.SetGatewayID("gateway-a")
		issueErr := errors.New("issue failed")
		issuer := &stubIssuer{err: issueErr}
		svc := NewSessionService(newFakeRepository(), issuer, &stubRegistry{publicHostMap: map[string]string{"gateway-a": testPublicHost}})

		err = svc.enrichWithConnectURL(context.Background(), session)
		if !errors.Is(err, issueErr) {
			t.Fatalf("enrichWithConnectURL() error = %v, want %v", err, issueErr)
		}
		if !strings.Contains(err.Error(), "issue token for connect URL") {
			t.Fatalf("enrichWithConnectURL() error = %v, want wrapped issue token error", err)
		}
	})
}

func TestBuildConnectURL(t *testing.T) {
	tests := []struct {
		name       string
		sessionID  string
		publicHost string
		token      string
		wantURL    string
	}{
		{
			name:       "simple token",
			sessionID:  "session-123",
			publicHost: testPublicHost,
			token:      "token-abc",
			wantURL:    "wss://" + testPublicHost + "/v1/sessions/session-123/game/connect?token=token-abc",
		},
		{
			name:       "token with special characters is escaped",
			sessionID:  "session-456",
			publicHost: "gateway-1-game.liukexin.com",
			token:      "tok+en&val=ue",
			wantURL:    "wss://gateway-1-game.liukexin.com/v1/sessions/session-456/game/connect?token=tok%2Ben%26val%3Due",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildConnectURL(tt.sessionID, tt.publicHost, tt.token)
			if got != tt.wantURL {
				t.Fatalf("buildConnectURL() = %q, want %q", got, tt.wantURL)
			}
		})
	}
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
	pickRandomAssignment    *gateway.Assignment
	pickRandomErr           error
	pickExcludingAssignment *gateway.Assignment
	pickExcludingErr        error
	publicHostMap           map[string]string
	publicHostErr           error
}

func (s *stubRegistry) PickRandom(_ context.Context) (*gateway.Assignment, error) {
	if s.pickRandomErr != nil {
		return nil, s.pickRandomErr
	}
	return s.pickRandomAssignment, nil
}

func (s *stubRegistry) PickRandomExcluding(_ context.Context, _ string) (*gateway.Assignment, error) {
	if s.pickExcludingErr != nil {
		return nil, s.pickExcludingErr
	}
	return s.pickExcludingAssignment, nil
}

func (s *stubRegistry) PublicHost(_ context.Context, gatewayID string) (string, error) {
	if s.publicHostErr != nil {
		return "", s.publicHostErr
	}
	if host, ok := s.publicHostMap[gatewayID]; ok {
		return host, nil
	}
	return "", gateway.ErrNoGatewayAvailable
}

type fakeRepository struct {
	mu        sync.RWMutex
	sessions  map[string]*domain.Session
	deleted   map[string]bool
	lastSaved *domain.Session
	getErr    error
	listErr   error
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

func (r *fakeRepository) List(_ context.Context) ([]*domain.Session, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	var sessions []*domain.Session
	for _, session := range r.sessions {
		if session.Snapshot().Status == domain.StatusEnded {
			continue
		}
		sessions = append(sessions, mustRehydrate(session.Snapshot()))
	}
	return sessions, nil
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
