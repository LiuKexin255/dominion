package handler

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	game "dominion/projects/game"
	"dominion/projects/game/session/domain"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// mockIDGenerator implements domain.IDGenerator for handler testing.
type mockIDGenerator struct {
	newIDFn func(ctx context.Context) (string, error)
}

func (m *mockIDGenerator) NewID(ctx context.Context) (string, error) {
	return m.newIDFn(ctx)
}

// mockSessionRepo implements domain.SessionRepository for handler testing.
type mockSessionRepo struct {
	createFn func(ctx context.Context, session *domain.Session) (*domain.Session, error)
	getFn    func(ctx context.Context, sessionID string) (*domain.Session, error)
	deleteFn func(ctx context.Context, sessionID string) error
	listFn   func(ctx context.Context, pageSize int, cursor *domain.ListPageCursor) (*domain.ListSessionsResult, error)
}

func (m *mockSessionRepo) Create(ctx context.Context, session *domain.Session) (*domain.Session, error) {
	return m.createFn(ctx, session)
}

func (m *mockSessionRepo) Get(ctx context.Context, sessionID string) (*domain.Session, error) {
	return m.getFn(ctx, sessionID)
}

func (m *mockSessionRepo) Delete(ctx context.Context, sessionID string) error {
	return m.deleteFn(ctx, sessionID)
}

func (m *mockSessionRepo) List(ctx context.Context, pageSize int, cursor *domain.ListPageCursor) (*domain.ListSessionsResult, error) {
	return m.listFn(ctx, pageSize, cursor)
}

// fixedIDGenerator returns an ID generator that always returns the given id.
func fixedIDGenerator(id string) *mockIDGenerator {
	return &mockIDGenerator{
		newIDFn: func(_ context.Context) (string, error) {
			return id, nil
		},
	}
}

func TestCreateSession(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name     string
		req      *game.CreateSessionRequest
		idGen    *mockIDGenerator
		mock     *mockSessionRepo
		wantName string
		wantCode codes.Code
	}{
		{
			name:  "success - handler generates ID and returns proto with template-scoped name",
			req:   &game.CreateSessionRequest{Parent: "templates/saolei"},
			idGen: fixedIDGenerator("test-id-123"),
			mock: &mockSessionRepo{
				createFn: func(_ context.Context, s *domain.Session) (*domain.Session, error) {
					if s.Template != "saolei" {
						t.Fatalf("repo.Create() template = %q, want %q", s.Template, "saolei")
					}
					return &domain.Session{
						Template:   s.Template,
						SessionID:  s.SessionID,
						CreateTime: time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
					}, nil
				},
			},
			wantName: "templates/saolei/sessions/test-id-123",
			wantCode: codes.OK,
		},
		{
			name:  "already exists - returns AlreadyExists status",
			req:   &game.CreateSessionRequest{Parent: "templates/saolei"},
			idGen: fixedIDGenerator("test-id-123"),
			mock: &mockSessionRepo{
				createFn: func(_ context.Context, _ *domain.Session) (*domain.Session, error) {
					return nil, domain.ErrAlreadyExists
				},
			},
			wantCode: codes.AlreadyExists,
		},
		{
			name: "id generation fails - returns Internal status",
			req:  &game.CreateSessionRequest{Parent: "templates/saolei"},
			idGen: &mockIDGenerator{
				newIDFn: func(_ context.Context) (string, error) {
					return "", errors.New("crypto failure")
				},
			},
			mock:     &mockSessionRepo{},
			wantCode: codes.Internal,
		},
		{
			name:     "invalid parent - no templates prefix returns InvalidArgument",
			req:      &game.CreateSessionRequest{Parent: "sessions"},
			idGen:    fixedIDGenerator("test-id-123"),
			mock:     &mockSessionRepo{},
			wantCode: codes.InvalidArgument,
		},
		{
			name:     "invalid parent - unknown template returns InvalidArgument",
			req:      &game.CreateSessionRequest{Parent: "templates/unknown-template"},
			idGen:    fixedIDGenerator("test-id-123"),
			mock:     &mockSessionRepo{},
			wantCode: codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			handler := NewSessionHandler(tt.mock, tt.idGen)

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
		})
	}
}

