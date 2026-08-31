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
// client writes the request while opening the stream (generic stream shape),
// so the request is recorded here rather than on the stream. The methods
// this file's tests never drive fail with a sentinel error.
type fakeAgentClient struct {
	sendStream *fakeSendStream
	sendErr    error
	sendReq    *gamev2.SendRequest
}

func (c *fakeAgentClient) Send(_ context.Context, req *gamev2.SendRequest, _ ...grpc.CallOption) (gamev2.AgentService_SendClient, error) {
	c.sendReq = req
	if c.sendErr != nil {
		return nil, c.sendErr
	}
	return c.sendStream, nil
}

func (c *fakeAgentClient) UpdateAgent(context.Context, *gamev2.UpdateAgentRequest, ...grpc.CallOption) (*gamev2.Agent, error) {
	return nil, errFakeNotImplemented
}

func (c *fakeAgentClient) GetAgent(context.Context, *gamev2.GetAgentRequest, ...grpc.CallOption) (*gamev2.Agent, error) {
	return nil, errFakeNotImplemented
}

func (c *fakeAgentClient) ListAgentMessages(context.Context, *gamev2.ListAgentMessagesRequest, ...grpc.CallOption) (*gamev2.ListAgentMessagesResponse, error) {
	return nil, errFakeNotImplemented
}

func (c *fakeAgentClient) CreatePreset(context.Context, *gamev2.CreatePresetRequest, ...grpc.CallOption) (*gamev2.Preset, error) {
	return nil, errFakeNotImplemented
}

func (c *fakeAgentClient) ListPresets(context.Context, *gamev2.ListPresetsRequest, ...grpc.CallOption) (*gamev2.ListPresetsResponse, error) {
	return nil, errFakeNotImplemented
}

func (c *fakeAgentClient) GetPreset(context.Context, *gamev2.GetPresetRequest, ...grpc.CallOption) (*gamev2.Preset, error) {
	return nil, errFakeNotImplemented
}

func (c *fakeAgentClient) UpdatePreset(context.Context, *gamev2.UpdatePresetRequest, ...grpc.CallOption) (*gamev2.Preset, error) {
	return nil, errFakeNotImplemented
}

func (c *fakeAgentClient) DeletePreset(context.Context, *gamev2.DeletePresetRequest, ...grpc.CallOption) (*emptypb.Empty, error) {
	return nil, errFakeNotImplemented
}

