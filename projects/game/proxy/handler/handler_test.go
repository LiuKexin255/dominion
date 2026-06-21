package handler

import (
	"context"
	"io"
	"regexp"
	"testing"

	game "dominion/projects/game"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// mockProxyService implements domain.ProxyService for testing.
type mockProxyService struct {
	getAgentResult     *game.Agent
	getAgentErr        error
	listMessagesResult *game.ListMessagesResponse
	listMessagesErr    error
	connectErr         error
	lastSessionID      string
	lastFirstFrame     *game.AgentFrame
}

func (m *mockProxyService) GetAgent(_ context.Context, sessionID string) (*game.Agent, error) {
	m.lastSessionID = sessionID
	return m.getAgentResult, m.getAgentErr
}

func (m *mockProxyService) ListMessages(_ context.Context, sessionID string, _ *game.ListMessagesRequest) (*game.ListMessagesResponse, error) {
	m.lastSessionID = sessionID
	return m.listMessagesResult, m.listMessagesErr
}

func (m *mockProxyService) Connect(_ context.Context, sessionID string, firstFrame *game.AgentFrame, _ game.ProxyService_ConnectAgentServer) error {
	m.lastSessionID = sessionID
	m.lastFirstFrame = firstFrame
	return m.connectErr
}

// mockProxyStream implements game.ProxyService_ConnectAgentServer for testing.
type mockProxyStream struct {
	ctx    context.Context
	recvCh <-chan *game.AgentFrame
	sendCh chan<- *game.AgentFrame
}

func (s *mockProxyStream) Recv() (*game.AgentFrame, error) {
	f, ok := <-s.recvCh
	if !ok {
		return nil, io.EOF
	}
	return f, nil
}

func (s *mockProxyStream) Send(f *game.AgentFrame) error {
	s.sendCh <- f
	return nil
}

func (s *mockProxyStream) SetHeader(metadata.MD) error  { return nil }
func (s *mockProxyStream) SendHeader(metadata.MD) error { return nil }
func (s *mockProxyStream) SetTrailer(metadata.MD)       {}
func (s *mockProxyStream) Context() context.Context     { return s.ctx }
func (s *mockProxyStream) SendMsg(m interface{}) error  { return nil }
func (s *mockProxyStream) RecvMsg(m interface{}) error  { return nil }

func TestGetAgent(t *testing.T) {
	ctx := context.Background()

	t.Run("success delegates to service", func(t *testing.T) {
		mockSvc := &mockProxyService{
			getAgentResult: &game.Agent{Name: "sessions/sid/agent", SessionId: "sid"},
		}
		h := NewProxyHandler(mockSvc)

		agent, err := h.GetAgent(ctx, &game.GetAgentRequest{Name: "sessions/sid/agent"})

		if err != nil {
			t.Fatalf("GetAgent() unexpected error: %v", err)
		}
		if agent.GetName() != "sessions/sid/agent" {
			t.Fatalf("GetAgent().Name = %q, want %q", agent.GetName(), "sessions/sid/agent")
		}
		if mockSvc.lastSessionID != "sid" {
			t.Fatalf("service sessionID = %q, want %q", mockSvc.lastSessionID, "sid")
		}
	})

	t.Run("invalid name", func(t *testing.T) {
		h := NewProxyHandler(&mockProxyService{})

		_, err := h.GetAgent(ctx, &game.GetAgentRequest{Name: "invalid-format"})

		if err == nil {
			t.Fatalf("GetAgent() expected error, got nil")
		}
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("GetAgent() status = %v, want InvalidArgument", status.Code(err))
		}
	})

	t.Run("service error propagates", func(t *testing.T) {
		mockSvc := &mockProxyService{
			getAgentErr: status.Error(codes.NotFound, "agent not found"),
		}
		h := NewProxyHandler(mockSvc)

		_, err := h.GetAgent(ctx, &game.GetAgentRequest{Name: "sessions/missing/agent"})

		if err == nil {
			t.Fatalf("GetAgent() expected error, got nil")
		}
		if status.Code(err) != codes.NotFound {
			t.Fatalf("GetAgent() status = %v, want NotFound", status.Code(err))
		}
	})
}

