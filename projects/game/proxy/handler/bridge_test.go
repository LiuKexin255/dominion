package handler

import (
	"context"
	"errors"
	"testing"

	game "dominion/projects/game"
	"dominion/projects/game/pkg/bind"
	"dominion/projects/game/proxy/domain"
	"dominion/projects/game/proxy/runtime/agentclient"
	gamev2 "dominion/projects/game/v2"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakeBridgeClient is the downstream DesktopBridgeServiceClient double: it
// hands the handler the scripted bidi stream (or refuses the open).
type fakeBridgeClient struct {
	connectStream gamev2.DesktopBridgeService_ConnectClient
	connectErr    error
}

func (c *fakeBridgeClient) Connect(_ context.Context, _ ...grpc.CallOption) (gamev2.DesktopBridgeService_ConnectClient, error) {
	return c.connectStream, c.connectErr
}

// setFakeBridgeClient replaces the bridge client constructor seam and
// restores it on cleanup.
func setFakeBridgeClient(t *testing.T, fake *fakeBridgeClient) {
	t.Helper()
	old := newBridgeClient
	newBridgeClient = func(_ *grpc.ClientConn) gamev2.DesktopBridgeServiceClient {
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
	handler := NewDesktopBridgeHandler(store, picker, manager, bind.NewBinder())
	return handler, store, manager
}

// probeFrame is the gateway-injected first frame: a StatusSignal ACTIVE
// probe carrying the URL-derived routing pair.
func probeFrame(templateID, sessionID string) *game.UserFrame {
	return &game.UserFrame{
		TemplateId: templateID,
		SessionId:  sessionID,
		Payload: &game.UserFrame_FlowParts{FlowParts: &game.FlowParts{Parts: []*game.FlowPart{
			{Kind: &game.FlowPart_Status{Status: &game.StatusSignal{Status: game.StatusSignalStatus_STATUS_SIGNAL_STATUS_ACTIVE}}},
		}}},
	}
}

// bridgePlumbing holds the connected stream pair of one Connect call:
// inbound carries the frames the handler reads from the desktop (pre-loaded
// with the probe), outbound collects the frames it sends downstream, and the
// agent-side channels observe the upstream direction.
type bridgePlumbing struct {
	stream    *mockProxyStream
	inbound   chan *game.UserFrame
	outbound  chan *game.TeamFrame
	agent     *mockAgentStream
	agentSent chan *game.UserFrame
	agentRecv chan *game.TeamFrame
}

// newBridgePlumbing wires a probe-carrying desktop stream against an open
// agent stream. Close inbound (and agentRecv) to end the relay.
func newBridgePlumbing(templateID, sessionID string) *bridgePlumbing {
	inbound := make(chan *game.UserFrame, 4)
	inbound <- probeFrame(templateID, sessionID)
	outbound := make(chan *game.TeamFrame, 4)
	agentSent := make(chan *game.UserFrame, 4)
	agentRecv := make(chan *game.TeamFrame, 4)
	return &bridgePlumbing{
		stream:    &mockProxyStream{ctx: context.Background(), recvCh: inbound, sendCh: outbound},
		inbound:   inbound,
		outbound:  outbound,
		agent:     &mockAgentStream{recvCh: agentRecv, sendCh: agentSent},
		agentSent: agentSent,
		agentRecv: agentRecv,
	}
}

// close ends the relay from the desktop side: inbound EOF is a clean close
// for the binder.
func (p *bridgePlumbing) close() {
	close(p.inbound)
	close(p.agentRecv)
}

func TestDesktopBridgeHandler_Connect_AllocatesOwnerAndReplaysFirstFrame(t *testing.T) {
	// given: a fresh store — the desktop connects before UpdateAgent
	plumbing := newBridgePlumbing("saolei", "conv-1")
	handler, store, manager := newBridgeHarness(t, &fakeBridgeClient{connectStream: plumbing.agent})

	done := make(chan error, 1)
	go func() { done <- handler.Connect(plumbing.stream) }()

	frame := <-plumbing.agentSent
	plumbing.close()

	// then: the owner was allocated get-or-create and the upstream saw the
	// exact probe frame (identity intact)
	if err := <-done; err != nil {
		t.Fatalf("Connect() error = %v, want nil", err)
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
	if frame.GetTemplateId() != "saolei" || frame.GetSessionId() != "conv-1" {
		t.Fatalf("replayed frame identity = (%q, %q), want (saolei, conv-1)",
			frame.GetTemplateId(), frame.GetSessionId())
	}
	if fs := frame.GetFlowParts().GetParts()[0].GetStatus(); fs == nil {
		t.Fatalf("replayed frame payload = %+v, want the status probe", frame)
	}
}

func TestDesktopBridgeHandler_Connect_ExistingOwnerReusedWithoutAllocation(t *testing.T) {
	// given: the owner already exists and no instances are listed — a
	// re-pick would fail, proving the existing owner is reused
	plumbing := newBridgePlumbing("saolei", "conv-1")
	handler, store, manager := newBridgeHarness(t, &fakeBridgeClient{connectStream: plumbing.agent})
	seedAgentOwner(store, 1)

	done := make(chan error, 1)
	go func() { done <- handler.Connect(plumbing.stream) }()

	<-plumbing.agentSent
	plumbing.close()

	if err := <-done; err != nil {
		t.Fatalf("Connect() error = %v, want nil", err)
	}
	if store.createCalls != 0 {
		t.Fatalf("owner Create calls = %d, want 0 (existing owner reused)", store.createCalls)
	}
	if len(manager.getCalls) != 1 || manager.getCalls[0] != 1 {
		t.Fatalf("manager Get calls = %v, want [1]", manager.getCalls)
	}
}

func TestDesktopBridgeHandler_Connect_RaceReusesWinningOwner(t *testing.T) {
	// given: a concurrent request already persisted the owner (Create loses)
	winner := &domain.AgentOwner{TemplateID: "saolei", SessionID: "conv-1", OwnerIndex: 1, Owner: "agent-1"}
	store := &raceOwnerStore{winner: winner}
	manager := &mockManager{}
	picker := &mockOwnerPicker{ref: agentclient.ConnRef{OwnerIndex: 2, Owner: "agent-2"}}
	plumbing := newBridgePlumbing("saolei", "conv-1")
	setFakeBridgeClient(t, &fakeBridgeClient{connectStream: plumbing.agent})
	handler := NewDesktopBridgeHandler(store, picker, manager, bind.NewBinder())

	done := make(chan error, 1)
	go func() { done <- handler.Connect(plumbing.stream) }()

	<-plumbing.agentSent
	plumbing.close()

	if err := <-done; err != nil {
		t.Fatalf("Connect() error = %v, want nil", err)
	}
	if len(manager.getCalls) != 1 || manager.getCalls[0] != 1 {
		t.Fatalf("manager Get calls = %v, want [1] (winner re-read)", manager.getCalls)
	}
}

func TestDesktopBridgeHandler_Connect_InvalidIdentity(t *testing.T) {
	tests := []struct {
		name       string
		templateID string
		sessionID  string
	}{
		{name: "missing template_id", templateID: "", sessionID: "conv-1"},
		{name: "missing session_id", templateID: "saolei", sessionID: ""},
		{name: "unknown template", templateID: "other", sessionID: "conv-1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plumbing := newBridgePlumbing(tt.templateID, tt.sessionID)
			handler, store, _ := newBridgeHarness(t, &fakeBridgeClient{connectStream: plumbing.agent})

			err := handler.Connect(plumbing.stream)

			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("Connect() code = %v, want InvalidArgument", status.Code(err))
			}
			if store.createCalls != 0 {
				t.Fatalf("owner Create calls = %d, want 0 (no allocation on invalid identity)", store.createCalls)
			}
		})
	}
}

func TestDesktopBridgeHandler_Connect_FirstFrameRecvError(t *testing.T) {
	handler, store, _ := newBridgeHarness(t, &fakeBridgeClient{})

	recvCh := make(chan *game.UserFrame)
	close(recvCh)
	err := handler.Connect(&mockProxyStream{ctx: context.Background(), recvCh: recvCh})

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Connect() code = %v, want InvalidArgument", status.Code(err))
	}
	if store.createCalls != 0 {
		t.Fatalf("owner Create calls = %d, want 0", store.createCalls)
	}
}

