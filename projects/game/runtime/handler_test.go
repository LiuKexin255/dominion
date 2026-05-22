package runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	"dominion/projects/game/runtime/domain"
	"dominion/projects/game/pkg/token"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type stubRuntimeService struct {
	snapshot    *domain.SnapshotRef
	snapshotErr error
	runtime     *domain.SessionRuntime
	runtimeErr  error

	createGameRuntimeFn  func(ctx context.Context, sessionID string, reconnectGeneration int64) (*domain.SessionRuntime, string, error)
	refreshGameRuntimeFn func(ctx context.Context, sessionID string, oldToken string) (*domain.SessionRuntime, string, error)
}

type stubHandlerVerifier struct {
	claims *token.Claims
	err    error
}

func (v *stubHandlerVerifier) Verify(_ string) (*token.Claims, error) {
	return v.claims, v.err
}

func (v *stubHandlerVerifier) VerifyWithGrace(_ string, _ time.Duration) (*token.Claims, error) {
	return v.claims, v.err
}

func newTestHandler(svc *stubRuntimeService) *Handler {
	return NewHandler(svc, &stubHandlerVerifier{})
}

func newTestRuntimeHandler(svc *stubRuntimeService) *GameRuntimeHandler {
	return NewRuntimeHandler(svc)
}

func (s *stubRuntimeService) GetSnapshot(_ context.Context, _ string) (*domain.SnapshotRef, error) {
	return s.snapshot, s.snapshotErr
}

func (s *stubRuntimeService) GetRuntime(_ context.Context, _ string) (*domain.SessionRuntime, error) {
	return s.runtime, s.runtimeErr
}

func (s *stubRuntimeService) ConnectSession(_ context.Context, _, _ string) (*domain.SessionRuntime, *token.Claims, error) {
	return nil, nil, nil
}

func (s *stubRuntimeService) ProcessHello(_ *domain.SessionRuntime, _ *token.Claims, _ domain.ClientRole, _ string) ([]*domain.RoutedMessage, error) {
	return nil, nil
}

func (s *stubRuntimeService) HandleAgentMessage(_ context.Context, _ string, _ *domain.Message) ([]*domain.RoutedMessage, error) {
	return nil, nil
}

func (s *stubRuntimeService) HandleWebMessage(_ context.Context, _, _ string, _ *domain.Message) ([]*domain.RoutedMessage, error) {
	return nil, nil
}

func (s *stubRuntimeService) DisconnectAgent(_ string) {}

func (s *stubRuntimeService) DisconnectWeb(_, _ string) {}

func (s *stubRuntimeService) TouchSession(_ string) error { return nil }

func (s *stubRuntimeService) AsyncMessages() <-chan *domain.RoutedMessage {
	return nil
}

// dynamicVerifier is a token.Verifier stub that derives the returned claims
// from the token string. Tokens must follow the format "session:{sessionID}".
type dynamicVerifier struct{}

func (d *dynamicVerifier) Verify(tokenStr string) (*token.Claims, error) {
	sessionID := strings.TrimPrefix(tokenStr, "session:")
	return &token.Claims{
		SessionID:      sessionID,
		OwnerRuntimeID: "gateway-test",
		OwnerEpoch:     1,
		Audience:       token.TokenAudienceInternal,
		IssuedAt:       time.Now().Unix(),
		ExpiresAt:      time.Now().Add(time.Hour).Unix(),
	}, nil
}

func (d *dynamicVerifier) VerifyWithGrace(tokenStr string, _ time.Duration) (*token.Claims, error) {
	return d.Verify(tokenStr)
}

// errVerifier is a token.Verifier stub that always returns the configured
// error.
type errVerifier struct {
	err error
}

func (e *errVerifier) Verify(_ string) (*token.Claims, error) {
	return nil, e.err
}

func (e *errVerifier) VerifyWithGrace(_ string, _ time.Duration) (*token.Claims, error) {
	return nil, e.err
}

// authCtxFromName extracts the session ID from a resource name and returns a
// context containing a valid token in gRPC metadata.
func authCtxFromName(name string) context.Context {
	sessionID := sessionIDFromName(name)
	if sessionID == "" {
		return context.Background()
	}
	return metadata.NewIncomingContext(context.Background(), metadata.Pairs("token", "session:"+sessionID))
}

