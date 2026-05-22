package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"dominion/projects/game/session/domain"
	"dominion/projects/game/session/runtime/runtimeclient"
)

const testAggregateHost = "game.liukexin.com"

func TestSessionServiceCreateSession(t *testing.T) {
	tests := []struct {
		name           string
		sessionType    domain.SessionType
		sessionID      string
		initResult     *runtimeclient.InitResult
		initErr        error
		wantErr        error
		wantSessionID  string
		wantRuntimeID  string
		wantIDNonEmpty bool
	}{
		{
			name:          "happy path with provided session id",
			sessionType:   domain.TypeSaolei,
			sessionID:     "session-123",
			initResult:    &runtimeclient.InitResult{OwnerRuntimeID: "gateway-a", OwnerEpoch: 1, Token: "token-abc", ExpiresAt: time.Now().Add(time.Hour)},
			wantSessionID: "session-123",
			wantRuntimeID: "gateway-a",
		},
		{
			name:           "generates session id when empty",
			sessionType:    domain.TypeSaolei,
			initResult:     &runtimeclient.InitResult{OwnerRuntimeID: "gateway-b", OwnerEpoch: 1, Token: "token-generated", ExpiresAt: time.Now().Add(time.Hour)},
			wantRuntimeID:  "gateway-b",
			wantIDNonEmpty: true,
		},
		{
			name:        "no gateway available",
			sessionType: domain.TypeSaolei,
			initErr:     runtimeclient.ErrNoRuntimeAvailable,
			wantErr:     domain.ErrNoRuntimeAvailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeRepository()
			client := &stubRuntimeClient{
				initResult: tt.initResult,
				initErr:    tt.initErr,
			}

			svc := NewSessionService(repo, client)

			session, err := svc.CreateSession(context.Background(), tt.sessionType, tt.sessionID)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("CreateSession() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}

			if tt.wantSessionID != "" && session.ID() != tt.wantSessionID {
				t.Fatalf("CreateSession() session ID = %q, want %q", session.ID(), tt.wantSessionID)
			}
			if tt.wantIDNonEmpty && session.ID() == "" {
				t.Fatal("CreateSession() generated empty session ID")
			}
			if session.OwnerRuntimeID() != tt.wantRuntimeID {
				t.Fatalf("CreateSession() runtime ID = %q, want %q", session.OwnerRuntimeID(), tt.wantRuntimeID)
			}
			if tt.wantRuntimeID != "" && session.Token() != tt.initResult.Token {
				t.Fatalf("CreateSession() token = %q, want %q", session.Token(), tt.initResult.Token)
			}

			if len(client.initCalls) != 1 {
				t.Fatalf("InitGameRuntime() calls = %d, want 1", len(client.initCalls))
			}
			if tt.wantSessionID != "" && client.initCalls[0].sessionID != tt.wantSessionID {
				t.Fatalf("InitGameRuntime() session ID = %q, want %q", client.initCalls[0].sessionID, tt.wantSessionID)
			}
			if tt.wantIDNonEmpty {
				if client.initCalls[0].sessionID != session.ID() {
					t.Fatalf("InitGameRuntime() session ID = %q, want %q", client.initCalls[0].sessionID, session.ID())
				}
				if !strings.HasPrefix(repo.lastSavedName(), sessionNamePrefix) {
					t.Fatalf("saved name = %q, want %q prefix", repo.lastSavedName(), sessionNamePrefix)
				}
			}
		})
	}
}

