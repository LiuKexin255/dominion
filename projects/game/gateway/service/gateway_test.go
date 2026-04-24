package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"dominion/projects/game/gateway/domain"
	"dominion/projects/game/gateway/domain/sessionmanager"
	"dominion/projects/game/pkg/token"
)

type stubVerifier struct {
	claims *token.Claims
	err    error
}

func (v *stubVerifier) Verify(_ string) (*token.Claims, error) {
	return v.claims, v.err
}

type stubMediaCache struct {
	initSeg     *domain.InitSegmentRef
	initOK      bool
	segments    []*domain.SegmentRef
	snapshot    *domain.SnapshotRef
	snapshotOK  bool
	snapshotErr error
}

func (c *stubMediaCache) StoreInitSegment(mimeType string, data []byte) error {
	c.initSeg = &domain.InitSegmentRef{MimeType: mimeType, Data: data}
	return nil
}

func (c *stubMediaCache) AddSegment(seg *domain.SegmentRef) error {
	c.segments = append(c.segments, seg)
	return nil
}

func (c *stubMediaCache) GetInitSegment() (*domain.InitSegmentRef, bool) {
	if c.initSeg == nil {
		return nil, false
	}
	return c.initSeg, c.initOK
}

func (c *stubMediaCache) GetSegmentsFromLastKeyframe() []*domain.SegmentRef {
	return c.segments
}

func (c *stubMediaCache) GetLatestSnapshot() (*domain.SnapshotRef, bool) {
	if c.snapshot == nil {
		return nil, false
	}
	return c.snapshot, c.snapshotOK
}

func (c *stubMediaCache) RefreshSnapshot() (*domain.SnapshotRef, error) {
	return c.snapshot, c.snapshotErr
}

func newTestService(gatewayID string, verifier token.Verifier) *GatewayService {
	return NewGatewayService(sessionmanager.NewManager(gatewayID), NewControlExecutor(), gatewayID, verifier)
}

func TestGatewayService_ConnectSession(t *testing.T) {
	tests := []struct {
		name          string
		pathSessionID string
		claims        *token.Claims
		verifyErr     error
		gatewayID     string
		wantErr       bool
	}{
		{
			name:          "valid token with matching gateway and session",
			pathSessionID: "session-1",
			claims: &token.Claims{
				SessionID: "session-1",
				GatewayID: "gw-0",
				ExpiresAt: time.Now().Add(5 * time.Minute).Unix(),
			},
			gatewayID: "gw-0",
			wantErr:   false,
		},
		{
			name:          "expired token rejected",
			pathSessionID: "session-1",
			verifyErr:     token.ErrTokenExpired,
			gatewayID:     "gw-0",
			wantErr:       true,
		},
		{
			name:          "invalid token rejected",
			pathSessionID: "session-1",
			verifyErr:     token.ErrTokenInvalid,
			gatewayID:     "gw-0",
			wantErr:       true,
		},
		{
			name:          "wrong gateway ID rejected",
			pathSessionID: "session-1",
			claims: &token.Claims{
				SessionID: "session-1",
				GatewayID: "gw-other",
				ExpiresAt: time.Now().Add(5 * time.Minute).Unix(),
			},
			gatewayID: "gw-0",
			wantErr:   true,
		},
		{
			name:          "wrong session ID rejected",
			pathSessionID: "session-1",
			claims: &token.Claims{
				SessionID: "session-other",
				GatewayID: "gw-0",
				ExpiresAt: time.Now().Add(5 * time.Minute).Unix(),
			},
			gatewayID: "gw-0",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestService(tt.gatewayID, &stubVerifier{claims: tt.claims, err: tt.verifyErr})

			// when
			rt, claims, err := svc.ConnectSession(context.Background(), tt.pathSessionID, "some-token")

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatal("ConnectSession() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("ConnectSession() unexpected error: %v", err)
			}
			if rt == nil {
				t.Fatal("ConnectSession() returned nil runtime")
			}
			if rt.SessionID != tt.pathSessionID {
				t.Fatalf("runtime.SessionID = %q, want %q", rt.SessionID, tt.pathSessionID)
			}
			if claims.SessionID != tt.pathSessionID {
				t.Fatalf("claims.SessionID = %q, want %q", claims.SessionID, tt.pathSessionID)
			}
			if claims.GatewayID != tt.gatewayID {
				t.Fatalf("claims.GatewayID = %q, want %q", claims.GatewayID, tt.gatewayID)
			}
		})
	}
}

