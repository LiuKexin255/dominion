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
	"google.golang.org/grpc/status"
)

// fakeSendStream is the upstream AgentService_SendClient double: the
// response half of a server-streaming call — the request was already sent
// and half-closed by the stream-open call (recorded on the client double) —
// so it replays scripted frames before a final Recv error.
type fakeSendStream struct {
	grpc.ClientStream

	frames  []*game.ChatEvent
	recvErr error
}

func (s *fakeSendStream) Recv() (*game.ChatEvent, error) {
	if len(s.frames) > 0 {
		frame := s.frames[0]
		s.frames = s.frames[1:]
		return frame, nil
	}
	return nil, s.recvErr
}

// fakeSendServer is the downstream AgentService_SendServer double: the
// relayed frame recorder with a real context for the handler.
type fakeSendServer struct {
	grpc.ServerStream

	ctx    context.Context
	frames []*game.ChatEvent
}

func (s *fakeSendServer) Context() context.Context { return s.ctx }

func (s *fakeSendServer) Send(event *game.ChatEvent) error {
	s.frames = append(s.frames, event)
	return nil
}

// fakeAgentClient is the downstream agent_v2 client double. The generated
// client writes the Send request while opening the stream (generic stream
// shape), so that request is recorded here rather than on the stream; the
// unary requests are recorded per method.
type fakeAgentClient struct {
	sendStream *fakeSendStream
	sendErr    error
	sendReq    *game.SendRequest

	updateTeamResult *game.Team
	updateTeamErr    error
	updateTeamReq    *game.UpdateTeamRequest

	getTeamResult *game.Team
	getTeamErr    error
	getTeamReq    *game.GetTeamRequest

	getMemberResult *game.TeamMember
	getMemberErr    error
	getMemberReq    *game.GetTeamMemberRequest

	listTeamResult *game.ListTeamMessagesResponse
	listTeamErr    error
	listTeamReq    *game.ListTeamMessagesRequest

	listMemberResult *game.ListMemberMessagesResponse
	listMemberErr    error
	listMemberReq    *game.ListMemberMessagesRequest

	cancelErr error
	cancelReq *game.CancelRequest
}

func (c *fakeAgentClient) Send(_ context.Context, req *game.SendRequest, _ ...grpc.CallOption) (game.AgentService_SendClient, error) {
	c.sendReq = req
	if c.sendErr != nil {
		return nil, c.sendErr
	}
	return c.sendStream, nil
}

func (c *fakeAgentClient) UpdateTeam(_ context.Context, req *game.UpdateTeamRequest, _ ...grpc.CallOption) (*game.Team, error) {
	c.updateTeamReq = req
	if c.updateTeamErr != nil {
		return nil, c.updateTeamErr
	}
	if c.updateTeamResult != nil {
		return c.updateTeamResult, nil
	}
	return &game.Team{Name: req.GetTeam().GetName()}, nil
}

func (c *fakeAgentClient) GetTeam(_ context.Context, req *game.GetTeamRequest, _ ...grpc.CallOption) (*game.Team, error) {
	c.getTeamReq = req
	if c.getTeamErr != nil {
		return nil, c.getTeamErr
	}
	if c.getTeamResult != nil {
		return c.getTeamResult, nil
	}
	return &game.Team{Name: req.GetName()}, nil
}

func (c *fakeAgentClient) GetTeamMember(_ context.Context, req *game.GetTeamMemberRequest, _ ...grpc.CallOption) (*game.TeamMember, error) {
	c.getMemberReq = req
	if c.getMemberErr != nil {
		return nil, c.getMemberErr
	}
	if c.getMemberResult != nil {
		return c.getMemberResult, nil
	}
	return &game.TeamMember{Name: req.GetName()}, nil
}

func (c *fakeAgentClient) ListTeamMessages(_ context.Context, req *game.ListTeamMessagesRequest, _ ...grpc.CallOption) (*game.ListTeamMessagesResponse, error) {
	c.listTeamReq = req
	if c.listTeamErr != nil {
		return nil, c.listTeamErr
	}
	if c.listTeamResult != nil {
		return c.listTeamResult, nil
	}
	return &game.ListTeamMessagesResponse{}, nil
}