// sessionIDFromName extracts the session ID from a resource name of the form
// "sessions/{id}/...".
func sessionIDFromName(name string) string {
	parts := strings.SplitN(strings.TrimPrefix(name, "sessions/"), "/", 2)
	if len(parts) < 1 || parts[0] == "" {
		return ""
	}
	return parts[0]
}

func (s *stubRuntimeService) CreateGameRuntime(ctx context.Context, sessionID string, reconnectGeneration int64) (*domain.SessionRuntime, string, error) {
	if s.createGameRuntimeFn != nil {
		return s.createGameRuntimeFn(ctx, sessionID, reconnectGeneration)
	}
	return nil, "", nil
}

func (s *stubRuntimeService) RefreshGameRuntime(ctx context.Context, sessionID string, oldToken string) (*domain.SessionRuntime, string, error) {
	if s.refreshGameRuntimeFn != nil {
		return s.refreshGameRuntimeFn(ctx, sessionID, oldToken)
	}
	return nil, "", nil
}

func TestHandler_GetGameSnapshot(t *testing.T) {
	captureTime := time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		svc      *stubRuntimeService
		request  *GetGameSnapshotRequest
		wantCode codes.Code
		check    func(t *testing.T, got *GameSnapshot)
	}{
		{
			name: "given valid name with snapshot, when GetGameSnapshot called, returns snapshot with all fields",
			svc: &stubRuntimeService{
				snapshot: &domain.SnapshotRef{
					Data:        []byte("jpeg-image-data"),
					MimeType:    "image/jpeg",
					CaptureTime: captureTime,
					Cached:      true,
				},
			},
			request:  &GetGameSnapshotRequest{Name: "sessions/session-1/game/snapshot"},
			wantCode: codes.OK,
			check: func(t *testing.T, got *GameSnapshot) {
				wantName := "sessions/session-1/game/snapshot"
				if got.GetName() != wantName {
					t.Fatalf("Name = %q, want %q", got.GetName(), wantName)
				}
				wantSession := "sessions/session-1"
				if got.GetSession() != wantSession {
					t.Fatalf("Session = %q, want %q", got.GetSession(), wantSession)
				}
				if got.GetSnapshotId() == "" {
					t.Fatalf("SnapshotId is empty, want non-empty")
				}
				if got.GetMimeType() != "image/jpeg" {
					t.Fatalf("MimeType = %q, want %q", got.GetMimeType(), "image/jpeg")
				}
				if string(got.GetImage()) != "jpeg-image-data" {
					t.Fatalf("Image = %q, want %q", string(got.GetImage()), "jpeg-image-data")
				}
				if !got.GetCached() {
					t.Fatal("Cached = false, want true")
				}
				if got.GetCaptureTime() == nil {
					t.Fatal("CaptureTime is nil, want non-nil")
				}
			},
		},
		{
			name:     "given nonexistent session, when GetGameSnapshot called, returns NOT_FOUND",
			svc:      &stubRuntimeService{snapshotErr: domain.ErrSessionNotFound},
			request:  &GetGameSnapshotRequest{Name: "sessions/nonexistent/game/snapshot"},
			wantCode: codes.NotFound,
		},
		{
			name:     "given empty name, when GetGameSnapshot called, returns INVALID_ARGUMENT",
			svc:      &stubRuntimeService{},
			request:  &GetGameSnapshotRequest{Name: ""},
			wantCode: codes.InvalidArgument,
		},
		{
			name:     "given malformed name, when GetGameSnapshot called, returns INVALID_ARGUMENT",
			svc:      &stubRuntimeService{},
			request:  &GetGameSnapshotRequest{Name: "invalid-name"},
			wantCode: codes.InvalidArgument,
		},
		{
			name:     "given name without snapshot suffix, when GetGameSnapshot called, returns INVALID_ARGUMENT",
			svc:      &stubRuntimeService{},
			request:  &GetGameSnapshotRequest{Name: "sessions/abc/game/runtime"},
			wantCode: codes.InvalidArgument,
		},
		{
			name:     "given name with empty session ID, when GetGameSnapshot called, returns INVALID_ARGUMENT",
			svc:      &stubRuntimeService{},
			request:  &GetGameSnapshotRequest{Name: "sessions//game/snapshot"},
			wantCode: codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			ctx := authCtxFromName(tt.request.GetName())
			handler := NewHandler(tt.svc, &dynamicVerifier{})

			// when
			got, err := handler.GetGameSnapshot(ctx, tt.request)

			// then
			assertStatusCode(t, err, tt.wantCode)
			if tt.wantCode != codes.OK {
				return
			}

			if got == nil {
				t.Fatal("GetGameSnapshot() returned nil, want non-nil")
			}
			tt.check(t, got)
		})
	}
}

