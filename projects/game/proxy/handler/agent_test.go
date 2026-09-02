package handler

import (
	"context"
	"errors"
	"io"
	"testing"

	"dominion/projects/game/pkg/bind"
	"dominion/projects/game/proxy/domain"
	"dominion/projects/game/proxy/runtime/agentclient"
	gamev2 "dominion/projects/game/v2"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// fakeSendStream is the upstream AgentService_SendClient double: the
// response half of a server-streaming call — the request was already sent
// and half-closed by the stream-open call (recorded on the client double) —
// so it replays scripted frames before a final Recv error.
type fakeSendStream struct {
	grpc.ClientStream

	frames  []*gamev2.ChatEvent
	recvErr error
}

func (s *fakeSendStream) Recv() (*gamev2.ChatEvent, error) {
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
	frames []*gamev2.ChatEvent
}

func (s *fakeSendServer) Context() context.Context { return s.ctx }

func (s *fakeSendServer) Send(event *gamev2.ChatEvent) error {
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
	sendReq    *gamev2.SendRequest

	updateAgentResult *gamev2.Agent
	updateAgentErr    error
	updateAgentReq    *gamev2.UpdateAgentRequest

	getAgentResult *gamev2.Agent
	getAgentErr    error
	getAgentReq    *gamev2.GetAgentRequest

	listMessagesResult *gamev2.ListAgentMessagesResponse
	listMessagesErr    error
	listMessagesReq    *gamev2.ListAgentMessagesRequest

	createPresetResult *gamev2.Preset
	createPresetErr    error
	createPresetReq    *gamev2.CreatePresetRequest

	listPresetsResult *gamev2.ListPresetsResponse
	listPresetsErr    error
	listPresetsReq    *gamev2.ListPresetsRequest

	getPresetResult *gamev2.Preset
	getPresetErr    error
	getPresetReq    *gamev2.GetPresetRequest

	updatePresetResult *gamev2.Preset
	updatePresetErr    error
	updatePresetReq    *gamev2.UpdatePresetRequest

	deletePresetResult *emptypb.Empty
	deletePresetErr    error
	deletePresetReq    *gamev2.DeletePresetRequest

	listModelsResult *gamev2.ListModelsResponse
	listModelsErr    error
	listModelsReq    *gamev2.ListModelsRequest
}

func (c *fakeAgentClient) Send(_ context.Context, req *gamev2.SendRequest, _ ...grpc.CallOption) (gamev2.AgentService_SendClient, error) {
	c.sendReq = req
	if c.sendErr != nil {
		return nil, c.sendErr
	}
	return c.sendStream, nil
}

func (c *fakeAgentClient) UpdateAgent(_ context.Context, req *gamev2.UpdateAgentRequest, _ ...grpc.CallOption) (*gamev2.Agent, error) {
	c.updateAgentReq = req
	if c.updateAgentErr != nil {
		return nil, c.updateAgentErr
	}
	if c.updateAgentResult != nil {
		return c.updateAgentResult, nil
	}
	return &gamev2.Agent{Name: req.GetAgent().GetName()}, nil
}

func (c *fakeAgentClient) GetAgent(_ context.Context, req *gamev2.GetAgentRequest, _ ...grpc.CallOption) (*gamev2.Agent, error) {
	c.getAgentReq = req
	if c.getAgentErr != nil {
		return nil, c.getAgentErr
	}
	if c.getAgentResult != nil {
		return c.getAgentResult, nil
	}
	return &gamev2.Agent{Name: req.GetName()}, nil
}

func (c *fakeAgentClient) ListAgentMessages(_ context.Context, req *gamev2.ListAgentMessagesRequest, _ ...grpc.CallOption) (*gamev2.ListAgentMessagesResponse, error) {
	c.listMessagesReq = req
	if c.listMessagesErr != nil {
		return nil, c.listMessagesErr
	}
	if c.listMessagesResult != nil {
		return c.listMessagesResult, nil
	}
	return &gamev2.ListAgentMessagesResponse{}, nil
}

func (c *fakeAgentClient) CreatePreset(_ context.Context, req *gamev2.CreatePresetRequest, _ ...grpc.CallOption) (*gamev2.Preset, error) {
	c.createPresetReq = req
	if c.createPresetErr != nil {
		return nil, c.createPresetErr
	}
	if c.createPresetResult != nil {
		return c.createPresetResult, nil
	}
	return &gamev2.Preset{Name: req.GetParent() + "/presets/" + req.GetPresetId()}, nil
}

func (c *fakeAgentClient) ListPresets(_ context.Context, req *gamev2.ListPresetsRequest, _ ...grpc.CallOption) (*gamev2.ListPresetsResponse, error) {
	c.listPresetsReq = req
	if c.listPresetsErr != nil {
		return nil, c.listPresetsErr
	}
	if c.listPresetsResult != nil {
		return c.listPresetsResult, nil
	}
	return &gamev2.ListPresetsResponse{}, nil
}

func (c *fakeAgentClient) GetPreset(_ context.Context, req *gamev2.GetPresetRequest, _ ...grpc.CallOption) (*gamev2.Preset, error) {
	c.getPresetReq = req
	if c.getPresetErr != nil {
		return nil, c.getPresetErr
	}
	if c.getPresetResult != nil {
		return c.getPresetResult, nil
	}
	return &gamev2.Preset{Name: req.GetName()}, nil
}

func (c *fakeAgentClient) UpdatePreset(_ context.Context, req *gamev2.UpdatePresetRequest, _ ...grpc.CallOption) (*gamev2.Preset, error) {
	c.updatePresetReq = req
	if c.updatePresetErr != nil {
		return nil, c.updatePresetErr
	}
	if c.updatePresetResult != nil {
		return c.updatePresetResult, nil
	}
	return &gamev2.Preset{Name: req.GetPreset().GetName()}, nil
}

func (c *fakeAgentClient) DeletePreset(_ context.Context, req *gamev2.DeletePresetRequest, _ ...grpc.CallOption) (*emptypb.Empty, error) {
	c.deletePresetReq = req
	if c.deletePresetErr != nil {
		return nil, c.deletePresetErr
	}
	if c.deletePresetResult != nil {
		return c.deletePresetResult, nil
	}
	return &emptypb.Empty{}, nil
}

func (c *fakeAgentClient) ListModels(_ context.Context, req *gamev2.ListModelsRequest, _ ...grpc.CallOption) (*gamev2.ListModelsResponse, error) {
	c.listModelsReq = req
	if c.listModelsErr != nil {
		return nil, c.listModelsErr
	}
	if c.listModelsResult != nil {
		return c.listModelsResult, nil
	}
	return &gamev2.ListModelsResponse{}, nil
}

// errFakeNotImplemented marks the double's undriven methods.
var errFakeNotImplemented = errors.New("fakeAgentClient: not implemented")

// setFakeAgentClient replaces the client constructor seam and restores it on
// cleanup.
func setFakeAgentClient(t *testing.T, fake *fakeAgentClient) {
	t.Helper()
	old := newAgentClient
	newAgentClient = func(_ *grpc.ClientConn) gamev2.AgentServiceClient {
		return fake
	}
	t.Cleanup(func() { newAgentClient = old })
}

// recordingPicker wraps mockOwnerPicker and records the hash keys the
// handler derives, so tests can assert the request-derived key of
// affinity-free routing.
type recordingPicker struct {
	mockOwnerPicker
	keys []string
}

func (p *recordingPicker) Pick(_ context.Context, key string, _ []*agentclient.ConnRef) (*agentclient.ConnRef, error) {
	p.keys = append(p.keys, key)
	return p.mockOwnerPicker.Pick(context.Background(), key, nil)
}

// newAgentHarness wires an AgentHandler with the shared test doubles; the
// manager is pre-populated so owner resolution succeeds unless a test
// overrides it.
func newAgentHarness(t *testing.T, fake *fakeAgentClient) (*AgentHandler, *mockOwnerStore, *mockManager, *recordingPicker) {
	t.Helper()
	setFakeAgentClient(t, fake)
	store := newMockOwnerStore()
	manager := &mockManager{}
	picker := &recordingPicker{mockOwnerPicker: mockOwnerPicker{ref: agentclient.ConnRef{OwnerIndex: 2, Owner: "agent-2"}}}
	handler := NewAgentHandler(store, picker, manager, bind.NewServerStreamBinder[gamev2.ChatEvent]())
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

func chatFrame(text string) *gamev2.ChatEvent {
	return &gamev2.ChatEvent{
		Session: agentSession,
		Payload: &gamev2.ChatEvent_Delta{Delta: &gamev2.BlockDeltaEvent{Index: 0, Text: text}},
	}
}

func TestAgentHandler_Send_NoOwnerReturnsNotFoundWithoutAllocation(t *testing.T) {
	// given: a fresh store — no UpdateAgent ever materialized the session
	fake := &fakeAgentClient{sendStream: &fakeSendStream{recvErr: io.EOF}}
	handler, store, _, _ := newAgentHarness(t, fake)

	// when
	err := handler.Send(&gamev2.SendRequest{Session: agentSession, Text: "hi"}, &fakeSendServer{ctx: context.Background()})

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
		frames:  []*gamev2.ChatEvent{chatFrame("a"), chatFrame("b")},
		recvErr: io.EOF,
	}
	fake := &fakeAgentClient{sendStream: upstream}
	handler, store, manager, _ := newAgentHarness(t, fake)
	seedAgentOwner(store, 1)
	server := &fakeSendServer{ctx: context.Background()}

	// when: one Send round-trips
	err := handler.Send(&gamev2.SendRequest{Session: agentSession, Text: "hi"}, server)

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

			err := handler.Send(&gamev2.SendRequest{Session: tt.session, Text: "hi"}, &fakeSendServer{ctx: context.Background()})

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
	err := handler.Send(&gamev2.SendRequest{Session: agentSession, Text: "hi"}, &fakeSendServer{ctx: context.Background()})

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

	err := handler.Send(&gamev2.SendRequest{Session: agentSession, Text: "hi"}, &fakeSendServer{ctx: context.Background()})

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

	err := handler.Send(&gamev2.SendRequest{Session: agentSession, Text: "hi"}, &fakeSendServer{ctx: context.Background()})

	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("Send() code = %v, want FailedPrecondition (original code preserved)", status.Code(err))
	}
}