func (c *fakeAgentClient) ListMemberMessages(_ context.Context, req *game.ListMemberMessagesRequest, _ ...grpc.CallOption) (*game.ListMemberMessagesResponse, error) {
	c.listMemberReq = req
	if c.listMemberErr != nil {
		return nil, c.listMemberErr
	}
	if c.listMemberResult != nil {
		return c.listMemberResult, nil
	}
	return &game.ListMemberMessagesResponse{}, nil
}

// Cancel mirrors the unary shape; the response is an empty message, so no
// result field is configurable.
func (c *fakeAgentClient) Cancel(_ context.Context, req *game.CancelRequest, _ ...grpc.CallOption) (*game.CancelResponse, error) {
	c.cancelReq = req
	if c.cancelErr != nil {
		return nil, c.cancelErr
	}
	return &game.CancelResponse{}, nil
}

// setFakeAgentClient replaces the client constructor seam and restores it on
// cleanup, so handler methods can be driven against the double without a
// real agent_v2 connection.
func setFakeAgentClient(t *testing.T, fake *fakeAgentClient) {
	t.Helper()
	old := newAgentClient
	newAgentClient = func(_ *grpc.ClientConn) game.AgentServiceClient {
		return fake
	}
	t.Cleanup(func() { newAgentClient = old })
}

// newAgentHarness wires an AgentHandler with the shared test doubles; the
// manager is pre-populated so owner resolution succeeds unless a test
// overrides it.
func newAgentHarness(t *testing.T, fake *fakeAgentClient) (*AgentHandler, *mockOwnerStore, *mockManager, *mockOwnerPicker) {
	t.Helper()
	setFakeAgentClient(t, fake)
	store := newMockOwnerStore()
	manager := &mockManager{}
	picker := &mockOwnerPicker{ref: agentclient.ConnRef{OwnerIndex: 2, Owner: "agent-2"}}
	handler := NewAgentHandler(store, picker, manager, bind.NewServerStreamBinder[game.ChatEvent]())
	return handler, store, manager, picker
}

// seedAgentOwner stores an existing owner for the test session, standing in
// for a previous UpdateTeam materialization.
func seedAgentOwner(store *mockOwnerStore, ownerIndex int) {
	store.records[ownerKey("saolei", "conv-1")] = &domain.AgentOwner{
		TemplateID: "saolei",
		SessionID:  "conv-1",
		OwnerIndex: ownerIndex,
		Owner:      "agent-owner",
	}
}

const agentSession = "templates/saolei/sessions/conv-1"
const teamResource = agentSession + "/team"
const playerMember = teamResource + "/members/player"

func chatFrame(text string) *game.ChatEvent {
	return &game.ChatEvent{
		Session: agentSession,
		Member:  "player",
		Payload: &game.ChatEvent_Delta{Delta: &game.BlockDeltaEvent{Index: 0, Text: text}},
	}
}

func TestAgentHandler_Send_NoOwnerReturnsNotFoundWithoutAllocation(t *testing.T) {
	// given: a fresh store — no UpdateTeam ever materialized the session
	fake := &fakeAgentClient{sendStream: &fakeSendStream{recvErr: io.EOF}}
	handler, store, _, _ := newAgentHarness(t, fake)

	// when
	err := handler.Send(&game.SendRequest{Session: agentSession, Text: "hi"}, &fakeSendServer{ctx: context.Background()})

	// then: Send looks the owner up only — NOT_FOUND, no owner record
	if status.Code(err) != codes.NotFound {
		t.Fatalf("Send() code = %v, want NotFound", status.Code(err))
	}
	if store.createCalls != 0 {
		t.Fatalf("owner Create calls = %d, want 0 (Send must not allocate)", store.createCalls)
	}
}

