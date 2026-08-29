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

// fakeSendStream is the upstream ConversationService_SendClient double: the
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

// fakeSendServer is the downstream ConversationService_SendServer double:
// the relayed frame recorder with a real context for the handler.
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

// fakeConversationClient is the downstream agent_v2 client double. The
// generated client writes the request while opening the stream (generic
// stream shape), so the request is recorded here rather than on the stream.
type fakeConversationClient struct {
	sendStream *fakeSendStream
	sendErr    error
	sendReq    *gamev2.SendRequest

	history    *gamev2.ListHistoryResponse
	historyErr error

	disposeErr error
}

func (c *fakeConversationClient) Send(_ context.Context, req *gamev2.SendRequest, _ ...grpc.CallOption) (gamev2.ConversationService_SendClient, error) {
	c.sendReq = req
	if c.sendErr != nil {
		return nil, c.sendErr
	}
	return c.sendStream, nil
}

func (c *fakeConversationClient) ListHistory(_ context.Context, _ *gamev2.ListHistoryRequest, _ ...grpc.CallOption) (*gamev2.ListHistoryResponse, error) {
	if c.historyErr != nil {
		return nil, c.historyErr
	}
	return c.history, nil
}

func (c *fakeConversationClient) Dispose(_ context.Context, _ *gamev2.DisposeRequest, _ ...grpc.CallOption) (*emptypb.Empty, error) {
	if c.disposeErr != nil {
		return nil, c.disposeErr
	}
	return &emptypb.Empty{}, nil
}

// setFakeConversationClient replaces the client constructor seam and restores
// it on cleanup.
func setFakeConversationClient(t *testing.T, fake *fakeConversationClient) {
	t.Helper()
	old := newConversationClient
	newConversationClient = func(_ *grpc.ClientConn) gamev2.ConversationServiceClient {
		return fake
	}
	t.Cleanup(func() { newConversationClient = old })
}

// newConversationHarness wires a ConversationHandler with the shared v1 test
// doubles; the manager is pre-populated so owner resolution succeeds unless a
// test overrides it.
func newConversationHarness(t *testing.T, fake *fakeConversationClient) (*ConversationHandler, *mockOwnerStore, *mockManager, *fakeSendServer) {
	t.Helper()
	setFakeConversationClient(t, fake)
	store := newMockOwnerStore()
	manager := &mockManager{}
	picker := &mockOwnerPicker{ref: agentclient.ConnRef{OwnerIndex: 2, Owner: "agent-2"}}
	handler := NewConversationHandler(store, picker, manager, bind.NewServerStreamBinder[gamev2.ChatEvent]())
	server := &fakeSendServer{ctx: context.Background()}
	return handler, store, manager, server
}

const conversationSession = "templates/saolei/sessions/conv-1"

func chatFrame(text string) *gamev2.ChatEvent {
	return &gamev2.ChatEvent{
		Session: conversationSession,
		Payload: &gamev2.ChatEvent_Delta{Delta: &gamev2.BlockDeltaEvent{Index: 0, Text: text}},
	}
}

