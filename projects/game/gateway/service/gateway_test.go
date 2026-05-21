package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"dominion/projects/game/gateway/config"
	"dominion/projects/game/gateway/domain"
	"dominion/projects/game/gateway/domain/sessionmanager"
	"dominion/projects/game/gateway/domain/token"
)

type stubVerifier struct {
	claims *token.Claims
	err    error
}

func (v *stubVerifier) Verify(_ string) (*token.Claims, error) {
	return v.claims, v.err
}

func (v *stubVerifier) VerifyWithGrace(_ string, _ time.Duration) (*token.Claims, error) {
	return v.claims, v.err
}

type stubMediaCache struct {
	initSeg      *domain.InitSegmentRef
	initOK       bool
	activeInitID string
	segments     []*domain.SegmentRef
	snapshot     *domain.SnapshotRef
	snapshotOK   bool
	snapshotErr  error
}

func (c *stubMediaCache) StoreInitSegment(ref *domain.InitSegmentRef) error {
	c.initSeg = ref
	c.activeInitID = ref.InitID
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

func (c *stubMediaCache) GetActiveInitID() string {
	return c.activeInitID
}

func (c *stubMediaCache) GetSegmentsFromLastRandomAccess() []*domain.SegmentRef {
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

func newTestService(t *testing.T, gatewayID string, verifier token.Verifier) *GatewayService {
	t.Helper()
	signer := token.NewHMACSigner("test-secret", 1*time.Hour)
	config := &config.OwnerConfig{
		GatewayID: gatewayID,
	}
	svc := NewGatewayService(sessionmanager.NewManager(gatewayID), NewControlExecutor(), config, signer, verifier)
	return svc
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
				SessionID:      "session-1",
				OwnerGatewayID: "gw-0",
				OwnerEpoch:     1,
				ExpiresAt:      time.Now().Add(5 * time.Minute).Unix(),
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
				SessionID:      "session-1",
				OwnerGatewayID: "gw-other",
				OwnerEpoch:     1,
				ExpiresAt:      time.Now().Add(5 * time.Minute).Unix(),
			},
			gatewayID: "gw-0",
			wantErr:   true,
		},
		{
			name:          "wrong session ID rejected",
			pathSessionID: "session-1",
			claims: &token.Claims{
				SessionID:      "session-other",
				OwnerGatewayID: "gw-0",
				OwnerEpoch:     1,
				ExpiresAt:      time.Now().Add(5 * time.Minute).Unix(),
			},
			gatewayID: "gw-0",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestService(t, tt.gatewayID, &stubVerifier{claims: tt.claims, err: tt.verifyErr})

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
			if claims.OwnerGatewayID != tt.gatewayID {
				t.Fatalf("claims.OwnerGatewayID = %q, want %q", claims.OwnerGatewayID, tt.gatewayID)
			}
		})
	}
}

func TestCreateGameRuntime(t *testing.T) {
	signer := token.NewHMACSigner("test-secret", 1*time.Hour)
	config := &config.OwnerConfig{
		GatewayID: "gw-0",
	}
	svc := NewGatewayService(sessionmanager.NewManager("gw-0"), NewControlExecutor(), config, signer, signer)

	rt, tokenStr, err := svc.CreateGameRuntime(context.Background(), "sess-1", 0)
	if err != nil {
		t.Fatalf("CreateGameRuntime() error = %v", err)
	}
	if rt == nil {
		t.Fatal("CreateGameRuntime() returned nil runtime")
	}
	if rt.OwnerGatewayID != "gw-0" {
		t.Fatalf("rt.OwnerGatewayID = %q, want %q", rt.OwnerGatewayID, "gw-0")
	}
	if rt.OwnerEpoch != 1 {
		t.Fatalf("rt.OwnerEpoch = %d, want %d (per-session starts at 1)", rt.OwnerEpoch, 1)
	}
	if rt.ReconnectGeneration != 0 {
		t.Fatalf("rt.ReconnectGeneration = %d, want %d", rt.ReconnectGeneration, 0)
	}
	if tokenStr == "" {
		t.Fatal("CreateGameRuntime() returned empty token")
	}

	claims, err := signer.Verify(tokenStr)
	if err != nil {
		t.Fatalf("Verify token: %v", err)
	}
	if claims.SessionID != "sess-1" {
		t.Fatalf("claims.SessionID = %q, want %q", claims.SessionID, "sess-1")
	}
	if claims.OwnerGatewayID != "gw-0" {
		t.Fatalf("claims.OwnerGatewayID = %q, want %q", claims.OwnerGatewayID, "gw-0")
	}
	if claims.OwnerEpoch != 1 {
		t.Fatalf("claims.OwnerEpoch = %d, want %d", claims.OwnerEpoch, 1)
	}
	if claims.Audience != token.TokenAudienceInternal {
		t.Fatalf("claims.Audience = %q, want %q", claims.Audience, token.TokenAudienceInternal)
	}
	if claims.ReconnectGeneration != 0 {
		t.Fatalf("claims.ReconnectGeneration = %d, want %d", claims.ReconnectGeneration, 0)
	}
	if rt.LastTrafficTime.IsZero() {
		t.Fatal("rt.LastTrafficTime is zero, expected TouchSession to set it")
	}
}

