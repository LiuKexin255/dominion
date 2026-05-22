package session

import (
	"context"
	"strings"
	"testing"

	"dominion/projects/game/session/domain"
	"dominion/projects/game/session/runtime/runtimeclient"
	"dominion/projects/game/session/runtime/storage"
	"dominion/projects/game/session/service"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// sessionName returns the resource name in "sessions/{id}" format.
func sessionName(id string) string {
	return "sessions/" + id
}

func sessionNames(sessions []*Session) []string {
	names := make([]string, len(sessions))
	for i, s := range sessions {
		names[i] = s.GetName()
	}
	return names
}

// newTestHandler creates a handler with a FakeStore and stub GatewayClient.
func newTestHandler(runtimeIDs ...string) (*Handler, *storage.FakeStore) {
	repo := storage.NewFakeStore()

	client := &stubHandlerClient{
		runtimeIDs: runtimeIDs,
	}
	svc := service.NewSessionService(repo, client)
	return NewHandler(svc), repo
}

// stubHandlerClient returns runtimeIDs[0] for InitGameRuntime and
// runtimeIDs[1] (or runtimeIDs[0] if only one) for RefreshGameRuntime.
type stubHandlerClient struct {
	runtimeIDs   []string
	initIndex    int
	refreshIndex int
}

func (s *stubHandlerClient) InitGameRuntime(_ context.Context, sessionID string, reconnectGeneration int64) (*runtimeclient.InitResult, error) {
	if len(s.runtimeIDs) == 0 {
		return nil, runtimeclient.ErrNoRuntimeAvailable
	}
	idx := s.initIndex % len(s.runtimeIDs)
	gwID := s.runtimeIDs[idx]
	s.initIndex++
	return &runtimeclient.InitResult{
		OwnerRuntimeID: gwID,
		OwnerEpoch:     1,
		Token:          "test-token-for-" + sessionID,
	}, nil
}

func (s *stubHandlerClient) RefreshGameRuntime(_ context.Context, sessionID string, oldToken string) (*runtimeclient.RefreshResult, error) {
	if len(s.runtimeIDs) == 0 {
		return nil, runtimeclient.ErrNoRuntimeAvailable
	}
	refreshOffset := 1
	if len(s.runtimeIDs) < 2 {
		refreshOffset = 0
	}
	idx := (refreshOffset + s.refreshIndex) % len(s.runtimeIDs)
	s.refreshIndex++
	gwID := s.runtimeIDs[idx]
	return &runtimeclient.RefreshResult{
		OwnerRuntimeID:      gwID,
		OwnerEpoch:          2,
		ReconnectGeneration: 1,
		Token:               "refresh-token-for-" + sessionID,
	}, nil
}

// seedSession creates a domain session in Active state with a gateway assigned,
// persists it, and returns the resource name.
func seedSession(t *testing.T, repo domain.Repository, sessionID, runtimeID string) string {
	t.Helper()

	session, err := domain.NewSession(domain.TypeSaolei, sessionID)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	session.SetOwnerRuntimeID(runtimeID)
	session.SetToken("seed-token")
	if err := session.MarkActive(); err != nil {
		t.Fatalf("MarkActive() error = %v", err)
	}
	if err := repo.Save(context.Background(), session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	return sessionName(sessionID)
}

// seedDisconnectedSession creates a session that is active then disconnected.
func seedDisconnectedSession(t *testing.T, repo domain.Repository, sessionID, runtimeID string) string {
	t.Helper()

	session, err := domain.NewSession(domain.TypeSaolei, sessionID)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	session.SetOwnerRuntimeID(runtimeID)
	session.SetToken("seed-token")
	if err := session.MarkActive(); err != nil {
		t.Fatalf("MarkActive() error = %v", err)
	}
	if err := session.MarkDisconnected(); err != nil {
		t.Fatalf("MarkDisconnected() error = %v", err)
	}
	if err := repo.Save(context.Background(), session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	return sessionName(sessionID)
}

func TestHandler_GetSession(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name           string
		request        *GetSessionRequest
		wantCode       codes.Code
		wantNamePrefix string
		wantType       SessionType
		wantRuntimeID  string
		wantToken      string
	}{
		{
			name:           "given a created session, when GetSession called, returns proto Session with matching fields",
			request:        &GetSessionRequest{Name: sessionName("session-1")},
			wantCode:       codes.OK,
			wantNamePrefix: "sessions/",
			wantType:       SessionType_SESSION_TYPE_SAOLEI,
			wantRuntimeID:  "gw-0",
			wantToken:      "seed-token",
		},
		{
			name:     "given no session, when GetSession called, returns NotFound gRPC error",
			request:  &GetSessionRequest{Name: sessionName("nonexistent")},
			wantCode: codes.NotFound,
		},
		{
			name:     "given empty name, when GetSession called, returns InvalidArgument",
			request:  &GetSessionRequest{Name: ""},
			wantCode: codes.InvalidArgument,
		},
		{
			name:     "given name without sessions prefix, when GetSession called, returns InvalidArgument",
			request:  &GetSessionRequest{Name: "invalid-name"},
			wantCode: codes.InvalidArgument,
		},
		{
			name:     "given name with empty ID, when GetSession called, returns InvalidArgument",
			request:  &GetSessionRequest{Name: "sessions/"},
			wantCode: codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, repo := newTestHandler("gw-0", "gw-1")
			if tt.wantCode == codes.OK {
				seedSession(t, repo, "session-1", "gw-0")
			}

			got, err := handler.GetSession(ctx, tt.request)

			assertStatusCode(t, err, tt.wantCode)
			if tt.wantCode != codes.OK {
				return
			}

			if !strings.HasPrefix(got.GetName(), tt.wantNamePrefix) {
				t.Fatalf("GetSession() name = %q, want prefix %q", got.GetName(), tt.wantNamePrefix)
			}
			if got.GetType() != tt.wantType {
				t.Fatalf("GetSession() type = %v, want %v", got.GetType(), tt.wantType)
			}
			if got.GetOwnerRuntimeId() != tt.wantRuntimeID {
				t.Fatalf("GetSession() runtime_id = %q, want %q", got.GetOwnerRuntimeId(), tt.wantRuntimeID)
			}
			if tt.wantToken != "" && got.GetToken() != tt.wantToken {
				t.Fatalf("GetSession() token = %q, want %q", got.GetToken(), tt.wantToken)
			}
			if got.GetCreateTime() == nil {
				t.Fatal("GetSession() create_time is nil, want non-nil")
			}
			if got.GetUpdateTime() == nil {
				t.Fatal("GetSession() update_time is nil, want non-nil")
			}
		})
	}
}

func TestHandler_CreateSession(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name           string
		request        *CreateSessionRequest
		runtimeIDs     []string
		wantCode       codes.Code
		wantType       SessionType
		wantRuntimeID  string
		wantSessionID  string
		wantIDNonEmpty bool
		wantToken      string
	}{
		{
			name: "given valid type SAOLEI, when CreateSession called, returns session",
			request: &CreateSessionRequest{
				Type: SessionType_SESSION_TYPE_SAOLEI,
			},
			runtimeIDs:     []string{"gw-0"},
			wantCode:       codes.OK,
			wantType:       SessionType_SESSION_TYPE_SAOLEI,
			wantRuntimeID:  "gw-0",
			wantIDNonEmpty: true,
		},
		{
			name: "given valid type with session_id, when CreateSession called, returns session with provided ID",
			request: &CreateSessionRequest{
				Type:      SessionType_SESSION_TYPE_SAOLEI,
				SessionId: "my-custom-id",
			},
			runtimeIDs:    []string{"gw-1"},
			wantCode:      codes.OK,
			wantType:      SessionType_SESSION_TYPE_SAOLEI,
			wantRuntimeID: "gw-1",
			wantSessionID: "my-custom-id",
			wantToken:     "test-token-for-my-custom-id",
		},
		{
			name: "given UNSPECIFIED type, when CreateSession called, returns InvalidArgument",
			request: &CreateSessionRequest{
				Type: SessionType_SESSION_TYPE_UNSPECIFIED,
			},
			runtimeIDs: []string{"gw-0"},
			wantCode:   codes.InvalidArgument,
		},
		{
			name: "given no gateway available, when CreateSession called, returns Internal",
			request: &CreateSessionRequest{
				Type: SessionType_SESSION_TYPE_SAOLEI,
			},
			runtimeIDs: nil,
			wantCode:   codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gwIDs := tt.runtimeIDs
			if gwIDs == nil {
				gwIDs = []string{}
			}
			handler, _ := newTestHandler(gwIDs...)

			got, err := handler.CreateSession(ctx, tt.request)

			assertStatusCode(t, err, tt.wantCode)
			if tt.wantCode != codes.OK {
				return
			}

			session := got.GetSession()
			if session == nil {
				t.Fatal("CreateSession() session is nil")
			}
			if session.GetType() != tt.wantType {
				t.Fatalf("CreateSession() type = %v, want %v", session.GetType(), tt.wantType)
			}
			if session.GetOwnerRuntimeId() != tt.wantRuntimeID {
				t.Fatalf("CreateSession() runtime_id = %q, want %q", session.GetOwnerRuntimeId(), tt.wantRuntimeID)
			}
			if tt.wantSessionID != "" {
				wantName := sessionName(tt.wantSessionID)
				if session.GetName() != wantName {
					t.Fatalf("CreateSession() name = %q, want %q", session.GetName(), wantName)
				}
			}
			if tt.wantIDNonEmpty {
				if session.GetName() == "" {
					t.Fatal("CreateSession() name is empty, want auto-generated")
				}
				if !strings.HasPrefix(session.GetName(), "sessions/") {
					t.Fatalf("CreateSession() name = %q, want 'sessions/' prefix", session.GetName())
				}
			}
			if tt.wantToken != "" && session.GetToken() != tt.wantToken {
				t.Fatalf("CreateSession() token = %q, want %q", session.GetToken(), tt.wantToken)
			}
		})
	}
}