func TestConversationHandler_Send_AssignsOwnerAndRelaysFrames(t *testing.T) {
	// given: a fresh store and an upstream streaming two deltas then io.EOF
	upstream := &fakeSendStream{
		frames:  []*gamev2.ChatEvent{chatFrame("a"), chatFrame("b")},
		recvErr: io.EOF,
	}
	fake := &fakeConversationClient{sendStream: upstream}
	handler, store, _, server := newConversationHarness(t, fake)

	// when: one Send round-trips
	err := handler.Send(&gamev2.SendRequest{Session: conversationSession, Text: "hi"}, server)

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

func TestConversationHandler_Send_RaceReusesWinningOwner(t *testing.T) {
	// given: a concurrent Send already persisted the owner (Create loses)
	winner := &domain.AgentOwner{TemplateID: "saolei", SessionID: "conv-1", OwnerIndex: 1, Owner: "agent-1"}
	store := &raceOwnerStore{winner: winner}
	manager := &mockManager{}
	picker := &mockOwnerPicker{ref: agentclient.ConnRef{OwnerIndex: 2, Owner: "agent-2"}}
	upstream := &fakeSendStream{recvErr: io.EOF}
	fake := &fakeConversationClient{sendStream: upstream}
	setFakeConversationClient(t, fake)
	handler := NewConversationHandler(store, picker, manager, bind.NewServerStreamBinder[gamev2.ChatEvent]())

	// when
	err := handler.Send(&gamev2.SendRequest{Session: conversationSession, Text: "hi"}, &fakeSendServer{ctx: context.Background()})

	// then: the winner's owner is reused, not re-picked
	if err != nil {
		t.Fatalf("Send() error = %v, want nil", err)
	}
	if manager.getCalls == nil || (len(manager.getCalls) > 0 && manager.getCalls[0] != 1) {
		t.Fatalf("manager Get calls = %v, want first lookup of winner index 1", manager.getCalls)
	}
}

func TestConversationHandler_Send_InvalidSessionRejectedWithoutAllocation(t *testing.T) {
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
			fake := &fakeConversationClient{sendStream: &fakeSendStream{recvErr: io.EOF}}
			handler, store, _, _ := newConversationHarness(t, fake)

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

func TestConversationHandler_Send_InstanceUnreachable(t *testing.T) {
	// given: the owner exists but its instance has no cached connection
	upstream := &fakeSendStream{recvErr: io.EOF}
	fake := &fakeConversationClient{sendStream: upstream}
	handler, store, manager, _ := newConversationHarness(t, fake)
	store.records[ownerKey("saolei", "conv-1")] = &domain.AgentOwner{TemplateID: "saolei", SessionID: "conv-1", OwnerIndex: 7, Owner: "agent-7"}
	manager.getErr = errors.New("no connection for owner index 7")

	// when
	err := handler.Send(&gamev2.SendRequest{Session: conversationSession, Text: "hi"}, &fakeSendServer{ctx: context.Background()})

	// then: proxy→agent_v2 break maps to UNAVAILABLE (503, conversation-api §2.1)
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("Send() code = %v, want Unavailable", status.Code(err))
	}
}

func TestConversationHandler_Send_UpstreamOpenFailed(t *testing.T) {
	// given: the upstream refuses the stream with a non-status transport
	// failure — a proxy→agent_v2 hop break maps to UNAVAILABLE
	fake := &fakeConversationClient{sendErr: errors.New("connection refused")}
	handler, _, _, _ := newConversationHarness(t, fake)

	err := handler.Send(&gamev2.SendRequest{Session: conversationSession, Text: "hi"}, &fakeSendServer{ctx: context.Background()})

	if status.Code(err) != codes.Unavailable {
		t.Fatalf("Send() code = %v, want Unavailable", status.Code(err))
	}
}

func TestConversationHandler_Send_UpstreamRejectsRequestWithOriginalCode(t *testing.T) {
	// given: agent_v2 rejects the request at stream open with a gRPC status
	// (e.g. empty text) — the proxy must preserve the agent-level code so the
	// front end sees the mapped HTTP 400, not a 503 hop failure
	// (contracts/conversation-api.md §2.1).
	fake := &fakeConversationClient{sendErr: status.Error(codes.InvalidArgument, "text must be non-empty")}
	handler, _, _, _ := newConversationHarness(t, fake)

	err := handler.Send(&gamev2.SendRequest{Session: conversationSession, Text: ""}, &fakeSendServer{ctx: context.Background()})

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Send() code = %v, want InvalidArgument (original code preserved)", status.Code(err))
	}
}

func TestConversationHandler_Send_EmptyTextRejectedWithoutAllocation(t *testing.T) {
	// given: a valid resource name but an empty message — the routing-layer
	// validation rejects it before any owner allocation
	fake := &fakeConversationClient{sendStream: &fakeSendStream{recvErr: io.EOF}}
	handler, store, _, _ := newConversationHarness(t, fake)

	err := handler.Send(&gamev2.SendRequest{Session: conversationSession, Text: ""}, &fakeSendServer{ctx: context.Background()})

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Send() code = %v, want InvalidArgument", status.Code(err))
	}
	if store.createCalls != 0 {
		t.Fatalf("owner Create calls = %d, want 0 (no allocation on invalid input)", store.createCalls)
	}
}

func TestConversationHandler_Send_UpstreamStatusPassthrough(t *testing.T) {
	// given: the upstream fails mid-stream with a gRPC status — the proxy
	// must not rewrite the agent-level code
	upstream := &fakeSendStream{
		frames:  []*gamev2.ChatEvent{chatFrame("partial")},
		recvErr: status.Error(codes.InvalidArgument, "empty text"),
	}
	fake := &fakeConversationClient{sendStream: upstream}
	handler, _, _, server := newConversationHarness(t, fake)

	err := handler.Send(&gamev2.SendRequest{Session: conversationSession, Text: "hi"}, server)

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Send() code = %v, want InvalidArgument (original code preserved)", status.Code(err))
	}
	if len(server.frames) != 1 {
		t.Fatalf("relayed frames = %d, want the 1 frame produced before the failure", len(server.frames))
	}
}

