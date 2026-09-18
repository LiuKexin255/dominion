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
// unary requests are recorded per method. Results/errors not configured for
// a test fail with the errFakeNotImplemented sentinel.
type fakeAgentClient struct {
	sendStream *fakeSendStream
	sendErr    error
	sendReq    *game.SendRequest

	updateAgentResult *game.Agent
	updateAgentErr    error
	updateAgentReq    *game.UpdateAgentRequest

	getAgentResult *game.Agent
	getAgentErr    error
	getAgentReq    *game.GetAgentRequest

	listMessagesResult *game.ListAgentMessagesResponse
	listMessagesErr    error
	listMessagesReq    *game.ListAgentMessagesRequest

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

func (c *fakeAgentClient) UpdateAgent(_ context.Context, req *game.UpdateAgentRequest, _ ...grpc.CallOption) (*game.Agent, error) {
	c.updateAgentReq = req
	if c.updateAgentErr != nil {
		return nil, c.updateAgentErr
	}
	if c.updateAgentResult != nil {
		return c.updateAgentResult, nil
	}
	return &game.Agent{Name: req.GetAgent().GetName()}, nil
}

func (c *fakeAgentClient) GetAgent(_ context.Context, req *game.GetAgentRequest, _ ...grpc.CallOption) (*game.Agent, error) {
	c.getAgentReq = req
	if c.getAgentErr != nil {
		return nil, c.getAgentErr
	}
	if c.getAgentResult != nil {
		return c.getAgentResult, nil
	}
	return &game.Agent{Name: req.GetName()}, nil
}

func (c *fakeAgentClient) ListAgentMessages(_ context.Context, req *game.ListAgentMessagesRequest, _ ...grpc.CallOption) (*game.ListAgentMessagesResponse, error) {
	c.listMessagesReq = req
	if c.listMessagesErr != nil {
		return nil, c.listMessagesErr
	}
	if c.listMessagesResult != nil {
		return c.listMessagesResult, nil
	}
	return &game.ListAgentMessagesResponse{}, nil
}

// Cancel mirrors the ListAgentMessages unary shape; the response is an empty
// message, so no result field is configurable.
func (c *fakeAgentClient) Cancel(_ context.Context, req *game.CancelRequest, _ ...grpc.CallOption) (*game.CancelResponse, error) {
	c.cancelReq = req
	if c.cancelErr != nil {
		return nil, c.cancelErr
	}
	return &game.CancelResponse{}, nil
}

// errFakeNotImplemented marks the double's undriven methods.
var errFakeNotImplemented = errors.New("fakeAgentClient: not implemented")

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
// for a previous UpdateAgent materialization.
func seedAgentOwner(store *mockOwnerStore, ownerIndex int) {
	store.records[ownerKey("saolei", "conv-1")] = &domain.AgentOwner{
		TemplateID: "saolei",
		SessionID:  "conv-1",
		OwnerIndex: ownerIndex,
		Owner:      "agent-owner",
	}
}

const agentSession = "templates/saolei/sessions/conv-1"
const agentResource = agentSession + "/agent"

func chatFrame(text string) *game.ChatEvent {
	return &game.ChatEvent{
		Session: agentSession,
		Payload: &game.ChatEvent_Delta{Delta: &game.BlockDeltaEvent{Index: 0, Text: text}},
	}
}

func TestAgentHandler_Send_NoOwnerReturnsNotFoundWithoutAllocation(t *testing.T) {
	// given: a fresh store — no UpdateAgent ever materialized the session
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
	// given: an owner from a previous UpdateAgent and an upstream streaming
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

	// then: proxy→agent_v2 break maps to UNAVAILABLE (503, agent-api §3)
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
	// the mapped HTTP 400, not a 503 hop failure (contracts/agent-api.md §3).
	fake := &fakeAgentClient{sendErr: status.Error(codes.FailedPrecondition, "agent not materialized; send UpdateAgent first")}
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

func TestAgentHandler_UpdateAgent_AllocatesOwnerAndForwards(t *testing.T) {
	// given: a fresh store — the first materialization of the session
	fake := &fakeAgentClient{}
	handler, store, manager, _ := newAgentHarness(t, fake)
	req := &game.UpdateAgentRequest{Agent: &game.Agent{Name: agentResource, Preset: "templates/saolei/presets/base"}}

	// when
	agent, err := handler.UpdateAgent(context.Background(), req)

	// then: the owner is allocated once and the request reaches the
	// upstream unchanged
	if err != nil {
		t.Fatalf("UpdateAgent() error = %v, want nil", err)
	}
	if store.createCalls != 1 {
		t.Fatalf("owner Create calls = %d, want 1 (UpdateAgent is the allocation point)", store.createCalls)
	}
	owner := store.records[ownerKey("saolei", "conv-1")]
	if owner == nil || owner.TemplateID != "saolei" || owner.SessionID != "conv-1" {
		t.Fatalf("allocated owner = %+v, want the (saolei, conv-1) composite key", owner)
	}
	if len(manager.getCalls) != 1 || manager.getCalls[0] != owner.OwnerIndex {
		t.Fatalf("manager Get calls = %v, want [allocated index]", manager.getCalls)
	}
	if fake.updateAgentReq != req {
		t.Fatal("downstream UpdateAgent did not receive the caller's request")
	}
	if agent.GetName() != agentResource {
		t.Fatalf("agent name = %q, want %q", agent.GetName(), agentResource)
	}
}

func TestAgentHandler_UpdateAgent_ReusesExistingOwnerWithoutAllocation(t *testing.T) {
	// given: the owner already exists (refresh case) and no instances are
	// listed — a re-pick would fail, proving the existing owner is reused
	fake := &fakeAgentClient{}
	handler, store, manager, _ := newAgentHarness(t, fake)
	seedAgentOwner(store, 1)

	agent, err := handler.UpdateAgent(context.Background(), &game.UpdateAgentRequest{Agent: &game.Agent{Name: agentResource}})

	if err != nil {
		t.Fatalf("UpdateAgent() error = %v, want nil", err)
	}
	if store.createCalls != 0 {
		t.Fatalf("owner Create calls = %d, want 0 (existing owner reused)", store.createCalls)
	}
	if len(manager.getCalls) != 1 || manager.getCalls[0] != 1 {
		t.Fatalf("manager Get calls = %v, want [1]", manager.getCalls)
	}
	if agent.GetName() != agentResource {
		t.Fatalf("agent name = %q, want %q", agent.GetName(), agentResource)
	}
}

func TestAgentHandler_UpdateAgent_RaceReusesWinningOwner(t *testing.T) {
	// given: a concurrent request already persisted the owner (Create loses)
	winner := &domain.AgentOwner{TemplateID: "saolei", SessionID: "conv-1", OwnerIndex: 1, Owner: "agent-1"}
	store := &raceOwnerStore{winner: winner}
	manager := &mockManager{}
	picker := &mockOwnerPicker{ref: agentclient.ConnRef{OwnerIndex: 2, Owner: "agent-2"}}
	fake := &fakeAgentClient{}
	setFakeAgentClient(t, fake)
	handler := NewAgentHandler(store, picker, manager, bind.NewServerStreamBinder[game.ChatEvent]())

	_, err := handler.UpdateAgent(context.Background(), &game.UpdateAgentRequest{Agent: &game.Agent{Name: agentResource}})

	// then: the winner's owner is reused, not re-picked
	if err != nil {
		t.Fatalf("UpdateAgent() error = %v, want nil", err)
	}
	if len(manager.getCalls) != 1 || manager.getCalls[0] != 1 {
		t.Fatalf("manager Get calls = %v, want [1] (winner re-read)", manager.getCalls)
	}
}

func TestAgentHandler_UpdateAgent_InvalidNameRejectedWithoutAllocation(t *testing.T) {
	tests := []struct {
		name string
		req  *game.UpdateAgentRequest
	}{
		{name: "missing agent body", req: &game.UpdateAgentRequest{}},
		{name: "malformed resource name", req: &game.UpdateAgentRequest{Agent: &game.Agent{Name: "projects/p1"}}},
		{name: "missing agent segment", req: &game.UpdateAgentRequest{Agent: &game.Agent{Name: agentSession}}},
		{name: "unknown template", req: &game.UpdateAgentRequest{Agent: &game.Agent{Name: "templates/unknown/sessions/s1/agent"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeAgentClient{}
			handler, store, _, _ := newAgentHarness(t, fake)

			_, err := handler.UpdateAgent(context.Background(), tt.req)

			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("UpdateAgent() code = %v, want InvalidArgument", status.Code(err))
			}
			if store.createCalls != 0 {
				t.Fatalf("owner Create calls = %d, want 0 (no allocation on invalid input)", store.createCalls)
			}
		})
	}
}

func TestAgentHandler_UpdateAgent_NoInstancesMapsToUnavailable(t *testing.T) {
	// given: no live agent_v2 instance to allocate
	fake := &fakeAgentClient{}
	handler, store, _, picker := newAgentHarness(t, fake)
	picker.err = domain.ErrNoAgentInstances

	_, err := handler.UpdateAgent(context.Background(), &game.UpdateAgentRequest{Agent: &game.Agent{Name: agentResource}})

	if status.Code(err) != codes.Unavailable {
		t.Fatalf("UpdateAgent() code = %v, want Unavailable", status.Code(err))
	}
	if store.createCalls != 0 {
		t.Fatalf("owner Create calls = %d, want 0", store.createCalls)
	}
}

func TestAgentHandler_UpdateAgent_DownstreamErrorPropagates(t *testing.T) {
	// given: agent_v2 rejects the materialization (e.g. preset missing) —
	// the agent-level code must survive the hop
	fake := &fakeAgentClient{updateAgentErr: status.Error(codes.NotFound, "preset not found")}
	handler, store, _, _ := newAgentHarness(t, fake)
	seedAgentOwner(store, 1)

	_, err := handler.UpdateAgent(context.Background(), &game.UpdateAgentRequest{Agent: &game.Agent{Name: agentResource}})

	if status.Code(err) != codes.NotFound {
		t.Fatalf("UpdateAgent() code = %v, want NotFound (original code preserved)", status.Code(err))
	}
}

func TestAgentHandler_GetAgent_SuccessForwardsName(t *testing.T) {
	fake := &fakeAgentClient{}
	handler, store, manager, _ := newAgentHarness(t, fake)
	seedAgentOwner(store, 3)

	agent, err := handler.GetAgent(context.Background(), &game.GetAgentRequest{Name: agentResource})

	if err != nil {
		t.Fatalf("GetAgent() error = %v, want nil", err)
	}
	if len(manager.getCalls) != 1 || manager.getCalls[0] != 3 {
		t.Fatalf("manager Get calls = %v, want [3]", manager.getCalls)
	}
	if fake.getAgentReq.GetName() != agentResource {
		t.Fatalf("downstream name = %q, want %q", fake.getAgentReq.GetName(), agentResource)
	}
	if agent.GetName() != agentResource {
		t.Fatalf("agent name = %q, want %q", agent.GetName(), agentResource)
	}
}

func TestAgentHandler_GetAgent_NoOwnerReturnsNotFoundWithoutAllocation(t *testing.T) {
	fake := &fakeAgentClient{}
	handler, store, _, _ := newAgentHarness(t, fake)

	_, err := handler.GetAgent(context.Background(), &game.GetAgentRequest{Name: agentResource})

	if status.Code(err) != codes.NotFound {
		t.Fatalf("GetAgent() code = %v, want NotFound", status.Code(err))
	}
	if store.createCalls != 0 {
		t.Fatalf("owner Create calls = %d, want 0 (GetAgent must not allocate)", store.createCalls)
	}
}

func TestAgentHandler_GetAgent_InvalidNameReturnsInvalidArgument(t *testing.T) {
	handler, _, _, _ := newAgentHarness(t, &fakeAgentClient{})

	_, err := handler.GetAgent(context.Background(), &game.GetAgentRequest{Name: "templates/saolei/sessions/s1"})

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("GetAgent() code = %v, want InvalidArgument", status.Code(err))
	}
}

func TestAgentHandler_ListAgentMessages_SuccessForwardsParent(t *testing.T) {
	fake := &fakeAgentClient{}
	handler, store, _, _ := newAgentHarness(t, fake)
	seedAgentOwner(store, 1)

	resp, err := handler.ListAgentMessages(context.Background(), &game.ListAgentMessagesRequest{Parent: agentResource})

	if err != nil {
		t.Fatalf("ListAgentMessages() error = %v, want nil", err)
	}
	if fake.listMessagesReq.GetParent() != agentResource {
		t.Fatalf("downstream parent = %q, want %q", fake.listMessagesReq.GetParent(), agentResource)
	}
	if resp == nil {
		t.Fatal("ListAgentMessages() got nil response")
	}
}

func TestAgentHandler_ListAgentMessages_NoOwnerReturnsNotFound(t *testing.T) {
	fake := &fakeAgentClient{}
	handler, store, _, _ := newAgentHarness(t, fake)

	_, err := handler.ListAgentMessages(context.Background(), &game.ListAgentMessagesRequest{Parent: agentResource})

	if status.Code(err) != codes.NotFound {
		t.Fatalf("ListAgentMessages() code = %v, want NotFound", status.Code(err))
	}
	if store.createCalls != 0 {
		t.Fatalf("owner Create calls = %d, want 0", store.createCalls)
	}
}

func TestAgentHandler_ListAgentMessages_InvalidParentReturnsInvalidArgument(t *testing.T) {
	handler, _, _, _ := newAgentHarness(t, &fakeAgentClient{})

	_, err := handler.ListAgentMessages(context.Background(), &game.ListAgentMessagesRequest{Parent: "templates/saolei/sessions/s1/team"})

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("ListAgentMessages() code = %v, want InvalidArgument", status.Code(err))
	}
}

