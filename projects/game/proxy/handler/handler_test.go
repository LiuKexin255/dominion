package handler

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
	"google.golang.org/protobuf/types/known/emptypb"
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
	getAgentErr        error
	listMessagesResult *game.ListMessagesResponse
	listMessagesErr    error
	connectErr         error
	agentStream        game.AgentService_ConnectClient
	refreshAgentResult *emptypb.Empty
	refreshAgentErr    error
	lastRefreshReq     *game.RefreshAgentRequest
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

func (c *mockAgentClient) RefreshAgent(_ context.Context, req *game.RefreshAgentRequest) (*emptypb.Empty, error) {
	c.lastRefreshReq = req
	if c.refreshAgentErr != nil {
		return nil, c.refreshAgentErr
	}
	if c.refreshAgentResult != nil {
		return c.refreshAgentResult, nil
	}
	return &emptypb.Empty{}, nil
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

const testAgentName = "sessions/sid/agent"

func TestGetAgent(t *testing.T) {
	ctx := context.Background()
	picker := &mockOwnerPicker{ref: agentclient.ConnRef{OwnerIndex: 1, Owner: "agent-1"}}

	t.Run("success with existing owner", func(t *testing.T) {
		store := newMockOwnerStore()
		store.records["sid"] = &domain.AgentOwner{SessionID: "sid", OwnerIndex: 1}
		agentMock := &mockAgentClient{}
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		h := NewProxyHandler(store, picker, &mockManager{}, &mockBinder{})

		agent, err := h.GetAgent(ctx, &game.GetAgentRequest{Name: testAgentName})

		if err != nil {
			t.Fatalf("GetAgent() unexpected error: %v", err)
		}
		if agent.GetSessionId() != "sid" {
			t.Fatalf("GetAgent().SessionId = %q, want %q", agent.GetSessionId(), "sid")
		}
	})

	t.Run("missing owner returns NotFound without lazy creation", func(t *testing.T) {
		store := newMockOwnerStore()
		mgr := &mockManager{connRefs: []*agentclient.ConnRef{{OwnerIndex: 1, Owner: "agent-1"}}}
		agentMock := &mockAgentClient{}
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		h := NewProxyHandler(store, picker, mgr, &mockBinder{})

		_, err := h.GetAgent(ctx, &game.GetAgentRequest{Name: "sessions/never-connected/agent"})

		if err == nil {
			t.Fatalf("GetAgent() expected error, got nil")
		}
		if status.Code(err) != codes.NotFound {
			t.Fatalf("GetAgent() status = %v, want NotFound", status.Code(err))
		}
		if _, ok := store.records["never-connected"]; ok {
			t.Fatal("GetAgent() unexpectedly created an owner")
		}
	})

	t.Run("invalid name returns InvalidArgument", func(t *testing.T) {
		h := NewProxyHandler(newMockOwnerStore(), picker, &mockManager{}, &mockBinder{})

		_, err := h.GetAgent(ctx, &game.GetAgentRequest{Name: "invalid-format"})

		if err == nil {
			t.Fatalf("GetAgent() expected error, got nil")
		}
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("GetAgent() status = %v, want InvalidArgument", status.Code(err))
		}
	})

	t.Run("agent error propagates", func(t *testing.T) {
		store := newMockOwnerStore()
		store.records["sid"] = &domain.AgentOwner{SessionID: "sid", OwnerIndex: 1}
		agentMock := &mockAgentClient{getAgentErr: status.Error(codes.NotFound, "agent not found")}
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		h := NewProxyHandler(store, picker, &mockManager{}, &mockBinder{})

		_, err := h.GetAgent(ctx, &game.GetAgentRequest{Name: testAgentName})

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

		h := NewProxyHandler(store, picker, &mockManager{}, &mockBinder{})

		resp, err := h.ListMessages(ctx, &game.ListMessagesRequest{Parent: "sessions/sid"})

		if err != nil {
			t.Fatalf("ListMessages() unexpected error: %v", err)
		}
		if len(resp.GetMessages()) != 1 {
			t.Fatalf("ListMessages() got %d messages, want 1", len(resp.GetMessages()))
		}
	})

	t.Run("missing owner returns NotFound", func(t *testing.T) {
		h := NewProxyHandler(newMockOwnerStore(), picker, &mockManager{}, &mockBinder{})

		_, err := h.ListMessages(ctx, &game.ListMessagesRequest{Parent: "sessions/missing"})

		if status.Code(err) != codes.NotFound {
			t.Fatalf("ListMessages() status = %v, want NotFound", status.Code(err))
		}
	})

	t.Run("invalid parent returns InvalidArgument", func(t *testing.T) {
		h := NewProxyHandler(newMockOwnerStore(), picker, &mockManager{}, &mockBinder{})

		_, err := h.ListMessages(ctx, &game.ListMessagesRequest{Parent: "invalid-format"})

		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("ListMessages() status = %v, want InvalidArgument", status.Code(err))
		}
	})
}