func TestAgentHandler_Send_EmptyTextRejectedBeforeOwnerLookup(t *testing.T) {
	// given: a valid resource name but an empty message — the routing-layer
	// validation rejects it before any owner interaction
	fake := &fakeAgentClient{sendStream: &fakeSendStream{recvErr: io.EOF}}
	handler, store, _, _ := newAgentHarness(t, fake)

	err := handler.Send(&gamev2.SendRequest{Session: agentSession, Text: ""}, &fakeSendServer{ctx: context.Background()})

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
		frames:  []*gamev2.ChatEvent{chatFrame("partial")},
		recvErr: status.Error(codes.InvalidArgument, "empty text"),
	}
	fake := &fakeAgentClient{sendStream: upstream}
	handler, store, _, _ := newAgentHarness(t, fake)
	seedAgentOwner(store, 1)
	server := &fakeSendServer{ctx: context.Background()}

	err := handler.Send(&gamev2.SendRequest{Session: agentSession, Text: "hi"}, server)

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
	req := &gamev2.UpdateAgentRequest{Agent: &gamev2.Agent{Name: agentResource, Preset: "templates/saolei/presets/base"}}

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

	agent, err := handler.UpdateAgent(context.Background(), &gamev2.UpdateAgentRequest{Agent: &gamev2.Agent{Name: agentResource}})

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
	handler := NewAgentHandler(store, picker, manager, bind.NewServerStreamBinder[gamev2.ChatEvent]())

	_, err := handler.UpdateAgent(context.Background(), &gamev2.UpdateAgentRequest{Agent: &gamev2.Agent{Name: agentResource}})

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
		req  *gamev2.UpdateAgentRequest
	}{
		{name: "missing agent body", req: &gamev2.UpdateAgentRequest{}},
		{name: "malformed resource name", req: &gamev2.UpdateAgentRequest{Agent: &gamev2.Agent{Name: "projects/p1"}}},
		{name: "missing agent segment", req: &gamev2.UpdateAgentRequest{Agent: &gamev2.Agent{Name: agentSession}}},
		{name: "unknown template", req: &gamev2.UpdateAgentRequest{Agent: &gamev2.Agent{Name: "templates/unknown/sessions/s1/agent"}}},
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

	_, err := handler.UpdateAgent(context.Background(), &gamev2.UpdateAgentRequest{Agent: &gamev2.Agent{Name: agentResource}})

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

	_, err := handler.UpdateAgent(context.Background(), &gamev2.UpdateAgentRequest{Agent: &gamev2.Agent{Name: agentResource}})

	if status.Code(err) != codes.NotFound {
		t.Fatalf("UpdateAgent() code = %v, want NotFound (original code preserved)", status.Code(err))
	}
}

