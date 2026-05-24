package handler

import (
	"context"
	"errors"
	"testing"
	"time"

	"dominion/projects/game/session/domain"

	game "dominion/projects/game"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
			handler := NewSessionHandler(tt.mock)

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
			handler := NewSessionHandler(tt.mock)

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
		name     string
		req      *game.DeleteSessionRequest
		mock     *mockSessionRepo
		wantCode codes.Code
	}{
		{
			name: "success - returns empty",
			req:  &game.DeleteSessionRequest{Name: "sessions/abc123"},
			mock: &mockSessionRepo{
				deleteFn: func(_ context.Context, _ string) error {
					return nil
				},
			},
			wantCode: codes.OK,
		},
		{
			name: "not found - returns NotFound status",
			req:  &game.DeleteSessionRequest{Name: "sessions/missing"},
			mock: &mockSessionRepo{
				deleteFn: func(_ context.Context, _ string) error {
					return domain.ErrNotFound
				},
			},
			wantCode: codes.NotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			handler := NewSessionHandler(tt.mock)

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
		name    string
		session *domain.Session
		wantNil bool
		wantName string
		wantID   string
	}{
		{
			name: "nil session returns nil",
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