func TestSessionServiceGetSession(t *testing.T) {
	t.Run("returns session without connect URL when no gateway", func(t *testing.T) {
		seed, err := domain.NewSession(domain.TypeSaolei, "session-1")
		if err != nil {
			t.Fatalf("NewSession() error = %v", err)
		}
		if err := seed.MarkActive(); err != nil {
			t.Fatalf("MarkActive() error = %v", err)
		}

		repo := newFakeRepository(seed)
		svc := NewSessionService(repo, &stubRuntimeClient{})

		session, err := svc.GetSession(context.Background(), sessionName("session-1"))
		if err != nil {
			t.Fatalf("GetSession() error = %v", err)
		}

		if session.ID() != "session-1" {
			t.Fatalf("GetSession() ID = %q, want %q", session.ID(), "session-1")
		}
	})

	t.Run("returns not found", func(t *testing.T) {
		repo := newFakeRepository()
		svc := NewSessionService(repo, &stubRuntimeClient{})

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
		seed.SetOwnerRuntimeID("gateway-a")

		repo := newFakeRepository(seed)
		svc := NewSessionService(repo, &stubRuntimeClient{})

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
		svc := NewSessionService(repo, &stubRuntimeClient{})

		err := svc.DeleteSession(context.Background(), sessionName("missing"))
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("DeleteSession() error = %v, want %v", err, domain.ErrNotFound)
		}
	})
}