func TestAgentHandler_Send_RelaysFramesForExistingOwner(t *testing.T) {
	// given: an owner from a previous UpdateTeam and an upstream streaming
	// two deltas then io.EOF
	upstream := &fakeSendStream{
		frames:  []*game.ChatEvent{chatFrame("a"), chatFrame("b")},
		recvErr: io.EOF,
	}
	fake := &fakeAgentClient{sendStream: upstream}
	handler, store, manager, _ := newAgentHarness(t, fake)
	seedAgentOwner(store, 1)
	server := &fakeSendServer{ctx: context.Background()}

	// when: one Send round-trips
	err := handler.Send(&game.SendRequest{Session: agentSession, Text: "hi"}, server)

	// then: the owner is reused (no allocation), the request reaches the
	// upstream via its instance, and every frame is relayed in order
	if err != nil {
		t.Fatalf("Send() error = %v, want nil", err)
	}
	if store.createCalls != 0 {
		t.Fatalf("owner Create calls = %d, want 0", store.createCalls)
	}
	if len(manager.getCalls) != 1 || manager.getCalls[0] != 1 {
		t.Fatalf("manager Get calls = %v, want [1] (seeded owner index)", manager.getCalls)
	}
	if fake.sendReq.GetText() != "hi" {
		t.Fatalf("downstream request text = %q, want %q", fake.sendReq.GetText(), "hi")
	}
	if len(server.frames) != 2 || server.frames[0].GetDelta().GetText() != "a" || server.frames[1].GetDelta().GetText() != "b" {
		t.Fatalf("relayed frames = %d, want [a b]", len(server.frames))
	}
}

func TestAgentHandler_Send_InvalidSessionRejectedWithoutAllocation(t *testing.T) {
	tests := []struct {
		name    string
		session string
	}{
		{name: "malformed resource name", session: "projects/p1"},
		{name: "unknown template", session: "templates/unknown/sessions/s1"},
		{name: "empty session segment", session: "templates/saolei/sessions/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeAgentClient{sendStream: &fakeSendStream{recvErr: io.EOF}}
			handler, store, _, _ := newAgentHarness(t, fake)

			err := handler.Send(&game.SendRequest{Session: tt.session, Text: "hi"}, &fakeSendServer{ctx: context.Background()})

			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("Send() code = %v, want InvalidArgument", status.Code(err))
			}
			if store.createCalls != 0 {
				t.Fatalf("owner Create calls = %d, want 0 (no allocation on invalid input)", store.createCalls)
			}
		})
	}
}

func TestAgentHandler_Send_InstanceUnreachable(t *testing.T) {
	// given: the owner exists but its instance has no cached connection
	upstream := &fakeSendStream{recvErr: io.EOF}
	fake := &fakeAgentClient{sendStream: upstream}
	handler, store, manager, _ := newAgentHarness(t, fake)
	seedAgentOwner(store, 7)
	manager.getErr = errors.New("no connection for owner index 7")

	// when
	err := handler.Send(&game.SendRequest{Session: agentSession, Text: "hi"}, &fakeSendServer{ctx: context.Background()})

	// then: proxy→agent_v2 break maps to UNAVAILABLE (503)
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("Send() code = %v, want Unavailable", status.Code(err))
	}
}

func TestAgentHandler_Send_UpstreamOpenFailed(t *testing.T) {
	// given: the upstream refuses the stream with a non-status transport
	// failure — a proxy→agent_v2 hop break maps to UNAVAILABLE
	fake := &fakeAgentClient{sendErr: errors.New("connection refused")}
	handler, store, _, _ := newAgentHarness(t, fake)
	seedAgentOwner(store, 1)

	err := handler.Send(&game.SendRequest{Session: agentSession, Text: "hi"}, &fakeSendServer{ctx: context.Background()})

	if status.Code(err) != codes.Unavailable {
		t.Fatalf("Send() code = %v, want Unavailable", status.Code(err))
	}
}