func TestAgentHandler_GetAgent_SuccessForwardsName(t *testing.T) {
	fake := &fakeAgentClient{}
	handler, store, manager, _ := newAgentHarness(t, fake)
	seedAgentOwner(store, 3)

	agent, err := handler.GetAgent(context.Background(), &gamev2.GetAgentRequest{Name: agentResource})

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

	_, err := handler.GetAgent(context.Background(), &gamev2.GetAgentRequest{Name: agentResource})

	if status.Code(err) != codes.NotFound {
		t.Fatalf("GetAgent() code = %v, want NotFound", status.Code(err))
	}
	if store.createCalls != 0 {
		t.Fatalf("owner Create calls = %d, want 0 (GetAgent must not allocate)", store.createCalls)
	}
}

func TestAgentHandler_GetAgent_InvalidNameReturnsInvalidArgument(t *testing.T) {
	handler, _, _, _ := newAgentHarness(t, &fakeAgentClient{})

	_, err := handler.GetAgent(context.Background(), &gamev2.GetAgentRequest{Name: "templates/saolei/sessions/s1"})

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("GetAgent() code = %v, want InvalidArgument", status.Code(err))
	}
}

func TestAgentHandler_ListAgentMessages_SuccessForwardsParent(t *testing.T) {
	fake := &fakeAgentClient{}
	handler, store, _, _ := newAgentHarness(t, fake)
	seedAgentOwner(store, 1)

	resp, err := handler.ListAgentMessages(context.Background(), &gamev2.ListAgentMessagesRequest{Parent: agentResource})

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

	_, err := handler.ListAgentMessages(context.Background(), &gamev2.ListAgentMessagesRequest{Parent: agentResource})

	if status.Code(err) != codes.NotFound {
		t.Fatalf("ListAgentMessages() code = %v, want NotFound", status.Code(err))
	}
	if store.createCalls != 0 {
		t.Fatalf("owner Create calls = %d, want 0", store.createCalls)
	}
}