func TestHandler_DeleteSession(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name     string
		seedName string
		request  *DeleteSessionRequest
		wantCode codes.Code
	}{
		{
			name:     "given existing session, when DeleteSession called, returns Empty",
			seedName: sessionName("session-1"),
			request:  &DeleteSessionRequest{Name: sessionName("session-1")},
			wantCode: codes.OK,
		},
		{
			name:     "given non-existent session, when DeleteSession called, returns NotFound",
			request:  &DeleteSessionRequest{Name: sessionName("missing")},
			wantCode: codes.NotFound,
		},
		{
			name:     "given empty name, when DeleteSession called, returns InvalidArgument",
			request:  &DeleteSessionRequest{Name: ""},
			wantCode: codes.InvalidArgument,
		},
		{
			name:     "given name without sessions prefix, when DeleteSession called, returns InvalidArgument",
			request:  &DeleteSessionRequest{Name: "invalid-name"},
			wantCode: codes.InvalidArgument,
		},
		{
			name:     "given name with empty ID, when DeleteSession called, returns InvalidArgument",
			request:  &DeleteSessionRequest{Name: "sessions/"},
			wantCode: codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, repo := newTestHandler("gw-0")
			if tt.seedName != "" {
				seedSession(t, repo, "session-1", "gw-0")
			}

			got, err := handler.DeleteSession(ctx, tt.request)

			assertStatusCode(t, err, tt.wantCode)
			if tt.wantCode != codes.OK {
				return
			}
			if got == nil {
				t.Fatal("DeleteSession() response is nil, want empty proto")
			}
		})
	}
}