func TestAgentHandler_Send_UpstreamRejectsRequestWithOriginalCode(t *testing.T) {
	// given: agent_v2 rejects the request at stream open with a gRPC status
	// — the proxy must preserve the agent-level code so the front end sees
	// the mapped HTTP 400, not a 503 hop failure.
	fake := &fakeAgentClient{sendErr: status.Error(codes.FailedPrecondition, "team not materialized; send UpdateTeam first")}
	handler, store, _, _ := newAgentHarness(t, fake)
	seedAgentOwner(store, 1)

	err := handler.Send(&game.SendRequest{Session: agentSession, Text: "hi"}, &fakeSendServer{ctx: context.Background()})

	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("Send() code = %v, want FailedPrecondition (original code preserved)", status.Code(err))
	}
}

func TestAgentHandler_Send_EmptyTextRejectedBeforeOwnerLookup(t *testing.T) {
	// given: a valid resource name but an empty message — the routing-layer
	// validation rejects it before any owner interaction
	fake := &fakeAgentClient{sendStream: &fakeSendStream{recvErr: io.EOF}}
	handler, store, _, _ := newAgentHarness(t, fake)

	err := handler.Send(&game.SendRequest{Session: agentSession, Text: ""}, &fakeSendServer{ctx: context.Background()})

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Send() code = %v, want InvalidArgument", status.Code(err))
	}
	if store.createCalls != 0 {
		t.Fatalf("owner Create calls = %d, want 0", store.createCalls)
	}
}

func TestAgentHandler_Send_UpstreamStatusPassthrough(t *testing.T) {
	// given: the upstream fails mid-stream with a gRPC status — the proxy
	// must not rewrite the agent-level code
	upstream := &fakeSendStream{
		frames:  []*game.ChatEvent{chatFrame("partial")},
		recvErr: status.Error(codes.InvalidArgument, "empty text"),
	}
	fake := &fakeAgentClient{sendStream: upstream}
	handler, store, _, _ := newAgentHarness(t, fake)
	seedAgentOwner(store, 1)
	server := &fakeSendServer{ctx: context.Background()}

	err := handler.Send(&game.SendRequest{Session: agentSession, Text: "hi"}, server)

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Send() code = %v, want InvalidArgument (original code preserved)", status.Code(err))
	}
	if len(server.frames) != 1 {
		t.Fatalf("relayed frames = %d, want the 1 frame produced before the failure", len(server.frames))
	}
}

func TestAgentHandler_UpdateTeam_AllocatesOwnerAndForwards(t *testing.T) {
	// given: a fresh store — the first materialization of the session
	fake := &fakeAgentClient{}
	handler, store, manager, _ := newAgentHarness(t, fake)
	req := &game.UpdateTeamRequest{Team: &game.Team{
		Name: teamResource,
		Members: []*game.TeamMember{
			{Role: "player", Preset: "templates/saolei/presets/player"},
			{Role: "planner", Preset: "templates/saolei/presets/planner"},
		},
	}}

	// when
	team, err := handler.UpdateTeam(context.Background(), req)

	// then: the owner is allocated once and the request reaches the
	// upstream unchanged
	if err != nil {
		t.Fatalf("UpdateTeam() error = %v, want nil", err)
	}
	if store.createCalls != 1 {
		t.Fatalf("owner Create calls = %d, want 1 (UpdateTeam is the allocation point)", store.createCalls)
	}
	owner := store.records[ownerKey("saolei", "conv-1")]
	if owner == nil || owner.TemplateID != "saolei" || owner.SessionID != "conv-1" {
		t.Fatalf("allocated owner = %+v, want the (saolei, conv-1) composite key", owner)
	}
	if len(manager.getCalls) != 1 || manager.getCalls[0] != owner.OwnerIndex {
		t.Fatalf("manager Get calls = %v, want [allocated index]", manager.getCalls)
	}
	if fake.updateTeamReq != req {
		t.Fatal("downstream UpdateTeam did not receive the caller's request")
	}
	if team.GetName() != teamResource {
		t.Fatalf("team name = %q, want %q", team.GetName(), teamResource)
	}
}