func TestSessionServiceReconnectSession(t *testing.T) {
	tests := []struct {
		name          string
		seedRuntimeID string
		seedToken     string
		initResult    *runtimeclient.InitResult
		initErr       error
		refreshResult *runtimeclient.RefreshResult
		refreshErr    error
		wantErr       error
		wantRuntimeID string
		wantToken     string
	}{
		{
			name:          "reassigns to different gateway",
			seedRuntimeID: "gateway-a",
			seedToken:     "old-token",
			refreshResult: &runtimeclient.RefreshResult{OwnerRuntimeID: "gateway-b", OwnerEpoch: 2, ReconnectGeneration: 1, Token: "token-next", ExpiresAt: time.Now().Add(time.Hour)},
			wantRuntimeID: "gateway-b",
			wantToken:     "token-next",
		},
		{
			name:          "falls back to same gateway when single gateway available",
			seedRuntimeID: "gateway-a",
			seedToken:     "old-token",
			refreshResult: &runtimeclient.RefreshResult{OwnerRuntimeID: "gateway-a", OwnerEpoch: 2, ReconnectGeneration: 1, Token: "token-same", ExpiresAt: time.Now().Add(time.Hour)},
			wantRuntimeID: "gateway-a",
			wantToken:     "token-same",
		},
		{
			name:          "no gateway available for refresh, but init succeeds",
			refreshErr:    errors.New("gateway unreachable"),
			initResult:    &runtimeclient.InitResult{OwnerRuntimeID: "gateway-b", Token: "rebuild-token"},
			wantRuntimeID: "gateway-b",
			wantToken:     "rebuild-token",
		},
		{
			name:       "no gateway available for both refresh and init",
			refreshErr: runtimeclient.ErrNoRuntimeAvailable,
			initErr:    runtimeclient.ErrNoRuntimeAvailable,
			wantErr:    domain.ErrNoRuntimeAvailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seed, err := domain.NewSession(domain.TypeSaolei, "session-1")
			if err != nil {
				t.Fatalf("NewSession() error = %v", err)
			}
			seed.SetOwnerRuntimeID(tt.seedRuntimeID)
			seed.SetToken(tt.seedToken)
			if err := seed.MarkActive(); err != nil {
				t.Fatalf("MarkActive() error = %v", err)
			}
			if err := seed.MarkDisconnected(); err != nil {
				t.Fatalf("MarkDisconnected() error = %v", err)
			}

			repo := newFakeRepository(seed)
			client := &stubRuntimeClient{
				initResult:    tt.initResult,
				initErr:       tt.initErr,
				refreshResult: tt.refreshResult,
				refreshErr:    tt.refreshErr,
			}
			svc := NewSessionService(repo, client)

			session, err := svc.ReconnectSession(context.Background(), sessionName("session-1"))

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ReconnectSession() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}

			if session.Status() != domain.StatusActive {
				t.Fatalf("ReconnectSession() status = %v, want %v", session.Status(), domain.StatusActive)
			}
			if session.OwnerRuntimeID() != tt.wantRuntimeID {
				t.Fatalf("ReconnectSession() runtime ID = %q, want %q", session.OwnerRuntimeID(), tt.wantRuntimeID)
			}
			if session.Token() != tt.wantToken {
				t.Fatalf("ReconnectSession() token = %q, want %q", session.Token(), tt.wantToken)
			}
			if len(client.refreshCalls) != 1 {
				t.Fatalf("RefreshGameRuntime() calls = %d, want 1", len(client.refreshCalls))
			}
		})
	}

	t.Run("RefreshGameRuntime fails, falls back to InitGameRuntime", func(t *testing.T) {
		seed, err := domain.NewSession(domain.TypeSaolei, "session-1")
		if err != nil {
			t.Fatalf("NewSession() error = %v", err)
		}
		seed.SetOwnerRuntimeID("gateway-a")
		seed.SetToken("old-token")
		// Set a specific reconnect generation to verify it gets incremented in fallback
		seed.SetReconnectGeneration(5)
		if err := seed.MarkActive(); err != nil {
			t.Fatalf("MarkActive() error = %v", err)
		}
		if err := seed.MarkDisconnected(); err != nil {
			t.Fatalf("MarkDisconnected() error = %v", err)
		}

		repo := newFakeRepository(seed)
		client := &stubRuntimeClient{
			refreshErr: errors.New("gateway unreachable"),
			initResult: &runtimeclient.InitResult{OwnerRuntimeID: "gateway-b", Token: "rebuild-token"},
		}
		svc := NewSessionService(repo, client)

		session, err := svc.ReconnectSession(context.Background(), sessionName("session-1"))
		if err != nil {
			t.Fatalf("ReconnectSession() error = %v", err)
		}

		// Verify fallback path was used
		if len(client.refreshCalls) != 1 {
			t.Fatalf("RefreshGameRuntime() calls = %d, want 1", len(client.refreshCalls))
		}
		if len(client.initCalls) != 1 {
			t.Fatalf("InitGameRuntime() calls = %d, want 1 (fallback)", len(client.initCalls))
		}
		// Verify reconnect generation was incremented for the Init call
		if client.initCalls[0].reconnectGeneration != 6 {
			t.Fatalf("InitGameRuntime() generation = %d, want 6 (5+1)", client.initCalls[0].reconnectGeneration)
		}
		// Verify session was updated
		if session.Token() != "rebuild-token" {
			t.Fatalf("ReconnectSession() token = %q, want %q", session.Token(), "rebuild-token")
		}
		if session.OwnerRuntimeID() != "gateway-b" {
			t.Fatalf("ReconnectSession() runtime ID = %q, want %q", session.OwnerRuntimeID(), "gateway-b")
		}
		if session.ReconnectGeneration() != 6 {
			t.Fatalf("ReconnectSession() reconnectGen = %d, want 6", session.ReconnectGeneration())
		}
	})

	t.Run("RefreshGameRuntime fails and InitGameRuntime also fails", func(t *testing.T) {
		seed, err := domain.NewSession(domain.TypeSaolei, "session-1")
		if err != nil {
			t.Fatalf("NewSession() error = %v", err)
		}
		seed.SetOwnerRuntimeID("gateway-a")
		seed.SetToken("old-token")
		if err := seed.MarkActive(); err != nil {
			t.Fatalf("MarkActive() error = %v", err)
		}
		if err := seed.MarkDisconnected(); err != nil {
			t.Fatalf("MarkDisconnected() error = %v", err)
		}

		repo := newFakeRepository(seed)
		client := &stubRuntimeClient{
			refreshErr: errors.New("gateway unreachable"),
			initErr:    runtimeclient.ErrNoRuntimeAvailable,
		}
		svc := NewSessionService(repo, client)

		_, err = svc.ReconnectSession(context.Background(), sessionName("session-1"))
		if !errors.Is(err, domain.ErrNoRuntimeAvailable) {
			t.Fatalf("ReconnectSession() error = %v, want %v", err, domain.ErrNoRuntimeAvailable)
		}
	})

	t.Run("returns not found", func(t *testing.T) {
		repo := newFakeRepository()
		svc := NewSessionService(repo, &stubRuntimeClient{})

		_, err := svc.ReconnectSession(context.Background(), sessionName("missing"))
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("ReconnectSession() error = %v, want %v", err, domain.ErrNotFound)
		}
	})
}

