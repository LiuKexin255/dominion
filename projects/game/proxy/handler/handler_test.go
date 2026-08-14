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

// ownerKey returns the composite storage key (templateID, sessionID) of an
// owner record: a session is identified by the resource pattern
// templates/{template}/sessions/{session}, so the same session ID under
// different templates is a distinct record.
func ownerKey(templateID, sessionID string) string {
	return templateID + "\x00" + sessionID
}

// mockOwnerStore implements domain.OwnerStore for testing. createCalls counts
// Create invocations, so tests can assert that assignOwner did not run.
type mockOwnerStore struct {
	records     map[string]*domain.AgentOwner
	getErr      error
	createCalls int
}

func newMockOwnerStore() *mockOwnerStore {
	return &mockOwnerStore{records: make(map[string]*domain.AgentOwner)}
}

func (s *mockOwnerStore) Create(_ context.Context, owner *domain.AgentOwner) error {
	s.createCalls++
	key := ownerKey(owner.TemplateID, owner.SessionID)
	if _, exists := s.records[key]; exists {
		return domain.ErrOwnerAlreadyExists
	}
	s.records[key] = owner
	return nil
}

func (s *mockOwnerStore) Get(_ context.Context, templateID, sessionID string) (*domain.AgentOwner, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	owner, exists := s.records[ownerKey(templateID, sessionID)]
	if !exists {
		return nil, domain.ErrOwnerNotFound
	}
	return owner, nil
}

func (s *mockOwnerStore) Delete(_ context.Context, templateID, sessionID string) error {
	key := ownerKey(templateID, sessionID)
	if _, exists := s.records[key]; !exists {
		return domain.ErrOwnerNotFound
	}
	delete(s.records, key)
	return nil
}

// mockManager implements agentclient.Manager for testing.
type mockManager struct {
	connRefs []*agentclient.ConnRef
	getErr   error
	listErr  error
	getCalls []int
}