func TestHandler_GetGameSnapshot_NoSnapshot(t *testing.T) {
	// given: session exists but service returns nil snapshot (no snapshot data available yet)
	handler := NewHandler(&stubRuntimeService{snapshot: nil}, &dynamicVerifier{})

	// when
	ctx := authCtxFromName("sessions/session-1/game/snapshot")
	got, err := handler.GetGameSnapshot(ctx, &GetGameSnapshotRequest{
		Name: "sessions/session-1/game/snapshot",
	})

	// then: nil snapshot still returns a valid GameSnapshot with name/session but empty image
	assertStatusCode(t, err, codes.OK)
	if got == nil {
		t.Fatal("GetGameSnapshot() returned nil, want non-nil even with nil snapshot")
	}
	wantName := "sessions/session-1/game/snapshot"
	if got.GetName() != wantName {
		t.Fatalf("Name = %q, want %q", got.GetName(), wantName)
	}
	if len(got.GetImage()) != 0 {
		t.Fatalf("Image = %q, want empty bytes", string(got.GetImage()))
	}
}

func TestHandler_GetGameRuntime(t *testing.T) {
	mediaTime := time.Date(2026, 4, 22, 10, 0, 0, 0, time.UTC)
	snapshotTime := time.Date(2026, 4, 22, 10, 0, 1, 0, time.UTC)
	opCreateTime := time.Date(2026, 4, 22, 10, 0, 2, 0, time.UTC)

	tests := []struct {
		name     string
		svc      *stubRuntimeService
		request  *GetGameRuntimeRequest
		wantCode codes.Code
		check    func(t *testing.T, got *GameRuntime)
	}{
		{
			name: "given valid name with full runtime, when GetGameRuntime called, returns runtime with all fields mapped",
			svc: &stubRuntimeService{
				runtime: &domain.SessionRuntime{
					SessionID:        "session-1",
					RuntimeID:        "rt-test",
					AgentConn:        &domain.AgentConnection{ConnID: "agent-1"},
					WebConns:         []*domain.WebConnection{{ConnID: "web-1"}, {ConnID: "web-2"}},
					StreamState:      domain.StreamStateActive,
					LastMediaTime:    mediaTime,
					LastSnapshotTime: snapshotTime,
					InflightOp: &domain.InflightOperation{
						OperationID:   "op-1",
						Kind:          domain.OperationKindMouseClick,
						FlashSnapshot: true,
						CreateTime:    opCreateTime,
					},
					LastError:           "test error",
					ReconnectGeneration: 3,
				},
			},
			request:  &GetGameRuntimeRequest{Name: "sessions/session-1/game/runtime"},
			wantCode: codes.OK,
			check: func(t *testing.T, got *GameRuntime) {
				wantName := "sessions/session-1/game/runtime"
				if got.GetName() != wantName {
					t.Fatalf("Name = %q, want %q", got.GetName(), wantName)
				}
				wantSession := "sessions/session-1"
				if got.GetSession() != wantSession {
					t.Fatalf("Session = %q, want %q", got.GetSession(), wantSession)
				}
				if got.GetRuntimeId() != "rt-test" {
					t.Fatalf("GatewayId = %q, want %q", got.GetRuntimeId(), "rt-test")
				}
				if !got.GetAgentConnected() {
					t.Fatal("AgentConnected = false, want true")
				}
				if got.GetWebConnectionCount() != 2 {
					t.Fatalf("WebConnectionCount = %d, want 2", got.GetWebConnectionCount())
				}
				if got.GetStreamStatus() != GameStreamStatus_GAME_STREAM_STATUS_ACTIVE {
					t.Fatalf("StreamStatus = %v, want ACTIVE", got.GetStreamStatus())
				}
				if got.GetLastMediaTime() == nil {
					t.Fatal("LastMediaTime is nil, want non-nil")
				}
				if got.GetLastSnapshotTime() == nil {
					t.Fatal("LastSnapshotTime is nil, want non-nil")
				}
				op := got.GetInflightOperation()
				if op == nil {
					t.Fatal("InflightOperation is nil, want non-nil")
				}
				if op.GetOperationId() != "op-1" {
					t.Fatalf("InflightOperation.OperationId = %q, want %q", op.GetOperationId(), "op-1")
				}
				if op.GetKind() != GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_CLICK {
					t.Fatalf("InflightOperation.Kind = %v, want MOUSE_CLICK", op.GetKind())
				}
				if !op.GetFlashSnapshot() {
					t.Fatal("InflightOperation.FlashSnapshot = false, want true")
				}
				if op.GetCreateTime() == nil {
					t.Fatal("InflightOperation.CreateTime is nil, want non-nil")
				}
				if got.GetLastError() != "test error" {
					t.Fatalf("LastError = %q, want %q", got.GetLastError(), "test error")
				}
				if got.GetReconnectGeneration() != 3 {
					t.Fatalf("ReconnectGeneration = %d, want 3", got.GetReconnectGeneration())
				}
			},
		},
		{
			name: "given valid name with empty runtime, when GetGameRuntime called, returns zero-value fields",
			svc: &stubRuntimeService{
				runtime: &domain.SessionRuntime{
					SessionID: "session-2",
					RuntimeID: "rt-test",
				},
			},
			request:  &GetGameRuntimeRequest{Name: "sessions/session-2/game/runtime"},
			wantCode: codes.OK,
			check: func(t *testing.T, got *GameRuntime) {
				if got.GetAgentConnected() {
					t.Fatal("AgentConnected = true, want false")
				}
				if got.GetWebConnectionCount() != 0 {
					t.Fatalf("WebConnectionCount = %d, want 0", got.GetWebConnectionCount())
				}
				if got.GetInflightOperation() != nil {
					t.Fatal("InflightOperation is non-nil, want nil")
				}
				if got.GetLastMediaTime() != nil {
					t.Fatal("LastMediaTime is non-nil, want nil for zero time")
				}
				if got.GetLastSnapshotTime() != nil {
					t.Fatal("LastSnapshotTime is non-nil, want nil for zero time")
				}
				if got.GetStreamStatus() != GameStreamStatus_GAME_STREAM_STATUS_UNSPECIFIED {
					t.Fatalf("StreamStatus = %v, want UNSPECIFIED", got.GetStreamStatus())
				}
			},
		},
		{
			name:     "given nonexistent session, when GetGameRuntime called, returns NOT_FOUND",
			svc:      &stubRuntimeService{runtimeErr: domain.ErrSessionNotFound},
			request:  &GetGameRuntimeRequest{Name: "sessions/nonexistent/game/runtime"},
			wantCode: codes.NotFound,
		},
		{
			name:     "given empty name, when GetGameRuntime called, returns INVALID_ARGUMENT",
			svc:      &stubRuntimeService{},
			request:  &GetGameRuntimeRequest{Name: ""},
			wantCode: codes.InvalidArgument,
		},
		{
			name:     "given malformed name, when GetGameRuntime called, returns INVALID_ARGUMENT",
			svc:      &stubRuntimeService{},
			request:  &GetGameRuntimeRequest{Name: "invalid-name"},
			wantCode: codes.InvalidArgument,
		},
		{
			name:     "given name with empty session ID, when GetGameRuntime called, returns INVALID_ARGUMENT",
			svc:      &stubRuntimeService{},
			request:  &GetGameRuntimeRequest{Name: "sessions//game/runtime"},
			wantCode: codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			ctx := authCtxFromName(tt.request.GetName())
			handler := NewHandler(tt.svc, &dynamicVerifier{})

			// when
			got, err := handler.GetGameRuntime(ctx, tt.request)

			// then
			assertStatusCode(t, err, tt.wantCode)
			if tt.wantCode != codes.OK {
				return
			}

			if got == nil {
				t.Fatal("GetGameRuntime() returned nil, want non-nil")
			}
			tt.check(t, got)
		})
	}
}