func TestAgentHandler_UpdateTeam_ReusesExistingOwnerWithoutAllocation(t *testing.T) {
	// given: the owner already exists (refresh case) and no instances are
	// listed — a re-pick would fail, proving the existing owner is reused
	fake := &fakeAgentClient{}
	handler, store, manager, _ := newAgentHarness(t, fake)
	seedAgentOwner(store, 1)

	team, err := handler.UpdateTeam(context.Background(), &game.UpdateTeamRequest{Team: &game.Team{Name: teamResource}})

	if err != nil {
		t.Fatalf("UpdateTeam() error = %v, want nil", err)
	}
	if store.createCalls != 0 {
		t.Fatalf("owner Create calls = %d, want 0 (existing owner reused)", store.createCalls)
	}
	if len(manager.getCalls) != 1 || manager.getCalls[0] != 1 {
		t.Fatalf("manager Get calls = %v, want [1]", manager.getCalls)
	}
	if team.GetName() != teamResource {
		t.Fatalf("team name = %q, want %q", team.GetName(), teamResource)
	}
}

func TestAgentHandler_UpdateTeam_RaceReusesWinningOwner(t *testing.T) {
	// given: a concurrent request already persisted the owner (Create loses)
	winner := &domain.AgentOwner{TemplateID: "saolei", SessionID: "conv-1", OwnerIndex: 1, Owner: "agent-1"}
	store := &raceOwnerStore{winner: winner}
	manager := &mockManager{}
	picker := &mockOwnerPicker{ref: agentclient.ConnRef{OwnerIndex: 2, Owner: "agent-2"}}
	fake := &fakeAgentClient{}
	setFakeAgentClient(t, fake)
	handler := NewAgentHandler(store, picker, manager, bind.NewServerStreamBinder[game.ChatEvent]())

	_, err := handler.UpdateTeam(context.Background(), &game.UpdateTeamRequest{Team: &game.Team{Name: teamResource}})

	// then: the winner's owner is reused, not re-picked
	if err != nil {
		t.Fatalf("UpdateTeam() error = %v, want nil", err)
	}
	if len(manager.getCalls) != 1 || manager.getCalls[0] != 1 {
		t.Fatalf("manager Get calls = %v, want [1] (winner re-read)", manager.getCalls)
	}
}

func TestAgentHandler_UpdateTeam_InvalidNameRejectedWithoutAllocation(t *testing.T) {
	tests := []struct {
		name string
		req  *game.UpdateTeamRequest
	}{
		{name: "missing team body", req: &game.UpdateTeamRequest{}},
		{name: "malformed resource name", req: &game.UpdateTeamRequest{Team: &game.Team{Name: "projects/p1"}}},
		{name: "missing team segment", req: &game.UpdateTeamRequest{Team: &game.Team{Name: agentSession}}},
		{name: "unknown template", req: &game.UpdateTeamRequest{Team: &game.Team{Name: "templates/unknown/sessions/s1/team"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeAgentClient{}
			handler, store, _, _ := newAgentHarness(t, fake)

			_, err := handler.UpdateTeam(context.Background(), tt.req)

			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("UpdateTeam() code = %v, want InvalidArgument", status.Code(err))
			}
			if store.createCalls != 0 {
				t.Fatalf("owner Create calls = %d, want 0 (no allocation on invalid input)", store.createCalls)
			}
		})
	}
}

func TestAgentHandler_UpdateTeam_NoInstancesMapsToUnavailable(t *testing.T) {
	// given: no live agent_v2 instance to allocate
	fake := &fakeAgentClient{}
	handler, store, _, picker := newAgentHarness(t, fake)
	picker.err = domain.ErrNoAgentInstances

	_, err := handler.UpdateTeam(context.Background(), &game.UpdateTeamRequest{Team: &game.Team{Name: teamResource}})

	if status.Code(err) != codes.Unavailable {
		t.Fatalf("UpdateTeam() code = %v, want Unavailable", status.Code(err))
	}
	if store.createCalls != 0 {
		t.Fatalf("owner Create calls = %d, want 0", store.createCalls)
	}
}