func TestConversationHandler_ListHistory_ShortCircuitWithoutOwner(t *testing.T) {
	// given: the session never sent a message (no owner)
	upstream := &fakeSendStream{recvErr: io.EOF}
	fake := &fakeConversationClient{sendStream: upstream}
	handler, store, manager, _ := newConversationHarness(t, fake)

	resp, err := handler.ListHistory(context.Background(), &gamev2.ListHistoryRequest{Session: conversationSession})

	// then: empty 200 response, no allocation, no downstream call
	if err != nil {
		t.Fatalf("ListHistory() error = %v, want nil", err)
	}
	if len(resp.GetMessages()) != 0 {
		t.Fatalf("messages = %d, want 0", len(resp.GetMessages()))
	}
	if store.createCalls != 0 {
		t.Fatalf("owner Create calls = %d, want 0 (read path never allocates)", store.createCalls)
	}
	if len(manager.getCalls) != 0 {
		t.Fatalf("manager Get calls = %d, want 0", len(manager.getCalls))
	}
}

func TestConversationHandler_ListHistory_ForwardsToOwner(t *testing.T) {
	// given: an allocated owner and a downstream history response
	fake := &fakeConversationClient{
		history: &gamev2.ListHistoryResponse{Messages: []*gamev2.HistoryMessage{{
			MessageId: "m1",
			Role:      gamev2.Role_ROLE_USER,
		}}},
	}
	handler, store, _, _ := newConversationHarness(t, fake)
	store.records[ownerKey("saolei", "conv-1")] = &domain.AgentOwner{TemplateID: "saolei", SessionID: "conv-1", OwnerIndex: 3, Owner: "agent-3"}

	resp, err := handler.ListHistory(context.Background(), &gamev2.ListHistoryRequest{Session: conversationSession})

	if err != nil {
		t.Fatalf("ListHistory() error = %v, want nil", err)
	}
	if len(resp.GetMessages()) != 1 || resp.GetMessages()[0].GetMessageId() != "m1" {
		t.Fatalf("messages = %v, want one m1 message", resp.GetMessages())
	}
}

func TestConversationHandler_ListHistory_InvalidSession(t *testing.T) {
	fake := &fakeConversationClient{}
	handler, _, _, _ := newConversationHarness(t, fake)

	_, err := handler.ListHistory(context.Background(), &gamev2.ListHistoryRequest{Session: "nope"})

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("ListHistory() code = %v, want InvalidArgument", status.Code(err))
	}
}

func TestConversationHandler_Dispose_ShortCircuitWithoutOwner(t *testing.T) {
	// given: no owner — dispose is idempotent at the proxy
	upstream := &fakeSendStream{recvErr: io.EOF}
	fake := &fakeConversationClient{sendStream: upstream}
	handler, store, manager, _ := newConversationHarness(t, fake)

	resp, err := handler.Dispose(context.Background(), &gamev2.DisposeRequest{Session: conversationSession})

	if err != nil {
		t.Fatalf("Dispose() error = %v, want nil (idempotent)", err)
	}
	if resp == nil {
		t.Fatal("Dispose() response = nil, want Empty")
	}
	if store.createCalls != 0 {
		t.Fatalf("owner Create calls = %d, want 0 (read path never allocates)", store.createCalls)
	}
	if len(manager.getCalls) != 0 {
		t.Fatalf("manager Get calls = %d, want 0", len(manager.getCalls))
	}
}

func TestConversationHandler_Dispose_ForwardsToOwner(t *testing.T) {
	// given: an allocated owner and a healthy downstream
	fake := &fakeConversationClient{}
	handler, store, _, _ := newConversationHarness(t, fake)
	store.records[ownerKey("saolei", "conv-1")] = &domain.AgentOwner{TemplateID: "saolei", SessionID: "conv-1", OwnerIndex: 3, Owner: "agent-3"}

	resp, err := handler.Dispose(context.Background(), &gamev2.DisposeRequest{Session: conversationSession})

	if err != nil {
		t.Fatalf("Dispose() error = %v, want nil", err)
	}
	if resp == nil {
		t.Fatal("Dispose() response = nil, want Empty")
	}
	// The owner record survives dispose: it is an affinity anchor, not
	// session state (specs/049-agent-v2-dsh-init/data-model.md §2.9).
	if _, ok := store.records[ownerKey("saolei", "conv-1")]; !ok {
		t.Fatal("owner record was deleted; it must outlive the session dispose")
	}
}