func TestGatewayService_ProcessHello_Agent(t *testing.T) {
	svc := newTestService("gw-0", &stubVerifier{})
	rt := svc.sessions.GetOrCreateRuntime("session-1")
	claims := &token.Claims{SessionID: "session-1", GatewayID: "gw-0"}

	// when
	msgs, err := svc.ProcessHello(rt, claims, domain.ClientRoleWindowsAgent, "agent-1")

	// then
	if err != nil {
		t.Fatalf("ProcessHello() error = %v", err)
	}
	if msgs != nil {
		t.Fatalf("ProcessHello() msgs = %v, want nil for agent", msgs)
	}
	updated := svc.sessions.GetRuntime("session-1")
	if updated.AgentConn == nil {
		t.Fatal("AgentConn is nil, want registered agent")
	}
	if updated.AgentConn.ConnID != "agent-1" {
		t.Fatalf("AgentConn.ConnID = %q, want %q", updated.AgentConn.ConnID, "agent-1")
	}
}

func TestGatewayService_ProcessHello_AgentAlreadyConnected(t *testing.T) {
	svc := newTestService("gw-0", &stubVerifier{})
	rt := svc.sessions.GetOrCreateRuntime("session-1")
	claims := &token.Claims{SessionID: "session-1", GatewayID: "gw-0"}

	svc.sessions.RegisterAgent("session-1", &domain.AgentConnection{ConnID: "agent-first"})

	// when
	_, err := svc.ProcessHello(rt, claims, domain.ClientRoleWindowsAgent, "agent-second")

	// then
	if !errors.Is(err, domain.ErrAgentAlreadyConnected) {
		t.Fatalf("ProcessHello() error = %v, want %v", err, domain.ErrAgentAlreadyConnected)
	}
}