func TestAgentHandler_UpdateTeam_DownstreamErrorPropagates(t *testing.T) {
	// given: agent_v2 rejects the materialization (e.g. preset role mismatch)
	// — the agent-level code must survive the hop
	fake := &fakeAgentClient{updateTeamErr: status.Error(codes.InvalidArgument, "preset role mismatch")}
	handler, store, _, _ := newAgentHarness(t, fake)
	seedAgentOwner(store, 1)

	_, err := handler.UpdateTeam(context.Background(), &game.UpdateTeamRequest{Team: &game.Team{Name: teamResource}})

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("UpdateTeam() code = %v, want InvalidArgument (original code preserved)", status.Code(err))
	}
}

func TestAgentHandler_GetTeam_SuccessForwardsName(t *testing.T) {
	fake := &fakeAgentClient{}
	handler, store, manager, _ := newAgentHarness(t, fake)
	seedAgentOwner(store, 3)

	team, err := handler.GetTeam(context.Background(), &game.GetTeamRequest{Name: teamResource})

	if err != nil {
		t.Fatalf("GetTeam() error = %v, want nil", err)
	}
	if len(manager.getCalls) != 1 || manager.getCalls[0] != 3 {
		t.Fatalf("manager Get calls = %v, want [3]", manager.getCalls)
	}
	if fake.getTeamReq.GetName() != teamResource {
		t.Fatalf("downstream name = %q, want %q", fake.getTeamReq.GetName(), teamResource)
	}
	if team.GetName() != teamResource {
		t.Fatalf("team name = %q, want %q", team.GetName(), teamResource)
	}
}

func TestAgentHandler_GetTeam_NoOwnerReturnsNotFoundWithoutAllocation(t *testing.T) {
	fake := &fakeAgentClient{}
	handler, store, _, _ := newAgentHarness(t, fake)

	_, err := handler.GetTeam(context.Background(), &game.GetTeamRequest{Name: teamResource})

	if status.Code(err) != codes.NotFound {
		t.Fatalf("GetTeam() code = %v, want NotFound", status.Code(err))
	}
	if store.createCalls != 0 {
		t.Fatalf("owner Create calls = %d, want 0 (GetTeam must not allocate)", store.createCalls)
	}
}

func TestAgentHandler_GetTeam_InvalidNameReturnsInvalidArgument(t *testing.T) {
	handler, _, _, _ := newAgentHarness(t, &fakeAgentClient{})

	_, err := handler.GetTeam(context.Background(), &game.GetTeamRequest{Name: agentSession})

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("GetTeam() code = %v, want InvalidArgument", status.Code(err))
	}
}

func TestAgentHandler_GetTeamMember_SuccessForwardsName(t *testing.T) {
	fake := &fakeAgentClient{}
	handler, store, _, _ := newAgentHarness(t, fake)
	seedAgentOwner(store, 1)

	member, err := handler.GetTeamMember(context.Background(), &game.GetTeamMemberRequest{Name: playerMember})

	if err != nil {
		t.Fatalf("GetTeamMember() error = %v, want nil", err)
	}
	if fake.getMemberReq.GetName() != playerMember {
		t.Fatalf("downstream name = %q, want %q", fake.getMemberReq.GetName(), playerMember)
	}
	if member.GetName() != playerMember {
		t.Fatalf("member name = %q, want %q", member.GetName(), playerMember)
	}
}

func TestAgentHandler_GetTeamMember_InvalidNameAndNoOwner(t *testing.T) {
	fake := &fakeAgentClient{}
	handler, store, _, _ := newAgentHarness(t, fake)

	_, err := handler.GetTeamMember(context.Background(), &game.GetTeamMemberRequest{Name: teamResource})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("GetTeamMember() code = %v, want InvalidArgument", status.Code(err))
	}

	_, err = handler.GetTeamMember(context.Background(), &game.GetTeamMemberRequest{Name: playerMember})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("GetTeamMember() code = %v, want NotFound", status.Code(err))
	}
	if store.createCalls != 0 {
		t.Fatalf("owner Create calls = %d, want 0 (lookup-only family)", store.createCalls)
	}
}