func TestSessionService_ListSessions(t *testing.T) {
	t.Run("returns non-ended sessions without connect URLs", func(t *testing.T) {
		active, err := domain.NewSession(domain.TypeSaolei, "session-active")
		if err != nil {
			t.Fatalf("NewSession() error = %v", err)
		}
		active.SetOwnerRuntimeID("gateway-a")
		if err := active.MarkActive(); err != nil {
			t.Fatalf("MarkActive() error = %v", err)
		}

		disconnected, err := domain.NewSession(domain.TypeSaolei, "session-disconnected")
		if err != nil {
			t.Fatalf("NewSession() error = %v", err)
		}
		disconnected.SetOwnerRuntimeID("gateway-b")
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
		ended.SetOwnerRuntimeID("gateway-c")
		if err := ended.MarkEnded(); err != nil {
			t.Fatalf("MarkEnded() error = %v", err)
		}

		repo := newFakeRepository(active, disconnected, ended)
		svc := NewSessionService(repo, &stubRuntimeClient{})

		sessions, err := svc.ListSessions(context.Background())
		if err != nil {
			t.Fatalf("ListSessions() error = %v", err)
		}

		if len(sessions) != 2 {
			t.Fatalf("ListSessions() count = %d, want 2", len(sessions))
		}
		gotIDs := map[string]bool{}
		for _, session := range sessions {
			gotIDs[session.ID()] = true
		}
		if !gotIDs["session-active"] {
			t.Fatal("ListSessions() missing session-active")
		}
		if !gotIDs["session-disconnected"] {
			t.Fatal("ListSessions() missing session-disconnected")
		}
	})

	t.Run("returns nil for empty store", func(t *testing.T) {
		repo := newFakeRepository()
		svc := NewSessionService(repo, &stubRuntimeClient{})

		sessions, err := svc.ListSessions(context.Background())
		if err != nil {
			t.Fatalf("ListSessions() error = %v", err)
		}
		if sessions != nil {
			t.Fatalf("ListSessions() sessions = %#v, want nil", sessions)
		}
	})

	t.Run("returns error when list fails", func(t *testing.T) {
		listErr := errors.New("list failed")
		repo := newFakeRepository()
		repo.listErr = listErr
		svc := NewSessionService(repo, &stubRuntimeClient{})

		_, err := svc.ListSessions(context.Background())
		if !errors.Is(err, listErr) {
			t.Fatalf("ListSessions() error = %v, want %v", err, listErr)
		}
	})
}

type initCall struct {
	sessionID           string
	reconnectGeneration int64
}

type refreshCall struct {
	sessionID string
	oldToken  string
}

type stubRuntimeClient struct {
	initResult    *runtimeclient.InitResult
	initErr       error
	initCalls     []initCall
	refreshResult *runtimeclient.RefreshResult
	refreshErr    error
	refreshCalls  []refreshCall
}

func (s *stubRuntimeClient) InitGameRuntime(_ context.Context, sessionID string, reconnectGeneration int64) (*runtimeclient.InitResult, error) {
	s.initCalls = append(s.initCalls, initCall{sessionID: sessionID, reconnectGeneration: reconnectGeneration})
	if s.initErr != nil {
		return nil, s.initErr
	}
	return s.initResult, nil
}

func (s *stubRuntimeClient) RefreshGameRuntime(_ context.Context, sessionID string, oldToken string) (*runtimeclient.RefreshResult, error) {
	s.refreshCalls = append(s.refreshCalls, refreshCall{sessionID: sessionID, oldToken: oldToken})
	if s.refreshErr != nil {
		return nil, s.refreshErr
	}
	return s.refreshResult, nil
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
		repo.sessions[sessionName(session.ID())] = mustRehydrate(session.Snapshot())
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
		if session.Status() == domain.StatusEnded {
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
	r.sessions[sessionName(cloned.ID())] = cloned
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
	return sessionName(r.lastSaved.ID())
}

func mustRehydrate(snapshot domain.SessionSnapshot) *domain.Session {
	session, err := domain.Rehydrate(snapshot)
	if err != nil {
		panic(err)
	}
	return session
}
