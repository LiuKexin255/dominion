package handler

import (
	"context"
	"errors"
	"testing"

	game "dominion/projects/game"
	"dominion/projects/game/pkg/bind"
	"dominion/projects/game/proxy/domain"
	"dominion/projects/game/proxy/runtime/agentclient"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakeBridgeClient is the downstream DesktopBridgeServiceClient double: it
// hands the handler the scripted response stream (or refuses the open) and
// records the forwarded SendFrame request.
type fakeBridgeClient struct {
	stream        game.DesktopBridgeService_StreamClient
	streamErr     error
	sendFrameResp *game.SendFrameResponse
	sendFrameErr  error
	sendFrameReq  *game.SendFrameRequest
}

func (c *fakeBridgeClient) Stream(_ context.Context, _ *game.StreamRequest, _ ...grpc.CallOption) (game.DesktopBridgeService_StreamClient, error) {
	return c.stream, c.streamErr
}

func (c *fakeBridgeClient) SendFrame(_ context.Context, req *game.SendFrameRequest, _ ...grpc.CallOption) (*game.SendFrameResponse, error) {
	c.sendFrameReq = req
	return c.sendFrameResp, c.sendFrameErr
}

// setFakeBridgeClient replaces the bridge client constructor seam and
// restores it on cleanup.
func setFakeBridgeClient(t *testing.T, fake *fakeBridgeClient) {
	t.Helper()
	old := newBridgeClient
	newBridgeClient = func(_ *grpc.ClientConn) game.DesktopBridgeServiceClient {
		return fake
	}
	t.Cleanup(func() { newBridgeClient = old })
}

// newBridgeHarness wires a DesktopBridgeHandler with the shared test doubles
// and the fake upstream stream.
func newBridgeHarness(t *testing.T, fake *fakeBridgeClient) (*DesktopBridgeHandler, *mockOwnerStore, *mockManager) {
	t.Helper()
	setFakeBridgeClient(t, fake)
	store := newMockOwnerStore()
	manager := &mockManager{}
	picker := &mockOwnerPicker{ref: agentclient.ConnRef{OwnerIndex: 2, Owner: "agent-2"}}
	handler := NewDesktopBridgeHandler(store, picker, manager, bind.NewServerStreamBinder[game.TeamFrame]())
	return handler, store, manager
}

// ---------------------------------------------------------------------------
// Stream forwarding tests
// ---------------------------------------------------------------------------

// streamPlumbing holds the stream pair of one Stream call: upstreamCh carries
// the response frames the agent_v2-side client emits, sent collects what the
// handler forwards downstream.
type streamPlumbing struct {
	upstreamCh chan *game.TeamFrame
	sent       chan *game.TeamFrame
	client     *fakeBridgeClient
	server     *mockProxyStream
}

// newStreamPlumbing wires an open agent_v2-side response stream against the
// downstream stream the handler writes to. Close upstreamCh to end the relay
// cleanly.
func newStreamPlumbing() *streamPlumbing {
	upstreamCh := make(chan *game.TeamFrame, 4)
	sent := make(chan *game.TeamFrame, 4)
	return &streamPlumbing{
		upstreamCh: upstreamCh,
		sent:       sent,
		client: &fakeBridgeClient{
			stream: &mockAgentStream{recvCh: upstreamCh},
		},
		server: &mockProxyStream{ctx: context.Background(), sendCh: sent},
	}
}

func TestDesktopBridgeHandler_Stream_AllocatesOwnerAndRelaysFrames(t *testing.T) {
	// given: a fresh store — the desktop opens the downlink before UpdateTeam
	plumbing := newStreamPlumbing()
	handler, store, manager := newBridgeHarness(t, plumbing.client)

	done := make(chan error, 1)
	go func() {
		done <- handler.Stream(&game.StreamRequest{Session: agentSession}, plumbing.server)
	}()

	plumbing.upstreamCh <- &game.TeamFrame{TemplateId: "saolei", SessionId: "conv-1"}
	relayed := <-plumbing.sent
	plumbing.upstreamCh <- &game.TeamFrame{
		TemplateId: "saolei",
		SessionId:  "conv-1",
		Payload: &game.TeamFrame_FlowParts{FlowParts: &game.FlowParts{Parts: []*game.FlowPart{
			{Kind: &game.FlowPart_KeyboardPress{KeyboardPress: &game.KeyboardPressPart{ToolId: "op-1", Key: game.KeyboardKey_KEYBOARD_KEY_F2}}},
		}}},
	}
	relayedOp := <-plumbing.sent
	close(plumbing.upstreamCh)

	// then: the owner was allocated get-or-create and both frames arrived
	if err := <-done; err != nil {
		t.Fatalf("Stream() error = %v, want nil", err)
	}
	if store.createCalls != 1 {
		t.Fatalf("owner Create calls = %d, want 1 (get-or-create)", store.createCalls)
	}
	owner := store.records[ownerKey("saolei", "conv-1")]
	if owner == nil || owner.TemplateID != "saolei" || owner.SessionID != "conv-1" {
		t.Fatalf("allocated owner = %+v, want the (saolei, conv-1) composite key", owner)
	}
	if len(manager.getCalls) != 1 || manager.getCalls[0] != owner.OwnerIndex {
		t.Fatalf("manager Get calls = %v, want [allocated index]", manager.getCalls)
	}
	if relayed.GetSessionId() != "conv-1" {
		t.Fatalf("relayed frame session_id = %q, want conv-1", relayed.GetSessionId())
	}
	if fr := relayedOp.GetFlowParts().GetParts()[0].GetKeyboardPress(); fr == nil || fr.GetToolId() != "op-1" {
		t.Fatalf("relayed frame = %+v, want the keyboard_press op-1", relayedOp)
	}
}

func TestDesktopBridgeHandler_Stream_ExistingOwnerReusedWithoutAllocation(t *testing.T) {
	// given: the owner already exists — the createCalls==0 assertion below
	// proves the existing owner is reused with no new allocation, and the
	// manager.Get call targets that owner's index
	plumbing := newStreamPlumbing()
	handler, store, manager := newBridgeHarness(t, plumbing.client)
	seedAgentOwner(store, 1)

	done := make(chan error, 1)
	go func() {
		done <- handler.Stream(&game.StreamRequest{Session: agentSession}, plumbing.server)
	}()

	close(plumbing.upstreamCh)

	if err := <-done; err != nil {
		t.Fatalf("Stream() error = %v, want nil", err)
	}
	if store.createCalls != 0 {
		t.Fatalf("owner Create calls = %d, want 0 (existing owner reused)", store.createCalls)
	}
	if len(manager.getCalls) != 1 || manager.getCalls[0] != 1 {
		t.Fatalf("manager Get calls = %v, want [1]", manager.getCalls)
	}
}

func TestDesktopBridgeHandler_Stream_InvalidSession(t *testing.T) {
	tests := []struct {
		name    string
		session string
	}{
		{name: "missing session", session: ""},
		{name: "malformed resource name", session: "templates/saolei/sessions"},
		{name: "unknown template", session: "templates/other/sessions/conv-1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plumbing := newStreamPlumbing()
			handler, store, _ := newBridgeHarness(t, plumbing.client)

			err := handler.Stream(&game.StreamRequest{Session: tt.session}, plumbing.server)

			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("Stream() code = %v, want InvalidArgument", status.Code(err))
			}
			if store.createCalls != 0 {
				t.Fatalf("owner Create calls = %d, want 0 (no allocation on invalid session)", store.createCalls)
			}
		})
	}
}