func TestConnectAgent(t *testing.T) {
	picker := &mockOwnerPicker{ref: agentclient.ConnRef{OwnerIndex: 1, Owner: "agent-1"}}

	t.Run("happy path with existing owner", func(t *testing.T) {
		store := newMockOwnerStore()
		store.records["sid"] = &domain.AgentOwner{SessionID: "sid", OwnerIndex: 1}

		agentStream := &mockAgentStream{recvCh: make(<-chan *game.AgentFrame), sendCh: make(chan<- *game.AgentFrame)}
		agentMock := &mockAgentClient{agentStream: agentStream}
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		h := NewProxyHandler(store, picker, &mockManager{}, &mockBinder{})

		err := h.ConnectAgent(makeProxyStream("sid"))

		if err != nil {
			t.Fatalf("ConnectAgent() unexpected error: %v", err)
		}
	})

	t.Run("lazy owner creation on connect", func(t *testing.T) {
		store := newMockOwnerStore()
		mgr := &mockManager{connRefs: []*agentclient.ConnRef{{OwnerIndex: 1, Owner: "agent-1"}}}

		agentStream := &mockAgentStream{recvCh: make(<-chan *game.AgentFrame), sendCh: make(chan<- *game.AgentFrame)}
		agentMock := &mockAgentClient{agentStream: agentStream}
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		h := NewProxyHandler(store, picker, mgr, &mockBinder{})

		err := h.ConnectAgent(makeProxyStream("new-session"))

		if err != nil {
			t.Fatalf("ConnectAgent() unexpected error: %v", err)
		}
		if _, ok := store.records["new-session"]; !ok {
			t.Fatal("ConnectAgent() did not create owner")
		}
	})

	t.Run("empty session_id", func(t *testing.T) {
		h := NewProxyHandler(newMockOwnerStore(), picker, &mockManager{}, &mockBinder{})

		err := h.ConnectAgent(makeProxyStream(""))

		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("ConnectAgent() status = %v, want InvalidArgument", status.Code(err))
		}
	})

	t.Run("recv error", func(t *testing.T) {
		h := NewProxyHandler(newMockOwnerStore(), picker, &mockManager{}, &mockBinder{})

		recvCh := make(chan *game.AgentFrame)
		close(recvCh)
		stream := &mockProxyStream{ctx: context.Background(), recvCh: recvCh}

		err := h.ConnectAgent(stream)

		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("ConnectAgent() status = %v, want InvalidArgument", status.Code(err))
		}
	})

	t.Run("manager get returns error maps to Internal", func(t *testing.T) {
		store := newMockOwnerStore()
		store.records["sid"] = &domain.AgentOwner{SessionID: "sid", OwnerIndex: 1}
		mgr := &mockManager{getErr: errors.New("no connection")}

		h := NewProxyHandler(store, picker, mgr, &mockBinder{})

		err := h.ConnectAgent(makeProxyStream("sid"))

		if status.Code(err) != codes.Internal {
			t.Fatalf("ConnectAgent() status = %v, want Internal", status.Code(err))
		}
	})

	t.Run("binder error propagates", func(t *testing.T) {
		store := newMockOwnerStore()
		store.records["sid"] = &domain.AgentOwner{SessionID: "sid", OwnerIndex: 1}

		agentStream := &mockAgentStream{recvCh: make(<-chan *game.AgentFrame), sendCh: make(chan<- *game.AgentFrame)}
		agentMock := &mockAgentClient{agentStream: agentStream}
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		h := NewProxyHandler(store, picker, &mockManager{}, &mockBinder{err: errors.New("bind failed")})

		err := h.ConnectAgent(makeProxyStream("sid"))

		if err == nil {
			t.Fatalf("ConnectAgent() expected error, got nil")
		}
	})
}

