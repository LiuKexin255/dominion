package handler

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"testing"
	"time"

	game "dominion/projects/game"
	"dominion/projects/game/proxy/domain"
	"dominion/projects/game/proxy/runtime/agentclient"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// mockOwnerStore implements domain.OwnerStore for testing.
type mockOwnerStore struct {
	records map[string]*domain.AgentOwner
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
	initErr      error
	getStatusErr error
	deleteErr    error
}

func (c *mockAgentClient) CreateAgent(_ context.Context, _ *game.AgentCreateRequest) (*game.AgentStatus, error) {
	return new(game.AgentStatus), c.initErr
}

func (c *mockAgentClient) DeleteAgent(_ context.Context, _ *game.AgentDeleteRequest) (*emptypb.Empty, error) {
	return new(emptypb.Empty), c.deleteErr
}

func (c *mockAgentClient) GetAgentStatus(_ context.Context, _ *game.GetAgentStatusRequest) (*game.AgentStatus, error) {
	return new(game.AgentStatus), c.getStatusErr
}

func (c *mockAgentClient) Connect(_ context.Context, _ ...grpc.CallOption) (game.AgentService_ConnectClient, error) {
	return nil, nil
}

// mockManager implements agentclient.Manager for testing.
type mockManager struct {
	connRefs []*agentclient.ConnRef
	listErr  error
	getErr   error
}

func newMockManager() *mockManager {
	return &mockManager{}
}

func (m *mockManager) Get(_ context.Context, ownerIndex int) (*agentclient.ConnRef, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return &agentclient.ConnRef{
		OwnerIndex: ownerIndex,
		Owner:      fmt.Sprintf("agent-%d", ownerIndex),
	}, nil
}

func (m *mockManager) List(_ context.Context) ([]*agentclient.ConnRef, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.connRefs, nil
}

func (m *mockManager) Close() error { return nil }

// mockConnectAgenter implements domain.ConnectAgenter for testing.
type mockConnectAgenter struct {
	err error
}

func (m *mockConnectAgenter) Connect(_ game.ProxyService_ConnectAgentServer) error {
	return m.err
}

func twoConnRefs() []*agentclient.ConnRef {
	return []*agentclient.ConnRef{
		{OwnerIndex: 0, Owner: "agent-0"},
		{OwnerIndex: 1, Owner: "agent-1"},
	}
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

func TestCreateAgent(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		// given
		store := newMockOwnerStore()
		picker := &mockOwnerPicker{ref: agentclient.ConnRef{OwnerIndex: 1, Owner: "agent-1"}}
		agentMock := new(mockAgentClient)
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		mgr := newMockManager()
		mgr.connRefs = twoConnRefs()
		connectAgenter := new(mockConnectAgenter)

		h := NewProxyHandler(store, picker, mgr, connectAgenter)

		req := &game.CreateAgentRequest{
			Parent: "sessions/test-session-001",
		}

		// when
		agent, err := h.CreateAgent(ctx, req)

		// then
		if err != nil {
			t.Fatalf("CreateAgent() unexpected error: %v", err)
		}
		if agent.GetName() != "sessions/test-session-001/agent" {
			t.Fatalf("CreateAgent().Name = %q, want %q", agent.GetName(), "sessions/test-session-001/agent")
		}
		if agent.GetSessionId() != "test-session-001" {
			t.Fatalf("CreateAgent().SessionId = %q, want %q", agent.GetSessionId(), "test-session-001")
		}
		if agent.GetOwnerIndex() != 1 {
			t.Fatalf("CreateAgent().OwnerIndex = %d, want %d", agent.GetOwnerIndex(), 1)
		}
		if agent.GetOwner() != "agent-1" {
			t.Fatalf("CreateAgent().Owner = %q, want %q", agent.GetOwner(), "agent-1")
		}
	})

	t.Run("invalid parent", func(t *testing.T) {
		// given
		store := newMockOwnerStore()
		picker := &mockOwnerPicker{ref: agentclient.ConnRef{OwnerIndex: 0, Owner: "agent-0"}}
		agentMock := new(mockAgentClient)
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		mgr := newMockManager()
		mgr.connRefs = twoConnRefs()
		connectAgenter := new(mockConnectAgenter)

		h := NewProxyHandler(store, picker, mgr, connectAgenter)

		req := &game.CreateAgentRequest{
			Parent: "invalid-format",
		}

		// when
		_, err := h.CreateAgent(ctx, req)

		// then
		if err == nil {
			t.Fatalf("CreateAgent() expected error, got nil")
		}
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("CreateAgent() status = %v, want InvalidArgument", status.Code(err))
		}
	})

	t.Run("no agent instances returns unavailable", func(t *testing.T) {
		// given
		store := newMockOwnerStore()
		picker := &mockOwnerPicker{err: domain.ErrNoAgentInstances}
		agentMock := new(mockAgentClient)
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		mgr := newMockManager()
		mgr.connRefs = twoConnRefs()
		connectAgenter := new(mockConnectAgenter)

		h := NewProxyHandler(store, picker, mgr, connectAgenter)

		req := &game.CreateAgentRequest{
			Parent: "sessions/test-session",
		}

		// when
		_, err := h.CreateAgent(ctx, req)

		// then
		if err == nil {
			t.Fatalf("CreateAgent() expected error, got nil")
		}
		if status.Code(err) != codes.Unavailable {
			t.Fatalf("CreateAgent() status = %v, want Unavailable, err=%v", status.Code(err), err)
		}
	})

	t.Run("init_agent_fails", func(t *testing.T) {
		// given
		store := newMockOwnerStore()
		picker := &mockOwnerPicker{ref: agentclient.ConnRef{OwnerIndex: 1, Owner: "agent-1"}}
		agentMock := &mockAgentClient{initErr: errors.New("agent init failed")}
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		mgr := newMockManager()
		mgr.connRefs = twoConnRefs()
		connectAgenter := new(mockConnectAgenter)

		h := NewProxyHandler(store, picker, mgr, connectAgenter)

		req := &game.CreateAgentRequest{
			Parent: "sessions/test-session-002",
		}

		// when
		_, err := h.CreateAgent(ctx, req)

		// then
		if err == nil {
			t.Fatalf("CreateAgent() expected error, got nil")
		}
		if status.Code(err) != codes.Internal {
			t.Fatalf("CreateAgent() status = %v, want Internal, err=%v", status.Code(err), err)
		}
	})

	t.Run("owner already exists", func(t *testing.T) {
		// given
		store := newMockOwnerStore()
		existing := &domain.AgentOwner{
			SessionID:  "session-exist",
			OwnerIndex: 0,
			Owner:      "agent-0",
			CreateTime: time.Now(),
		}
		store.records["session-exist"] = existing

		picker := &mockOwnerPicker{ref: agentclient.ConnRef{OwnerIndex: 0, Owner: "agent-0"}}
		agentMock := new(mockAgentClient)
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		mgr := newMockManager()
		mgr.connRefs = twoConnRefs()
		connectAgenter := new(mockConnectAgenter)

		h := NewProxyHandler(store, picker, mgr, connectAgenter)

		req := &game.CreateAgentRequest{
			Parent: "sessions/session-exist",
		}

		// when
		_, err := h.CreateAgent(ctx, req)

		// then
		if err == nil {
			t.Fatalf("CreateAgent() expected error, got nil")
		}
		if status.Code(err) != codes.AlreadyExists {
			t.Fatalf("CreateAgent() status = %v, want AlreadyExists, err=%v", status.Code(err), err)
		}
	})
}