func TestDesktopBridgeHandler_Stream_NoInstancesMapsToUnavailable(t *testing.T) {
	// given: no live agent_v2 instance to allocate
	plumbing := newStreamPlumbing()
	store := newMockOwnerStore()
	picker := &mockOwnerPicker{err: domain.ErrNoAgentInstances}
	handler := NewDesktopBridgeHandler(store, picker, &mockManager{}, bind.NewServerStreamBinder[game.TeamFrame]())

	err := handler.Stream(&game.StreamRequest{Session: agentSession}, plumbing.server)

	if status.Code(err) != codes.Unavailable {
		t.Fatalf("Stream() code = %v, want Unavailable", status.Code(err))
	}
	if store.createCalls != 0 {
		t.Fatalf("owner Create calls = %d, want 0", store.createCalls)
	}
}

func TestDesktopBridgeHandler_Stream_InstanceUnreachable(t *testing.T) {
	plumbing := newStreamPlumbing()
	handler, store, manager := newBridgeHarness(t, plumbing.client)
	seedAgentOwner(store, 7)
	manager.getErr = errors.New("no connection for owner index 7")

	err := handler.Stream(&game.StreamRequest{Session: agentSession}, plumbing.server)

	if status.Code(err) != codes.Unavailable {
		t.Fatalf("Stream() code = %v, want Unavailable", status.Code(err))
	}
}