func Test_parseResourceName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		suffix  string
		wantID  string
		wantErr bool
	}{
		{
			name:   "valid snapshot name",
			input:  "sessions/abc-123/game/snapshot",
			suffix: "/game/snapshot",
			wantID: "abc-123",
		},
		{
			name:   "valid runtime name",
			input:  "sessions/xyz/game/runtime",
			suffix: "/game/runtime",
			wantID: "xyz",
		},
		{
			name:    "empty name",
			input:   "",
			suffix:  "/game/snapshot",
			wantErr: true,
		},
		{
			name:    "missing sessions prefix",
			input:   "other/abc/game/snapshot",
			suffix:  "/game/snapshot",
			wantErr: true,
		},
		{
			name:    "wrong suffix",
			input:   "sessions/abc/game/runtime",
			suffix:  "/game/snapshot",
			wantErr: true,
		},
		{
			name:    "empty session ID",
			input:   "sessions//game/snapshot",
			suffix:  "/game/snapshot",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got, err := parseResourceName(tt.input, tt.suffix)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatal("parseResourceName() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("parseResourceName() unexpected error: %v", err)
			}
			if got != tt.wantID {
				t.Fatalf("parseResourceName() = %q, want %q", got, tt.wantID)
			}
		})
	}
}

func TestHandler_GetGameSnapshot_NoToken(t *testing.T) {
	handler := NewHandler(&stubRuntimeService{}, &dynamicVerifier{})

	// No metadata in context → token extraction fails
	_, err := handler.GetGameSnapshot(context.Background(), &GetGameSnapshotRequest{Name: "sessions/session-1/game/snapshot"})
	assertStatusCode(t, err, codes.Unauthenticated)
}