func TestGetAgent(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		// given
		store := newMockOwnerStore()
		owner := &domain.AgentOwner{
			SessionID:  "session-get",
			OwnerIndex: 2,
			Owner:      "agent-2",
			CreateTime: time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC),
		}
		store.records["session-get"] = owner

		picker := &mockOwnerPicker{ref: agentclient.ConnRef{OwnerIndex: 0, Owner: "agent-0"}}
		agentMock := new(mockAgentClient)
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		mgr := newMockManager()
		connectAgenter := new(mockConnectAgenter)

		h := NewProxyHandler(store, picker, mgr, connectAgenter)

		req := &game.GetAgentRequest{
			Name: "sessions/session-get/agent",
		}

		// when
		agent, err := h.GetAgent(ctx, req)

		// then
		if err != nil {
			t.Fatalf("GetAgent() unexpected error: %v", err)
		}
		if agent.GetName() != "sessions/session-get/agent" {
			t.Fatalf("GetAgent().Name = %q, want %q", agent.GetName(), "sessions/session-get/agent")
		}
		if agent.GetSessionId() != "session-get" {
			t.Fatalf("GetAgent().SessionId = %q, want %q", agent.GetSessionId(), "session-get")
		}
		if agent.GetOwnerIndex() != 2 {
			t.Fatalf("GetAgent().OwnerIndex = %d, want %d", agent.GetOwnerIndex(), 2)
		}
		if agent.GetOwner() != "agent-2" {
			t.Fatalf("GetAgent().Owner = %q, want %q", agent.GetOwner(), "agent-2")
		}
	})

	t.Run("not found", func(t *testing.T) {
		// given
		store := newMockOwnerStore()
		picker := &mockOwnerPicker{ref: agentclient.ConnRef{OwnerIndex: 0, Owner: "agent-0"}}
		agentMock := new(mockAgentClient)
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		mgr := newMockManager()
		connectAgenter := new(mockConnectAgenter)

		h := NewProxyHandler(store, picker, mgr, connectAgenter)

		req := &game.GetAgentRequest{
			Name: "sessions/nonexistent/agent",
		}

		// when
		_, err := h.GetAgent(ctx, req)

		// then
		if err == nil {
			t.Fatalf("GetAgent() expected error, got nil")
		}
		if status.Code(err) != codes.NotFound {
			t.Fatalf("GetAgent() status = %v, want NotFound, err=%v", status.Code(err), err)
		}
	})

	t.Run("get_status_fails", func(t *testing.T) {
		// given
		store := newMockOwnerStore()
		owner := &domain.AgentOwner{
			SessionID:  "session-get-status-fail",
			OwnerIndex: 0,
			Owner:      "agent-0",
			CreateTime: time.Now(),
		}
		store.records["session-get-status-fail"] = owner

		picker := &mockOwnerPicker{ref: agentclient.ConnRef{OwnerIndex: 0, Owner: "agent-0"}}
		agentMock := &mockAgentClient{getStatusErr: errors.New("agent status failed")}
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		mgr := newMockManager()
		connectAgenter := new(mockConnectAgenter)

		h := NewProxyHandler(store, picker, mgr, connectAgenter)

		req := &game.GetAgentRequest{
			Name: "sessions/session-get-status-fail/agent",
		}

		// when
		_, err := h.GetAgent(ctx, req)

		// then
		if err == nil {
			t.Fatalf("GetAgent() expected error, got nil")
		}
		if status.Code(err) != codes.Internal {
			t.Fatalf("GetAgent() status = %v, want Internal, err=%v", status.Code(err), err)
		}
	})

	t.Run("invalid name", func(t *testing.T) {
		// given
		store := newMockOwnerStore()
		picker := &mockOwnerPicker{ref: agentclient.ConnRef{OwnerIndex: 0, Owner: "agent-0"}}
		agentMock := new(mockAgentClient)
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		mgr := newMockManager()
		connectAgenter := new(mockConnectAgenter)

		h := NewProxyHandler(store, picker, mgr, connectAgenter)

		req := &game.GetAgentRequest{
			Name: "invalid-format",
		}

		// when
		_, err := h.GetAgent(ctx, req)

		// then
		if err == nil {
			t.Fatalf("GetAgent() expected error, got nil")
		}
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("GetAgent() status = %v, want InvalidArgument", status.Code(err))
		}
	})
}