func (c *fakeAgentClient) ListModels(context.Context, *gamev2.ListModelsRequest, ...grpc.CallOption) (*gamev2.ListModelsResponse, error) {
	return nil, errFakeNotImplemented
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

// newAgentHarness wires an AgentHandler with the shared v1 test doubles; the
// manager is pre-populated so owner resolution succeeds unless a test
// overrides it.
func newAgentHarness(t *testing.T, fake *fakeAgentClient) (*AgentHandler, *mockOwnerStore, *mockManager, *fakeSendServer) {
	t.Helper()
	setFakeAgentClient(t, fake)
	store := newMockOwnerStore()
	manager := &mockManager{}
	picker := &mockOwnerPicker{ref: agentclient.ConnRef{OwnerIndex: 2, Owner: "agent-2"}}
	handler := NewAgentHandler(store, picker, manager, bind.NewServerStreamBinder[gamev2.ChatEvent]())
	server := &fakeSendServer{ctx: context.Background()}
	return handler, store, manager, server
}

const agentSession = "templates/saolei/sessions/conv-1"

func chatFrame(text string) *gamev2.ChatEvent {
	return &gamev2.ChatEvent{
		Session: agentSession,
		Payload: &gamev2.ChatEvent_Delta{Delta: &gamev2.BlockDeltaEvent{Index: 0, Text: text}},
	}
}

func TestAgentHandler_Send_AssignsOwnerAndRelaysFrames(t *testing.T) {
	// given: a fresh store and an upstream streaming two deltas then io.EOF
	upstream := &fakeSendStream{
		frames:  []*gamev2.ChatEvent{chatFrame("a"), chatFrame("b")},
		recvErr: io.EOF,
	}
	fake := &fakeAgentClient{sendStream: upstream}
	handler, store, _, server := newAgentHarness(t, fake)

	// when: one Send round-trips
	err := handler.Send(&gamev2.SendRequest{Session: agentSession, Text: "hi"}, server)

	// then: the owner is allocated once, the request reaches the upstream,
	// and every frame is relayed in order to a clean close
	if err != nil {
		t.Fatalf("Send() error = %v, want nil", err)
	}
	if store.createCalls != 1 {
		t.Fatalf("owner Create calls = %d, want 1", store.createCalls)
	}
	if fake.sendReq.GetText() != "hi" {
		t.Fatalf("downstream request text = %q, want %q", fake.sendReq.GetText(), "hi")
	}
	if len(server.frames) != 2 || server.frames[0].GetDelta().GetText() != "a" || server.frames[1].GetDelta().GetText() != "b" {
		t.Fatalf("relayed frames = %d, want [a b]", len(server.frames))
	}
}

func TestAgentHandler_Send_RaceReusesWinningOwner(t *testing.T) {
	// given: a concurrent request already persisted the owner (Create loses)
	winner := &domain.AgentOwner{TemplateID: "saolei", SessionID: "conv-1", OwnerIndex: 1, Owner: "agent-1"}
	store := &raceOwnerStore{winner: winner}
	manager := &mockManager{}
	picker := &mockOwnerPicker{ref: agentclient.ConnRef{OwnerIndex: 2, Owner: "agent-2"}}
	upstream := &fakeSendStream{recvErr: io.EOF}
	fake := &fakeAgentClient{sendStream: upstream}
	setFakeAgentClient(t, fake)
	handler := NewAgentHandler(store, picker, manager, bind.NewServerStreamBinder[gamev2.ChatEvent]())

	// when
	err := handler.Send(&gamev2.SendRequest{Session: agentSession, Text: "hi"}, &fakeSendServer{ctx: context.Background()})

	// then: the winner's owner is reused, not re-picked
	if err != nil {
		t.Fatalf("Send() error = %v, want nil", err)
	}
	if manager.getCalls == nil || (len(manager.getCalls) > 0 && manager.getCalls[0] != 1) {
		t.Fatalf("manager Get calls = %v, want first lookup of winner index 1", manager.getCalls)
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
	store.records[ownerKey("saolei", "conv-1")] = &domain.AgentOwner{TemplateID: "saolei", SessionID: "conv-1", OwnerIndex: 7, Owner: "agent-7"}
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
	handler, _, _, _ := newAgentHarness(t, fake)

	err := handler.Send(&gamev2.SendRequest{Session: agentSession, Text: "hi"}, &fakeSendServer{ctx: context.Background()})

	if status.Code(err) != codes.Unavailable {
		t.Fatalf("Send() code = %v, want Unavailable", status.Code(err))
	}
}

func TestAgentHandler_Send_UpstreamRejectsRequestWithOriginalCode(t *testing.T) {
	// given: agent_v2 rejects the request at stream open with a gRPC status
	// (e.g. empty text) — the proxy must preserve the agent-level code so the
	// front end sees the mapped HTTP 400, not a 503 hop failure
	// (contracts/agent-api.md §3).
	fake := &fakeAgentClient{sendErr: status.Error(codes.InvalidArgument, "text must be non-empty")}
	handler, _, _, _ := newAgentHarness(t, fake)

	err := handler.Send(&gamev2.SendRequest{Session: agentSession, Text: ""}, &fakeSendServer{ctx: context.Background()})

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Send() code = %v, want InvalidArgument (original code preserved)", status.Code(err))
	}
}

func TestAgentHandler_Send_EmptyTextRejectedWithoutAllocation(t *testing.T) {
	// given: a valid resource name but an empty message — the routing-layer
	// validation rejects it before any owner allocation
	fake := &fakeAgentClient{sendStream: &fakeSendStream{recvErr: io.EOF}}
	handler, store, _, _ := newAgentHarness(t, fake)

	err := handler.Send(&gamev2.SendRequest{Session: agentSession, Text: ""}, &fakeSendServer{ctx: context.Background()})

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Send() code = %v, want InvalidArgument", status.Code(err))
	}
	if store.createCalls != 0 {
		t.Fatalf("owner Create calls = %d, want 0 (no allocation on invalid input)", store.createCalls)
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
	handler, _, _, server := newAgentHarness(t, fake)

	err := handler.Send(&gamev2.SendRequest{Session: agentSession, Text: "hi"}, server)

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Send() code = %v, want InvalidArgument (original code preserved)", status.Code(err))
	}
	if len(server.frames) != 1 {
		t.Fatalf("relayed frames = %d, want the 1 frame produced before the failure", len(server.frames))
	}
}