func TestCreateGameRuntime_SamePodExisting(t *testing.T) {
	signer := token.NewHMACSigner("test-secret", 1*time.Hour)
	config := &config.OwnerConfig{
		GatewayID: "gw-0",
	}
	svc := NewGatewayService(sessionmanager.NewManager("gw-0"), NewControlExecutor(), config, signer, signer)

	// First call creates the runtime
	rt1, _, err := svc.CreateGameRuntime(context.Background(), "sess-1", 5)
	if err != nil {
		t.Fatalf("first CreateGameRuntime() error = %v", err)
	}
	if rt1.OwnerEpoch != 1 {
		t.Fatalf("first OwnerEpoch = %d, want 1", rt1.OwnerEpoch)
	}
	if rt1.ReconnectGeneration != 5 {
		t.Fatalf("first ReconnectGeneration = %d, want 5", rt1.ReconnectGeneration)
	}

	// Second call: runtime already exists on same pod, epoch increments
	rt2, tokenStr, err := svc.CreateGameRuntime(context.Background(), "sess-1", 5)
	if err != nil {
		t.Fatalf("second CreateGameRuntime() error = %v", err)
	}
	if rt2.OwnerEpoch != 2 {
		t.Fatalf("second OwnerEpoch = %d, want 2 (incremented)", rt2.OwnerEpoch)
	}
	if rt2.ReconnectGeneration != 5 {
		t.Fatalf("second ReconnectGeneration = %d, want 5 (unchanged when not higher)", rt2.ReconnectGeneration)
	}

	claims, err := signer.Verify(tokenStr)
	if err != nil {
		t.Fatalf("Verify token: %v", err)
	}
	if claims.OwnerEpoch != 2 {
		t.Fatalf("token OwnerEpoch = %d, want 2", claims.OwnerEpoch)
	}
}

func TestCreateGameRuntime_SamePodHigherGeneration(t *testing.T) {
	signer := token.NewHMACSigner("test-secret", 1*time.Hour)
	config := &config.OwnerConfig{
		GatewayID: "gw-0",
	}
	svc := NewGatewayService(sessionmanager.NewManager("gw-0"), NewControlExecutor(), config, signer, signer)

	// First call with gen=3
	_, _, _ = svc.CreateGameRuntime(context.Background(), "sess-1", 3)

	// Second call with higher gen=7
	rt, _, err := svc.CreateGameRuntime(context.Background(), "sess-1", 7)
	if err != nil {
		t.Fatalf("CreateGameRuntime() error = %v", err)
	}
	if rt.ReconnectGeneration != 7 {
		t.Fatalf("ReconnectGeneration = %d, want 7 (updated to higher)", rt.ReconnectGeneration)
	}
	if rt.OwnerEpoch != 2 {
		t.Fatalf("OwnerEpoch = %d, want 2 (incremented)", rt.OwnerEpoch)
	}
}