func TestHandler_ReconnectSession(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name           string
		seedSessionID  string
		seedRuntimeID  string
		seedDisconnect bool
		request        *ReconnectSessionRequest
		runtimeIDs     []string
		wantCode       codes.Code
		wantRuntimeID  string
	}{
		{
			name:           "given existing session, when ReconnectSession called, returns session with new gateway",
			seedSessionID:  "session-1",
			seedRuntimeID:  "gw-0",
			seedDisconnect: true,
			request:        &ReconnectSessionRequest{Name: sessionName("session-1")},
			runtimeIDs:     []string{"gw-0", "gw-1"},
			wantCode:       codes.OK,
			wantRuntimeID:  "gw-1",
		},
		{
			name:       "given non-existent session, when ReconnectSession called, returns NotFound",
			request:    &ReconnectSessionRequest{Name: sessionName("missing")},
			runtimeIDs: []string{"gw-0"},
			wantCode:   codes.NotFound,
		},
		{
			name:       "given empty name, when ReconnectSession called, returns InvalidArgument",
			request:    &ReconnectSessionRequest{Name: ""},
			runtimeIDs: []string{"gw-0"},
			wantCode:   codes.InvalidArgument,
		},
		{
			name:       "given name without sessions prefix, when ReconnectSession called, returns InvalidArgument",
			request:    &ReconnectSessionRequest{Name: "invalid-name"},
			runtimeIDs: []string{"gw-0"},
			wantCode:   codes.InvalidArgument,
		},
		{
			name:       "given name with empty ID, when ReconnectSession called, returns InvalidArgument",
			request:    &ReconnectSessionRequest{Name: "sessions/"},
			runtimeIDs: []string{"gw-0"},
			wantCode:   codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, repo := newTestHandler(tt.runtimeIDs...)
			if tt.seedSessionID != "" {
				if tt.seedDisconnect {
					seedDisconnectedSession(t, repo, tt.seedSessionID, tt.seedRuntimeID)
				} else {
					seedSession(t, repo, tt.seedSessionID, tt.seedRuntimeID)
				}
			}

			got, err := handler.ReconnectSession(ctx, tt.request)

			assertStatusCode(t, err, tt.wantCode)
			if tt.wantCode != codes.OK {
				return
			}

			session := got.GetSession()
			if session == nil {
				t.Fatal("ReconnectSession() session is nil")
			}
			if session.GetOwnerRuntimeId() != tt.wantRuntimeID {
				t.Fatalf("ReconnectSession() runtime_id = %q, want %q", session.GetOwnerRuntimeId(), tt.wantRuntimeID)
			}
			if session.GetToken() != "refresh-token-for-session-1" {
				t.Fatalf("ReconnectSession() token = %q, want %q", session.GetToken(), "refresh-token-for-session-1")
			}
			if session.GetStatus() != SessionStatus_SESSION_STATUS_ACTIVE {
				t.Fatalf("ReconnectSession() status = %v, want ACTIVE", session.GetStatus())
			}
		})
	}
}