func TestAgentHandler_ListAgentMessages_InvalidParentReturnsInvalidArgument(t *testing.T) {
	handler, _, _, _ := newAgentHarness(t, &fakeAgentClient{})

	_, err := handler.ListAgentMessages(context.Background(), &gamev2.ListAgentMessagesRequest{Parent: "templates/saolei/sessions/s1/team"})

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("ListAgentMessages() code = %v, want InvalidArgument", status.Code(err))
	}
}

// affinityFreeCases enumerate the preset CRUD + ListModels RPCs with a
// request, its derived hash key, and a downstream request recorder — the
// shared logic under test: forward to the hash-picked instance with no
// owner-store interaction.
func affinityFreeCases(fake *fakeAgentClient) []struct {
	name     string
	call     func(h *AgentHandler) error
	wantKey  string
	downstream func() bool
} {
	return []struct {
		name     string
		call     func(h *AgentHandler) error
		wantKey  string
		downstream func() bool
	}{
		{
			name: "CreatePreset",
			call: func(h *AgentHandler) error {
				_, err := h.CreatePreset(context.Background(), &gamev2.CreatePresetRequest{
					Parent:   "templates/saolei",
					PresetId: "base",
					Preset:   &gamev2.Preset{PlayerPrompt: "hi"},
				})
				return err
			},
			wantKey:    "templates/saolei/presets/base",
			downstream: func() bool { return fake.createPresetReq.GetPresetId() == "base" },
		},
		{
			name: "ListPresets",
			call: func(h *AgentHandler) error {
				_, err := h.ListPresets(context.Background(), &gamev2.ListPresetsRequest{Parent: "templates/saolei"})
				return err
			},
			wantKey:    "templates/saolei",
			downstream: func() bool { return fake.listPresetsReq.GetParent() == "templates/saolei" },
		},
		{
			name: "GetPreset",
			call: func(h *AgentHandler) error {
				_, err := h.GetPreset(context.Background(), &gamev2.GetPresetRequest{Name: "templates/saolei/presets/base"})
				return err
			},
			wantKey:    "templates/saolei/presets/base",
			downstream: func() bool { return fake.getPresetReq.GetName() == "templates/saolei/presets/base" },
		},
		{
			name: "UpdatePreset",
			call: func(h *AgentHandler) error {
				_, err := h.UpdatePreset(context.Background(), &gamev2.UpdatePresetRequest{
					Preset: &gamev2.Preset{Name: "templates/saolei/presets/base", PlayerPrompt: "hi"},
				})
				return err
			},
			wantKey:    "templates/saolei/presets/base",
			downstream: func() bool { return fake.updatePresetReq.GetPreset().GetName() == "templates/saolei/presets/base" },
		},
		{
			name: "DeletePreset",
			call: func(h *AgentHandler) error {
				_, err := h.DeletePreset(context.Background(), &gamev2.DeletePresetRequest{Name: "templates/saolei/presets/base"})
				return err
			},
			wantKey:    "templates/saolei/presets/base",
			downstream: func() bool { return fake.deletePresetReq.GetName() == "templates/saolei/presets/base" },
		},
		{
			name: "ListModels",
			call: func(h *AgentHandler) error {
				_, err := h.ListModels(context.Background(), &gamev2.ListModelsRequest{})
				return err
			},
			wantKey:    listModelsPickKey,
			downstream: func() bool { return fake.listModelsReq != nil },
		},
	}
}