func TestCreateGameRuntime_SamePodLowerGenerationIgnored(t *testing.T) {
	signer := token.NewHMACSigner("test-secret", 1*time.Hour)
	config := &config.OwnerConfig{
		GatewayID: "gw-0",
	}
	svc := NewGatewayService(sessionmanager.NewManager("gw-0"), NewControlExecutor(), config, signer, signer)

	// First call with gen=10
	_, _, _ = svc.CreateGameRuntime(context.Background(), "sess-1", 10)

	// Second call with lower gen=3
	rt, _, err := svc.CreateGameRuntime(context.Background(), "sess-1", 3)
	if err != nil {
		t.Fatalf("CreateGameRuntime() error = %v", err)
	}
	if rt.ReconnectGeneration != 10 {
		t.Fatalf("ReconnectGeneration = %d, want 10 (unchanged, lower gen ignored)", rt.ReconnectGeneration)
	}
	if rt.OwnerEpoch != 2 {
		t.Fatalf("OwnerEpoch = %d, want 2 (still incremented)", rt.OwnerEpoch)
	}
}

func TestGatewayService_ProcessHello_Agent(t *testing.T) {
	svc := newTestService(t, "gw-0", &stubVerifier{})
	rt := svc.sessions.GetOrCreateRuntime("session-1")
	claims := &token.Claims{SessionID: "session-1", OwnerGatewayID: "gw-0"}

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
	svc := newTestService(t, "gw-0", &stubVerifier{})
	rt := svc.sessions.GetOrCreateRuntime("session-1")
	claims := &token.Claims{SessionID: "session-1", OwnerGatewayID: "gw-0"}

	svc.sessions.RegisterAgent("session-1", &domain.AgentConnection{ConnID: "agent-first"})

	// when
	_, err := svc.ProcessHello(rt, claims, domain.ClientRoleWindowsAgent, "agent-second")

	// then
	if !errors.Is(err, domain.ErrAgentAlreadyConnected) {
		t.Fatalf("ProcessHello() error = %v, want %v", err, domain.ErrAgentAlreadyConnected)
	}
}