func TestDesktopBridgeHandler_Stream_UpstreamOpenFailed(t *testing.T) {
	// given: the upstream refuses the stream with a non-status transport
	// failure — a proxy→agent_v2 hop break maps to UNAVAILABLE
	plumbing := newStreamPlumbing()
	plumbing.client.streamErr = errors.New("connection refused")
	handler, store, _ := newBridgeHarness(t, plumbing.client)
	seedAgentOwner(store, 1)

	err := handler.Stream(&game.StreamRequest{Session: agentSession}, plumbing.server)

	if status.Code(err) != codes.Unavailable {
		t.Fatalf("Stream() code = %v, want Unavailable", status.Code(err))
	}
}

func TestDesktopBridgeHandler_Stream_UpstreamOpenStatusPreserved(t *testing.T) {
	// given: the upstream rejects the open with a gRPC status — the code is
	// kept unchanged
	plumbing := newStreamPlumbing()
	plumbing.client.streamErr = status.Error(codes.PermissionDenied, "rejected")
	handler, store, _ := newBridgeHarness(t, plumbing.client)
	seedAgentOwner(store, 1)

	err := handler.Stream(&game.StreamRequest{Session: agentSession}, plumbing.server)

	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("Stream() code = %v, want PermissionDenied (original code preserved)", status.Code(err))
	}
}

func TestDesktopBridgeHandler_Stream_BinderErrorPropagates(t *testing.T) {
	// given: the pump reports a failure — it is returned for the gRPC layer
	plumbing := newStreamPlumbing()
	setFakeBridgeClient(t, plumbing.client)
	store := newMockOwnerStore()
	seedAgentOwner(store, 1)
	handler := NewDesktopBridgeHandler(store, &mockOwnerPicker{}, &mockManager{}, &mockServerStreamBinder{err: errors.New("stream bind failed")})

	err := handler.Stream(&game.StreamRequest{Session: agentSession}, plumbing.server)

	if err == nil || err.Error() != "stream bind failed" {
		t.Fatalf("Stream() error = %v, want the binder error propagated", err)
	}
}

func TestDesktopBridgeHandler_SendFrame_ForwardsRequestForExistingOwner(t *testing.T) {
	// given: an owner from a previous Stream/UpdateTeam
	req := &game.SendFrameRequest{
		Session: agentSession,
		Frame:   &game.UserFrame{SessionId: "conv-1"},
	}
	fake := &fakeBridgeClient{sendFrameResp: &game.SendFrameResponse{}}
	handler, store, manager := newBridgeHarness(t, fake)
	seedAgentOwner(store, 3)

	// when
	resp, err := handler.SendFrame(context.Background(), req)

	// then: the owner is looked up only (no allocation), the request reaches
	// the upstream via its instance unchanged, and the response passes through
	if err != nil {
		t.Fatalf("SendFrame() error = %v, want nil", err)
	}
	if resp == nil {
		t.Fatal("SendFrame() got nil response")
	}
	if fake.sendFrameReq != req {
		t.Fatalf("downstream request = %p, want the original %p", fake.sendFrameReq, req)
	}
	if store.createCalls != 0 {
		t.Fatalf("owner Create calls = %d, want 0 (SendFrame must not allocate)", store.createCalls)
	}
	if len(manager.getCalls) != 1 || manager.getCalls[0] != 3 {
		t.Fatalf("manager Get calls = %v, want [3] (seeded owner index)", manager.getCalls)
	}
}

