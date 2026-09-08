package handler

// Shared test doubles for the forwarding-handler tests (agent_test.go /
// bridge_test.go): owner-store, picker, manager, and stream fakes used by
// both files (style/large_test.md §反模式3 — shared helpers, not copied).

import (
	"context"
	"io"

	game "dominion/projects/game"
	"dominion/projects/game/pkg/bind"
	"dominion/projects/game/proxy/domain"
	"dominion/projects/game/proxy/runtime/agentclient"

	"google.golang.org/grpc/metadata"
)

// ownerKey returns the composite storage key (templateID, sessionID) of an
// owner record: a session is identified by the resource pattern
// templates/{template}/sessions/{session}, so the same session ID under
// different templates is a distinct record.
func ownerKey(templateID, sessionID string) string {
	return templateID + "\x00" + sessionID
}

// mockOwnerStore implements domain.OwnerStore for testing. createCalls counts
// Create invocations, so tests can assert that no allocation ran.
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

// raceOwnerStore simulates a concurrent allocation race: assignAgentOwner's
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

// mockProxyStream implements game.DesktopBridgeService_ConnectServer for
// testing. It is a bind.UserFrameStream (left side): Recv UserFrame / Send
// TeamFrame.
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
