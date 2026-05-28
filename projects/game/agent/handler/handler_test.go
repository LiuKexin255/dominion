package handler

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"dominion/projects/game/agent/domain"

	game "dominion/projects/game"

	"google.golang.org/grpc/metadata"
)

// mockRuntime implements domain.Runtime for testing.
// It stores sessions in an in-memory map without external dependencies.
type mockRuntime struct {
	mu       sync.Mutex
	sessions map[string]*domain.Status
}

func newMockRuntime() *mockRuntime {
	return &mockRuntime{
		sessions: make(map[string]*domain.Status),
	}
}

func (m *mockRuntime) Create(_ context.Context, sessionID string) (*domain.Status, error) {
	s := &domain.Status{
		SessionId:  sessionID,
		Status:     "initialized",
		CreateTime: time.Now(),
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
	s, ok := m.sessions[sessionID]
	m.mu.Unlock()
	if ok {
		cp := *s
		return &cp, nil
	}
	return &domain.Status{
		SessionId: sessionID,
		Status:    "unknown",
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
				{SessionId: "test", Type: "status"},
			},
			wantResponses: []*game.AgentFrame{
				{SessionId: "test", Type: "status", Payload: []byte("initialized")},
			},
		},
		{
			name: "status for unknown session returns unknown",
			frames: []*game.AgentFrame{
				{SessionId: "nonexistent", Type: "status"},
			},
			wantResponses: []*game.AgentFrame{
				{SessionId: "nonexistent", Type: "status", Payload: []byte("unknown")},
			},
		},
		{
			name: "echo frame returns echo",
			frames: []*game.AgentFrame{
				{SessionId: "test", Type: "text", Payload: []byte("hello")},
			},
			wantResponses: []*game.AgentFrame{
				{SessionId: "test", Type: "echo", Payload: []byte("hello")},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			rt := newMockRuntime()
			_, _ = rt.Create(context.Background(), "test")

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
				if r.Type != tt.wantResponses[i].Type {
					t.Errorf("response[%d].Type = %q, want %q", i, r.Type, tt.wantResponses[i].Type)
				}
				if string(r.Payload) != string(tt.wantResponses[i].Payload) {
					t.Errorf("response[%d].Payload = %q, want %q", i, string(r.Payload), string(tt.wantResponses[i].Payload))
				}
				if r.SessionId != tt.wantResponses[i].SessionId {
					t.Errorf("response[%d].SessionId = %q, want %q", i, r.SessionId, tt.wantResponses[i].SessionId)
				}
			}
		})
	}
}

func TestCreateAgent(t *testing.T) {
	// given
	rt := newMockRuntime()
	h := NewAgentHandler(rt)
	ctx := context.Background()

	// when
	resp, err := h.CreateAgent(ctx, &game.AgentCreateRequest{SessionId: "test"})

	// then
	if err != nil {
		t.Fatalf("CreateAgent() unexpected error: %v", err)
	}
	if resp.GetSessionId() != "test" {
		t.Errorf("SessionId = %q, want %q", resp.GetSessionId(), "test")
	}
	if resp.GetStatus() != "initialized" {
		t.Errorf("Status = %q, want %q", resp.GetStatus(), "initialized")
	}
	if resp.GetCreateTime() == nil {
		t.Error("CreateTime is nil, want non-nil timestamp")
	}
}

func TestGetAgentStatus(t *testing.T) {
	// given
	rt := newMockRuntime()
	h := NewAgentHandler(rt)
	ctx := context.Background()
	_, _ = rt.Create(ctx, "known-session")

	// when
	resp, err := h.GetAgentStatus(ctx, &game.GetAgentStatusRequest{SessionId: "known-session"})

	// then
	if err != nil {
		t.Fatalf("GetAgentStatus() unexpected error: %v", err)
	}
	if resp.GetSessionId() != "known-session" {
		t.Errorf("SessionId = %q, want %q", resp.GetSessionId(), "known-session")
	}
	if resp.GetStatus() != "initialized" {
		t.Errorf("Status = %q, want %q", resp.GetStatus(), "initialized")
	}
}

func TestGetAgentStatusUnknown(t *testing.T) {
	// given
	rt := newMockRuntime()
	h := NewAgentHandler(rt)
	ctx := context.Background()

	tests := []struct {
		name       string
		sessionID  string
		wantStatus string
	}{
		{name: "non-existent session", sessionID: "nonexistent", wantStatus: "unknown"},
		{name: "empty session id", sessionID: "", wantStatus: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			resp, err := h.GetAgentStatus(ctx, &game.GetAgentStatusRequest{SessionId: tt.sessionID})

			// then — "unknown" is NOT an error; it returns a valid AgentStatus
			if err != nil {
				t.Fatalf("GetAgentStatus() unexpected error: %v", err)
			}
			if resp.GetStatus() != tt.wantStatus {
				t.Errorf("Status = %q, want %q", resp.GetStatus(), tt.wantStatus)
			}
			if resp.GetSessionId() != tt.sessionID {
				t.Errorf("SessionId = %q, want %q", resp.GetSessionId(), tt.sessionID)
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
			_, _ = rt.Create(ctx, "to-delete")

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