func (m *mockManager) Get(_ context.Context, ownerIndex int) (*agentclient.ConnRef, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	m.getCalls = append(m.getCalls, ownerIndex)
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

// raceOwnerStore simulates a concurrent UpdateTeam allocation: assignOwner's
// initial Get misses (no owner yet), Create loses the race (another request
// already persisted its owner), and the follow-up Get returns the winner's
// record.
type raceOwnerStore struct {
	winner *domain.AgentOwner
	gets   int
}

func (s *raceOwnerStore) Create(_ context.Context, _ *domain.AgentOwner) error {
	return domain.ErrOwnerAlreadyExists
}

func (s *raceOwnerStore) Get(_ context.Context, _, _ string) (*domain.AgentOwner, error) {
	s.gets++
	if s.gets == 1 {
		return nil, domain.ErrOwnerNotFound
	}
	return s.winner, nil
}

func (s *raceOwnerStore) Delete(_ context.Context, _, _ string) error {
	return domain.ErrOwnerNotFound
}

// mockAgentClient implements agentclient.Client for testing.
type mockAgentClient struct {
	updateTeamResult   *game.Team
	updateTeamErr      error
	lastUpdateTeamReq  *game.UpdateTeamRequest
	getTeamResult      *game.Team
	getTeamErr         error
	listMessagesResult *game.ListMessagesResponse
	listMessagesErr    error
	connectErr         error
	agentStream        game.TeamService_ConnectClient
	refreshTeamResult  *emptypb.Empty
	refreshTeamErr     error
	lastRefreshReq     *game.RefreshTeamRequest
	lastGetTeamReq     *game.GetTeamRequest
}

func (c *mockAgentClient) UpdateTeam(_ context.Context, req *game.UpdateTeamRequest) (*game.Team, error) {
	c.lastUpdateTeamReq = req
	if c.updateTeamErr != nil {
		return nil, c.updateTeamErr
	}
	if c.updateTeamResult != nil {
		return c.updateTeamResult, nil
	}
	return &game.Team{Name: req.GetTeam().GetName()}, nil
}

func (c *mockAgentClient) GetTeam(_ context.Context, req *game.GetTeamRequest) (*game.Team, error) {
	c.lastGetTeamReq = req
	if c.getTeamErr != nil {
		return nil, c.getTeamErr
	}
	if c.getTeamResult != nil {
		return c.getTeamResult, nil
	}
	return &game.Team{
		Name: req.GetName(),
		Agents: []*game.TeamAgent{
			{Name: "player", AcceptsUserInput: true},
			{Name: "planner", AcceptsUserInput: false},
		},
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

func (c *mockAgentClient) Connect(_ context.Context, _ ...grpc.CallOption) (game.TeamService_ConnectClient, error) {
	if c.connectErr != nil {
		return nil, c.connectErr
	}
	return c.agentStream, nil
}

func (c *mockAgentClient) RefreshTeam(_ context.Context, req *game.RefreshTeamRequest) (*emptypb.Empty, error) {
	c.lastRefreshReq = req
	if c.refreshTeamErr != nil {
		return nil, c.refreshTeamErr
	}
	if c.refreshTeamResult != nil {
		return c.refreshTeamResult, nil
	}
	return &emptypb.Empty{}, nil
}

// mockAgentStream implements game.TeamService_ConnectClient for testing.
// It is a bind.TeamFrameStream (right side): Send UserFrame / Recv TeamFrame.
type mockAgentStream struct {
	recvCh  <-chan *game.TeamFrame
	sendCh  chan<- *game.UserFrame
	sendErr error
}

func (s *mockAgentStream) Recv() (*game.TeamFrame, error) {
	f, ok := <-s.recvCh
	if !ok {
		return nil, io.EOF
	}
	return f, nil
}

func (s *mockAgentStream) Send(f *game.UserFrame) error {
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

// mockProxyStream implements game.TeamService_ConnectServer for testing.
// It is a bind.UserFrameStream (left side): Recv UserFrame / Send TeamFrame.
type mockProxyStream struct {
	ctx    context.Context
	recvCh <-chan *game.UserFrame
	sendCh chan<- *game.TeamFrame
}

func (s *mockProxyStream) Recv() (*game.UserFrame, error) {
	f, ok := <-s.recvCh
	if !ok {
		return nil, io.EOF
	}
	return f, nil
}

func (s *mockProxyStream) Send(f *game.TeamFrame) error {
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

func (b *mockBinder) Bind(_ bind.UserFrameStream, _ bind.TeamFrameStream) error {
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

const (
	testTeamName       = "templates/saolei/sessions/sid/team"
	testMessagesParent = "templates/saolei/sessions/sid/team/agents/player"
)

func TestUpdateTeam(t *testing.T) {
	ctx := context.Background()
	picker := &mockOwnerPicker{ref: agentclient.ConnRef{OwnerIndex: 1, Owner: "agent-1"}}
	updateReq := &game.UpdateTeamRequest{
		Team: &game.Team{
			Name:    "templates/saolei/sessions/sid/team",
			Profile: "templates/saolei/profiles/default",
		},
		AllowMissing: true,
	}

	t.Run("materializes owner and forwards to the agent", func(t *testing.T) {
		store := newMockOwnerStore()
		mgr := &mockManager{connRefs: []*agentclient.ConnRef{{OwnerIndex: 1, Owner: "agent-1"}}}
		agentMock := &mockAgentClient{}
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		h := NewTeamHandler(store, picker, mgr, &mockBinder{})

		team, err := h.UpdateTeam(ctx, updateReq)

		if err != nil {
			t.Fatalf("UpdateTeam() unexpected error: %v", err)
		}
		if team.GetName() != "templates/saolei/sessions/sid/team" {
			t.Fatalf("UpdateTeam() name = %q, want %q", team.GetName(), "templates/saolei/sessions/sid/team")
		}
		// The owner was allocated (UpdateTeam's assignOwner is the only
		// allocation point).
		if _, ok := store.records[ownerKey("saolei", "sid")]; !ok {
			t.Fatal("UpdateTeam() did not allocate an owner")
		}
		// The owner record carries the template of the team's resource name
		// (composite key routing).
		if store.records[ownerKey("saolei", "sid")].TemplateID != "saolei" {
			t.Fatal("UpdateTeam() allocated owner without the template ID")
		}
		// The downstream agent received the caller's request unchanged.
		if agentMock.lastUpdateTeamReq != updateReq {
			t.Fatal("downstream UpdateTeam did not receive the caller's request")
		}
	})

	t.Run("reuses the existing owner on repeat update (idempotent)", func(t *testing.T) {
		store := newMockOwnerStore()
		store.records[ownerKey("saolei", "sid")] = &domain.AgentOwner{TemplateID: "saolei", SessionID: "sid", OwnerIndex: 1}
		// No connRefs: a pick would fail — proving the existing owner is reused.
		mgr := &mockManager{}
		agentMock := &mockAgentClient{}
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		h := NewTeamHandler(store, picker, mgr, &mockBinder{})

		team, err := h.UpdateTeam(ctx, updateReq)

		if err != nil {
			t.Fatalf("UpdateTeam() unexpected error: %v", err)
		}
		if team.GetName() != "templates/saolei/sessions/sid/team" {
			t.Fatalf("UpdateTeam() name = %q, want %q", team.GetName(), "templates/saolei/sessions/sid/team")
		}
		if len(store.records) != 1 {
			t.Fatalf("UpdateTeam() store records = %d, want 1 (owner not re-created)", len(store.records))
		}
	})

	t.Run("concurrent allocation race re-reads the winner's owner instead of AlreadyExists", func(t *testing.T) {
		// Simulates two UpdateTeam requests racing: assignOwner's initial Get
		// misses (Get #1), its Create loses to the other request
		// (ErrOwnerAlreadyExists), and the follow-up Get (#2) returns the
		// winner's record — the proxy-layer owner allocation is idempotent
		// (specs/040-team-singleton-conformance/research.md §R10).
		winner := &domain.AgentOwner{TemplateID: "saolei", SessionID: "sid", OwnerIndex: 2, Owner: "agent-2"}
		store := &raceOwnerStore{winner: winner}
		mgr := &mockManager{}
		agentMock := &mockAgentClient{}
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		h := NewTeamHandler(store, picker, mgr, &mockBinder{})

		team, err := h.UpdateTeam(ctx, updateReq)

		if err != nil {
			t.Fatalf("UpdateTeam() unexpected error: %v", err)
		}
		if team.GetName() != "templates/saolei/sessions/sid/team" {
			t.Fatalf("UpdateTeam() name = %q, want %q", team.GetName(), "templates/saolei/sessions/sid/team")
		}
		// The downstream agent was reached via the winner's owner (index 2,
		// not the picker's index 1) — proving the re-read, not a re-pick.
		if len(mgr.getCalls) != 1 || mgr.getCalls[0] != 2 {
			t.Fatalf("UpdateTeam() owner index used = %v, want [2] (winner re-read)", mgr.getCalls)
		}
		if agentMock.lastUpdateTeamReq != updateReq {
			t.Fatal("downstream UpdateTeam did not receive the caller's request")
		}
	})

	t.Run("existing owner is reused without a new owner-store Create", func(t *testing.T) {
		// assignOwner is get-or-create: with an existing owner the Get hits
		// and no Create runs — the routing resolution is idempotent.
		store := newMockOwnerStore()
		store.records[ownerKey("saolei", "sid")] = &domain.AgentOwner{TemplateID: "saolei", SessionID: "sid", OwnerIndex: 1}
		// No connRefs: a pick would fail — proving the existing owner is reused.
		mgr := &mockManager{}
		agentMock := &mockAgentClient{}
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		h := NewTeamHandler(store, picker, mgr, &mockBinder{})

		team, err := h.UpdateTeam(ctx, updateReq)

		if err != nil {
			t.Fatalf("UpdateTeam() unexpected error: %v", err)
		}
		if team.GetName() != "templates/saolei/sessions/sid/team" {
			t.Fatalf("UpdateTeam() name = %q, want %q", team.GetName(), "templates/saolei/sessions/sid/team")
		}
		if store.createCalls != 0 {
			t.Fatalf("UpdateTeam() owner-store Create calls = %d, want 0 (existing owner reused)", store.createCalls)
		}
		if agentMock.lastUpdateTeamReq != updateReq {
			t.Fatal("downstream UpdateTeam did not receive the caller's request")
		}
	})

	t.Run("allow_missing is forwarded to the agent unchanged", func(t *testing.T) {
		// The proxy is a routing layer: it does not inspect or rewrite
		// allow_missing — Team-resource semantics (materialize/NOT_FOUND/
		// idempotent/rebuild) belong to the agent's SessionTeamStore.update
		// (specs/040-team-singleton-conformance/contracts/api-contract.md §2.5).
		for _, allowMissing := range []bool{true, false} {
			store := newMockOwnerStore()
			mgr := &mockManager{connRefs: []*agentclient.ConnRef{{OwnerIndex: 1, Owner: "agent-1"}}}
			agentMock := &mockAgentClient{}
			restore := setMockNewAgentClient(agentMock)

			h := NewTeamHandler(store, picker, mgr, &mockBinder{})

			req := &game.UpdateTeamRequest{
				Team: &game.Team{
					Name:    "templates/saolei/sessions/sid/team",
					Profile: "templates/saolei/profiles/default",
				},
				AllowMissing: allowMissing,
			}

			if _, err := h.UpdateTeam(ctx, req); err != nil {
				t.Fatalf("UpdateTeam(allow_missing=%v) unexpected error: %v", allowMissing, err)
			}
			if agentMock.lastUpdateTeamReq.GetAllowMissing() != allowMissing {
				t.Fatalf("downstream allow_missing = %v, want %v (unchanged)", agentMock.lastUpdateTeamReq.GetAllowMissing(), allowMissing)
			}
			restore()
		}
	})

	t.Run("invalid team name returns InvalidArgument", func(t *testing.T) {
		h := NewTeamHandler(newMockOwnerStore(), picker, &mockManager{}, &mockBinder{})

		_, err := h.UpdateTeam(ctx, &game.UpdateTeamRequest{
			Team: &game.Team{
				Name:    "sessions/sid",
				Profile: "templates/saolei/profiles/default",
			},
		})

		if err == nil {
			t.Fatalf("UpdateTeam() expected error, got nil")
		}
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("UpdateTeam() status = %v, want InvalidArgument", status.Code(err))
		}
	})

	t.Run("no agent instances maps to Unavailable", func(t *testing.T) {
		store := newMockOwnerStore()
		picker := &mockOwnerPicker{err: domain.ErrNoAgentInstances}
		agentMock := &mockAgentClient{}
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		h := NewTeamHandler(store, picker, &mockManager{}, &mockBinder{})

		_, err := h.UpdateTeam(ctx, updateReq)

		if status.Code(err) != codes.Unavailable {
			t.Fatalf("UpdateTeam() status = %v, want Unavailable", status.Code(err))
		}
	})

	t.Run("downstream error propagates", func(t *testing.T) {
		store := newMockOwnerStore()
		mgr := &mockManager{connRefs: []*agentclient.ConnRef{{OwnerIndex: 1, Owner: "agent-1"}}}
		agentMock := &mockAgentClient{updateTeamErr: status.Error(codes.NotFound, "profile not found")}
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		h := NewTeamHandler(store, picker, mgr, &mockBinder{})

		_, err := h.UpdateTeam(ctx, updateReq)

		if status.Code(err) != codes.NotFound {
			t.Fatalf("UpdateTeam() status = %v, want NotFound", status.Code(err))
		}
	})
}

func TestGetTeam(t *testing.T) {
	ctx := context.Background()
	picker := &mockOwnerPicker{ref: agentclient.ConnRef{OwnerIndex: 1, Owner: "agent-1"}}

	t.Run("success with existing owner", func(t *testing.T) {
		store := newMockOwnerStore()
		store.records[ownerKey("saolei", "sid")] = &domain.AgentOwner{TemplateID: "saolei", SessionID: "sid", OwnerIndex: 1}
		agentMock := &mockAgentClient{}
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		h := NewTeamHandler(store, picker, &mockManager{}, &mockBinder{})

		team, err := h.GetTeam(ctx, &game.GetTeamRequest{Name: testTeamName})

		if err != nil {
			t.Fatalf("GetTeam() unexpected error: %v", err)
		}
		if team.GetName() != testTeamName {
			t.Fatalf("GetTeam() name = %q, want %q", team.GetName(), testTeamName)
		}
		if len(team.GetAgents()) != 2 {
			t.Fatalf("GetTeam() got %d agents, want 2", len(team.GetAgents()))
		}
		// downstream receives the caller's team name unchanged
		if agentMock.lastGetTeamReq.GetName() != testTeamName {
			t.Fatalf("downstream GetTeam name = %q, want %q", agentMock.lastGetTeamReq.GetName(), testTeamName)
		}
	})

	t.Run("missing owner returns NotFound without lazy creation", func(t *testing.T) {
		store := newMockOwnerStore()
		mgr := &mockManager{connRefs: []*agentclient.ConnRef{{OwnerIndex: 1, Owner: "agent-1"}}}
		agentMock := &mockAgentClient{}
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		h := NewTeamHandler(store, picker, mgr, &mockBinder{})

		_, err := h.GetTeam(ctx, &game.GetTeamRequest{Name: "templates/saolei/sessions/never-connected/team"})

		if err == nil {
			t.Fatalf("GetTeam() expected error, got nil")
		}
		if status.Code(err) != codes.NotFound {
			t.Fatalf("GetTeam() status = %v, want NotFound", status.Code(err))
		}
		if _, ok := store.records[ownerKey("saolei", "never-connected")]; ok {
			t.Fatal("GetTeam() unexpectedly created an owner")
		}
	})

	t.Run("invalid name returns InvalidArgument", func(t *testing.T) {
		h := NewTeamHandler(newMockOwnerStore(), picker, &mockManager{}, &mockBinder{})

		_, err := h.GetTeam(ctx, &game.GetTeamRequest{Name: "invalid-format"})

		if err == nil {
			t.Fatalf("GetTeam() expected error, got nil")
		}
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("GetTeam() status = %v, want InvalidArgument", status.Code(err))
		}
	})

	t.Run("agent error propagates", func(t *testing.T) {
		store := newMockOwnerStore()
		store.records[ownerKey("saolei", "sid")] = &domain.AgentOwner{TemplateID: "saolei", SessionID: "sid", OwnerIndex: 1}
		agentMock := &mockAgentClient{getTeamErr: status.Error(codes.NotFound, "team not found")}
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		h := NewTeamHandler(store, picker, &mockManager{}, &mockBinder{})

		_, err := h.GetTeam(ctx, &game.GetTeamRequest{Name: testTeamName})

		if status.Code(err) != codes.NotFound {
			t.Fatalf("GetTeam() status = %v, want NotFound", status.Code(err))
		}
	})

	t.Run("same session id under another template does not reuse the owner", func(t *testing.T) {
		// The owner exists only for template "saolei"; the same session id
		// under another template is a distinct session and must not reuse it.
		store := newMockOwnerStore()
		store.records[ownerKey("saolei", "sid")] = &domain.AgentOwner{TemplateID: "saolei", SessionID: "sid", OwnerIndex: 1}
		agentMock := &mockAgentClient{}
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		h := NewTeamHandler(store, picker, &mockManager{}, &mockBinder{})

		_, err := h.GetTeam(ctx, &game.GetTeamRequest{Name: "templates/other/sessions/sid/team"})

		if status.Code(err) != codes.NotFound {
			t.Fatalf("GetTeam() status = %v, want NotFound", status.Code(err))
		}
	})
}

func TestListMessages(t *testing.T) {
	ctx := context.Background()
	picker := &mockOwnerPicker{ref: agentclient.ConnRef{OwnerIndex: 1, Owner: "agent-1"}}

	t.Run("success with existing owner", func(t *testing.T) {
		store := newMockOwnerStore()
		store.records[ownerKey("saolei", "sid")] = &domain.AgentOwner{TemplateID: "saolei", SessionID: "sid", OwnerIndex: 1}
		agentMock := &mockAgentClient{}
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		h := NewTeamHandler(store, picker, &mockManager{}, &mockBinder{})

		resp, err := h.ListMessages(ctx, &game.ListMessagesRequest{Parent: testMessagesParent})

		if err != nil {
			t.Fatalf("ListMessages() unexpected error: %v", err)
		}
		if len(resp.GetMessages()) != 1 {
			t.Fatalf("ListMessages() got %d messages, want 1", len(resp.GetMessages()))
		}
	})

	t.Run("missing owner returns NotFound", func(t *testing.T) {
		h := NewTeamHandler(newMockOwnerStore(), picker, &mockManager{}, &mockBinder{})

		_, err := h.ListMessages(ctx, &game.ListMessagesRequest{Parent: "templates/saolei/sessions/missing/team/agents/player"})

		if status.Code(err) != codes.NotFound {
			t.Fatalf("ListMessages() status = %v, want NotFound", status.Code(err))
		}
	})

	t.Run("invalid parent returns InvalidArgument", func(t *testing.T) {
		h := NewTeamHandler(newMockOwnerStore(), picker, &mockManager{}, &mockBinder{})

		_, err := h.ListMessages(ctx, &game.ListMessagesRequest{Parent: "invalid-format"})

		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("ListMessages() status = %v, want InvalidArgument", status.Code(err))
		}
	})

	t.Run("invalid parent - missing agents segment returns InvalidArgument", func(t *testing.T) {
		h := NewTeamHandler(newMockOwnerStore(), picker, &mockManager{}, &mockBinder{})

		_, err := h.ListMessages(ctx, &game.ListMessagesRequest{Parent: "templates/saolei/sessions/sid/team"})

		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("ListMessages() status = %v, want InvalidArgument", status.Code(err))
		}
	})
}

func TestConnect(t *testing.T) {
	picker := &mockOwnerPicker{ref: agentclient.ConnRef{OwnerIndex: 1, Owner: "agent-1"}}

	t.Run("happy path with existing owner", func(t *testing.T) {
		store := newMockOwnerStore()
		store.records[ownerKey("saolei", "sid")] = &domain.AgentOwner{TemplateID: "saolei", SessionID: "sid", OwnerIndex: 1}

		agentStream := &mockAgentStream{recvCh: make(<-chan *game.TeamFrame), sendCh: make(chan<- *game.UserFrame)}
		agentMock := &mockAgentClient{agentStream: agentStream}
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		h := NewTeamHandler(store, picker, &mockManager{}, &mockBinder{})

		err := h.Connect(makeProxyStream("saolei", "sid"))

		if err != nil {
			t.Fatalf("Connect() unexpected error: %v", err)
		}
	})

	t.Run("missing owner returns NotFound without lazy creation", func(t *testing.T) {
		store := newMockOwnerStore()
		mgr := &mockManager{connRefs: []*agentclient.ConnRef{{OwnerIndex: 1, Owner: "agent-1"}}}

		agentStream := &mockAgentStream{recvCh: make(<-chan *game.TeamFrame), sendCh: make(chan<- *game.UserFrame)}
		agentMock := &mockAgentClient{agentStream: agentStream}
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		h := NewTeamHandler(store, picker, mgr, &mockBinder{})

		err := h.Connect(makeProxyStream("saolei", "new-session"))

		// Connect must NOT allocate an owner anymore — UpdateTeam is the only
		// allocation point (Agent 移除懒加载模式).
		if status.Code(err) != codes.NotFound {
			t.Fatalf("Connect() status = %v, want NotFound", status.Code(err))
		}
		if _, ok := store.records[ownerKey("saolei", "new-session")]; ok {
			t.Fatal("Connect() unexpectedly created an owner")
		}
	})

	t.Run("missing template_id", func(t *testing.T) {
		h := NewTeamHandler(newMockOwnerStore(), picker, &mockManager{}, &mockBinder{})

		err := h.Connect(makeProxyStream("", "sid"))

		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("Connect() status = %v, want InvalidArgument", status.Code(err))
		}
	})

	t.Run("missing session_id", func(t *testing.T) {
		h := NewTeamHandler(newMockOwnerStore(), picker, &mockManager{}, &mockBinder{})

		err := h.Connect(makeProxyStream("saolei", ""))

		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("Connect() status = %v, want InvalidArgument", status.Code(err))
		}
	})

	t.Run("recv error", func(t *testing.T) {
		h := NewTeamHandler(newMockOwnerStore(), picker, &mockManager{}, &mockBinder{})

		recvCh := make(chan *game.UserFrame)
		close(recvCh)
		stream := &mockProxyStream{ctx: context.Background(), recvCh: recvCh}

		err := h.Connect(stream)

		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("Connect() status = %v, want InvalidArgument", status.Code(err))
		}
	})

	t.Run("manager get returns error maps to Internal", func(t *testing.T) {
		store := newMockOwnerStore()
		store.records[ownerKey("saolei", "sid")] = &domain.AgentOwner{TemplateID: "saolei", SessionID: "sid", OwnerIndex: 1}
		mgr := &mockManager{getErr: errors.New("no connection")}

		h := NewTeamHandler(store, picker, mgr, &mockBinder{})

		err := h.Connect(makeProxyStream("saolei", "sid"))

		if status.Code(err) != codes.Internal {
			t.Fatalf("Connect() status = %v, want Internal", status.Code(err))
		}
	})

	t.Run("binder error propagates", func(t *testing.T) {
		store := newMockOwnerStore()
		store.records[ownerKey("saolei", "sid")] = &domain.AgentOwner{TemplateID: "saolei", SessionID: "sid", OwnerIndex: 1}

		agentStream := &mockAgentStream{recvCh: make(<-chan *game.TeamFrame), sendCh: make(chan<- *game.UserFrame)}
		agentMock := &mockAgentClient{agentStream: agentStream}
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		h := NewTeamHandler(store, picker, &mockManager{}, &mockBinder{err: errors.New("bind failed")})

		err := h.Connect(makeProxyStream("saolei", "sid"))

		if err == nil {
			t.Fatalf("Connect() expected error, got nil")
		}
	})
}

