package service

import (
	"context"
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
	getErr  error
	listErr error
}

func (m *mockManager) Get(_ context.Context, ownerIndex int) (*agentclient.ConnRef, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return &agentclient.ConnRef{
		OwnerIndex: ownerIndex,
	}, nil
}

func (m *mockManager) List(_ context.Context) ([]*agentclient.ConnRef, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return nil, nil
}

func (m *mockManager) Close() error { return nil }

// mockAgentClient implements agentclient.Client for testing.
type mockAgentClient struct {
	connectErr  error
	agentStream game.AgentService_ConnectClient
}

func (c *mockAgentClient) CreateAgent(_ context.Context, _ *game.AgentCreateRequest) (*game.AgentStatus, error) {
	return new(game.AgentStatus), nil
}

func (c *mockAgentClient) DeleteAgent(_ context.Context, _ *game.AgentDeleteRequest) (*emptypb.Empty, error) {
	return new(emptypb.Empty), nil
}

func (c *mockAgentClient) GetAgentStatus(_ context.Context, _ *game.GetAgentStatusRequest) (*game.AgentStatus, error) {
	return new(game.AgentStatus), nil
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

// makeFirstFrame creates an AgentFrame with the given session_id.
func makeFirstFrame(sessionID string) *game.AgentFrame {
	return &game.AgentFrame{SessionId: sessionID, Payload: []byte("hello")}
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

func TestConnect(t *testing.T) {
	const testSessionID = "session-001"

	tests := []struct {
		name     string
		wantCode codes.Code
		wantErr  bool
		setup    func(t *testing.T) (domain.OwnerStore, agentclient.Manager, bind.Binder, game.ProxyService_ConnectAgentServer)
	}{
		{
			name:     "happy path",
			wantErr:  false,
			wantCode: codes.OK,
			setup: func(t *testing.T) (domain.OwnerStore, agentclient.Manager, bind.Binder, game.ProxyService_ConnectAgentServer) {
				// given: owner exists in store
				store := newMockOwnerStore()
				store.records[testSessionID] = &domain.AgentOwner{
					SessionID:  testSessionID,
					OwnerIndex: 1,
					Owner:      "agent-1",
				}

				// agent stream that accepts frames
				agentRecvCh := make(chan *game.AgentFrame, 8)
				agentSendCh := make(chan *game.AgentFrame, 8)
				agentStream := &mockAgentStream{
					recvCh: agentRecvCh,
					sendCh: agentSendCh,
				}

				agentMock := &mockAgentClient{agentStream: agentStream}
				restore := setMockNewAgentClient(agentMock)
				t.Cleanup(restore)

				mgr := &mockManager{}
				binder := &mockBinder{err: nil}

				// proxy stream sends one frame then closes
				proxyRecvCh := make(chan *game.AgentFrame, 8)
				proxyRecvCh <- makeFirstFrame(testSessionID)
				close(proxyRecvCh)
				proxySendCh := make(chan *game.AgentFrame, 8)
				proxyStream := &mockProxyStream{
					ctx:    context.Background(),
					recvCh: proxyRecvCh,
					sendCh: proxySendCh,
				}

				return store, mgr, binder, proxyStream
			},
		},
		{
			name:     "missing session_id",
			wantErr:  true,
			wantCode: codes.InvalidArgument,
			setup: func(t *testing.T) (domain.OwnerStore, agentclient.Manager, bind.Binder, game.ProxyService_ConnectAgentServer) {
				store := newMockOwnerStore()
				mgr := &mockManager{}
				binder := &mockBinder{}

				// first frame has empty session_id
				proxyRecvCh := make(chan *game.AgentFrame, 8)
				proxyRecvCh <- &game.AgentFrame{SessionId: "", Payload: []byte("hello")}
				close(proxyRecvCh)
				proxySendCh := make(chan *game.AgentFrame, 8)
				proxyStream := &mockProxyStream{
					ctx:    context.Background(),
					recvCh: proxyRecvCh,
					sendCh: proxySendCh,
				}

				return store, mgr, binder, proxyStream
			},
		},
		{
			name:     "owner not found",
			wantErr:  true,
			wantCode: codes.NotFound,
			setup: func(t *testing.T) (domain.OwnerStore, agentclient.Manager, bind.Binder, game.ProxyService_ConnectAgentServer) {
				// empty store — no owner for session
				store := newMockOwnerStore()
				mgr := &mockManager{}
				binder := &mockBinder{}

				proxyRecvCh := make(chan *game.AgentFrame, 8)
				proxyRecvCh <- makeFirstFrame(testSessionID)
				close(proxyRecvCh)
				proxySendCh := make(chan *game.AgentFrame, 8)
				proxyStream := &mockProxyStream{
					ctx:    context.Background(),
					recvCh: proxyRecvCh,
					sendCh: proxySendCh,
				}

				return store, mgr, binder, proxyStream
			},
		},
		{
			name:     "owner store returns other error",
			wantErr:  true,
			wantCode: codes.Internal,
			setup: func(t *testing.T) (domain.OwnerStore, agentclient.Manager, bind.Binder, game.ProxyService_ConnectAgentServer) {
				store := newMockOwnerStore()
				store.getErr = status.Error(codes.Internal, "db connection lost")
				mgr := &mockManager{}
				binder := &mockBinder{}

				proxyRecvCh := make(chan *game.AgentFrame, 8)
				proxyRecvCh <- makeFirstFrame(testSessionID)
				close(proxyRecvCh)
				proxySendCh := make(chan *game.AgentFrame, 8)
				proxyStream := &mockProxyStream{
					ctx:    context.Background(),
					recvCh: proxyRecvCh,
					sendCh: proxySendCh,
				}

				return store, mgr, binder, proxyStream
			},
		},
		{
			name:     "manager get returns error",
			wantErr:  true,
			wantCode: codes.Internal,
			setup: func(t *testing.T) (domain.OwnerStore, agentclient.Manager, bind.Binder, game.ProxyService_ConnectAgentServer) {
				store := newMockOwnerStore()
				store.records[testSessionID] = &domain.AgentOwner{
					SessionID:  testSessionID,
					OwnerIndex: 1,
					Owner:      "agent-1",
				}
				mgr := &mockManager{getErr: status.Error(codes.NotFound, "no connection for index 1")}
				binder := &mockBinder{}

				proxyRecvCh := make(chan *game.AgentFrame, 8)
				proxyRecvCh <- makeFirstFrame(testSessionID)
				close(proxyRecvCh)
				proxySendCh := make(chan *game.AgentFrame, 8)
				proxyStream := &mockProxyStream{
					ctx:    context.Background(),
					recvCh: proxyRecvCh,
					sendCh: proxySendCh,
				}

				return store, mgr, binder, proxyStream
			},
		},
		{
			name:     "agent stream open error",
			wantErr:  true,
			wantCode: codes.Internal,
			setup: func(t *testing.T) (domain.OwnerStore, agentclient.Manager, bind.Binder, game.ProxyService_ConnectAgentServer) {
				store := newMockOwnerStore()
				store.records[testSessionID] = &domain.AgentOwner{
					SessionID:  testSessionID,
					OwnerIndex: 1,
					Owner:      "agent-1",
				}

				agentMock := &mockAgentClient{connectErr: status.Error(codes.Unavailable, "agent unreachable")}
				restore := setMockNewAgentClient(agentMock)
				t.Cleanup(restore)

				mgr := &mockManager{}
				binder := &mockBinder{}

				proxyRecvCh := make(chan *game.AgentFrame, 8)
				proxyRecvCh <- makeFirstFrame(testSessionID)
				close(proxyRecvCh)
				proxySendCh := make(chan *game.AgentFrame, 8)
				proxyStream := &mockProxyStream{
					ctx:    context.Background(),
					recvCh: proxyRecvCh,
					sendCh: proxySendCh,
				}

				return store, mgr, binder, proxyStream
			},
		},
		{
			name:     "first frame recv error",
			wantErr:  true,
			wantCode: codes.InvalidArgument,
			setup: func(t *testing.T) (domain.OwnerStore, agentclient.Manager, bind.Binder, game.ProxyService_ConnectAgentServer) {
				store := newMockOwnerStore()
				mgr := &mockManager{}
				binder := &mockBinder{}

				// proxy stream is already closed — Recv returns EOF immediately
				proxyRecvCh := make(chan *game.AgentFrame, 8)
				close(proxyRecvCh)
				proxySendCh := make(chan *game.AgentFrame, 8)
				proxyStream := &mockProxyStream{
					ctx:    context.Background(),
					recvCh: proxyRecvCh,
					sendCh: proxySendCh,
				}

				return store, mgr, binder, proxyStream
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			ownerStore, mgr, binder, proxyStream := tt.setup(t)

			ca := NewConnectAgenter(ownerStore, mgr, binder)

			// when
			err := ca.Connect(proxyStream)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Connect() expected error with code %v, got nil", tt.wantCode)
				}
				if status.Code(err) != tt.wantCode {
					t.Fatalf("Connect() status = %v, want %v, err=%v", status.Code(err), tt.wantCode, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Connect() unexpected error: %v", err)
			}
		})
	}
}

func TestConnect_binderError(t *testing.T) {
	const testSessionID = "session-binder"

	// given: full setup with a binder that returns an error
	store := newMockOwnerStore()
	store.records[testSessionID] = &domain.AgentOwner{
		SessionID:  testSessionID,
		OwnerIndex: 1,
		Owner:      "agent-1",
	}

	agentRecvCh := make(chan *game.AgentFrame, 8)
	agentSendCh := make(chan *game.AgentFrame, 8)
	agentStream := &mockAgentStream{
		recvCh: agentRecvCh,
		sendCh: agentSendCh,
	}

	agentMock := &mockAgentClient{agentStream: agentStream}
	restore := setMockNewAgentClient(agentMock)
	defer restore()

	mgr := &mockManager{}
	binder := &mockBinder{err: status.Error(codes.Internal, "bind failed")}

	proxyRecvCh := make(chan *game.AgentFrame, 8)
	proxyRecvCh <- makeFirstFrame(testSessionID)
	close(proxyRecvCh)
	proxySendCh := make(chan *game.AgentFrame, 8)
	proxyStream := &mockProxyStream{
		ctx:    context.Background(),
		recvCh: proxyRecvCh,
		sendCh: proxySendCh,
	}

	ca := NewConnectAgenter(store, mgr, binder)

	// when
	err := ca.Connect(proxyStream)

	// then
	if err == nil {
		t.Fatalf("Connect() expected error, got nil")
	}
	if status.Code(err) != codes.Internal {
		t.Fatalf("Connect() status = %v, want Internal, err=%v", status.Code(err), err)
	}
}

func TestConnect_firstFrameSendError(t *testing.T) {
	const testSessionID = "session-send-err"

	// given: agent stream Send returns error on first call
	store := newMockOwnerStore()
	store.records[testSessionID] = &domain.AgentOwner{
		SessionID:  testSessionID,
		OwnerIndex: 1,
		Owner:      "agent-1",
	}

	agentRecvCh := make(chan *game.AgentFrame, 8)
	agentSendCh := make(chan *game.AgentFrame, 8)
	agentStream := &mockAgentStream{
		recvCh:  agentRecvCh,
		sendCh:  agentSendCh,
		sendErr: status.Error(codes.Internal, "send failed"),
	}

	agentMock := &mockAgentClient{agentStream: agentStream}
	restore := setMockNewAgentClient(agentMock)
	defer restore()

	mgr := &mockManager{}

	// Use a binder that reads the prefixed first frame via Recv, then
	// attempts Send on the agent stream which fails.
	firstFrameSendBinder := &firstFrameSendErrorBinder{}

	proxyRecvCh := make(chan *game.AgentFrame, 8)
	proxyRecvCh <- makeFirstFrame(testSessionID)
	close(proxyRecvCh)
	proxySendCh := make(chan *game.AgentFrame, 8)
	proxyStream := &mockProxyStream{
		ctx:    context.Background(),
		recvCh: proxyRecvCh,
		sendCh: proxySendCh,
	}

	ca := NewConnectAgenter(store, mgr, firstFrameSendBinder)

	// when
	err := ca.Connect(proxyStream)

	// then
	if err == nil {
		t.Fatalf("Connect() expected error, got nil")
	}
}

// firstFrameSendErrorBinder reads one frame from left via Recv then returns
// an error to simulate the first frame failing to forward to the agent.
type firstFrameSendErrorBinder struct{}

func (b *firstFrameSendErrorBinder) Bind(left bind.AgentFrameStream, _ bind.AgentFrameStream) error {
	_, err := left.Recv()
	if err != nil {
		return err
	}
	return status.Error(codes.Internal, "send failed")
}