func TestDeleteAgent(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		// given
		store := newMockOwnerStore()
		owner := &domain.AgentOwner{
			SessionID:  "session-del",
			OwnerIndex: 0,
			Owner:      "agent-0",
			CreateTime: time.Now(),
		}
		store.records["session-del"] = owner

		picker := &mockOwnerPicker{ref: agentclient.ConnRef{OwnerIndex: 0, Owner: "agent-0"}}
		agentMock := new(mockAgentClient)
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		mgr := newMockManager()
		connectAgenter := new(mockConnectAgenter)

		h := NewProxyHandler(store, picker, mgr, connectAgenter)

		req := &game.DeleteAgentRequest{
			Name: "sessions/session-del/agent",
		}

		// when
		_, err := h.DeleteAgent(ctx, req)

		// then
		if err != nil {
			t.Fatalf("DeleteAgent() unexpected error: %v", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		// given
		store := newMockOwnerStore()
		picker := &mockOwnerPicker{ref: agentclient.ConnRef{OwnerIndex: 0, Owner: "agent-0"}}
		agentMock := new(mockAgentClient)
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		mgr := newMockManager()
		connectAgenter := new(mockConnectAgenter)

		h := NewProxyHandler(store, picker, mgr, connectAgenter)

		req := &game.DeleteAgentRequest{
			Name: "sessions/nonexistent/agent",
		}

		// when
		_, err := h.DeleteAgent(ctx, req)

		// then
		if err == nil {
			t.Fatalf("DeleteAgent() expected error, got nil")
		}
		if status.Code(err) != codes.NotFound {
			t.Fatalf("DeleteAgent() status = %v, want NotFound, err=%v", status.Code(err), err)
		}
	})

	t.Run("invalid name", func(t *testing.T) {
		// given
		store := newMockOwnerStore()
		picker := &mockOwnerPicker{ref: agentclient.ConnRef{OwnerIndex: 0, Owner: "agent-0"}}
		agentMock := new(mockAgentClient)
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		mgr := newMockManager()
		connectAgenter := new(mockConnectAgenter)

		h := NewProxyHandler(store, picker, mgr, connectAgenter)

		req := &game.DeleteAgentRequest{
			Name: "invalid-format",
		}

		// when
		_, err := h.DeleteAgent(ctx, req)

		// then
		if err == nil {
			t.Fatalf("DeleteAgent() expected error, got nil")
		}
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("DeleteAgent() status = %v, want InvalidArgument", status.Code(err))
		}
	})
}

func Test_mapDomainError(t *testing.T) {
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
			// when
			got := mapDomainError(tt.err)

			// then
			if status.Code(got) != tt.wantCode {
				t.Fatalf("mapDomainError(%v) status = %v, want %v", tt.err, status.Code(got), tt.wantCode)
			}
		})
	}
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

			// when
			got, err := extractSessionID(tt.input, pattern)

			// then
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
