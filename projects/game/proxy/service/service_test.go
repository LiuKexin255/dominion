package service

import (
	"context"
	"errors"
	"io"
	"testing"

	game "dominion/projects/game"
	"dominion/projects/game/pkg/bind"
	"dominion/projects/game/proxy/domain"
	"dominion/projects/game/proxy/runtime/agentclient"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// mockOwnerStore implements domain.OwnerStore for testing.
type mockOwnerStore struct {
	records map[string]*domain.AgentOwner
	getErr  error
}

func newMockOwnerStore() *mockOwnerStore {
	return &mockOwnerStore{records: make(map[string]*domain.AgentOwner)}
}

func (s *mockOwnerStore) Create(_ context.Context, owner *domain.AgentOwner) error {
	if _, exists := s.records[owner.SessionID]; exists {
		return domain.ErrOwnerAlreadyExists
	}
	s.records[owner.SessionID] = owner
	return nil
}

func (s *mockOwnerStore) Get(_ context.Context, sessionID string) (*domain.AgentOwner, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	owner, exists := s.records[sessionID]
	if !exists {
		return nil, domain.ErrOwnerNotFound
	}
	return owner, nil
}

func (s *mockOwnerStore) Delete(_ context.Context, sessionID string) error {
	if _, exists := s.records[sessionID]; !exists {
		return domain.ErrOwnerNotFound
	}
	delete(s.records, sessionID)
	return nil
}

// mockManager implements agentclient.Manager for testing.
type mockManager struct {
	connRefs []*agentclient.ConnRef
	getErr   error
	listErr  error
}

func (m *mockManager) Get(_ context.Context, ownerIndex int) (*agentclient.ConnRef, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return &agentclient.ConnRef{
		OwnerIndex: ownerIndex,
		Owner:      "agent",
	}, nil
}

func (m *mockManager) List(_ context.Context) ([]*agentclient.ConnRef, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.connRefs, nil
}

func (m *mockManager) Close() error { return nil }

// mockOwnerPicker implements domain.OwnerPicker for testing.
type mockOwnerPicker struct {
	ref agentclient.ConnRef
	err error
}

func (p *mockOwnerPicker) Pick(_ context.Context, _ string, _ []*agentclient.ConnRef) (*agentclient.ConnRef, error) {
	if p.err != nil {
		return nil, p.err
	}
	return &agentclient.ConnRef{
		OwnerIndex: p.ref.OwnerIndex,
		Owner:      p.ref.Owner,
	}, nil
}

// mockAgentClient implements agentclient.Client for testing.
type mockAgentClient struct {
	getAgentErr    error
	listMessagesResult *game.ListMessagesResponse
	listMessagesErr    error
	connectErr     error
	agentStream    game.AgentService_ConnectClient
}

func (c *mockAgentClient) GetAgent(_ context.Context, req *game.AgentGetRequest) (*game.Agent, error) {
	if c.getAgentErr != nil {
		return nil, c.getAgentErr
	}
	return &game.Agent{
		Name:      "sessions/" + req.GetSessionId() + "/agent",
		SessionId: req.GetSessionId(),
	}, nil
}

func (c *mockAgentClient) ListMessages(_ context.Context, req *game.ListMessagesRequest) (*game.ListMessagesResponse, error) {
	if c.listMessagesErr != nil {
		return nil, c.listMessagesErr
	}
	if c.listMessagesResult != nil {
		return c.listMessagesResult, nil
	}
	return &game.ListMessagesResponse{
		Messages: []*game.Message{
			{Name: req.GetParent() + "/messages/msg-001"},
		},
	}, nil
}

func (c *mockAgentClient) Connect(_ context.Context, _ ...grpc.CallOption) (game.AgentService_ConnectClient, error) {
	if c.connectErr != nil {
		return nil, c.connectErr
	}
	return c.agentStream, nil
}

// mockAgentStream implements game.AgentService_ConnectClient for testing.
type mockAgentStream struct {
	recvCh  <-chan *game.AgentFrame
	sendCh  chan<- *game.AgentFrame
	sendErr error
}

func (s *mockAgentStream) Recv() (*game.AgentFrame, error) {
	f, ok := <-s.recvCh
	if !ok {
		return nil, io.EOF
	}
	return f, nil
}

func (s *mockAgentStream) Send(f *game.AgentFrame) error {
	if s.sendErr != nil {
		return s.sendErr
	}
	s.sendCh <- f
	return nil
}

func (s *mockAgentStream) Header() (metadata.MD, error) { return nil, nil }
func (s *mockAgentStream) Trailer() metadata.MD         { return nil }
func (s *mockAgentStream) CloseSend() error             { return nil }
func (s *mockAgentStream) Context() context.Context     { return context.Background() }
func (s *mockAgentStream) SendMsg(m interface{}) error  { return nil }
func (s *mockAgentStream) RecvMsg(m interface{}) error  { return nil }

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

// mockBinder implements bind.Binder for testing.
type mockBinder struct {
	err error
}

func (b *mockBinder) Bind(_ bind.AgentFrameStream, _ bind.AgentFrameStream) error {
	return b.err
}

// setMockNewAgentClient replaces agentclient.NewAgentClient with a factory
// that returns the given mockClient, and returns a restore function.
func setMockNewAgentClient(mockClient agentclient.Client) func() {
	old := agentclient.NewAgentClient
	agentclient.NewAgentClient = func(conn *grpc.ClientConn) agentclient.Client {
		return mockClient
	}
	return func() { agentclient.NewAgentClient = old }
}

func TestGetAgent(t *testing.T) {
	ctx := context.Background()
	picker := &mockOwnerPicker{ref: agentclient.ConnRef{OwnerIndex: 1, Owner: "agent-1"}}

	t.Run("success with existing owner", func(t *testing.T) {
		store := newMockOwnerStore()
		store.records["sid"] = &domain.AgentOwner{SessionID: "sid", OwnerIndex: 1}
		agentMock := &mockAgentClient{}
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		svc := NewProxyService(store, picker, &mockManager{}, &mockBinder{})

		agent, err := svc.GetAgent(ctx, "sid")

		if err != nil {
			t.Fatalf("GetAgent() unexpected error: %v", err)
		}
		if agent.GetSessionId() != "sid" {
			t.Fatalf("GetAgent().SessionId = %q, want %q", agent.GetSessionId(), "sid")
		}
	})

	t.Run("lazy owner creation", func(t *testing.T) {
		store := newMockOwnerStore()
		mgr := &mockManager{connRefs: []*agentclient.ConnRef{{OwnerIndex: 1, Owner: "agent-1"}}}
		agentMock := &mockAgentClient{}
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		svc := NewProxyService(store, picker, mgr, &mockBinder{})

		_, err := svc.GetAgent(ctx, "lazy-sid")

		if err != nil {
			t.Fatalf("GetAgent() unexpected error: %v", err)
		}
		if _, ok := store.records["lazy-sid"]; !ok {
			t.Fatal("GetAgent() did not create owner lazily")
		}
	})

	t.Run("agent error propagates", func(t *testing.T) {
		store := newMockOwnerStore()
		store.records["sid"] = &domain.AgentOwner{SessionID: "sid", OwnerIndex: 1}
		agentMock := &mockAgentClient{getAgentErr: status.Error(codes.NotFound, "agent not found")}
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		svc := NewProxyService(store, picker, &mockManager{}, &mockBinder{})

		_, err := svc.GetAgent(ctx, "sid")

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
	picker := &mockOwnerPicker{ref: agentclient.ConnRef{OwnerIndex: 1, Owner: "agent-1"}}

	t.Run("success with existing owner", func(t *testing.T) {
		store := newMockOwnerStore()
		store.records["sid"] = &domain.AgentOwner{SessionID: "sid", OwnerIndex: 1}
		agentMock := &mockAgentClient{}
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		svc := NewProxyService(store, picker, &mockManager{}, &mockBinder{})

		resp, err := svc.ListMessages(ctx, "sid", &game.ListMessagesRequest{Parent: "sessions/sid"})

		if err != nil {
			t.Fatalf("ListMessages() unexpected error: %v", err)
		}
		if len(resp.GetMessages()) != 1 {
			t.Fatalf("ListMessages() got %d messages, want 1", len(resp.GetMessages()))
		}
	})

	t.Run("lazy owner creation", func(t *testing.T) {
		store := newMockOwnerStore()
		mgr := &mockManager{connRefs: []*agentclient.ConnRef{{OwnerIndex: 1, Owner: "agent-1"}}}
		agentMock := &mockAgentClient{}
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		svc := NewProxyService(store, picker, mgr, &mockBinder{})

		_, err := svc.ListMessages(ctx, "lazy-sid", &game.ListMessagesRequest{Parent: "sessions/lazy-sid"})

		if err != nil {
			t.Fatalf("ListMessages() unexpected error: %v", err)
		}
		if _, ok := store.records["lazy-sid"]; !ok {
			t.Fatal("ListMessages() did not create owner lazily")
		}
	})
}

func TestConnect(t *testing.T) {
	const testSessionID = "session-001"
	picker := &mockOwnerPicker{ref: agentclient.ConnRef{OwnerIndex: 1, Owner: "agent-1"}}

	t.Run("happy path with existing owner", func(t *testing.T) {
		store := newMockOwnerStore()
		store.records[testSessionID] = &domain.AgentOwner{SessionID: testSessionID, OwnerIndex: 1}

		agentStream := &mockAgentStream{recvCh: make(<-chan *game.AgentFrame), sendCh: make(chan<- *game.AgentFrame)}
		agentMock := &mockAgentClient{agentStream: agentStream}
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		svc := NewProxyService(store, picker, &mockManager{}, &mockBinder{})

		err := svc.Connect(context.Background(), testSessionID, makeFirstFrame(testSessionID), &mockProxyStream{})

		if err != nil {
			t.Fatalf("Connect() unexpected error: %v", err)
		}
	})

	t.Run("lazy owner creation", func(t *testing.T) {
		store := newMockOwnerStore()
		mgr := &mockManager{connRefs: []*agentclient.ConnRef{{OwnerIndex: 1, Owner: "agent-1"}}}

		agentStream := &mockAgentStream{recvCh: make(<-chan *game.AgentFrame), sendCh: make(chan<- *game.AgentFrame)}
		agentMock := &mockAgentClient{agentStream: agentStream}
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		svc := NewProxyService(store, picker, mgr, &mockBinder{})

		err := svc.Connect(context.Background(), testSessionID, makeFirstFrame(testSessionID), &mockProxyStream{})

		if err != nil {
			t.Fatalf("Connect() unexpected error: %v", err)
		}
		if _, ok := store.records[testSessionID]; !ok {
			t.Fatal("Connect() did not create owner lazily")
		}
	})

	t.Run("owner store returns other error", func(t *testing.T) {
		store := newMockOwnerStore()
		store.getErr = status.Error(codes.Internal, "db connection lost")

		svc := NewProxyService(store, picker, &mockManager{}, &mockBinder{})

		err := svc.Connect(context.Background(), testSessionID, makeFirstFrame(testSessionID), &mockProxyStream{})

		if err == nil {
			t.Fatalf("Connect() expected error, got nil")
		}
		if status.Code(err) != codes.Internal {
			t.Fatalf("Connect() status = %v, want Internal", status.Code(err))
		}
	})

	t.Run("manager get returns error", func(t *testing.T) {
		store := newMockOwnerStore()
		store.records[testSessionID] = &domain.AgentOwner{SessionID: testSessionID, OwnerIndex: 1}
		mgr := &mockManager{getErr: errors.New("no connection")}

		svc := NewProxyService(store, picker, mgr, &mockBinder{})

		err := svc.Connect(context.Background(), testSessionID, makeFirstFrame(testSessionID), &mockProxyStream{})

		if err == nil {
			t.Fatalf("Connect() expected error, got nil")
		}
		if status.Code(err) != codes.Internal {
			t.Fatalf("Connect() status = %v, want Internal", status.Code(err))
		}
	})

	t.Run("agent stream open error", func(t *testing.T) {
		store := newMockOwnerStore()
		store.records[testSessionID] = &domain.AgentOwner{SessionID: testSessionID, OwnerIndex: 1}
		agentMock := &mockAgentClient{connectErr: status.Error(codes.Unavailable, "agent unreachable")}
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		svc := NewProxyService(store, picker, &mockManager{}, &mockBinder{})

		err := svc.Connect(context.Background(), testSessionID, makeFirstFrame(testSessionID), &mockProxyStream{})

		if err == nil {
			t.Fatalf("Connect() expected error, got nil")
		}
		if status.Code(err) != codes.Internal {
			t.Fatalf("Connect() status = %v, want Internal", status.Code(err))
		}
	})

	t.Run("binder error", func(t *testing.T) {
		store := newMockOwnerStore()
		store.records[testSessionID] = &domain.AgentOwner{SessionID: testSessionID, OwnerIndex: 1}

		agentStream := &mockAgentStream{recvCh: make(<-chan *game.AgentFrame), sendCh: make(chan<- *game.AgentFrame)}
		agentMock := &mockAgentClient{agentStream: agentStream}
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		svc := NewProxyService(store, picker, &mockManager{}, &mockBinder{err: errors.New("bind failed")})

		err := svc.Connect(context.Background(), testSessionID, makeFirstFrame(testSessionID), &mockProxyStream{})

		if err == nil {
			t.Fatalf("Connect() expected error, got nil")
		}
	})
}

func TestMapDomainError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode codes.Code
	}{
		{name: "owner not found", err: domain.ErrOwnerNotFound, wantCode: codes.NotFound},
		{name: "owner already exists", err: domain.ErrOwnerAlreadyExists, wantCode: codes.AlreadyExists},
		{name: "no agent instances", err: domain.ErrNoAgentInstances, wantCode: codes.Unavailable},
		{name: "unknown error", err: errors.New("something else"), wantCode: codes.Unknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapDomainError(tt.err)

			if status.Code(got) != tt.wantCode {
				t.Fatalf("mapDomainError(%v) status = %v, want %v", tt.err, status.Code(got), tt.wantCode)
			}
		})
	}
}

// makeFirstFrame creates an AgentFrame with the given session_id.
func makeFirstFrame(sessionID string) *game.AgentFrame {
	return &game.AgentFrame{
		SessionId: sessionID,
		Payload:   &game.AgentFrame_Status{Status: &game.AgentStatusFrame{Status: "ready"}},
	}
}