func TestAgentHandler_AffinityFreeRPCs_ForwardWithoutOwnerAllocation(t *testing.T) {
	fake := &fakeAgentClient{}
	for _, tt := range affinityFreeCases(fake) {
		t.Run(tt.name, func(t *testing.T) {
			handler, store, manager, picker := newAgentHarness(t, fake)

			err := tt.call(handler)

			// then: one hash pick by the request-derived key, one connection
			// resolution, zero owner-store interaction
			if err != nil {
				t.Fatalf("%s() error = %v, want nil", tt.name, err)
			}
			if len(picker.keys) != 1 || picker.keys[0] != tt.wantKey {
				t.Fatalf("%s() pick keys = %v, want [%q]", tt.name, picker.keys, tt.wantKey)
			}
			if len(manager.getCalls) != 1 || manager.getCalls[0] != 2 {
				t.Fatalf("%s() manager Get calls = %v, want [2] (picked index)", tt.name, manager.getCalls)
			}
			if len(store.records) != 0 || store.createCalls != 0 {
				t.Fatalf("%s() touched the owner store (records=%d, creates=%d), want none",
					tt.name, len(store.records), store.createCalls)
			}
			if !tt.downstream() {
				t.Fatalf("%s() did not reach the downstream client with the caller's request", tt.name)
			}
		})
	}
}

