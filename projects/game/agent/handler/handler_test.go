package handler

import (
	"context"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	game "dominion/projects/game"
	"dominion/projects/game/agent/domain"

	"google.golang.org/grpc/codes"
	grpcStatus "google.golang.org/grpc/status"
	"google.golang.org/grpc/metadata"
)

// mockRuntime implements domain.Runtime for testing.
// It stores sessions in an in-memory map without external dependencies.
type mockRuntime struct {
	mu       sync.Mutex
	sessions map[string]*domain.Status
	// lastProfileName records the profile name passed to CreateWithProfile.
	lastProfileName string
	// screenshotFrames is the list of frames to return from ReceiveScreenshot.
	// If nil, a default text frame is returned.
	screenshotFrames []*domain.Frame
}

func newMockRuntime() *mockRuntime {
	return &mockRuntime{
		sessions: make(map[string]*domain.Status),
	}
}

func (m *mockRuntime) CreateWithProfile(_ context.Context, sessionID string, config *domain.InvokeRuntimeConfig) (*domain.Status, error) {
	m.mu.Lock()
	profileName := ""
	if config != nil {
		profileName = config.ProfileName
	}
	m.lastProfileName = profileName
	m.mu.Unlock()

	s := &domain.Status{
		SessionId:   sessionID,
		Status:      "initialized",
		ProfileName: profileName,
		CreateTime:  time.Now(),
	}
	m.mu.Lock()
	m.sessions[sessionID] = s
	m.mu.Unlock()
	cp := *s
	return &cp, nil
}

func (m *mockRuntime) Delete(_ context.Context, sessionID string) error {
	m.mu.Lock()
	delete(m.sessions, sessionID)
	m.mu.Unlock()
	return nil
}

func (m *mockRuntime) Status(_ context.Context, sessionID string) (*domain.Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[sessionID]
	if !ok {
		return nil, fmt.Errorf("%w: %q", domain.ErrNotFound, sessionID)
	}
	return s, nil
}

func (m *mockRuntime) ReceiveScreenshot(_ context.Context, sessionID string, _ *domain.ScreenshotInput) ([]*domain.Frame, error) {
	m.mu.Lock()
	frames := m.screenshotFrames
	m.mu.Unlock()
	if frames != nil {
		return frames, nil
	}
	return []*domain.Frame{
		{Type: domain.FrameTypeText, Content: "screenshot received"},
	}, nil
}

// mockAgentConnectServer implements game.AgentService_ConnectServer for testing.
type mockAgentConnectServer struct {
	frames []*game.AgentFrame
	index  int
	sent   []*game.AgentFrame
	mu     sync.Mutex
}

func newMockAgentConnectServer(frames []*game.AgentFrame) *mockAgentConnectServer {
	return &mockAgentConnectServer{
		frames: frames,
	}
}

func (m *mockAgentConnectServer) Recv() (*game.AgentFrame, error) {
	if m.index >= len(m.frames) {
		return nil, io.EOF
	}
	f := m.frames[m.index]
	m.index++
	return f, nil
}

func (m *mockAgentConnectServer) Send(f *game.AgentFrame) error {
	m.mu.Lock()
	m.sent = append(m.sent, f)
	m.mu.Unlock()
	return nil
}

func (m *mockAgentConnectServer) Sent() []*game.AgentFrame {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]*game.AgentFrame, len(m.sent))
	copy(cp, m.sent)
	return cp
}

func (m *mockAgentConnectServer) SetHeader(md metadata.MD) error  { return nil }
func (m *mockAgentConnectServer) SendHeader(md metadata.MD) error { return nil }
func (m *mockAgentConnectServer) SetTrailer(md metadata.MD)       {}
func (m *mockAgentConnectServer) Context() context.Context        { return context.Background() }
func (m *mockAgentConnectServer) SendMsg(msg interface{}) error   { return nil }
func (m *mockAgentConnectServer) RecvMsg(msg interface{}) error   { return nil }