func TestListMessages(t *testing.T) {
	ctx := context.Background()

	t.Run("success delegates to service", func(t *testing.T) {
		mockSvc := &mockProxyService{
			listMessagesResult: &game.ListMessagesResponse{
				Messages: []*game.Message{{Name: "sessions/sid/messages/msg-001"}},
			},
		}
		h := NewProxyHandler(mockSvc)

		resp, err := h.ListMessages(ctx, &game.ListMessagesRequest{Parent: "sessions/sid"})

		if err != nil {
			t.Fatalf("ListMessages() unexpected error: %v", err)
		}
		if len(resp.GetMessages()) != 1 {
			t.Fatalf("ListMessages() got %d messages, want 1", len(resp.GetMessages()))
		}
		if mockSvc.lastSessionID != "sid" {
			t.Fatalf("service sessionID = %q, want %q", mockSvc.lastSessionID, "sid")
		}
	})

	t.Run("invalid parent", func(t *testing.T) {
		h := NewProxyHandler(&mockProxyService{})

		_, err := h.ListMessages(ctx, &game.ListMessagesRequest{Parent: "invalid-format"})

		if err == nil {
			t.Fatalf("ListMessages() expected error, got nil")
		}
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("ListMessages() status = %v, want InvalidArgument", status.Code(err))
		}
	})

	t.Run("service error propagates", func(t *testing.T) {
		mockSvc := &mockProxyService{
			listMessagesErr: status.Error(codes.Internal, "agent error"),
		}
		h := NewProxyHandler(mockSvc)

		_, err := h.ListMessages(ctx, &game.ListMessagesRequest{Parent: "sessions/sid"})

		if err == nil {
			t.Fatalf("ListMessages() expected error, got nil")
		}
		if status.Code(err) != codes.Internal {
			t.Fatalf("ListMessages() status = %v, want Internal", status.Code(err))
		}
	})
}

func TestConnectAgent(t *testing.T) {
	t.Run("success delegates first frame to service", func(t *testing.T) {
		mockSvc := &mockProxyService{}
		h := NewProxyHandler(mockSvc)

		firstFrame := &game.AgentFrame{
			SessionId: "sid",
			Payload:   &game.AgentFrame_Status{Status: &game.AgentStatusFrame{Status: "ready"}},
		}
		recvCh := make(chan *game.AgentFrame, 1)
		recvCh <- firstFrame
		close(recvCh)
		stream := &mockProxyStream{ctx: context.Background(), recvCh: recvCh}

		err := h.ConnectAgent(stream)

		if err != nil {
			t.Fatalf("ConnectAgent() unexpected error: %v", err)
		}
		if mockSvc.lastSessionID != "sid" {
			t.Fatalf("service sessionID = %q, want %q", mockSvc.lastSessionID, "sid")
		}
		if mockSvc.lastFirstFrame != firstFrame {
			t.Fatal("ConnectAgent() did not pass first frame to service")
		}
	})

	t.Run("empty session_id", func(t *testing.T) {
		h := NewProxyHandler(&mockProxyService{})

		recvCh := make(chan *game.AgentFrame, 1)
		recvCh <- &game.AgentFrame{SessionId: ""}
		close(recvCh)
		stream := &mockProxyStream{ctx: context.Background(), recvCh: recvCh}

		err := h.ConnectAgent(stream)

		if err == nil {
			t.Fatalf("ConnectAgent() expected error, got nil")
		}
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("ConnectAgent() status = %v, want InvalidArgument", status.Code(err))
		}
	})

	t.Run("recv error", func(t *testing.T) {
		h := NewProxyHandler(&mockProxyService{})

		recvCh := make(chan *game.AgentFrame)
		close(recvCh)
		stream := &mockProxyStream{ctx: context.Background(), recvCh: recvCh}

		err := h.ConnectAgent(stream)

		if err == nil {
			t.Fatalf("ConnectAgent() expected error, got nil")
		}
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("ConnectAgent() status = %v, want InvalidArgument", status.Code(err))
		}
	})

	t.Run("service error propagates", func(t *testing.T) {
		mockSvc := &mockProxyService{
			connectErr: status.Error(codes.Unavailable, "agent unreachable"),
		}
		h := NewProxyHandler(mockSvc)

		recvCh := make(chan *game.AgentFrame, 1)
		recvCh <- &game.AgentFrame{SessionId: "sid"}
		close(recvCh)
		stream := &mockProxyStream{ctx: context.Background(), recvCh: recvCh}

		err := h.ConnectAgent(stream)

		if err == nil {
			t.Fatalf("ConnectAgent() expected error, got nil")
		}
		if status.Code(err) != codes.Unavailable {
			t.Fatalf("ConnectAgent() status = %v, want Unavailable", status.Code(err))
		}
	})
}

func TestExtractSessionID(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		pattern string
		want    string
		wantErr bool
	}{
		{
			name:    "valid parent pattern",
			input:   "sessions/abc123",
			pattern: `^sessions/([^/]+)$`,
			want:    "abc123",
		},
		{
			name:    "valid agent pattern",
			input:   "sessions/abc123/agent",
			pattern: `^sessions/([^/]+)/agent$`,
			want:    "abc123",
		},
		{
			name:    "invalid format",
			input:   "invalid",
			pattern: `^sessions/([^/]+)$`,
			wantErr: true,
		},
		{
			name:    "empty input",
			input:   "",
			pattern: `^sessions/([^/]+)$`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern := regexp.MustCompile(tt.pattern)

			got, err := extractSessionID(tt.input, pattern)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("extractSessionID(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("extractSessionID(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("extractSessionID(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