func TestAgentHandler_ListTeamMessages_SuccessForwardsParent(t *testing.T) {
	fake := &fakeAgentClient{}
	handler, store, _, _ := newAgentHarness(t, fake)
	seedAgentOwner(store, 1)

	resp, err := handler.ListTeamMessages(context.Background(), &game.ListTeamMessagesRequest{Parent: teamResource})

	if err != nil {
		t.Fatalf("ListTeamMessages() error = %v, want nil", err)
	}
	if fake.listTeamReq.GetParent() != teamResource {
		t.Fatalf("downstream parent = %q, want %q", fake.listTeamReq.GetParent(), teamResource)
	}
	if resp == nil {
		t.Fatal("ListTeamMessages() got nil response")
	}
}

func TestAgentHandler_ListTeamMessages_InvalidParentAndNoOwner(t *testing.T) {
	fake := &fakeAgentClient{}
	handler, store, _, _ := newAgentHarness(t, fake)

	_, err := handler.ListTeamMessages(context.Background(), &game.ListTeamMessagesRequest{Parent: agentSession})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("ListTeamMessages() code = %v, want InvalidArgument", status.Code(err))
	}

	_, err = handler.ListTeamMessages(context.Background(), &game.ListTeamMessagesRequest{Parent: teamResource})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("ListTeamMessages() code = %v, want NotFound", status.Code(err))
	}
	if store.createCalls != 0 {
		t.Fatalf("owner Create calls = %d, want 0", store.createCalls)
	}
}

func TestAgentHandler_ListMemberMessages_SuccessForwardsParent(t *testing.T) {
	fake := &fakeAgentClient{}
	handler, store, _, _ := newAgentHarness(t, fake)
	seedAgentOwner(store, 1)

	resp, err := handler.ListMemberMessages(context.Background(), &game.ListMemberMessagesRequest{Parent: playerMember})

	if err != nil {
		t.Fatalf("ListMemberMessages() error = %v, want nil", err)
	}
	if fake.listMemberReq.GetParent() != playerMember {
		t.Fatalf("downstream parent = %q, want %q", fake.listMemberReq.GetParent(), playerMember)
	}
	if resp == nil {
		t.Fatal("ListMemberMessages() got nil response")
	}
}

func TestAgentHandler_ListMemberMessages_InvalidParentAndNoOwner(t *testing.T) {
	fake := &fakeAgentClient{}
	handler, store, _, _ := newAgentHarness(t, fake)

	_, err := handler.ListMemberMessages(context.Background(), &game.ListMemberMessagesRequest{Parent: teamResource})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("ListMemberMessages() code = %v, want InvalidArgument", status.Code(err))
	}

	_, err = handler.ListMemberMessages(context.Background(), &game.ListMemberMessagesRequest{Parent: playerMember})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("ListMemberMessages() code = %v, want NotFound", status.Code(err))
	}
	if store.createCalls != 0 {
		t.Fatalf("owner Create calls = %d, want 0", store.createCalls)
	}
}

func TestAgentHandler_Cancel_SuccessForwardsName(t *testing.T) {
	fake := &fakeAgentClient{}
	handler, store, manager, _ := newAgentHarness(t, fake)
	seedAgentOwner(store, 3)

	resp, err := handler.Cancel(context.Background(), &game.CancelRequest{Name: teamResource})

	if err != nil {
		t.Fatalf("Cancel() error = %v, want nil", err)
	}
	if resp == nil {
		t.Fatal("Cancel() got nil response")
	}
	if len(manager.getCalls) != 1 || manager.getCalls[0] != 3 {
		t.Fatalf("manager Get calls = %v, want [3]", manager.getCalls)
	}
	if fake.cancelReq.GetName() != teamResource {
		t.Fatalf("downstream name = %q, want %q", fake.cancelReq.GetName(), teamResource)
	}
	if store.createCalls != 0 {
		t.Fatalf("owner Create calls = %d, want 0 (cancel must not allocate)", store.createCalls)
	}
}