func TestListSessions(t *testing.T) {
	ctx := context.Background()

	t.Run("success - returns all sessions", func(t *testing.T) {
		// given
		sessionA := &domain.Session{
			Template:   "saolei",
			SessionID:  "aaa",
			CreateTime: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		}
		sessionB := &domain.Session{
			Template:   "saolei",
			SessionID:  "bbb",
			CreateTime: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		}
		mockRepo := &mockSessionRepo{
			listFn: func(_ context.Context, pageSize int, cursor *domain.ListPageCursor) (*domain.ListSessionsResult, error) {
				return &domain.ListSessionsResult{
					Sessions:      []*domain.Session{sessionA, sessionB},
					NextPageToken: "",
				}, nil
			},
		}
		handler := NewSessionHandler(mockRepo, fixedIDGenerator("unused"))

		// when
		got, err := handler.ListSessions(ctx, &game.ListSessionsRequest{Parent: "templates/saolei", PageSize: 10})

		// then
		assertStatusCode(t, err, codes.OK)
		if len(got.GetSessions()) != 2 {
			t.Fatalf("ListSessions() returned %d sessions, want 2", len(got.GetSessions()))
		}
		if got.GetSessions()[0].GetName() != "templates/saolei/sessions/aaa" {
			t.Fatalf("ListSessions()[0] name = %q, want %q", got.GetSessions()[0].GetName(), "templates/saolei/sessions/aaa")
		}
		if got.GetSessions()[1].GetName() != "templates/saolei/sessions/bbb" {
			t.Fatalf("ListSessions()[1] name = %q, want %q", got.GetSessions()[1].GetName(), "templates/saolei/sessions/bbb")
		}
		if got.GetNextPageToken() != "" {
			t.Fatalf("ListSessions() next_page_token = %q, want empty", got.GetNextPageToken())
		}
	})

	t.Run("invalid parent returns InvalidArgument", func(t *testing.T) {
		// given
		handler := NewSessionHandler(&mockSessionRepo{}, fixedIDGenerator("unused"))

		// when
		_, err := handler.ListSessions(ctx, &game.ListSessionsRequest{Parent: "sessions"})

		// then
		assertStatusCode(t, err, codes.InvalidArgument)
	})
}