func TestConnect(t *testing.T) {
	tests := []struct {
		name          string
		frames        []*game.AgentFrame
		wantResponses []*game.AgentFrame
	}{
		{
			name: "status frame returns status string",
			frames: []*game.AgentFrame{
				{
					SessionId: "test",
					Payload:   &game.AgentFrame_Status{Status: &game.AgentStatusFrame{}},
				},
			},
			wantResponses: []*game.AgentFrame{
				{
					SessionId: "test",
					Payload:   &game.AgentFrame_Status{Status: &game.AgentStatusFrame{Status: "initialized"}},
				},
			},
		},
		{
			name: "echo frame returns echo",
			frames: []*game.AgentFrame{
				{
					SessionId: "test",
					Payload:   &game.AgentFrame_Echo{Echo: &game.AgentEchoFrame{Data: []byte("hello")}},
				},
			},
			wantResponses: []*game.AgentFrame{
				{
					SessionId: "test",
					Payload:   &game.AgentFrame_Echo{Echo: &game.AgentEchoFrame{Data: []byte("hello")}},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			rt := newMockRuntime()
			_, _ = rt.CreateWithProfile(context.Background(), "test", &domain.InvokeRuntimeConfig{})

			stream := newMockAgentConnectServer(tt.frames)
			handler := NewAgentHandler(rt)

			// when
			err := handler.Connect(stream)

			// then
			if err != nil {
				t.Fatalf("Connect() error: %v", err)
			}

			got := stream.Sent()
			if len(got) != len(tt.wantResponses) {
				t.Fatalf("got %d responses, want %d", len(got), len(tt.wantResponses))
			}
			for i, r := range got {
				if r.SessionId != tt.wantResponses[i].SessionId {
					t.Errorf("response[%d].SessionId = %q, want %q", i, r.SessionId, tt.wantResponses[i].SessionId)
				}
				switch want := tt.wantResponses[i].GetPayload().(type) {
				case *game.AgentFrame_Status:
					s, ok := r.GetPayload().(*game.AgentFrame_Status)
					if !ok {
						t.Errorf("response[%d]: expected status payload, got %T", i, r.GetPayload())
						continue
					}
					if s.Status.GetStatus() != want.Status.GetStatus() {
						t.Errorf("response[%d].status = %q, want %q", i, s.Status.GetStatus(), want.Status.GetStatus())
					}
				case *game.AgentFrame_Echo:
					e, ok := r.GetPayload().(*game.AgentFrame_Echo)
					if !ok {
						t.Errorf("response[%d]: expected echo payload, got %T", i, r.GetPayload())
						continue
					}
					if string(e.Echo.GetData()) != string(want.Echo.GetData()) {
						t.Errorf("response[%d].echo.data = %q, want %q", i, string(e.Echo.GetData()), string(want.Echo.GetData()))
					}
				case *game.AgentFrame_Text:
					txt, ok := r.GetPayload().(*game.AgentFrame_Text)
					if !ok {
						t.Errorf("response[%d]: expected text payload, got %T", i, r.GetPayload())
						continue
					}
					if txt.Text.GetContent() != want.Text.GetContent() {
						t.Errorf("response[%d].text.content = %q, want %q", i, txt.Text.GetContent(), want.Text.GetContent())
					}
				}
			}
		})
	}
}

func TestConnect_Screenshot(t *testing.T) {
	// given
	rt := newMockRuntime()
	rt.screenshotFrames = []*domain.Frame{
		{Type: domain.FrameTypeText, Content: "analyzing screenshot..."},
		{
			Type:         domain.FrameTypeOperation,
			OperationID:  "op-test-1",
			ScreenshotID: "capture-001",
			Sequence:     1,
			IsMouse:      true,
			Button:       1,
			ClickType:    1,
			XPx:          960,
			YPx:          540,
		},
	}
	handler := NewAgentHandler(rt)

	frames := []*game.AgentFrame{
		{
			SessionId: "test",
			Payload: &game.AgentFrame_Screenshot{
				Screenshot: &game.AgentScreenshotFrame{
					CaptureId: "capture-001",
					Encoding:  game.ImageEncoding_IMAGE_ENCODING_PNG,
					Data:      []byte("png-data"),
					WidthPx:   1920,
					HeightPx:  1080,
				},
			},
		},
	}

	stream := newMockAgentConnectServer(frames)

	// when
	err := handler.Connect(stream)

	// then
	if err != nil {
		t.Fatalf("Connect() error: %v", err)
	}

	got := stream.Sent()
	if len(got) != 2 {
		t.Fatalf("got %d responses, want 2", len(got))
	}

	// First response: text frame
	textPayload, ok := got[0].GetPayload().(*game.AgentFrame_Text)
	if !ok {
		t.Fatalf("response[0]: expected text payload, got %T", got[0].GetPayload())
	}
	if textPayload.Text.GetContent() != "analyzing screenshot..." {
		t.Errorf("response[0].text.content = %q, want %q", textPayload.Text.GetContent(), "analyzing screenshot...")
	}

	// Second response: operation frame
	opPayload, ok := got[1].GetPayload().(*game.AgentFrame_Operation)
	if !ok {
		t.Fatalf("response[1]: expected operation payload, got %T", got[1].GetPayload())
	}
	op := opPayload.Operation
	if op.GetOperationId() != "op-test-1" {
		t.Errorf("operation_id = %q, want %q", op.GetOperationId(), "op-test-1")
	}
	if op.GetScreenshotId() != "capture-001" {
		t.Errorf("screenshot_id = %q, want %q", op.GetScreenshotId(), "capture-001")
	}
	mouse := op.GetMouse()
	if mouse == nil {
		t.Fatal("operation mouse is nil, want non-nil")
	}
	if mouse.GetXPx() != 960 || mouse.GetYPx() != 540 {
		t.Errorf("mouse position = (%d, %d), want (960, 540)", mouse.GetXPx(), mouse.GetYPx())
	}
}

func TestConnect_StatusUnknownSession(t *testing.T) {
	// given: runtime has no agent for the session; Status returns ErrNotFound.
	rt := newMockRuntime()
	handler := NewAgentHandler(rt)

	stream := newMockAgentConnectServer([]*game.AgentFrame{
		{
			SessionId: "nonexistent",
			Payload:   &game.AgentFrame_Status{Status: &game.AgentStatusFrame{}},
		},
	})

	// when
	err := handler.Connect(stream)

	// then
	if err == nil {
		t.Fatal("Connect() expected error for unknown session status query, got nil")
	}
}

func TestConnect_WarnFrame(t *testing.T) {
	// given: runtime returns a warn frame for stale state
	rt := newMockRuntime()
	rt.screenshotFrames = []*domain.Frame{
		{
			Type:        domain.FrameTypeWarn,
			WarnMessage: "screenshot received while waiting for operation result",
			WarnCode:    "WRONG_STATE",
		},
	}
	handler := NewAgentHandler(rt)

	frames := []*game.AgentFrame{
		{
			SessionId: "test",
			Payload: &game.AgentFrame_Screenshot{
				Screenshot: &game.AgentScreenshotFrame{
					CaptureId: "capture-002",
					WidthPx:   800,
					HeightPx:  600,
				},
			},
		},
	}

	stream := newMockAgentConnectServer(frames)

	// when
	err := handler.Connect(stream)

	// then
	if err != nil {
		t.Fatalf("Connect() error: %v", err)
	}

	got := stream.Sent()
	if len(got) != 1 {
		t.Fatalf("got %d responses, want 1", len(got))
	}

	warnPayload, ok := got[0].GetPayload().(*game.AgentFrame_Warn)
	if !ok {
		t.Fatalf("response[0]: expected warn payload, got %T", got[0].GetPayload())
	}
	if warnPayload.Warn.GetMessage() != "screenshot received while waiting for operation result" {
		t.Errorf("warn.message = %q, want %q", warnPayload.Warn.GetMessage(), "screenshot received while waiting for operation result")
	}
	if warnPayload.Warn.GetCode() != "WRONG_STATE" {
		t.Errorf("warn.code = %q, want %q", warnPayload.Warn.GetCode(), "WRONG_STATE")
	}
}

func TestConnect_EmptyPayload(t *testing.T) {
	// given
	rt := newMockRuntime()
	handler := NewAgentHandler(rt)

	echoData := []byte("after-empty")
	frames := []*game.AgentFrame{
		{SessionId: "test"},
		{
			SessionId: "test",
			Payload:   &game.AgentFrame_Echo{Echo: &game.AgentEchoFrame{Data: echoData}},
		},
	}

	stream := newMockAgentConnectServer(frames)

	// when
	err := handler.Connect(stream)

	// then
	if err != nil {
		t.Fatalf("Connect() error: %v", err)
	}

	// Empty frame is skipped; only the echo frame produces a response.
	got := stream.Sent()
	if len(got) != 1 {
		t.Fatalf("got %d responses, want 1 (empty frame skipped)", len(got))
	}

	echo := got[0].GetEcho()
	if echo == nil {
		t.Fatal("response payload is not echo")
	}
	if string(echo.GetData()) != string(echoData) {
		t.Errorf("echo data = %q, want %q", string(echo.GetData()), string(echoData))
	}
}

func TestCreateAgent(t *testing.T) {
	// given
	rt := newMockRuntime()
	h := NewAgentHandler(rt)
	ctx := context.Background()

	// when
	resp, err := h.CreateAgent(ctx, &game.AgentCreateRequest{
		SessionId:       "test",
		AgentProfileName: "my-profile",
	})

	// then
	if err != nil {
		t.Fatalf("CreateAgent() unexpected error: %v", err)
	}
	if resp.GetName() != "sessions/test/agent" {
		t.Errorf("Name = %q, want %q", resp.GetName(), "sessions/test/agent")
	}
	if resp.GetSessionId() != "test" {
		t.Errorf("SessionId = %q, want %q", resp.GetSessionId(), "test")
	}
	if resp.GetAgentProfileName() != "my-profile" {
		t.Errorf("AgentProfileName = %q, want %q", resp.GetAgentProfileName(), "my-profile")
	}

	rt.mu.Lock()
	profileName := rt.lastProfileName
	rt.mu.Unlock()
	if profileName != "my-profile" {
		t.Errorf("profile name passed to runtime = %q, want %q", profileName, "my-profile")
	}
}

func TestGetAgent(t *testing.T) {
	// given
	rt := newMockRuntime()
	h := NewAgentHandler(rt)
	ctx := context.Background()
	_, _ = rt.CreateWithProfile(ctx, "known-session", &domain.InvokeRuntimeConfig{ProfileName: "my-profile"})

	// when
	resp, err := h.GetAgent(ctx, &game.AgentGetRequest{SessionId: "known-session"})

	// then
	if err != nil {
		t.Fatalf("GetAgent() unexpected error: %v", err)
	}
	if resp.GetSessionId() != "known-session" {
		t.Errorf("SessionId = %q, want %q", resp.GetSessionId(), "known-session")
	}
	if resp.GetAgentProfileName() == "" {
		t.Error("AgentProfileName should not be empty")
	}
}

func TestGetAgentNotFound(t *testing.T) {
	// given
	rt := newMockRuntime()
	h := NewAgentHandler(rt)
	ctx := context.Background()

	tests := []struct {
		name      string
		sessionID string
	}{
		{name: "non-existent session", sessionID: "nonexistent"},
		{name: "empty session id", sessionID: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			_, err := h.GetAgent(ctx, &game.AgentGetRequest{SessionId: tt.sessionID})

			// then — expect NotFound error
			if err == nil {
				t.Fatal("GetAgent() expected error, got nil")
			}
			if grpcStatus.Code(err) != codes.NotFound {
				t.Fatalf("GetAgent() status = %v, want NotFound", grpcStatus.Code(err))
			}
		})
	}
}