func TestAgentHandler_AffinityFreeRPCs_NoInstancesMapsToUnavailable(t *testing.T) {
	for _, tt := range affinityFreeCases(&fakeAgentClient{}) {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeAgentClient{}
			handler, _, _, picker := newAgentHarness(t, fake)
			picker.err = domain.ErrNoAgentInstances

			err := tt.call(handler)

			if status.Code(err) != codes.Unavailable {
				t.Fatalf("%s() code = %v, want Unavailable", tt.name, status.Code(err))
			}
		})
	}
}

func TestAgentHandler_AffinityFreeRPCs_DownstreamErrorPropagates(t *testing.T) {
	for _, tt := range affinityFreeCases(&fakeAgentClient{}) {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeAgentClient{
				createPresetErr: status.Error(codes.AlreadyExists, "preset exists"),
				listPresetsErr:  status.Error(codes.AlreadyExists, "preset exists"),
				getPresetErr:    status.Error(codes.NotFound, "preset missing"),
				updatePresetErr: status.Error(codes.NotFound, "preset missing"),
				deletePresetErr: status.Error(codes.NotFound, "preset missing"),
				listModelsErr:   status.Error(codes.Internal, "catalog failure"),
			}
			handler, _, _, _ := newAgentHarness(t, fake)

			err := tt.call(handler)

			if err == nil {
				t.Fatalf("%s() expected the downstream error to propagate, got nil", tt.name)
			}
			if status.Code(err) == codes.Unknown {
				t.Fatalf("%s() code = Unknown, want the downstream code preserved", tt.name)
			}
		})
	}
}

func TestAgentHandler_AffinityFreeRPCs_InvalidResourceNames(t *testing.T) {
	tests := []struct {
		name string
		call func(h *AgentHandler) error
	}{
		{
			name: "CreatePreset malformed parent",
			call: func(h *AgentHandler) error {
				_, err := h.CreatePreset(context.Background(), &gamev2.CreatePresetRequest{Parent: "templates", PresetId: "p"})
				return err
			},
		},
		{
			name: "CreatePreset unknown template",
			call: func(h *AgentHandler) error {
				_, err := h.CreatePreset(context.Background(), &gamev2.CreatePresetRequest{Parent: "templates/other", PresetId: "p"})
				return err
			},
		},
		{
			name: "CreatePreset missing preset_id",
			call: func(h *AgentHandler) error {
				_, err := h.CreatePreset(context.Background(), &gamev2.CreatePresetRequest{Parent: "templates/saolei"})
				return err
			},
		},
		{
			name: "ListPresets unknown template",
			call: func(h *AgentHandler) error {
				_, err := h.ListPresets(context.Background(), &gamev2.ListPresetsRequest{Parent: "templates/other"})
				return err
			},
		},
		{
			name: "GetPreset malformed name",
			call: func(h *AgentHandler) error {
				_, err := h.GetPreset(context.Background(), &gamev2.GetPresetRequest{Name: "templates/saolei/presets"})
				return err
			},
		},
		{
			name: "UpdatePreset missing name",
			call: func(h *AgentHandler) error {
				_, err := h.UpdatePreset(context.Background(), &gamev2.UpdatePresetRequest{Preset: &gamev2.Preset{PlayerPrompt: "hi"}})
				return err
			},
		},
		{
			name: "UpdatePreset missing preset body",
			call: func(h *AgentHandler) error {
				_, err := h.UpdatePreset(context.Background(), &gamev2.UpdatePresetRequest{})
				return err
			},
		},
		{
			name: "DeletePreset unknown template",
			call: func(h *AgentHandler) error {
				_, err := h.DeletePreset(context.Background(), &gamev2.DeletePresetRequest{Name: "templates/other/presets/p"})
				return err
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeAgentClient{}
			handler, store, _, picker := newAgentHarness(t, fake)

			err := tt.call(handler)

			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("%s() code = %v, want InvalidArgument", tt.name, status.Code(err))
			}
			if len(picker.keys) != 0 || store.createCalls != 0 {
				t.Fatalf("%s() routed or allocated before validation (keys=%v, creates=%d)",
					tt.name, picker.keys, store.createCalls)
			}
		})
	}
}