func TestGatewayService_ProcessHello_Web(t *testing.T) {
	svc := newTestService("gw-0", &stubVerifier{})
	rt := svc.sessions.GetOrCreateRuntime("session-1")
	claims := &token.Claims{SessionID: "session-1", GatewayID: "gw-0"}

	// given: media cache has init segment and one keyframe segment
	cache := &stubMediaCache{
		initSeg: &domain.InitSegmentRef{
			MimeType: "video/mp4",
			Data:     []byte("init-data"),
		},
		initOK: true,
		segments: []*domain.SegmentRef{
			{SegmentID: "seg-1", Data: []byte("seg-data"), KeyFrame: true},
		},
	}
	svc.mediaCaches["session-1"] = cache

	// when
	msgs, err := svc.ProcessHello(rt, claims, domain.ClientRoleWeb, "web-1")

	// then
	if err != nil {
		t.Fatalf("ProcessHello() error = %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("len(msgs) = %d, want 2 (media_init + segment)", len(msgs))
	}

	// first message: media_init broadcast
	if msgs[0].TargetConnID != "" {
		t.Fatalf("msgs[0].TargetConnID = %q, want empty (broadcast)", msgs[0].TargetConnID)
	}
	initPayload, ok := msgs[0].Message.Payload.(domain.MediaInitPayload)
	if !ok {
		t.Fatal("first message payload is not media_init")
	}
	if initPayload.MimeType != "video/mp4" {
		t.Fatalf("MimeType = %q, want %q", initPayload.MimeType, "video/mp4")
	}
	if string(initPayload.Segment) != "init-data" {
		t.Fatalf("Segment = %q, want %q", string(initPayload.Segment), "init-data")
	}

	// second message: segment broadcast
	if msgs[1].TargetConnID != "" {
		t.Fatalf("msgs[1].TargetConnID = %q, want empty (broadcast)", msgs[1].TargetConnID)
	}
	segPayload, ok := msgs[1].Message.Payload.(domain.MediaSegmentPayload)
	if !ok {
		t.Fatal("second message payload is not media_segment")
	}
	if segPayload.SegmentID != "seg-1" {
		t.Fatalf("SegmentID = %q, want %q", segPayload.SegmentID, "seg-1")
	}

	// web connection registered
	updated := svc.sessions.GetRuntime("session-1")
	if len(updated.WebConns) != 1 {
		t.Fatalf("len(WebConns) = %d, want 1", len(updated.WebConns))
	}
	if updated.WebConns[0].ConnID != "web-1" {
		t.Fatalf("WebConns[0].ConnID = %q, want %q", updated.WebConns[0].ConnID, "web-1")
	}
}

func TestGatewayService_ProcessHello_WebNoCache(t *testing.T) {
	svc := newTestService("gw-0", &stubVerifier{})
	rt := svc.sessions.GetOrCreateRuntime("session-1")
	claims := &token.Claims{SessionID: "session-1", GatewayID: "gw-0"}

	// when: no media cache data
	msgs, err := svc.ProcessHello(rt, claims, domain.ClientRoleWeb, "web-1")

	// then
	if err != nil {
		t.Fatalf("ProcessHello() error = %v", err)
	}
	if msgs != nil {
		t.Fatalf("ProcessHello() msgs = %v, want nil when no cache data", msgs)
	}
}

func TestGatewayService_HandleAgentMessage_MediaInit(t *testing.T) {
	svc := newTestService("gw-0", &stubVerifier{})
	svc.sessions.GetOrCreateRuntime("session-1")
	cache := new(stubMediaCache)
	svc.mediaCaches["session-1"] = cache

	msg := &domain.Message{
		SessionID: "session-1",
		Payload: domain.MediaInitPayload{
			MimeType: "video/mp4",
			Segment:  []byte("init-bytes"),
		},
	}

	// when
	msgs, err := svc.HandleAgentMessage(context.Background(), "session-1", msg)

	// then
	if err != nil {
		t.Fatalf("HandleAgentMessage() error = %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("len(msgs) = %d, want 1", len(msgs))
	}
	if msgs[0].TargetConnID != "" {
		t.Fatalf("TargetConnID = %q, want empty (broadcast)", msgs[0].TargetConnID)
	}
	// init stored in cache
	if cache.initSeg == nil {
		t.Fatal("init segment not stored in cache")
	}
	if cache.initSeg.MimeType != "video/mp4" {
		t.Fatalf("MimeType = %q, want %q", cache.initSeg.MimeType, "video/mp4")
	}
	if string(cache.initSeg.Data) != "init-bytes" {
		t.Fatalf("Data = %q, want %q", string(cache.initSeg.Data), "init-bytes")
	}
}

func TestGatewayService_HandleAgentMessage_MediaSegment(t *testing.T) {
	svc := newTestService("gw-0", &stubVerifier{})
	svc.sessions.GetOrCreateRuntime("session-1")
	cache := new(stubMediaCache)
	svc.mediaCaches["session-1"] = cache

	msg := &domain.Message{
		SessionID: "session-1",
		Payload: domain.MediaSegmentPayload{
			SegmentID: "seg-42",
			Segment:   []byte("fMP4-chunk"),
			KeyFrame:  true,
		},
	}

	// when
	msgs, err := svc.HandleAgentMessage(context.Background(), "session-1", msg)

	// then
	if err != nil {
		t.Fatalf("HandleAgentMessage() error = %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("len(msgs) = %d, want 1", len(msgs))
	}
	if msgs[0].TargetConnID != "" {
		t.Fatalf("TargetConnID = %q, want empty (broadcast)", msgs[0].TargetConnID)
	}
	// segment added to cache
	if len(cache.segments) != 1 {
		t.Fatalf("len(cache.segments) = %d, want 1", len(cache.segments))
	}
	if cache.segments[0].SegmentID != "seg-42" {
		t.Fatalf("SegmentID = %q, want %q", cache.segments[0].SegmentID, "seg-42")
	}
	if !cache.segments[0].KeyFrame {
		t.Fatal("KeyFrame = false, want true")
	}
}

func TestGatewayService_HandleAgentMessage_ControlAck(t *testing.T) {
	svc := newTestService("gw-0", &stubVerifier{})
	svc.sessions.GetOrCreateRuntime("session-1")

	// given: inflight operation from web-1
	req := domain.ControlRequestPayload{
		RequestID: "op-ack",
		Kind:      domain.OperationKindMouseClick,
		X:         100,
		Y:         200,
	}
	svc.control.SubmitOperation("session-1", req, "web-1")

	msg := &domain.Message{
		SessionID: "session-1",
		Payload: domain.ControlAckPayload{
			RequestID: "op-ack",
		},
	}

	// when
	msgs, err := svc.HandleAgentMessage(context.Background(), "session-1", msg)

	// then
	if err != nil {
		t.Fatalf("HandleAgentMessage() error = %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("len(msgs) = %d, want 1", len(msgs))
	}
	if msgs[0].TargetConnID != "web-1" {
		t.Fatalf("TargetConnID = %q, want %q", msgs[0].TargetConnID, "web-1")
	}
	ackPayload, ok := msgs[0].Message.Payload.(domain.ControlAckPayload)
	if !ok {
		t.Fatal("payload is not control_ack")
	}
	if ackPayload.RequestID != "op-ack" {
		t.Fatalf("RequestID = %q, want %q", ackPayload.RequestID, "op-ack")
	}

	// inflight still active (not cleared by ack)
	if !svc.control.HasInflightOperation("session-1") {
		t.Fatal("inflight cleared after ack, should still be active")
	}
}

func TestGatewayService_HandleAgentMessage_ControlResult(t *testing.T) {
	svc := newTestService("gw-0", &stubVerifier{})
	svc.sessions.GetOrCreateRuntime("session-1")

	req := domain.ControlRequestPayload{
		RequestID: "op-result",
		Kind:      domain.OperationKindMouseClick,
		X:         50,
		Y:         75,
	}
	svc.control.SubmitOperation("session-1", req, "web-2")

	msg := &domain.Message{
		SessionID: "session-1",
		Payload: domain.ControlResultPayload{
			RequestID: "op-result",
			Success:   true,
		},
	}

	// when
	msgs, err := svc.HandleAgentMessage(context.Background(), "session-1", msg)

	// then
	if err != nil {
		t.Fatalf("HandleAgentMessage() error = %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("len(msgs) = %d, want 1", len(msgs))
	}
	if msgs[0].TargetConnID != "web-2" {
		t.Fatalf("TargetConnID = %q, want %q", msgs[0].TargetConnID, "web-2")
	}
	resultPayload, ok := msgs[0].Message.Payload.(domain.ControlResultPayload)
	if !ok {
		t.Fatal("payload is not control_result")
	}
	if resultPayload.RequestID != "op-result" {
		t.Fatalf("RequestID = %q, want %q", resultPayload.RequestID, "op-result")
	}
	if !resultPayload.Success {
		t.Fatal("Success = false, want true")
	}

	// inflight cleared
	if svc.control.HasInflightOperation("session-1") {
		t.Fatal("inflight still active after result, should be cleared")
	}
}

func TestGatewayService_HandleAgentMessage_ControlResultFlashSnapshot(t *testing.T) {
	svc := newTestService("gw-0", &stubVerifier{})
	svc.sessions.GetOrCreateRuntime("session-1")

	req := domain.ControlRequestPayload{
		RequestID:     "op-flash",
		Kind:          domain.OperationKindMouseClick,
		FlashSnapshot: true,
		X:             0,
		Y:             0,
	}
	svc.control.SubmitOperation("session-1", req, "web-3")

	now := time.Now()
	cache := &stubMediaCache{
		snapshot: &domain.SnapshotRef{
			Data:        []byte("jpeg-snapshot"),
			MimeType:    "image/jpeg",
			CaptureTime: now,
			Cached:      false,
		},
	}
	svc.mediaCaches["session-1"] = cache

	msg := &domain.Message{
		SessionID: "session-1",
		Payload: domain.ControlResultPayload{
			RequestID: "op-flash",
			Success:   true,
		},
	}

	// when
	msgs, err := svc.HandleAgentMessage(context.Background(), "session-1", msg)

	// then
	if err != nil {
		t.Fatalf("HandleAgentMessage() error = %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("len(msgs) = %d, want 1", len(msgs))
	}
	if msgs[0].TargetConnID != "web-3" {
		t.Fatalf("TargetConnID = %q, want %q", msgs[0].TargetConnID, "web-3")
	}

	// snapshot refreshed and stored on runtime
	rt := svc.sessions.GetRuntime("session-1")
	if rt.LatestSnapshot == nil {
		t.Fatal("LatestSnapshot is nil, want snapshot from refresh")
	}
	if string(rt.LatestSnapshot.Data) != "jpeg-snapshot" {
		t.Fatalf("LatestSnapshot.Data = %q, want %q", string(rt.LatestSnapshot.Data), "jpeg-snapshot")
	}
}

func TestGatewayService_HandleAgentMessage_ControlAckNoInflight(t *testing.T) {
	svc := newTestService("gw-0", &stubVerifier{})
	svc.sessions.GetOrCreateRuntime("session-1")

	msg := &domain.Message{
		SessionID: "session-1",
		Payload: domain.ControlAckPayload{
			RequestID: "op-missing",
		},
	}

	// when
	_, err := svc.HandleAgentMessage(context.Background(), "session-1", msg)

	// then
	if !errors.Is(err, domain.ErrSessionNotFound) {
		t.Fatalf("HandleAgentMessage() error = %v, want %v", err, domain.ErrSessionNotFound)
	}
}

func TestGatewayService_HandleAgentMessage_ControlResultNoInflight(t *testing.T) {
	svc := newTestService("gw-0", &stubVerifier{})
	svc.sessions.GetOrCreateRuntime("session-1")

	msg := &domain.Message{
		SessionID: "session-1",
		Payload: domain.ControlResultPayload{
			RequestID: "op-missing",
			Success:   true,
		},
	}

	// when
	_, err := svc.HandleAgentMessage(context.Background(), "session-1", msg)

	// then
	if !errors.Is(err, domain.ErrSessionNotFound) {
		t.Fatalf("HandleAgentMessage() error = %v, want %v", err, domain.ErrSessionNotFound)
	}
}

func TestGatewayService_HandleAgentMessage_UnknownPayload(t *testing.T) {
	svc := newTestService("gw-0", &stubVerifier{})
	svc.sessions.GetOrCreateRuntime("session-1")

	msg := &domain.Message{
		SessionID: "session-1",
		Payload:   domain.HelloPayload{Role: domain.ClientRoleWeb},
	}

	// when
	msgs, err := svc.HandleAgentMessage(context.Background(), "session-1", msg)

	// then
	if err != nil {
		t.Fatalf("HandleAgentMessage() unexpected error: %v", err)
	}
	if msgs != nil {
		t.Fatalf("msgs = %v, want nil for unhandled payload", msgs)
	}
}

func TestGatewayService_HandleWebMessage_ControlRequest(t *testing.T) {
	svc := newTestService("gw-0", &stubVerifier{})
	svc.sessions.GetOrCreateRuntime("session-1")

	msg := &domain.Message{
		SessionID: "session-1",
		Payload: domain.ControlRequestPayload{
			RequestID: "op-req",
			Kind:      domain.OperationKindMouseClick,
			X:         10,
			Y:         20,
		},
	}

	// when
	msgs, err := svc.HandleWebMessage(context.Background(), "session-1", "web-1", msg)

	// then
	if err != nil {
		t.Fatalf("HandleWebMessage() error = %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("len(msgs) = %d, want 1", len(msgs))
	}
	// forwarded to agent (TargetConnID="" = agent is single)
	if msgs[0].TargetConnID != "" {
		t.Fatalf("TargetConnID = %q, want empty (forward to agent)", msgs[0].TargetConnID)
	}
	reqPayload, ok := msgs[0].Message.Payload.(domain.ControlRequestPayload)
	if !ok {
		t.Fatal("payload is not control_request")
	}
	if reqPayload.RequestID != "op-req" {
		t.Fatalf("RequestID = %q, want %q", reqPayload.RequestID, "op-req")
	}

	// inflight registered with correct requester
	op := svc.control.GetInflightOperation("session-1")
	if op == nil {
		t.Fatal("inflight operation not registered")
	}
	if op.RequesterConnID != "web-1" {
		t.Fatalf("RequesterConnID = %q, want %q", op.RequesterConnID, "web-1")
	}
	if op.OperationID != "op-req" {
		t.Fatalf("OperationID = %q, want %q", op.OperationID, "op-req")
	}
}

func TestGatewayService_HandleWebMessage_ControlRequestInvalid(t *testing.T) {
	svc := newTestService("gw-0", &stubVerifier{})
	svc.sessions.GetOrCreateRuntime("session-1")

	msg := &domain.Message{
		SessionID: "session-1",
		Payload: domain.ControlRequestPayload{
			RequestID: "op-bad",
			Kind:      domain.OperationKind(""),
		},
	}

	// when
	_, err := svc.HandleWebMessage(context.Background(), "session-1", "web-1", msg)

	// then
	if !errors.Is(err, domain.ErrInvalidMouseAction) {
		t.Fatalf("HandleWebMessage() error = %v, want %v", err, domain.ErrInvalidMouseAction)
	}
}

func TestGatewayService_HandleWebMessage_Ping(t *testing.T) {
	svc := newTestService("gw-0", &stubVerifier{})
	svc.sessions.GetOrCreateRuntime("session-1")

	msg := &domain.Message{
		SessionID: "session-1",
		Payload: domain.PingPayload{
			Nonce: "abc123",
		},
	}

	// when
	msgs, err := svc.HandleWebMessage(context.Background(), "session-1", "web-1", msg)

	// then
	if err != nil {
		t.Fatalf("HandleWebMessage() error = %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("len(msgs) = %d, want 1", len(msgs))
	}
	if msgs[0].TargetConnID != "web-1" {
		t.Fatalf("TargetConnID = %q, want %q", msgs[0].TargetConnID, "web-1")
	}
	pongPayload, ok := msgs[0].Message.Payload.(domain.PongPayload)
	if !ok {
		t.Fatal("payload is not pong")
	}
	if pongPayload.Nonce != "abc123" {
		t.Fatalf("Nonce = %q, want %q", pongPayload.Nonce, "abc123")
	}
}

func TestGatewayService_HandleWebMessage_UnknownPayload(t *testing.T) {
	svc := newTestService("gw-0", &stubVerifier{})
	svc.sessions.GetOrCreateRuntime("session-1")

	msg := &domain.Message{
		SessionID: "session-1",
		Payload:   domain.MediaInitPayload{MimeType: "video/mp4", Segment: []byte("x")},
	}

	// when
	msgs, err := svc.HandleWebMessage(context.Background(), "session-1", "web-1", msg)

	// then
	if err != nil {
		t.Fatalf("HandleWebMessage() unexpected error: %v", err)
	}
	if msgs != nil {
		t.Fatalf("msgs = %v, want nil for unhandled payload", msgs)
	}
}

func TestGatewayService_GetSnapshot(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name       string
		sessionID  string
		setupCache func(svc *GatewayService)
		wantErr    bool
		wantData   string
		wantCached bool
	}{
		{
			name:      "cached snapshot returned",
			sessionID: "session-1",
			setupCache: func(svc *GatewayService) {
				svc.sessions.GetOrCreateRuntime("session-1")
				svc.mediaCaches["session-1"] = &stubMediaCache{
					snapshot: &domain.SnapshotRef{
						Data:        []byte("cached-jpeg"),
						MimeType:    "image/jpeg",
						CaptureTime: now,
						Cached:      true,
					},
					snapshotOK: true,
				}
			},
			wantErr:    false,
			wantData:   "cached-jpeg",
			wantCached: true,
		},
		{
			name:      "stale snapshot triggers refresh",
			sessionID: "session-2",
			setupCache: func(svc *GatewayService) {
				svc.sessions.GetOrCreateRuntime("session-2")
				svc.mediaCaches["session-2"] = &stubMediaCache{
					snapshot: &domain.SnapshotRef{
						Data:        []byte("refreshed-jpeg"),
						MimeType:    "image/jpeg",
						CaptureTime: now,
						Cached:      false,
					},
					snapshotOK: false,
				}
			},
			wantErr:    false,
			wantData:   "refreshed-jpeg",
			wantCached: false,
		},
		{
			name:      "refresh error returned",
			sessionID: "session-3",
			setupCache: func(svc *GatewayService) {
				svc.sessions.GetOrCreateRuntime("session-3")
				svc.mediaCaches["session-3"] = &stubMediaCache{
					snapshotErr: errors.New("no key frame available"),
				}
			},
			wantErr: true,
		},
		{
			name:      "session not found",
			sessionID: "nonexistent",
			setupCache: func(_ *GatewayService) {
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestService("gw-0", &stubVerifier{})
			tt.setupCache(svc)

			// when
			snap, err := svc.GetSnapshot(context.Background(), tt.sessionID)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatal("GetSnapshot() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("GetSnapshot() unexpected error: %v", err)
			}
			if string(snap.Data) != tt.wantData {
				t.Fatalf("Data = %q, want %q", string(snap.Data), tt.wantData)
			}
			if snap.Cached != tt.wantCached {
				t.Fatalf("Cached = %v, want %v", snap.Cached, tt.wantCached)
			}
		})
	}
}

func TestGatewayService_GetRuntime(t *testing.T) {
	tests := []struct {
		name      string
		sessionID string
		wantErr   bool
	}{
		{
			name:      "existing session returns runtime",
			sessionID: "session-1",
			wantErr:   false,
		},
		{
			name:      "nonexistent session returns error",
			sessionID: "nonexistent",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestService("gw-0", &stubVerifier{})
			svc.sessions.GetOrCreateRuntime("session-1")

			// when
			rt, err := svc.GetRuntime(context.Background(), tt.sessionID)

			// then
			if tt.wantErr {
				if !errors.Is(err, domain.ErrSessionNotFound) {
					t.Fatalf("GetRuntime() error = %v, want %v", err, domain.ErrSessionNotFound)
				}
				return
			}

			if err != nil {
				t.Fatalf("GetRuntime() unexpected error: %v", err)
			}
			if rt == nil {
				t.Fatal("GetRuntime() returned nil runtime")
			}
		})
	}
}

func TestRoutedMessage(t *testing.T) {
	// given
	tests := []struct {
		name       string
		routed     domain.RoutedMessage
		wantTarget string
		wantNil    bool
	}{
		{
			name:       "broadcast has empty target",
			routed:     domain.RoutedMessage{TargetConnID: "", Message: &domain.Message{SessionID: "s1"}},
			wantTarget: "",
			wantNil:    false,
		},
		{
			name:       "unicast has specific target",
			routed:     domain.RoutedMessage{TargetConnID: "web-1", Message: &domain.Message{SessionID: "s1"}},
			wantTarget: "web-1",
			wantNil:    false,
		},
		{
			name:       "nil message is allowed",
			routed:     domain.RoutedMessage{TargetConnID: ""},
			wantTarget: "",
			wantNil:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			gotTarget := tt.routed.TargetConnID
			gotNil := tt.routed.Message == nil

			// then
			if gotTarget != tt.wantTarget {
				t.Fatalf("TargetConnID = %q, want %q", gotTarget, tt.wantTarget)
			}
			if gotNil != tt.wantNil {
				t.Fatalf("Message == nil = %v, want %v", gotNil, tt.wantNil)
			}
		})
	}
}