func TestListSessions_Pagination(t *testing.T) {
	ctx := context.Background()

	t.Run("success - paginates with page_size and page_token", func(t *testing.T) {
		// given
		session1 := &domain.Session{Template: "saolei", SessionID: "s1", CreateTime: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)}
		session2 := &domain.Session{Template: "saolei", SessionID: "s2", CreateTime: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)}
		session3 := &domain.Session{Template: "saolei", SessionID: "s3", CreateTime: time.Date(2025, 1, 3, 0, 0, 0, 0, time.UTC)}

		nextPageCursor := &domain.ListPageCursor{
			SessionID:  "s2",
			CreateTime: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		}
		nextPageToken, err := domain.EncodePageToken(nextPageCursor)
		if err != nil {
			t.Fatalf("EncodePageToken() error: %v", err)
		}

		mockRepo := &mockSessionRepo{
			listFn: func(_ context.Context, pageSize int, cursor *domain.ListPageCursor) (*domain.ListSessionsResult, error) {
				if cursor == nil {
					return &domain.ListSessionsResult{
						Sessions:      []*domain.Session{session1, session2},
						NextPageToken: nextPageToken,
					}, nil
				}
				return &domain.ListSessionsResult{
					Sessions:      []*domain.Session{session3},
					NextPageToken: "",
				}, nil
			},
		}
		handler := NewSessionHandler(mockRepo, fixedIDGenerator("unused"))

		// when - first page
		page1, err := handler.ListSessions(ctx, &game.ListSessionsRequest{Parent: "templates/saolei", PageSize: 2})

		// then
		assertStatusCode(t, err, codes.OK)
		if len(page1.GetSessions()) != 2 {
			t.Fatalf("page 1: got %d sessions, want 2", len(page1.GetSessions()))
		}
		if page1.GetNextPageToken() != nextPageToken {
			t.Fatalf("page 1: next_page_token = %q, want %q", page1.GetNextPageToken(), nextPageToken)
		}

		// when - second page
		page2, err := handler.ListSessions(ctx, &game.ListSessionsRequest{
			Parent:    "templates/saolei",
			PageSize:  2,
			PageToken: page1.GetNextPageToken(),
		})

		// then
		assertStatusCode(t, err, codes.OK)
		if len(page2.GetSessions()) != 1 {
			t.Fatalf("page 2: got %d sessions, want 1", len(page2.GetSessions()))
		}
		if page2.GetSessions()[0].GetName() != "templates/saolei/sessions/s3" {
			t.Fatalf("page 2: name = %q, want %q", page2.GetSessions()[0].GetName(), "templates/saolei/sessions/s3")
		}
		if page2.GetNextPageToken() != "" {
			t.Fatalf("page 2: next_page_token = %q, want empty", page2.GetNextPageToken())
		}
	})
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
			req:  &game.GetSessionRequest{Name: "templates/saolei/sessions/abc123"},
			mock: &mockSessionRepo{
				getFn: func(_ context.Context, sessionID string) (*domain.Session, error) {
					return &domain.Session{
						Template:   "saolei",
						SessionID:  sessionID,
						CreateTime: time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
					}, nil
				},
			},
			wantName: "templates/saolei/sessions/abc123",
			wantCode: codes.OK,
		},
		{
			name: "not found - returns NotFound status",
			req:  &game.GetSessionRequest{Name: "templates/saolei/sessions/missing"},
			mock: &mockSessionRepo{
				getFn: func(_ context.Context, _ string) (*domain.Session, error) {
					return nil, domain.ErrNotFound
				},
			},
			wantCode: codes.NotFound,
		},
		{
			name:     "invalid name - no templates prefix returns InvalidArgument",
			req:      &game.GetSessionRequest{Name: "sessions/abc123"},
			mock:     &mockSessionRepo{},
			wantCode: codes.InvalidArgument,
		},
		{
			name:     "invalid name - unknown template returns InvalidArgument",
			req:      &game.GetSessionRequest{Name: "templates/nope/sessions/abc123"},
			mock:     &mockSessionRepo{},
			wantCode: codes.InvalidArgument,
		},
		{
			name:     "invalid name - empty ID returns InvalidArgument",
			req:      &game.GetSessionRequest{Name: "templates/saolei/sessions/"},
			mock:     &mockSessionRepo{},
			wantCode: codes.InvalidArgument,
		},
		{
			name:     "invalid name - extra path separator returns InvalidArgument",
			req:      &game.GetSessionRequest{Name: "templates/saolei/sessions/a/b"},
			mock:     &mockSessionRepo{},
			wantCode: codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			handler := NewSessionHandler(tt.mock, fixedIDGenerator("unused"))

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
		mockRepo *mockSessionRepo
		wantCode codes.Code
	}{
		{
			name: "success - session deleted",
			req:  &game.DeleteSessionRequest{Name: "templates/saolei/sessions/abc123"},
			mockRepo: &mockSessionRepo{
				deleteFn: func(_ context.Context, sessionID string) error {
					if sessionID != "abc123" {
						t.Fatalf("repo.Delete() sessionID = %q, want %q", sessionID, "abc123")
					}
					return nil
				},
			},
			wantCode: codes.OK,
		},
		{
			name: "repo NotFound - returns NotFound",
			req:  &game.DeleteSessionRequest{Name: "templates/saolei/sessions/missing"},
			mockRepo: &mockSessionRepo{
				deleteFn: func(_ context.Context, _ string) error {
					return domain.ErrNotFound
				},
			},
			wantCode: codes.NotFound,
		},
		{
			name:     "invalid name - no templates prefix returns InvalidArgument",
			req:      &game.DeleteSessionRequest{Name: "sessions/abc123"},
			mockRepo: &mockSessionRepo{},
			wantCode: codes.InvalidArgument,
		},
		{
			name:     "invalid name - unknown template returns InvalidArgument",
			req:      &game.DeleteSessionRequest{Name: "templates/nope/sessions/abc123"},
			mockRepo: &mockSessionRepo{},
			wantCode: codes.InvalidArgument,
		},
		{
			name:     "invalid name - empty ID returns InvalidArgument",
			req:      &game.DeleteSessionRequest{Name: "templates/saolei/sessions/"},
			mockRepo: &mockSessionRepo{},
			wantCode: codes.InvalidArgument,
		},
		{
			name:     "invalid name - extra path separator returns InvalidArgument",
			req:      &game.DeleteSessionRequest{Name: "templates/saolei/sessions/a/b"},
			mockRepo: &mockSessionRepo{},
			wantCode: codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			handler := NewSessionHandler(tt.mockRepo, fixedIDGenerator("unused"))

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
	}{
		{
			name:    "nil session returns nil",
			session: nil,
			wantNil: true,
		},
		{
			name: "session with fields",
			session: &domain.Session{
				Template:   "saolei",
				SessionID:  "test",
				CreateTime: time.Date(2025, 3, 20, 8, 0, 0, 0, time.UTC),
			},
			wantNil:  false,
			wantName: "templates/saolei/sessions/test",
		},
		{
			name: "session with zero create time has no create_time",
			session: &domain.Session{
				Template:  "saolei",
				SessionID: "notime",
			},
			wantNil:  false,
			wantName: "templates/saolei/sessions/notime",
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
		})
	}
}