func TestAgentHandler_Cancel_InvalidNameReturnsInvalidArgument(t *testing.T) {
	tests := []struct {
		name string
		req  *game.CancelRequest
	}{
		{name: "malformed resource name", req: &game.CancelRequest{Name: "projects/p1"}},
		{name: "missing team segment", req: &game.CancelRequest{Name: agentSession}},
		{name: "unknown template", req: &game.CancelRequest{Name: "templates/unknown/sessions/s1/team"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeAgentClient{}
			handler, store, _, _ := newAgentHarness(t, fake)

			_, err := handler.Cancel(context.Background(), tt.req)

			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("Cancel() code = %v, want InvalidArgument", status.Code(err))
			}
			if store.createCalls != 0 {
				t.Fatalf("owner Create calls = %d, want 0 (no allocation on invalid input)", store.createCalls)
			}
		})
	}
}

func TestAgentHandler_Cancel_NoOwnerReturnsNotFoundWithoutAllocation(t *testing.T) {
	// given: a fresh store — no UpdateTeam ever materialized the session
	fake := &fakeAgentClient{}
	handler, store, _, _ := newAgentHarness(t, fake)

	// when
	_, err := handler.Cancel(context.Background(), &game.CancelRequest{Name: teamResource})

	// then: for routing purposes there is no team to cancel — NOT_FOUND,
	// and Cancel is not a materialization entry point
	if status.Code(err) != codes.NotFound {
		t.Fatalf("Cancel() code = %v, want NotFound", status.Code(err))
	}
	if store.createCalls != 0 {
		t.Fatalf("owner Create calls = %d, want 0 (cancel must not allocate)", store.createCalls)
	}
}

func TestAgentHandler_Cancel_InstanceUnreachable(t *testing.T) {
	// given: the owner exists but its instance has no cached connection
	fake := &fakeAgentClient{}
	handler, store, manager, _ := newAgentHarness(t, fake)
	seedAgentOwner(store, 7)
	manager.getErr = errors.New("no connection for owner index 7")

	// when
	_, err := handler.Cancel(context.Background(), &game.CancelRequest{Name: teamResource})

	// then: proxy→agent_v2 break maps to UNAVAILABLE (503)
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("Cancel() code = %v, want Unavailable", status.Code(err))
	}
}

func TestAgentHandler_Cancel_DownstreamErrorPropagates(t *testing.T) {
	// given: agent_v2 rejects the cancel (unmaterialized team — the owner
	// was found but the team is gone, e.g. after an agent_v2 restart); the
	// agent-level code must survive the hop.
	fake := &fakeAgentClient{cancelErr: status.Error(codes.FailedPrecondition, "team not materialized; send UpdateTeam first")}
	handler, store, _, _ := newAgentHarness(t, fake)
	seedAgentOwner(store, 1)

	_, err := handler.Cancel(context.Background(), &game.CancelRequest{Name: teamResource})

	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("Cancel() code = %v, want FailedPrecondition (original code preserved)", status.Code(err))
	}
}

// TestMapDomainError pins the shared domain→gRPC error mapping used by both
// forwarding handlers (mapDomainError in agent.go).
func TestMapDomainError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode codes.Code
	}{
		{name: "owner not found", err: domain.ErrOwnerNotFound, wantCode: codes.NotFound},
		{name: "owner already exists", err: domain.ErrOwnerAlreadyExists, wantCode: codes.AlreadyExists},
		{name: "no agent instances", err: domain.ErrNoAgentInstances, wantCode: codes.Unavailable},
		// Unexpected store failures (e.g. Mongo unreachable) map to Internal,
		// not grpc-go's Unknown fallback for a bare error.
		{name: "unknown error", err: errors.New("something else"), wantCode: codes.Internal},
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