func TestDeleteAgent(t *testing.T) {
	tests := []struct {
		name      string
		sessionID string
	}{
		{name: "existing session", sessionID: "to-delete"},
		{name: "non-existent session", sessionID: "nonexistent"},
		{name: "empty session id", sessionID: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			rt := newMockRuntime()
			h := NewAgentHandler(rt)
			ctx := context.Background()
			_, _ = rt.CreateWithProfile(ctx, "to-delete", &domain.InvokeRuntimeConfig{})

			// when
			resp, err := h.DeleteAgent(ctx, &game.AgentDeleteRequest{SessionId: tt.sessionID})

			// then
			if err != nil {
				t.Fatalf("DeleteAgent() unexpected error: %v", err)
			}
			if resp == nil {
				t.Fatal("DeleteAgent() resp is nil, want non-nil")
			}
		})
	}
}

func TestCreateAgent_EmptyProfile_ReturnsInvalidArgument(t *testing.T) {
	// given
	rt := newMockRuntime()
	h := NewAgentHandler(rt)
	ctx := context.Background()

	// when
	_, err := h.CreateAgent(ctx, &game.AgentCreateRequest{
		SessionId:        "test",
		AgentProfileName: "",
	})

	// then
	if err == nil {
		t.Fatal("expected InvalidArgument error, got nil")
	}
	if grpcStatus.Code(err) != codes.InvalidArgument {
		t.Errorf("status code = %v, want %v", grpcStatus.Code(err), codes.InvalidArgument)
	}
}