func TestHandler_GetGameSnapshot_InvalidToken(t *testing.T) {
	// Context has token but verifier rejects it
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("token", "bad-token"))
	handler := NewHandler(&stubRuntimeService{}, &errVerifier{err: token.ErrTokenInvalid})

	_, err := handler.GetGameSnapshot(ctx, &GetGameSnapshotRequest{Name: "sessions/session-1/game/snapshot"})
	assertStatusCode(t, err, codes.Unauthenticated)
}

func TestHandler_GetGameSnapshot_WrongSession(t *testing.T) {
	// Token claims contain a different session ID than the request path
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("token", "session:session-2"))
	handler := NewHandler(&stubRuntimeService{}, &dynamicVerifier{})

	_, err := handler.GetGameSnapshot(ctx, &GetGameSnapshotRequest{Name: "sessions/session-1/game/snapshot"})
	assertStatusCode(t, err, codes.PermissionDenied)
}

func TestHandler_GetGameRuntime_NoToken(t *testing.T) {
	handler := NewHandler(&stubRuntimeService{}, &dynamicVerifier{})

	_, err := handler.GetGameRuntime(context.Background(), &GetGameRuntimeRequest{Name: "sessions/session-1/game/runtime"})
	assertStatusCode(t, err, codes.Unauthenticated)
}

func TestHandler_GetGameRuntime_ValidToken(t *testing.T) {
	ctx := authCtxFromName("sessions/session-1/game/runtime")
	handler := NewHandler(&stubRuntimeService{
		runtime: &domain.SessionRuntime{SessionID: "session-1", RuntimeID: "rt-test"},
	}, &dynamicVerifier{})

	got, err := handler.GetGameRuntime(ctx, &GetGameRuntimeRequest{Name: "sessions/session-1/game/runtime"})
	assertStatusCode(t, err, codes.OK)
	if got == nil {
		t.Fatal("GetGameRuntime() returned nil, want non-nil")
	}
}

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