func TestGatewayService_ProcessHello_Web(t *testing.T) {
	svc := newTestService(t, "gw-0", &stubVerifier{})
	rt := svc.sessions.GetOrCreateRuntime("session-1")
	claims := &token.Claims{SessionID: "session-1", OwnerGatewayID: "gw-0"}

	// given: media cache has init segment and one random access segment
	cache := &stubMediaCache{
		initSeg: &domain.InitSegmentRef{
			StreamID: "stream-1",
			InitID:   "init-1",
			MimeType: "video/mp4",
			Codec:    "avc1",
			Data:     []byte("init-data"),
		},
		activeInitID: "init-1",
		initOK:       true,
		segments: []*domain.SegmentRef{
			{StreamID: "stream-1", InitID: "init-1", Sequence: 1, Data: []byte("seg-data"), RandomAccess: true},
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
	if segPayload.Sequence != 1 {
		t.Fatalf("Sequence = %d, want %d", segPayload.Sequence, 1)
	}
	if string(segPayload.Segment) != "seg-data" {
		t.Fatalf("Segment = %q, want %q", string(segPayload.Segment), "seg-data")
	}
	if !segPayload.RandomAccess {
		t.Fatal("RandomAccess = false, want true")
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
	svc := newTestService(t, "gw-0", &stubVerifier{})
	rt := svc.sessions.GetOrCreateRuntime("session-1")
	claims := &token.Claims{SessionID: "session-1", OwnerGatewayID: "gw-0"}

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
	svc := newTestService(t, "gw-0", &stubVerifier{})
	svc.sessions.GetOrCreateRuntime("session-1")
	cache := &stubMediaCache{}
	svc.mediaCaches["session-1"] = cache

	msg := &domain.Message{
		SessionID: "session-1",
		Payload: domain.MediaInitPayload{
			StreamID: "stream-1",
			InitID:   "init-1",
			MimeType: "video/mp4",
			Codec:    "avc1",
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
	if cache.initSeg.InitID != "init-1" {
		t.Fatalf("InitID = %q, want %q", cache.initSeg.InitID, "init-1")
	}
	if cache.initSeg.MimeType != "video/mp4" {
		t.Fatalf("MimeType = %q, want %q", cache.initSeg.MimeType, "video/mp4")
	}
	if string(cache.initSeg.Data) != "init-bytes" {
		t.Fatalf("Data = %q, want %q", string(cache.initSeg.Data), "init-bytes")
	}
	// sequence tracking reset
	svc.mediaMu.Lock()
	lastSeq := svc.lastSequences["session-1"]
	svc.mediaMu.Unlock()
	if lastSeq != 0 {
		t.Fatalf("lastSequences = %d, want 0 after init", lastSeq)
	}
}

func TestGatewayService_HandleAgentMessage_MediaSegment(t *testing.T) {
	svc := newTestService(t, "gw-0", &stubVerifier{})
	svc.sessions.GetOrCreateRuntime("session-1")
	cache := &stubMediaCache{activeInitID: "init-1"}
	svc.mediaCaches["session-1"] = cache

	msg := &domain.Message{
		SessionID: "session-1",
		Payload: domain.MediaSegmentPayload{
			StreamID:     "stream-1",
			InitID:       "init-1",
			Sequence:     42,
			Segment:      []byte("fMP4-chunk"),
			RandomAccess: true,
			DurationMS:   33,
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
	if cache.segments[0].Sequence != 42 {
		t.Fatalf("Sequence = %d, want %d", cache.segments[0].Sequence, 42)
	}
	if !cache.segments[0].RandomAccess {
		t.Fatal("RandomAccess = false, want true")
	}
}

func TestGatewayService_HandleAgentMessage_ControlAck(t *testing.T) {
	svc := newTestService(t, "gw-0", &stubVerifier{})
	svc.sessions.GetOrCreateRuntime("session-1")

	// given: inflight operation from web-1
	req := domain.ControlRequestPayload{
		OperationID: "op-ack",
		ActionKind:  domain.OperationKindMouseClick,
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
	svc := newTestService(t, "gw-0", &stubVerifier{})
	svc.sessions.GetOrCreateRuntime("session-1")

	req := domain.ControlRequestPayload{
		OperationID: "op-result",
		ActionKind:  domain.OperationKindMouseClick,
	}
	svc.control.SubmitOperation("session-1", req, "web-2")

	msg := &domain.Message{
		SessionID: "session-1",
		Payload: domain.ControlResultPayload{
			OperationID: "op-result",
			Status:      domain.ControlResultStatusSucceeded,
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
	if resultPayload.OperationID != "op-result" {
		t.Fatalf("OperationID = %q, want %q", resultPayload.OperationID, "op-result")
	}
	if resultPayload.Status != domain.ControlResultStatusSucceeded {
		t.Fatalf("Status = %d, want %d (Succeeded)", resultPayload.Status, domain.ControlResultStatusSucceeded)
	}

	// inflight cleared
	if svc.control.HasInflightOperation("session-1") {
		t.Fatal("inflight still active after result, should be cleared")
	}
}

func TestGatewayService_HandleAgentMessage_ControlResultFlashSnapshot(t *testing.T) {
	svc := newTestService(t, "gw-0", &stubVerifier{})
	svc.sessions.GetOrCreateRuntime("session-1")

	req := domain.ControlRequestPayload{
		OperationID:   "op-flash",
		ActionKind:    domain.OperationKindMouseClick,
		FlashSnapshot: true,
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
			OperationID: "op-flash",
			Status:      domain.ControlResultStatusSucceeded,
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
	svc := newTestService(t, "gw-0", &stubVerifier{})
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
	svc := newTestService(t, "gw-0", &stubVerifier{})
	svc.sessions.GetOrCreateRuntime("session-1")

	msg := &domain.Message{
		SessionID: "session-1",
		Payload: domain.ControlResultPayload{
			OperationID: "op-missing",
			Status:      domain.ControlResultStatusSucceeded,
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
	svc := newTestService(t, "gw-0", &stubVerifier{})
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
	svc := newTestService(t, "gw-0", &stubVerifier{})
	svc.sessions.GetOrCreateRuntime("session-1")

	msg := &domain.Message{
		SessionID: "session-1",
		Payload: domain.ControlRequestPayload{
			OperationID: "op-req",
			ActionKind:  domain.OperationKindMouseClick,
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
	if msgs[0].TargetKind != domain.RouteTargetAgent {
		t.Fatalf("TargetKind = %d, want %d (Agent)", msgs[0].TargetKind, domain.RouteTargetAgent)
	}
	reqPayload, ok := msgs[0].Message.Payload.(domain.ControlRequestPayload)
	if !ok {
		t.Fatal("payload is not control_request")
	}
	if reqPayload.OperationID != "op-req" {
		t.Fatalf("OperationID = %q, want %q", reqPayload.OperationID, "op-req")
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
	svc := newTestService(t, "gw-0", &stubVerifier{})
	svc.sessions.GetOrCreateRuntime("session-1")

	msg := &domain.Message{
		SessionID: "session-1",
		Payload: domain.ControlRequestPayload{
			OperationID: "op-bad",
			ActionKind:  domain.OperationKind(""),
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
	svc := newTestService(t, "gw-0", &stubVerifier{})
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
	svc := newTestService(t, "gw-0", &stubVerifier{})
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
			svc := newTestService(t, "gw-0", &stubVerifier{})
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
			svc := newTestService(t, "gw-0", &stubVerifier{})
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
		wantEmpty  bool
	}{
		{
			name:       "broadcast has empty target",
			routed:     domain.RoutedMessage{TargetKind: domain.RouteTargetWebBroadcast, Message: domain.Message{SessionID: "s1"}},
			wantTarget: "",
			wantEmpty:  false,
		},
		{
			name:       "unicast has specific target",
			routed:     domain.RoutedMessage{TargetKind: domain.RouteTargetConn, TargetConnID: "web-1", Message: domain.Message{SessionID: "s1"}},
			wantTarget: "web-1",
			wantEmpty:  false,
		},
		{
			name:       "agent route",
			routed:     domain.RoutedMessage{TargetKind: domain.RouteTargetAgent},
			wantTarget: "",
			wantEmpty:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			gotTarget := tt.routed.TargetConnID
			gotEmpty := tt.routed.Message.SessionID == ""

			// then
			if gotTarget != tt.wantTarget {
				t.Fatalf("TargetConnID = %q, want %q", gotTarget, tt.wantTarget)
			}
			if gotEmpty != tt.wantEmpty {
				t.Fatalf("Message.SessionID empty = %v, want %v", gotEmpty, tt.wantEmpty)
			}
		})
	}
}

func newRefreshTestService(t *testing.T, gatewayID string) (*GatewayService, *token.HMACSigner) {
	t.Helper()
	signer := token.NewHMACSigner("test-refresh-secret", 15*time.Minute)
	config := &config.OwnerConfig{
		GatewayID:         gatewayID,
		TokenRefreshGrace: 60 * time.Minute,
	}
	svc := NewGatewayService(sessionmanager.NewManager(gatewayID), NewControlExecutor(), config, signer, signer)
	return svc, signer
}

func TestRefreshGameRuntime_SameOwner(t *testing.T) {
	svc, signer := newRefreshTestService(t, "gw-0")

	// Runtime is created and owned by gw-0
	rt, _, err := svc.CreateGameRuntime(context.Background(), "session-1", 3)
	if err != nil {
		t.Fatalf("CreateGameRuntime: %v", err)
	}
	if rt.OwnerEpoch != 1 {
		t.Fatalf("initial OwnerEpoch = %d, want 1", rt.OwnerEpoch)
	}

	oldToken, err := signer.Issue("session-1", "gw-0", 1, token.TokenAudienceInternal, 3)
	if err != nil {
		t.Fatalf("issue old token: %v", err)
	}

	gotRT, newToken, err := svc.RefreshGameRuntime(context.Background(), "session-1", oldToken)
	if err != nil {
		t.Fatalf("RefreshGameRuntime() error = %v", err)
	}
	if gotRT.ReconnectGeneration != 3 {
		t.Fatalf("ReconnectGeneration = %d, want 3 (unchanged)", gotRT.ReconnectGeneration)
	}
	if gotRT.OwnerGatewayID != "gw-0" {
		t.Fatalf("OwnerGatewayID = %q, want %q", gotRT.OwnerGatewayID, "gw-0")
	}
	if gotRT.OwnerEpoch != 2 {
		t.Fatalf("OwnerEpoch = %d, want 2 (per-session incremented)", gotRT.OwnerEpoch)
	}

	claims, err := signer.Verify(newToken)
	if err != nil {
		t.Fatalf("verify new token: %v", err)
	}
	if claims.ReconnectGeneration != 3 {
		t.Fatalf("new token ReconnectGeneration = %d, want 3", claims.ReconnectGeneration)
	}
	if claims.OwnerGatewayID != "gw-0" {
		t.Fatalf("new token OwnerGatewayID = %q, want %q", claims.OwnerGatewayID, "gw-0")
	}
	if claims.OwnerEpoch != 2 {
		t.Fatalf("new token OwnerEpoch = %d, want 2", claims.OwnerEpoch)
	}
}

func TestRefreshGameRuntime_TakeoverRejected(t *testing.T) {
	svc, signer := newRefreshTestService(t, "gw-0")

	// Token from a different gateway should be rejected since refresh
	// does not support takeover.
	oldToken, err := signer.Issue("session-1", "gw-1", 5, token.TokenAudienceInternal, 2)
	if err != nil {
		t.Fatalf("issue old token: %v", err)
	}

	_, _, err = svc.RefreshGameRuntime(context.Background(), "session-1", oldToken)
	if err == nil {
		t.Fatal("RefreshGameRuntime() expected error for non-owner token, got nil")
	}
}

func TestRefreshGameRuntime_RuntimeNotFound(t *testing.T) {
	svc, signer := newRefreshTestService(t, "gw-0")

	oldToken, err := signer.Issue("session-1", "gw-0", 1, token.TokenAudienceInternal, 5)
	if err != nil {
		t.Fatalf("issue old token: %v", err)
	}

	_, _, err = svc.RefreshGameRuntime(context.Background(), "session-1", oldToken)
	if err == nil {
		t.Fatal("RefreshGameRuntime() expected error for missing runtime, got nil")
	}
}

func TestRefreshGameRuntime_InvalidToken(t *testing.T) {
	svc, _ := newRefreshTestService(t, "gw-0")

	_, _, err := svc.RefreshGameRuntime(context.Background(), "session-1", "not-a-valid-token")
	if err == nil {
		t.Fatal("RefreshGameRuntime() expected error for invalid token, got nil")
	}
}

func TestRefreshGameRuntime_WrongSession(t *testing.T) {
	svc, signer := newRefreshTestService(t, "gw-0")

	oldToken, err := signer.Issue("session-other", "gw-0", 1, token.TokenAudienceInternal, 1)
	if err != nil {
		t.Fatalf("issue old token: %v", err)
	}

	_, _, err = svc.RefreshGameRuntime(context.Background(), "session-1", oldToken)
	if err == nil {
		t.Fatal("RefreshGameRuntime() expected error for session mismatch, got nil")
	}
	if !errors.Is(err, errSessionMismatch) {
		t.Fatalf("error = %v, want errSessionMismatch", err)
	}
}

func TestRefreshGameRuntime_OldTokenWithLowerEpochAllowed(t *testing.T) {
	svc, signer := newRefreshTestService(t, "gw-0")

	// Runtime created with epoch=3 (simulating that CreateGameRuntime was called 3 times)
	rt, _, err := svc.CreateGameRuntime(context.Background(), "session-1", 2)
	if err != nil {
		t.Fatalf("CreateGameRuntime #1: %v", err)
	}
	// Call again to increment epoch
	rt, _, err = svc.CreateGameRuntime(context.Background(), "session-1", 2)
	if err != nil {
		t.Fatalf("CreateGameRuntime #2: %v", err)
	}
	// Call again to increment epoch
	rt, _, err = svc.CreateGameRuntime(context.Background(), "session-1", 2)
	if err != nil {
		t.Fatalf("CreateGameRuntime #3: %v", err)
	}
	if rt.OwnerEpoch != 3 {
		t.Fatalf("OwnerEpoch = %d, want 3 after 3 CreateGameRuntime calls", rt.OwnerEpoch)
	}

	// Refresh with an old token at epoch=1 (lower than current=3)
	oldToken, err := signer.Issue("session-1", "gw-0", 1, token.TokenAudienceInternal, 2)
	if err != nil {
		t.Fatalf("issue old token at epoch 1: %v", err)
	}

	gotRT, newToken, err := svc.RefreshGameRuntime(context.Background(), "session-1", oldToken)
	if err != nil {
		t.Fatalf("RefreshGameRuntime() error = %v", err)
	}
	if gotRT.OwnerEpoch != 4 {
		t.Fatalf("OwnerEpoch = %d, want 4 (incremented from 3)", gotRT.OwnerEpoch)
	}

	claims, err := signer.Verify(newToken)
	if err != nil {
		t.Fatalf("verify new token: %v", err)
	}
	if claims.OwnerEpoch != 4 {
		t.Fatalf("new token OwnerEpoch = %d, want 4", claims.OwnerEpoch)
	}
}

func TestRefreshGameRuntime_NewerTokenEpochRejected(t *testing.T) {
	svc, signer := newRefreshTestService(t, "gw-0")

	// Runtime at epoch=1
	rt, _, err := svc.CreateGameRuntime(context.Background(), "session-1", 2)
	if err != nil {
		t.Fatalf("CreateGameRuntime: %v", err)
	}
	if rt.OwnerEpoch != 1 {
		t.Fatalf("OwnerEpoch = %d, want 1", rt.OwnerEpoch)
	}

	// Try to refresh with token at epoch=5 (> current=1) — should fail
	oldToken, err := signer.Issue("session-1", "gw-0", 5, token.TokenAudienceInternal, 2)
	if err != nil {
		t.Fatalf("issue token at epoch 5: %v", err)
	}

	_, _, err = svc.RefreshGameRuntime(context.Background(), "session-1", oldToken)
	if err == nil {
		t.Fatal("RefreshGameRuntime() expected error for newer epoch token, got nil")
	}
}

func TestRefreshGameRuntime_GenerationMismatchRejected(t *testing.T) {
	svc, signer := newRefreshTestService(t, "gw-0")

	// Runtime at gen=2
	_, _, err := svc.CreateGameRuntime(context.Background(), "session-1", 2)
	if err != nil {
		t.Fatalf("CreateGameRuntime: %v", err)
	}

	// Token at gen=3 (mismatch)
	oldToken, err := signer.Issue("session-1", "gw-0", 1, token.TokenAudienceInternal, 3)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	_, _, err = svc.RefreshGameRuntime(context.Background(), "session-1", oldToken)
	if err == nil {
		t.Fatal("RefreshGameRuntime() expected error for generation mismatch, got nil")
	}
}

func TestRefreshGameRuntime_GraceWindow(t *testing.T) {
	frozenNow := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	signer := token.NewHMACSigner("test-refresh-secret", 15*time.Minute)
	signer.SetNow(func() time.Time { return frozenNow })

	config := &config.OwnerConfig{
		GatewayID:         "gw-0",
		TokenRefreshGrace: 60 * time.Minute,
	}
	svc := NewGatewayService(sessionmanager.NewManager("gw-0"), NewControlExecutor(), config, signer, signer)

	rt := svc.sessions.GetOrCreateRuntime("session-1")
	rt.OwnerGatewayID = "gw-0"
	rt.OwnerEpoch = 1
	rt.ReconnectGeneration = 2

	pastSigner := token.NewHMACSigner("test-refresh-secret", 15*time.Minute)
	pastSigner.SetNow(func() time.Time { return frozenNow.Add(-20 * time.Minute) })

	oldToken, err := pastSigner.Issue("session-1", "gw-0", 1, token.TokenAudienceInternal, 2)
	if err != nil {
		t.Fatalf("issue expired token: %v", err)
	}

	gotRT, _, err := svc.RefreshGameRuntime(context.Background(), "session-1", oldToken)
	if err != nil {
		t.Fatalf("RefreshGameRuntime() error = %v, want success within grace window", err)
	}
	if gotRT.ReconnectGeneration != 2 {
		t.Fatalf("ReconnectGeneration = %d, want 2 (same owner, unchanged)", gotRT.ReconnectGeneration)
	}
}

func TestStartCompletionWorker(t *testing.T) {
	svc := newTestService(t, "gw-0", &stubVerifier{})

	builder := svc.StartCompletionWorker()
	if builder == nil {
		t.Fatal("StartCompletionWorker() returned nil WorkerBuilder")
	}

	worker, err := builder.Build(context.Background())
	if err != nil {
		t.Fatalf("Build(): %v", err)
	}
	if worker == nil {
		t.Fatal("Build() returned nil worker")
	}
}