func TestDesktopBridgeHandler_SendFrame_NoOwnerReturnsNotFoundWithoutAllocation(t *testing.T) {
	// given: a fresh store — no Stream/UpdateTeam ever materialized the session
	fake := &fakeBridgeClient{sendFrameResp: &game.SendFrameResponse{}}
	handler, store, _ := newBridgeHarness(t, fake)

	// when
	_, err := handler.SendFrame(context.Background(), &game.SendFrameRequest{
		Session: agentSession,
		Frame:   &game.UserFrame{SessionId: "conv-1"},
	})

	// then: lookup-only — NOT_FOUND, no owner record, no downstream call
	if status.Code(err) != codes.NotFound {
		t.Fatalf("SendFrame() code = %v, want NotFound", status.Code(err))
	}
	if store.createCalls != 0 {
		t.Fatalf("owner Create calls = %d, want 0 (SendFrame must not allocate)", store.createCalls)
	}
	if fake.sendFrameReq != nil {
		t.Fatal("downstream SendFrame was called without an owner")
	}
}

func TestDesktopBridgeHandler_SendFrame_InvalidSessionRejectedWithoutAllocation(t *testing.T) {
	tests := []struct {
		name    string
		session string
	}{
		{name: "missing session", session: ""},
		{name: "malformed resource name", session: "templates/saolei/sessions"},
		{name: "unknown template", session: "templates/other/sessions/conv-1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeBridgeClient{sendFrameResp: &game.SendFrameResponse{}}
			handler, store, _ := newBridgeHarness(t, fake)

			_, err := handler.SendFrame(context.Background(), &game.SendFrameRequest{
				Session: tt.session,
				Frame:   &game.UserFrame{SessionId: "conv-1"},
			})

			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("SendFrame() code = %v, want InvalidArgument", status.Code(err))
			}
			if store.createCalls != 0 {
				t.Fatalf("owner Create calls = %d, want 0 (no allocation on invalid input)", store.createCalls)
			}
		})
	}
}

func TestDesktopBridgeHandler_SendFrame_InstanceUnreachable(t *testing.T) {
	// given: the owner exists but its instance has no cached connection
	fake := &fakeBridgeClient{sendFrameResp: &game.SendFrameResponse{}}
	handler, store, manager := newBridgeHarness(t, fake)
	seedAgentOwner(store, 7)
	manager.getErr = errors.New("no connection for owner index 7")

	_, err := handler.SendFrame(context.Background(), &game.SendFrameRequest{
		Session: agentSession,
		Frame:   &game.UserFrame{SessionId: "conv-1"},
	})

	if status.Code(err) != codes.Unavailable {
		t.Fatalf("SendFrame() code = %v, want Unavailable", status.Code(err))
	}
}

func TestDesktopBridgeHandler_SendFrame_DownstreamStatusPreserved(t *testing.T) {
	// given: agent_v2 rejects the uplink frame — the agent-level code must
	// survive the hop unchanged (e.g. no active downlink stream)
	fake := &fakeBridgeClient{sendFrameErr: status.Error(codes.FailedPrecondition, "desktop stream not active")}
	handler, store, _ := newBridgeHarness(t, fake)
	seedAgentOwner(store, 1)

	_, err := handler.SendFrame(context.Background(), &game.SendFrameRequest{
		Session: agentSession,
		Frame:   &game.UserFrame{SessionId: "conv-1"},
	})

	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("SendFrame() code = %v, want FailedPrecondition (original code preserved)", status.Code(err))
	}
}

func TestDesktopBridgeHandler_SendFrame_TransportErrorMapsToInternal(t *testing.T) {
	// given: a non-status transport failure on the proxy→agent_v2 hop —
	// propagateAgentError wraps it as Internal (Cancel forwarding shape)
	fake := &fakeBridgeClient{sendFrameErr: errors.New("connection refused")}
	handler, store, _ := newBridgeHarness(t, fake)
	seedAgentOwner(store, 1)

	_, err := handler.SendFrame(context.Background(), &game.SendFrameRequest{
		Session: agentSession,
		Frame:   &game.UserFrame{SessionId: "conv-1"},
	})

	if status.Code(err) != codes.Internal {
		t.Fatalf("SendFrame() code = %v, want Internal", status.Code(err))
	}
}