func TestRuntimeHandler_CreateGameRuntime(t *testing.T) {
	tests := []struct {
		name     string
		svc      *stubRuntimeService
		request  *CreateGameRuntimeRequest
		wantCode codes.Code
	}{
		{
			name: "given valid parent and service returns unimplemented, when CreateGameRuntime called, returns UNIMPLEMENTED",
			svc: &stubRuntimeService{
				createGameRuntimeFn: func(_ context.Context, _ string, _ int64) (*domain.SessionRuntime, string, error) {
					return nil, "", status.Error(codes.Unimplemented, "not implemented")
				},
			},
			request:  &CreateGameRuntimeRequest{Parent: "sessions/session-1", ReconnectGeneration: 1},
			wantCode: codes.Unimplemented,
		},
		{
			name:     "given empty parent, when CreateGameRuntime called, returns INVALID_ARGUMENT",
			svc:      &stubRuntimeService{},
			request:  &CreateGameRuntimeRequest{Parent: "", ReconnectGeneration: 1},
			wantCode: codes.InvalidArgument,
		},
		{
			name:     "given parent without sessions prefix, when CreateGameRuntime called, returns INVALID_ARGUMENT",
			svc:      &stubRuntimeService{},
			request:  &CreateGameRuntimeRequest{Parent: "invalid-parent", ReconnectGeneration: 1},
			wantCode: codes.InvalidArgument,
		},
		{
			name:     "given parent with empty session ID, when CreateGameRuntime called, returns INVALID_ARGUMENT",
			svc:      &stubRuntimeService{},
			request:  &CreateGameRuntimeRequest{Parent: "sessions/", ReconnectGeneration: 1},
			wantCode: codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewRuntimeHandler(tt.svc)

			got, err := handler.CreateGameRuntime(context.Background(), tt.request)

			assertStatusCode(t, err, tt.wantCode)
			if tt.wantCode != codes.OK {
				return
			}

			if got == nil {
				t.Fatal("CreateGameRuntime() returned nil, want non-nil")
			}
		})
	}
}

func TestRuntimeHandler_RefreshGameRuntime(t *testing.T) {
	tests := []struct {
		name     string
		svc      *stubRuntimeService
		request  *RefreshGameRuntimeRequest
		wantCode codes.Code
	}{
		{
			name: "given valid name and service returns unimplemented, when RefreshGameRuntime called, returns UNIMPLEMENTED",
			svc: &stubRuntimeService{
				refreshGameRuntimeFn: func(_ context.Context, _ string, _ string) (*domain.SessionRuntime, string, error) {
					return nil, "", status.Error(codes.Unimplemented, "not implemented")
				},
			},
			request:  &RefreshGameRuntimeRequest{Name: "sessions/session-1/game/runtime", OldToken: "tok-1"},
			wantCode: codes.Unimplemented,
		},
		{
			name:     "given empty name, when RefreshGameRuntime called, returns INVALID_ARGUMENT",
			svc:      &stubRuntimeService{},
			request:  &RefreshGameRuntimeRequest{Name: "", OldToken: "tok-1"},
			wantCode: codes.InvalidArgument,
		},
		{
			name:     "given malformed name, when RefreshGameRuntime called, returns INVALID_ARGUMENT",
			svc:      &stubRuntimeService{},
			request:  &RefreshGameRuntimeRequest{Name: "invalid-name", OldToken: "tok-1"},
			wantCode: codes.InvalidArgument,
		},
		{
			name:     "given name with empty session ID, when RefreshGameRuntime called, returns INVALID_ARGUMENT",
			svc:      &stubRuntimeService{},
			request:  &RefreshGameRuntimeRequest{Name: "sessions//game/runtime", OldToken: "tok-1"},
			wantCode: codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewRuntimeHandler(tt.svc)

			got, err := handler.RefreshGameRuntime(context.Background(), tt.request)

			assertStatusCode(t, err, tt.wantCode)
			if tt.wantCode != codes.OK {
				return
			}

			if got == nil {
				t.Fatal("RefreshGameRuntime() returned nil, want non-nil")
			}
		})
	}
}

func Test_parseSessionIDFromParent(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantID  string
		wantErr bool
	}{
		{
			name:   "valid parent",
			input:  "sessions/session-1",
			wantID: "session-1",
		},
		{
			name:   "valid parent with complex ID",
			input:  "sessions/abc-123-xyz",
			wantID: "abc-123-xyz",
		},
		{
			name:    "empty parent",
			input:   "",
			wantErr: true,
		},
		{
			name:    "missing sessions prefix",
			input:   "other/session-1",
			wantErr: true,
		},
		{
			name:    "empty session ID",
			input:   "sessions/",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSessionIDFromParent(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Fatal("parseSessionIDFromParent() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("parseSessionIDFromParent() unexpected error: %v", err)
			}
			if got != tt.wantID {
				t.Fatalf("parseSessionIDFromParent() = %q, want %q", got, tt.wantID)
			}
		})
	}
}