func TestDesktopBridgeHandler_Connect_NoInstancesMapsToUnavailable(t *testing.T) {
	// given: no live agent_v2 instance to allocate
	plumbing := newBridgePlumbing("saolei", "conv-1")
	store := newMockOwnerStore()
	picker := &mockOwnerPicker{err: domain.ErrNoAgentInstances}
	handler := NewDesktopBridgeHandler(store, picker, &mockManager{}, bind.NewBinder())

	err := handler.Connect(plumbing.stream)

	if status.Code(err) != codes.Unavailable {
		t.Fatalf("Connect() code = %v, want Unavailable", status.Code(err))
	}
	if store.createCalls != 0 {
		t.Fatalf("owner Create calls = %d, want 0", store.createCalls)
	}
}

func TestDesktopBridgeHandler_Connect_InstanceUnreachable(t *testing.T) {
	plumbing := newBridgePlumbing("saolei", "conv-1")
	handler, store, manager := newBridgeHarness(t, &fakeBridgeClient{connectStream: plumbing.agent})
	seedAgentOwner(store, 7)
	manager.getErr = errors.New("no connection for owner index 7")

	err := handler.Connect(plumbing.stream)

	if status.Code(err) != codes.Unavailable {
		t.Fatalf("Connect() code = %v, want Unavailable", status.Code(err))
	}
}

