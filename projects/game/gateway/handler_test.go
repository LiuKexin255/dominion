package gateway

import (
	"context"
	"testing"
	"time"

	"dominion/projects/game/gateway/domain"
	"dominion/projects/game/pkg/token"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type stubGatewayService struct {
	snapshot    *domain.SnapshotRef
	snapshotErr error
	runtime     *domain.SessionRuntime
	runtimeErr  error
}

func (s *stubGatewayService) GetSnapshot(_ context.Context, _ string) (*domain.SnapshotRef, error) {
	return s.snapshot, s.snapshotErr
}

func (s *stubGatewayService) GetRuntime(_ context.Context, _ string) (*domain.SessionRuntime, error) {
	return s.runtime, s.runtimeErr
}

func (s *stubGatewayService) ConnectSession(_ context.Context, _, _ string) (*domain.SessionRuntime, *token.Claims, error) {
	return nil, nil, nil
}

func (s *stubGatewayService) ProcessHello(_ *domain.SessionRuntime, _ *token.Claims, _ domain.ClientRole, _ string) ([]*domain.RoutedMessage, error) {
	return nil, nil
}

func (s *stubGatewayService) HandleAgentMessage(_ context.Context, _ string, _ *domain.Message) ([]*domain.RoutedMessage, error) {
	return nil, nil
}

func (s *stubGatewayService) HandleWebMessage(_ context.Context, _, _ string, _ *domain.Message) ([]*domain.RoutedMessage, error) {
	return nil, nil
}

func (s *stubGatewayService) DisconnectAgent(_ string) {}

func (s *stubGatewayService) DisconnectWeb(_, _ string) {}

func TestHandler_GetGameSnapshot(t *testing.T) {
	ctx := context.Background()
	captureTime := time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		svc      *stubGatewayService
		request  *GetGameSnapshotRequest
		wantCode codes.Code
		check    func(t *testing.T, got *GameSnapshot)
	}{
		{
			name: "given valid name with snapshot, when GetGameSnapshot called, returns snapshot with all fields",
			svc: &stubGatewayService{
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
			svc:      &stubGatewayService{snapshotErr: domain.ErrSessionNotFound},
			request:  &GetGameSnapshotRequest{Name: "sessions/nonexistent/game/snapshot"},
			wantCode: codes.NotFound,
		},
		{
			name:     "given empty name, when GetGameSnapshot called, returns INVALID_ARGUMENT",
			svc:      &stubGatewayService{},
			request:  &GetGameSnapshotRequest{Name: ""},
			wantCode: codes.InvalidArgument,
		},
		{
			name:     "given malformed name, when GetGameSnapshot called, returns INVALID_ARGUMENT",
			svc:      &stubGatewayService{},
			request:  &GetGameSnapshotRequest{Name: "invalid-name"},
			wantCode: codes.InvalidArgument,
		},
		{
			name:     "given name without snapshot suffix, when GetGameSnapshot called, returns INVALID_ARGUMENT",
			svc:      &stubGatewayService{},
			request:  &GetGameSnapshotRequest{Name: "sessions/abc/game/runtime"},
			wantCode: codes.InvalidArgument,
		},
		{
			name:     "given name with empty session ID, when GetGameSnapshot called, returns INVALID_ARGUMENT",
			svc:      &stubGatewayService{},
			request:  &GetGameSnapshotRequest{Name: "sessions//game/snapshot"},
			wantCode: codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			handler := NewHandler(tt.svc)

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
	ctx := context.Background()

	// given: session exists but service returns nil snapshot (no snapshot data available yet)
	handler := NewHandler(&stubGatewayService{snapshot: nil})

	// when
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
	ctx := context.Background()
	mediaTime := time.Date(2026, 4, 22, 10, 0, 0, 0, time.UTC)
	snapshotTime := time.Date(2026, 4, 22, 10, 0, 1, 0, time.UTC)
	opCreateTime := time.Date(2026, 4, 22, 10, 0, 2, 0, time.UTC)

	tests := []struct {
		name     string
		svc      *stubGatewayService
		request  *GetGameRuntimeRequest
		wantCode codes.Code
		check    func(t *testing.T, got *GameRuntime)
	}{
		{
			name: "given valid name with full runtime, when GetGameRuntime called, returns runtime with all fields mapped",
			svc: &stubGatewayService{
				runtime: &domain.SessionRuntime{
					SessionID:        "session-1",
					GatewayID:        "gw-test",
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
				if got.GetGatewayId() != "gw-test" {
					t.Fatalf("GatewayId = %q, want %q", got.GetGatewayId(), "gw-test")
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
			svc: &stubGatewayService{
				runtime: &domain.SessionRuntime{
					SessionID: "session-2",
					GatewayID: "gw-test",
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
			svc:      &stubGatewayService{runtimeErr: domain.ErrSessionNotFound},
			request:  &GetGameRuntimeRequest{Name: "sessions/nonexistent/game/runtime"},
			wantCode: codes.NotFound,
		},
		{
			name:     "given empty name, when GetGameRuntime called, returns INVALID_ARGUMENT",
			svc:      &stubGatewayService{},
			request:  &GetGameRuntimeRequest{Name: ""},
			wantCode: codes.InvalidArgument,
		},
		{
			name:     "given malformed name, when GetGameRuntime called, returns INVALID_ARGUMENT",
			svc:      &stubGatewayService{},
			request:  &GetGameRuntimeRequest{Name: "invalid-name"},
			wantCode: codes.InvalidArgument,
		},
		{
			name:     "given name with empty session ID, when GetGameRuntime called, returns INVALID_ARGUMENT",
			svc:      &stubGatewayService{},
			request:  &GetGameRuntimeRequest{Name: "sessions//game/runtime"},
			wantCode: codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			handler := NewHandler(tt.svc)

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
