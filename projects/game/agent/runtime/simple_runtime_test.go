package runtime

import (
	"context"
	"io"
	"sync"
	"testing"

	game "dominion/projects/game"
)

func TestCreate(t *testing.T) {
	// given
	rt := NewSimpleRuntime()
	ctx := context.Background()

	// when
	_, err := rt.Create(ctx, "session1")

	// then
	if err != nil {
		t.Fatalf("Create() unexpected error: %v", err)
	}

	s, err := rt.Status(ctx, "session1")
	if err != nil {
		t.Fatalf("Status() unexpected error: %v", err)
	}
	if s.SessionId != "session1" {
		t.Errorf("Status().SessionId = %q, want %q", s.SessionId, "session1")
	}
	if s.Status != "initialized" {
		t.Errorf("Status().Status = %q, want %q", s.Status, "initialized")
	}
}

func TestStatusInitialized(t *testing.T) {
	// given
	rt := NewSimpleRuntime()
	ctx := context.Background()
	_, _ = rt.Create(ctx, "session1")

	// when
	s, err := rt.Status(ctx, "session1")

	// then
	if err != nil {
		t.Fatalf("Status() unexpected error: %v", err)
	}
	if s.Status != "initialized" {
		t.Errorf("Status().Status = %q, want %q", s.Status, "initialized")
	}
	if s.CreateTime.IsZero() {
		t.Error("Status().CreateTime is zero, want non-zero")
	}
}

func TestStatusUnknown(t *testing.T) {
	// given
	rt := NewSimpleRuntime()
	ctx := context.Background()

	// when
	s, err := rt.Status(ctx, "nonexistent")

	// then
	if err != nil {
		t.Fatalf("Status() unexpected error: %v", err)
	}
	if s.Status != "unknown" {
		t.Errorf("Status().Status = %q, want %q", s.Status, "unknown")
	}
	if s.SessionId != "nonexistent" {
		t.Errorf("Status().SessionId = %q, want %q", s.SessionId, "nonexistent")
	}
	if !s.CreateTime.IsZero() {
		t.Error("Status().CreateTime should be zero for unknown session")
	}
}

func TestInitMultipleSessions(t *testing.T) {
	// given
	tests := []struct {
		name       string
		sessionID  string
		wantStatus string
	}{
		{name: "session s1", sessionID: "s1", wantStatus: "initialized"},
		{name: "session s2", sessionID: "s2", wantStatus: "initialized"},
	}
	rt := NewSimpleRuntime()
	ctx := context.Background()
	_, _ = rt.Create(ctx, "s1")
	_, _ = rt.Create(ctx, "s2")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			s, err := rt.Status(ctx, tt.sessionID)

			// then
			if err != nil {
				t.Fatalf("Status() unexpected error: %v", err)
			}
			if s.SessionId != tt.sessionID {
				t.Errorf("SessionId = %q, want %q", s.SessionId, tt.sessionID)
			}
			if s.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", s.Status, tt.wantStatus)
			}
		})
	}
}

func TestConcurrentInit(t *testing.T) {
	// given
	rt := NewSimpleRuntime()
	ctx := context.Background()
	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)

	// when — concurrent Create calls using different session IDs
	for i := 0; i < n; i++ {
		go func(id int) {
			defer wg.Done()
			sessionID := "session-concurrent"
			_, err := rt.Create(ctx, sessionID)
			if err != nil {
				t.Errorf("Init() unexpected error: %v", err)
			}
		}(i)
	}
	wg.Wait()

	// then — status must be "initialized" after concurrent creates
	s, err := rt.Status(ctx, "session-concurrent")
	if err != nil {
		t.Fatalf("Status() unexpected error: %v", err)
	}
	if s.Status != "initialized" {
		t.Errorf("Status = %q, want %q", s.Status, "initialized")
	}
}