func TestHandler_ListSessions(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		seedFunc  func(t *testing.T, repo domain.Repository)
		wantCode  codes.Code
		wantCount int
		wantNames []string
	}{
		{
			name: "given active sessions, when ListSessions called, returns sessions",
			seedFunc: func(t *testing.T, repo domain.Repository) {
				t.Helper()
				seedSession(t, repo, "session-1", "gw-0")
				seedSession(t, repo, "session-2", "gw-1")
			},
			wantCode:  codes.OK,
			wantCount: 2,
			wantNames: []string{sessionName("session-1"), sessionName("session-2")},
		},
		{
			name: "given ended session, when ListSessions called, excludes ended sessions",
			seedFunc: func(t *testing.T, repo domain.Repository) {
				t.Helper()
				seedSession(t, repo, "session-active", "gw-0")

				name := seedSession(t, repo, "session-ended", "gw-0")
				session, err := repo.Get(ctx, name)
				if err != nil {
					t.Fatalf("Get() error = %v", err)
				}
				if err := session.MarkEnded(); err != nil {
					t.Fatalf("MarkEnded() error = %v", err)
				}
				if err := repo.Save(ctx, session); err != nil {
					t.Fatalf("Save() error = %v", err)
				}
			},
			wantCode:  codes.OK,
			wantCount: 1,
			wantNames: []string{sessionName("session-active")},
		},
		{
			name:      "given no sessions, when ListSessions called, returns empty list",
			seedFunc:  nil,
			wantCode:  codes.OK,
			wantCount: 0,
			wantNames: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, repo := newTestHandler("gw-0", "gw-1")
			if tt.seedFunc != nil {
				tt.seedFunc(t, repo)
			}

			got, err := handler.ListSessions(ctx, &ListSessionsRequest{})

			assertStatusCode(t, err, tt.wantCode)
			if tt.wantCode != codes.OK {
				return
			}

			sessions := got.GetSessions()
			if len(sessions) != tt.wantCount {
				t.Fatalf("ListSessions() count = %d, want %d", len(sessions), tt.wantCount)
			}
			if tt.wantNames != nil {
				gotNames := make(map[string]bool, len(sessions))
				for _, s := range sessions {
					gotNames[s.GetName()] = true
				}
				for _, wantName := range tt.wantNames {
					if !gotNames[wantName] {
						t.Fatalf("ListSessions() missing session %q, got %v", wantName, sessionNames(sessions))
					}
				}
			}
		})
	}
}

// assertStatusCode checks that the error matches the expected gRPC status code.
func assertStatusCode(t *testing.T, err error, want codes.Code) {
	t.Helper()

	if want == codes.OK {
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		return
	}

	if err == nil {
		t.Fatalf("error = nil, want code %v", want)
	}
	if status.Code(err) != want {
		t.Fatalf("status.Code() = %v, want %v", status.Code(err), want)
	}
}
