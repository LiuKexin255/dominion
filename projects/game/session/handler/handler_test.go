package handler

import (
	"context"
	"errors"
	"testing"
	"time"

	"dominion/projects/game/session/domain"

	game "dominion/projects/game"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// mockSessionRepo implements domain.SessionRepository for handler testing.
type mockSessionRepo struct {
	createFn func(ctx context.Context, session *domain.Session) (*domain.Session, error)
	getFn    func(ctx context.Context, name string) (*domain.Session, error)
	deleteFn func(ctx context.Context, name string) error
}

func (m *mockSessionRepo) Create(ctx context.Context, session *domain.Session) (*domain.Session, error) {
	return m.createFn(ctx, session)
}

func (m *mockSessionRepo) Get(ctx context.Context, name string) (*domain.Session, error) {
	return m.getFn(ctx, name)
}

func (m *mockSessionRepo) Delete(ctx context.Context, name string) error {
	return m.deleteFn(ctx, name)
}

// mockProxyClient implements game.ProxyServiceClient for handler testing.
type mockProxyClient struct {
	deleteAgentFn func(ctx context.Context, req *game.DeleteAgentRequest, opts ...grpc.CallOption) (*emptypb.Empty, error)
}

func (m *mockProxyClient) DeleteAgent(ctx context.Context, req *game.DeleteAgentRequest, _ ...grpc.CallOption) (*emptypb.Empty, error) {
	return m.deleteAgentFn(ctx, req)
}

func (m *mockProxyClient) CreateAgent(_ context.Context, _ *game.CreateAgentRequest, _ ...grpc.CallOption) (*game.Agent, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}

func (m *mockProxyClient) GetAgent(_ context.Context, _ *game.GetAgentRequest, _ ...grpc.CallOption) (*game.Agent, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}

func (m *mockProxyClient) ConnectAgent(_ context.Context, _ ...grpc.CallOption) (game.ProxyService_ConnectAgentClient, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}

// noopProxyClient returns a proxy client whose DeleteAgent always succeeds.
func noopProxyClient() *mockProxyClient {
	return &mockProxyClient{
		deleteAgentFn: func(_ context.Context, _ *game.DeleteAgentRequest, _ ...grpc.CallOption) (*emptypb.Empty, error) {
			return new(emptypb.Empty), nil
		},
	}
}

func TestCreateSession(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name     string
		req      *game.CreateSessionRequest
		mock     *mockSessionRepo
		wantName string
		wantCode codes.Code
	}{
		{
			name: "success - returns proto with correct name",
			req:  &game.CreateSessionRequest{SessionId: "abc123"},
			mock: &mockSessionRepo{
				createFn: func(_ context.Context, s *domain.Session) (*domain.Session, error) {
					return &domain.Session{
						Name:       s.Name,
						SessionID:  s.SessionID,
						CreateTime: time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
					}, nil
				},
			},
			wantName: "sessions/abc123",
			wantCode: codes.OK,
		},
		{
			name: "already exists - returns AlreadyExists status",
			req:  &game.CreateSessionRequest{SessionId: "abc123"},
			mock: &mockSessionRepo{
				createFn: func(_ context.Context, _ *domain.Session) (*domain.Session, error) {
					return nil, domain.ErrAlreadyExists
				},
			},
			wantCode: codes.AlreadyExists,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			handler := NewSessionHandler(tt.mock, noopProxyClient())

			// when
			got, err := handler.CreateSession(ctx, tt.req)

			// then
			assertStatusCode(t, err, tt.wantCode)
			if tt.wantCode != codes.OK {
				return
			}
			if got.GetName() != tt.wantName {
				t.Fatalf("CreateSession() name = %q, want %q", got.GetName(), tt.wantName)
			}
			if got.GetSessionId() != tt.req.GetSessionId() {
				t.Fatalf("CreateSession() session_id = %q, want %q", got.GetSessionId(), tt.req.GetSessionId())
			}
		})
	}
}

func TestGetSession(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name     string
		req      *game.GetSessionRequest
		mock     *mockSessionRepo
		wantName string
		wantCode codes.Code
	}{
		{
			name: "success - returns proto session",
			req:  &game.GetSessionRequest{Name: "sessions/abc123"},
			mock: &mockSessionRepo{
				getFn: func(_ context.Context, name string) (*domain.Session, error) {
					return &domain.Session{
						Name:       name,
						SessionID:  "abc123",
						CreateTime: time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
					}, nil
				},
			},
			wantName: "sessions/abc123",
			wantCode: codes.OK,
		},
		{
			name: "not found - returns NotFound status",
			req:  &game.GetSessionRequest{Name: "sessions/missing"},
			mock: &mockSessionRepo{
				getFn: func(_ context.Context, _ string) (*domain.Session, error) {
					return nil, domain.ErrNotFound
				},
			},
			wantCode: codes.NotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			handler := NewSessionHandler(tt.mock, noopProxyClient())

			// when
			got, err := handler.GetSession(ctx, tt.req)

			// then
			assertStatusCode(t, err, tt.wantCode)
			if tt.wantCode != codes.OK {
				return
			}
			if got.GetName() != tt.wantName {
				t.Fatalf("GetSession() name = %q, want %q", got.GetName(), tt.wantName)
			}
		})
	}
}