// TestListSessions_DefaultPageSize verifies that page_size <= 0 is treated as 0 and
// the handler defaults to domain.DefaultListSessionsPageSize.
func TestListSessions_DefaultPageSize(t *testing.T) {
	ctx := context.Background()

	// given
	var capturedPageSize int
	mockRepo := &mockSessionRepo{
		listFn: func(_ context.Context, pageSize int, _ *domain.ListPageCursor) (*domain.ListSessionsResult, error) {
			capturedPageSize = pageSize
			return &domain.ListSessionsResult{}, nil
		},
	}
	handler := NewSessionHandler(mockRepo, fixedIDGenerator("unused"))

	// when
	_, err := handler.ListSessions(ctx, &game.ListSessionsRequest{Parent: "templates/saolei", PageSize: 0})

	// then
	assertStatusCode(t, err, codes.OK)
	if capturedPageSize != domain.DefaultListSessionsPageSize {
		t.Fatalf("pageSize = %d, want %d", capturedPageSize, domain.DefaultListSessionsPageSize)
	}
}

// TestListSessions_MaxPageSizeExceeded verifies that page_size > MaxListSessionsPageSize returns InvalidArgument.
func TestListSessions_MaxPageSizeExceeded(t *testing.T) {
	ctx := context.Background()

	// given
	handler := NewSessionHandler(&mockSessionRepo{}, fixedIDGenerator("unused"))

	// when
	_, err := handler.ListSessions(ctx, &game.ListSessionsRequest{Parent: "templates/saolei", PageSize: 2000})

	// then
	assertStatusCode(t, err, codes.InvalidArgument)
}

// TestListSessions_NextPageToken verifies that next_page_token is present in the response
// when the repository returns one.
func TestListSessions_NextPageToken(t *testing.T) {
	ctx := context.Background()

	// given
	mockRepo := &mockSessionRepo{
		listFn: func(_ context.Context, _ int, _ *domain.ListPageCursor) (*domain.ListSessionsResult, error) {
			return &domain.ListSessionsResult{
				NextPageToken: "token-for-next-page",
			}, nil
		},
	}
	handler := NewSessionHandler(mockRepo, fixedIDGenerator("unused"))

	// when
	got, err := handler.ListSessions(ctx, &game.ListSessionsRequest{Parent: "templates/saolei", PageSize: 10})

	// then
	assertStatusCode(t, err, codes.OK)
	if got.GetNextPageToken() != "token-for-next-page" {
		t.Fatalf("next_page_token = %q, want %q", got.GetNextPageToken(), "token-for-next-page")
	}
}

// TestListSessions_EmptyResult verifies that a nil result from the repository returns
// an empty, non-nil proto response.
func TestListSessions_EmptyResult(t *testing.T) {
	ctx := context.Background()

	// given
	mockRepo := &mockSessionRepo{
		listFn: func(_ context.Context, _ int, _ *domain.ListPageCursor) (*domain.ListSessionsResult, error) {
			return nil, nil
		},
	}
	handler := NewSessionHandler(mockRepo, fixedIDGenerator("unused"))

	// when
	got, err := handler.ListSessions(ctx, &game.ListSessionsRequest{Parent: "templates/saolei", PageSize: 10})

	// then
	assertStatusCode(t, err, codes.OK)
	if got == nil {
		t.Fatal("ListSessions() returned nil, want non-nil response")
	}
	if len(got.GetSessions()) != 0 {
		t.Fatalf("ListSessions() returned %d sessions, want 0", len(got.GetSessions()))
	}
}

// TestListSessions_InvalidToken verifies that invalid page tokens return InvalidArgument.
func TestListSessions_InvalidToken(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		pageToken string
		wantCode  codes.Code
	}{
		{
			name:      "empty token - success",
			pageToken: "",
			wantCode:  codes.OK,
		},
		{
			name:      "invalid base64 token",
			pageToken: "!!!not-valid!!!",
			wantCode:  codes.InvalidArgument,
		},
		{
			name:      "valid base64 but invalid JSON",
			pageToken: base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte("not-json")),
			wantCode:  codes.InvalidArgument,
		},
		{
			name:      "missing create_time",
			pageToken: base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte(`{"session_id":"abc"}`)),
			wantCode:  codes.InvalidArgument,
		},
		{
			name:      "missing session_id",
			pageToken: base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte(`{"create_time":"2026-05-29T12:34:56Z"}`)),
			wantCode:  codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			mockRepo := &mockSessionRepo{
				listFn: func(_ context.Context, _ int, _ *domain.ListPageCursor) (*domain.ListSessionsResult, error) {
					return &domain.ListSessionsResult{}, nil
				},
			}
			handler := NewSessionHandler(mockRepo, fixedIDGenerator("unused"))

			// when
			_, err := handler.ListSessions(ctx, &game.ListSessionsRequest{Parent: "templates/saolei", PageToken: tt.pageToken})

			// then
			assertStatusCode(t, err, tt.wantCode)
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
