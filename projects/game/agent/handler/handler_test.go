package handler

import (
	"context"
	"sync"
	"testing"
	"time"

	"dominion/projects/game/agent/domain"

	game "dominion/projects/game"
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

func (m *mockRuntime) Init(_ context.Context, sessionID string) (*domain.Status, error) {
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

func (m *mockRuntime) Connect(_ domain.AgentStream) error {
	return nil
}

func TestInitAgent(t *testing.T) {
	// given
	rt := newMockRuntime()
	h := NewAgentHandler(rt)
	ctx := context.Background()

	// when
	resp, err := h.InitAgent(ctx, &game.InitAgentRequest{SessionId: "test"})

	// then
	if err != nil {
		t.Fatalf("InitAgent() unexpected error: %v", err)
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
	_, _ = rt.Init(ctx, "known-session")

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