func TestDeleteSession(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		req         *game.DeleteSessionRequest
		mockRepo    *mockSessionRepo
		mockProxy   *mockProxyClient
		wantCode    codes.Code
		wantDeleted bool
	}{
		{
			name: "success - proxy deletes agent then session deleted",
			req:  &game.DeleteSessionRequest{Name: "sessions/abc123"},
			mockRepo: &mockSessionRepo{
				deleteFn: func(_ context.Context, name string) error {
					if name != "sessions/abc123" {
						t.Fatalf("repo.Delete() name = %q, want %q", name, "sessions/abc123")
					}
					return nil
				},
			},
			mockProxy: &mockProxyClient{
				deleteAgentFn: func(_ context.Context, req *game.DeleteAgentRequest, _ ...grpc.CallOption) (*emptypb.Empty, error) {
					wantName := "sessions/abc123/agent"
					if req.GetName() != wantName {
						t.Fatalf("DeleteAgent() name = %q, want %q", req.GetName(), wantName)
					}
					return new(emptypb.Empty), nil
				},
			},
			wantCode:    codes.OK,
			wantDeleted: true,
		},
		{
			name: "proxy NotFound - session still deleted (idempotent)",
			req:  &game.DeleteSessionRequest{Name: "sessions/abc123"},
			mockRepo: &mockSessionRepo{
				deleteFn: func(_ context.Context, _ string) error {
					return nil
				},
			},
			mockProxy: &mockProxyClient{
				deleteAgentFn: func(_ context.Context, _ *game.DeleteAgentRequest, _ ...grpc.CallOption) (*emptypb.Empty, error) {
					return nil, status.Error(codes.NotFound, "agent not found")
				},
			},
			wantCode:    codes.OK,
			wantDeleted: true,
		},
		{
			name: "proxy error - session NOT deleted",
			req:  &game.DeleteSessionRequest{Name: "sessions/abc123"},
			mockRepo: &mockSessionRepo{
				deleteFn: func(_ context.Context, _ string) error {
					t.Fatal("repo.Delete() should not be called when proxy fails")
					return nil
				},
			},
			mockProxy: &mockProxyClient{
				deleteAgentFn: func(_ context.Context, _ *game.DeleteAgentRequest, _ ...grpc.CallOption) (*emptypb.Empty, error) {
					return nil, status.Error(codes.Unavailable, "proxy unavailable")
				},
			},
			wantCode:    codes.Internal,
			wantDeleted: false,
		},
		{
			name: "repo NotFound - returns NotFound after proxy succeeds",
			req:  &game.DeleteSessionRequest{Name: "sessions/missing"},
			mockRepo: &mockSessionRepo{
				deleteFn: func(_ context.Context, _ string) error {
					return domain.ErrNotFound
				},
			},
			mockProxy: &mockProxyClient{
				deleteAgentFn: func(_ context.Context, _ *game.DeleteAgentRequest, _ ...grpc.CallOption) (*emptypb.Empty, error) {
					return new(emptypb.Empty), nil
				},
			},
			wantCode:    codes.NotFound,
			wantDeleted: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			handler := NewSessionHandler(tt.mockRepo, tt.mockProxy)

			// when
			got, err := handler.DeleteSession(ctx, tt.req)

			// then
			assertStatusCode(t, err, tt.wantCode)
			if tt.wantCode != codes.OK {
				return
			}
			if got == nil {
				t.Fatalf("DeleteSession() got nil, want non-nil empty response")
			}
		})
	}
}

func Test_toStatusError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode codes.Code
	}{
		{
			name:     "ErrNotFound maps to NotFound",
			err:      domain.ErrNotFound,
			wantCode: codes.NotFound,
		},
		{
			name:     "ErrAlreadyExists maps to AlreadyExists",
			err:      domain.ErrAlreadyExists,
			wantCode: codes.AlreadyExists,
		},
		{
			name:     "unknown error maps to Internal",
			err:      errors.New("something broke"),
			wantCode: codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got := toStatusError(tt.err)

			// then
			s, ok := status.FromError(got)
			if !ok {
				t.Fatalf("toStatusError() did not return a status error, got %v", got)
			}
			if s.Code() != tt.wantCode {
				t.Fatalf("toStatusError() code = %v, want %v", s.Code(), tt.wantCode)
			}
		})
	}
}

func Test_sessionToProto(t *testing.T) {
	tests := []struct {
		name     string
		session  *domain.Session
		wantNil  bool
		wantName string
		wantID   string
	}{
		{
			name:    "nil session returns nil",
			session: nil,
			wantNil: true,
		},
		{
			name: "session with fields",
			session: &domain.Session{
				Name:       "sessions/test",
				SessionID:  "test",
				CreateTime: time.Date(2025, 3, 20, 8, 0, 0, 0, time.UTC),
			},
			wantNil:  false,
			wantName: "sessions/test",
			wantID:   "test",
		},
		{
			name: "session with zero create time has no create_time",
			session: &domain.Session{
				Name:      "sessions/notime",
				SessionID: "notime",
			},
			wantNil:  false,
			wantName: "sessions/notime",
			wantID:   "notime",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got := sessionToProto(tt.session)

			// then
			if tt.wantNil {
				if got != nil {
					t.Fatalf("sessionToProto() = %v, want nil", got)
				}
				return
			}
			if got.GetName() != tt.wantName {
				t.Fatalf("sessionToProto() name = %q, want %q", got.GetName(), tt.wantName)
			}
			if got.GetSessionId() != tt.wantID {
				t.Fatalf("sessionToProto() session_id = %q, want %q", got.GetSessionId(), tt.wantID)
			}
		})
	}
}

// assertStatusCode checks that the gRPC status code of err matches want.
func assertStatusCode(t *testing.T, err error, want codes.Code) {
	t.Helper()
	if want == codes.OK {
		if err != nil {
			t.Fatalf("expected OK, got error: %v", err)
		}
		return
	}
	if err == nil {
		t.Fatalf("expected code %v, got nil error", want)
	}
	got := status.Code(err)
	if got != want {
		t.Fatalf("status code = %v, want %v (error: %v)", got, want, err)
	}
}