func TestRefreshTeam(t *testing.T) {
	ctx := context.Background()
	picker := &mockOwnerPicker{ref: agentclient.ConnRef{OwnerIndex: 1, Owner: "agent-1"}}

	t.Run("forwards to owner node", func(t *testing.T) {
		store := newMockOwnerStore()
		store.records[ownerKey("saolei", "sid")] = &domain.AgentOwner{TemplateID: "saolei", SessionID: "sid", OwnerIndex: 1}
		agentMock := &mockAgentClient{}
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		h := NewTeamHandler(store, picker, &mockManager{}, &mockBinder{})

		resp, err := h.RefreshTeam(ctx, &game.RefreshTeamRequest{Name: testTeamName})

		if err != nil {
			t.Fatalf("RefreshTeam() unexpected error: %v", err)
		}
		if resp == nil {
			t.Fatal("RefreshTeam() got nil response, want non-nil Empty")
		}
		if agentMock.lastRefreshReq == nil {
			t.Fatal("RefreshTeam() did not call downstream agent RefreshTeam")
		}
		if agentMock.lastRefreshReq.GetName() != testTeamName {
			t.Fatalf("downstream RefreshTeam name = %q, want %q", agentMock.lastRefreshReq.GetName(), testTeamName)
		}
	})

	t.Run("missing owner returns NotFound without lazy creation", func(t *testing.T) {
		store := newMockOwnerStore()
		mgr := &mockManager{connRefs: []*agentclient.ConnRef{{OwnerIndex: 1, Owner: "agent-1"}}}
		agentMock := &mockAgentClient{}
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		h := NewTeamHandler(store, picker, mgr, &mockBinder{})

		_, err := h.RefreshTeam(ctx, &game.RefreshTeamRequest{Name: "templates/saolei/sessions/no-owner/team"})

		if status.Code(err) != codes.NotFound {
			t.Fatalf("RefreshTeam() status = %v, want NotFound", status.Code(err))
		}
		if agentMock.lastRefreshReq != nil {
			t.Fatal("RefreshTeam() unexpectedly called downstream agent for missing session")
		}
		if _, ok := store.records[ownerKey("saolei", "no-owner")]; ok {
			t.Fatal("RefreshTeam() unexpectedly created owner")
		}
	})

	t.Run("invalid name returns InvalidArgument", func(t *testing.T) {
		h := NewTeamHandler(newMockOwnerStore(), picker, &mockManager{}, &mockBinder{})

		_, err := h.RefreshTeam(ctx, &game.RefreshTeamRequest{Name: ""})

		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("RefreshTeam() status = %v, want InvalidArgument", status.Code(err))
		}
	})

	t.Run("manager get returns error maps to Internal", func(t *testing.T) {
		store := newMockOwnerStore()
		store.records[ownerKey("saolei", "sid")] = &domain.AgentOwner{TemplateID: "saolei", SessionID: "sid", OwnerIndex: 1}
		mgr := &mockManager{getErr: errors.New("no connection")}
		restore := setMockNewAgentClient(&mockAgentClient{})
		defer restore()

		h := NewTeamHandler(store, picker, mgr, &mockBinder{})

		_, err := h.RefreshTeam(ctx, &game.RefreshTeamRequest{Name: testTeamName})

		if status.Code(err) != codes.Internal {
			t.Fatalf("RefreshTeam() status = %v, want Internal", status.Code(err))
		}
	})

	t.Run("downstream error propagates", func(t *testing.T) {
		store := newMockOwnerStore()
		store.records[ownerKey("saolei", "sid")] = &domain.AgentOwner{TemplateID: "saolei", SessionID: "sid", OwnerIndex: 1}
		agentMock := &mockAgentClient{refreshTeamErr: status.Error(codes.FailedPrecondition, "turn in flight")}
		restore := setMockNewAgentClient(agentMock)
		defer restore()

		h := NewTeamHandler(store, picker, &mockManager{}, &mockBinder{})

		_, err := h.RefreshTeam(ctx, &game.RefreshTeamRequest{Name: testTeamName})

		if status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("RefreshTeam() status = %v, want FailedPrecondition", status.Code(err))
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

func TestParseMessagesParent(t *testing.T) {
	tests := []struct {
		name        string
		parent      string
		want        string
		wantSession string
		wantAgent   string
		wantErr     bool
	}{
		{
			name:        "valid parent",
			parent:      "templates/saolei/sessions/abc/team/agents/player",
			want:        "saolei",
			wantSession: "abc",
			wantAgent:   "player",
		},
		{
			name:    "missing agents segment",
			parent:  "templates/saolei/sessions/abc/team",
			wantErr: true,
		},
		{
			name:    "old agent partition",
			parent:  "templates/saolei/sessions/abc/agent",
			wantErr: true,
		},
		{
			name:    "empty agent",
			parent:  "templates/saolei/sessions/abc/team/agents/",
			wantErr: true,
		},
		{
			name:    "extra segment",
			parent:  "templates/saolei/sessions/abc/team/agents/player/messages",
			wantErr: true,
		},
		{
			name:    "empty string",
			parent:  "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotSession, gotAgent, err := parseMessagesParent(tt.parent)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseMessagesParent(%q) expected error, got nil", tt.parent)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseMessagesParent(%q) unexpected error: %v", tt.parent, err)
			}
			if got != tt.want {
				t.Fatalf("parseMessagesParent(%q) template = %q, want %q", tt.parent, got, tt.want)
			}
			if gotSession != tt.wantSession {
				t.Fatalf("parseMessagesParent(%q) session = %q, want %q", tt.parent, gotSession, tt.wantSession)
			}
			if gotAgent != tt.wantAgent {
				t.Fatalf("parseMessagesParent(%q) agent = %q, want %q", tt.parent, gotAgent, tt.wantAgent)
			}
		})
	}
}

// makeProxyStream builds a mockProxyStream whose first Recv yields a status
// FlowPart UserFrame carrying the given template/session id pair (both bare
// segments; the gateway injects them from the connect URL path). status is a
// FlowPart kind (spec 023 C3 / FR-003 — specs/023-saolei-mcp-refine/contracts/content-model-contract.md §2).
func makeProxyStream(templateID, sessionID string) *mockProxyStream {
	recvCh := make(chan *game.UserFrame, 1)
	recvCh <- &game.UserFrame{
		TemplateId: templateID,
		SessionId:  sessionID,
		Payload: &game.UserFrame_FlowParts{FlowParts: &game.FlowParts{Parts: []*game.FlowPart{
			{Kind: &game.FlowPart_Status{Status: &game.StatusSignal{Status: game.StatusSignalStatus_STATUS_SIGNAL_STATUS_IDLE}}},
		}}},
	}
	close(recvCh)
	return &mockProxyStream{ctx: context.Background(), recvCh: recvCh}
}