func TestAgentHandler_Cancel_SuccessForwardsName(t *testing.T) {
	fake := &fakeAgentClient{}
	handler, store, manager, _ := newAgentHarness(t, fake)
	seedAgentOwner(store, 3)

	resp, err := handler.Cancel(context.Background(), &game.CancelRequest{Name: agentResource})

	if err != nil {
		t.Fatalf("Cancel() error = %v, want nil", err)
	}
	if resp == nil {
		t.Fatal("Cancel() got nil response")
	}
	if len(manager.getCalls) != 1 || manager.getCalls[0] != 3 {
		t.Fatalf("manager Get calls = %v, want [3]", manager.getCalls)
	}
	if fake.cancelReq.GetName() != agentResource {
		t.Fatalf("downstream name = %q, want %q", fake.cancelReq.GetName(), agentResource)
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
		{name: "missing agent segment", req: &game.CancelRequest{Name: agentSession}},
		{name: "unknown template", req: &game.CancelRequest{Name: "templates/unknown/sessions/s1/agent"}},
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
	// given: a fresh store — no UpdateAgent ever materialized the session
	fake := &fakeAgentClient{}
	handler, store, _, _ := newAgentHarness(t, fake)

	// when
	_, err := handler.Cancel(context.Background(), &game.CancelRequest{Name: agentResource})

	// then: for routing purposes there is no agent to cancel — NOT_FOUND,
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
	_, err := handler.Cancel(context.Background(), &game.CancelRequest{Name: agentResource})

	// then: proxy→agent_v2 break maps to UNAVAILABLE (503, agent-api §3)
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("Cancel() code = %v, want Unavailable", status.Code(err))
	}
}

func TestAgentHandler_Cancel_DownstreamErrorPropagates(t *testing.T) {
	// given: agent_v2 rejects the cancel (unmaterialized agent — the owner
	// was found but the agent is gone, e.g. after an agent_v2 restart); the
	// agent-level code must survive the hop so the front end sees the mapped
	// HTTP 400 rather than a 5xx hop failure (agent-api §3).
	fake := &fakeAgentClient{cancelErr: status.Error(codes.FailedPrecondition, "agent not materialized; send UpdateAgent first")}
	handler, store, _, _ := newAgentHarness(t, fake)
	seedAgentOwner(store, 1)

	_, err := handler.Cancel(context.Background(), &game.CancelRequest{Name: agentResource})

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
		// not grpc-go's Unknown fallback for a bare error (agent-api §3).
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