func TestDesktopBridgeHandler_Connect_UpstreamOpenFailed(t *testing.T) {
	// given: the upstream refuses the bidi stream with a non-status
	// transport failure — a proxy→agent_v2 hop break maps to UNAVAILABLE
	plumbing := newBridgePlumbing("saolei", "conv-1")
	handler, store, _ := newBridgeHarness(t, &fakeBridgeClient{connectErr: errors.New("connection refused")})
	seedAgentOwner(store, 1)

	err := handler.Connect(plumbing.stream)

	if status.Code(err) != codes.Unavailable {
		t.Fatalf("Connect() code = %v, want Unavailable", status.Code(err))
	}
}

func TestDesktopBridgeHandler_Connect_UpstreamOpenStatusPreserved(t *testing.T) {
	// given: the upstream rejects the open with a gRPC status — the code is
	// kept unchanged (propagateAgentError semantics)
	plumbing := newBridgePlumbing("saolei", "conv-1")
	handler, store, _ := newBridgeHarness(t, &fakeBridgeClient{
		connectErr: status.Error(codes.Unimplemented, "method Connect not implemented"),
	})
	seedAgentOwner(store, 1)

	err := handler.Connect(plumbing.stream)

	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("Connect() code = %v, want Unimplemented (original code preserved)", status.Code(err))
	}
}

func TestDesktopBridgeHandler_Connect_BinderErrorPropagates(t *testing.T) {
	// given: the pump reports a failure — it is returned for the gRPC layer
	store := newMockOwnerStore()
	seedAgentOwner(store, 1)
	plumbing := newBridgePlumbing("saolei", "conv-1")
	setFakeBridgeClient(t, &fakeBridgeClient{connectStream: plumbing.agent})
	handler := NewDesktopBridgeHandler(store, &mockOwnerPicker{}, &mockManager{}, &mockBinder{err: errors.New("bind failed")})

	err := handler.Connect(plumbing.stream)

	if err == nil || err.Error() != "bind failed" {
		t.Fatalf("Connect() error = %v, want the binder error propagated", err)
	}
}

func TestDesktopBridgeHandler_Connect_RelaysBothDirections(t *testing.T) {
	// given: an open relay — a result frame rides desktop → upstream and an
	// operation frame rides upstream → desktop
	plumbing := newBridgePlumbing("saolei", "conv-1")
	handler, store, _ := newBridgeHarness(t, &fakeBridgeClient{connectStream: plumbing.agent})
	seedAgentOwner(store, 1)

	done := make(chan error, 1)
	go func() { done <- handler.Connect(plumbing.stream) }()

	<-plumbing.agentSent // the first-frame replay drains

	plumbing.inbound <- &game.UserFrame{
		TemplateId: "saolei",
		SessionId:  "conv-1",
		Payload: &game.UserFrame_FlowParts{FlowParts: &game.FlowParts{Parts: []*game.FlowPart{
			{Kind: &game.FlowPart_FlowResult{FlowResult: &game.FlowResultPart{ToolId: "op-1", Status: game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED}}},
		}}},
	}
	upstreamFrame := <-plumbing.agentSent

	plumbing.agentRecv <- &game.TeamFrame{
		TemplateId: "saolei",
		SessionId:  "conv-1",
		Payload: &game.TeamFrame_FlowParts{FlowParts: &game.FlowParts{Parts: []*game.FlowPart{
			{Kind: &game.FlowPart_KeyboardPress{KeyboardPress: &game.KeyboardPressPart{ToolId: "op-2", Key: game.KeyboardKey_KEYBOARD_KEY_F2}}},
		}}},
	}
	downstreamFrame := <-plumbing.outbound

	plumbing.close()

	if err := <-done; err != nil {
		t.Fatalf("Connect() error = %v, want nil", err)
	}
	if fr := upstreamFrame.GetFlowParts().GetParts()[0].GetFlowResult(); fr == nil || fr.GetToolId() != "op-1" {
		t.Fatalf("upstream frame = %+v, want the flow_result op-1 relayed", upstreamFrame)
	}
	if fr := downstreamFrame.GetFlowParts().GetParts()[0].GetKeyboardPress(); fr == nil || fr.GetToolId() != "op-2" {
		t.Fatalf("downstream frame = %+v, want the keyboard_press op-2 relayed", downstreamFrame)
	}
}