func TestRefreshAgent(t *testing.T) {
	ctx := context.Background()
	picker := &mockOwnerPicker{ref: agentclient.ConnRef{OwnerIndex: 1, Owner: "agent-1"}}

	t.Run("forwards to owner node", func(t *testing.T) {
		store := newMockOwnerStore()
		store.records["sid"] = &domain.AgentOwner{SessionID: "sid", OwnerIndex: 1}
		agentMock := &mockAgentClient{}
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		h := NewProxyHandler(store, picker, &mockManager{}, &mockBinder{})

		resp, err := h.RefreshAgent(ctx, &game.RefreshAgentRequest{Name: testAgentName})

		if err != nil {
			t.Fatalf("RefreshAgent() unexpected error: %v", err)
		}
		if resp == nil {
			t.Fatal("RefreshAgent() got nil response, want non-nil Empty")
		}
		if agentMock.lastRefreshReq == nil {
			t.Fatal("RefreshAgent() did not call downstream agent RefreshAgent")
		}
		if agentMock.lastRefreshReq.GetName() != testAgentName {
			t.Fatalf("downstream RefreshAgent name = %q, want %q", agentMock.lastRefreshReq.GetName(), testAgentName)
		}
	})

	t.Run("missing owner returns NotFound without lazy creation", func(t *testing.T) {
		store := newMockOwnerStore()
		mgr := &mockManager{connRefs: []*agentclient.ConnRef{{OwnerIndex: 1, Owner: "agent-1"}}}
		agentMock := &mockAgentClient{}
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		h := NewProxyHandler(store, picker, mgr, &mockBinder{})

		_, err := h.RefreshAgent(ctx, &game.RefreshAgentRequest{Name: "sessions/no-owner/agent"})

		if status.Code(err) != codes.NotFound {
			t.Fatalf("RefreshAgent() status = %v, want NotFound", status.Code(err))
		}
		if agentMock.lastRefreshReq != nil {
			t.Fatal("RefreshAgent() unexpectedly called downstream agent for missing session")
		}
		if _, ok := store.records["no-owner"]; ok {
			t.Fatal("RefreshAgent() unexpectedly created owner")
		}
	})

	t.Run("invalid name returns InvalidArgument", func(t *testing.T) {
		h := NewProxyHandler(newMockOwnerStore(), picker, &mockManager{}, &mockBinder{})

		_, err := h.RefreshAgent(ctx, &game.RefreshAgentRequest{Name: ""})

		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("RefreshAgent() status = %v, want InvalidArgument", status.Code(err))
		}
	})

	t.Run("manager get returns error maps to Internal", func(t *testing.T) {
		store := newMockOwnerStore()
		store.records["sid"] = &domain.AgentOwner{SessionID: "sid", OwnerIndex: 1}
		mgr := &mockManager{getErr: errors.New("no connection")}
		restore := setMockNewAgentClient(&mockAgentClient{})
		defer restore()

		h := NewProxyHandler(store, picker, mgr, &mockBinder{})

		_, err := h.RefreshAgent(ctx, &game.RefreshAgentRequest{Name: testAgentName})

		if status.Code(err) != codes.Internal {
			t.Fatalf("RefreshAgent() status = %v, want Internal", status.Code(err))
		}
	})

	t.Run("downstream error propagates", func(t *testing.T) {
		store := newMockOwnerStore()
		store.records["sid"] = &domain.AgentOwner{SessionID: "sid", OwnerIndex: 1}
		agentMock := &mockAgentClient{refreshAgentErr: status.Error(codes.FailedPrecondition, "turn in flight")}
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		h := NewProxyHandler(store, picker, &mockManager{}, &mockBinder{})

		_, err := h.RefreshAgent(ctx, &game.RefreshAgentRequest{Name: testAgentName})

		if status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("RefreshAgent() status = %v, want FailedPrecondition", status.Code(err))
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

// makeProxyStream builds a mockProxyStream whose first Recv yields a status
// frame carrying the given sessionID.
func makeProxyStream(sessionID string) *mockProxyStream {
	recvCh := make(chan *game.AgentFrame, 1)
	recvCh <- &game.AgentFrame{
		SessionId: sessionID,
		Payload:   &game.AgentFrame_Status{Status: &game.StatusSignal{Status: "ready"}},
	}
	close(recvCh)
	return &mockProxyStream{ctx: context.Background(), recvCh: recvCh}
}