func TestDelete(t *testing.T) {
	// given
	rt := NewSimpleRuntime()
	ctx := context.Background()
	_, _ = rt.Create(ctx, "session1")

	// when
	err := rt.Delete(ctx, "session1")

	// then
	if err != nil {
		t.Fatalf("Delete() unexpected error: %v", err)
	}
	s, err := rt.Status(ctx, "session1")
	if err != nil {
		t.Fatalf("Status() unexpected error: %v", err)
	}
	if s.Status != "unknown" {
		t.Errorf("Status = %q, want %q after delete", s.Status, "unknown")
	}
}

func TestDeleteIdempotent(t *testing.T) {
	tests := []struct {
		name      string
		sessionID string
	}{
		{name: "non-existent session", sessionID: "nonexistent"},
		{name: "empty session id", sessionID: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			rt := NewSimpleRuntime()
			ctx := context.Background()

			// when
			err := rt.Delete(ctx, tt.sessionID)

			// then
			if err != nil {
				t.Fatalf("Delete() unexpected error for %q: %v", tt.sessionID, err)
			}
		})
	}
}

// mockAgentStream implements domain.AgentStream for testing Connect.
type mockAgentStream struct {
	recvCh  <-chan *game.AgentFrame
	sent    []*game.AgentFrame
	recvErr error
}

func (m *mockAgentStream) Recv() (*game.AgentFrame, error) {
	if m.recvErr != nil {
		return nil, m.recvErr
	}
	f, ok := <-m.recvCh
	if !ok {
		return nil, io.EOF
	}
	return f, nil
}

func (m *mockAgentStream) Send(f *game.AgentFrame) error {
	m.sent = append(m.sent, f)
	return nil
}

func TestConnect(t *testing.T) {
	// given: a runtime with an initialized session
	rt := NewSimpleRuntime()
	ctx := context.Background()
	_, _ = rt.Create(ctx, "session-connect")

	recvCh := make(chan *game.AgentFrame, 4)
	stream := &mockAgentStream{recvCh: recvCh}

	go func() {
		recvCh <- &game.AgentFrame{SessionId: "session-connect", Type: "status"}
		recvCh <- &game.AgentFrame{SessionId: "session-connect", Type: "text", Payload: []byte("hello")}
		recvCh <- &game.AgentFrame{SessionId: "unknown-session", Type: "status"}
		close(recvCh)
	}()

	// when: Connect processes the frames
	err := rt.Connect(stream)

	// then: no error on clean close
	if err != nil {
		t.Fatalf("Connect() unexpected error: %v", err)
	}

	// then: 3 responses were sent
	if len(stream.sent) != 3 {
		t.Fatalf("Connect() sent %d frames, want 3", len(stream.sent))
	}

	// then: status frame for initialized session
	if stream.sent[0].Type != "status" || string(stream.sent[0].Payload) != "initialized" {
		t.Fatalf("frame 0: type=%q payload=%q, want status/initialized", stream.sent[0].Type, string(stream.sent[0].Payload))
	}

	// then: echo frame
	if stream.sent[1].Type != "echo" || string(stream.sent[1].Payload) != "hello" {
		t.Fatalf("frame 1: type=%q payload=%q, want echo/hello", stream.sent[1].Type, string(stream.sent[1].Payload))
	}

	// then: status frame for unknown session
	if stream.sent[2].Type != "status" || string(stream.sent[2].Payload) != "unknown" {
		t.Fatalf("frame 2: type=%q payload=%q, want status/unknown", stream.sent[2].Type, string(stream.sent[2].Payload))
	}
}

func TestConnect_EOF(t *testing.T) {
	// given: empty stream
	rt := NewSimpleRuntime()
	recvCh := make(chan *game.AgentFrame)
	close(recvCh)
	stream := &mockAgentStream{recvCh: recvCh}

	// when
	err := rt.Connect(stream)

	// then: EOF is clean close
	if err != nil {
		t.Fatalf("Connect() on EOF unexpected error: %v", err)
	}
}
