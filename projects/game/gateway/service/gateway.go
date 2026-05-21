package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"dominion/common/gopkg/bootstrap"
	"dominion/common/gopkg/logs"
	"dominion/common/gopkg/logs/event"
	"dominion/projects/game/gateway/domain"
	"dominion/projects/game/gateway/domain/mediacache"
	"dominion/projects/game/gateway/domain/sessionmanager"
	"dominion/projects/game/gateway/token"
)

var (
	errGatewayMismatch = errors.New("gateway ID mismatch")
	errSessionMismatch = errors.New("session ID mismatch")
)

const (
	logFieldRequesterConnID = "requester_conn_id"
)

type GatewayService struct {
	sessions      *sessionmanager.Manager
	mediaCaches   map[string]domain.MediaCache
	lastSequences map[string]uint64
	mediaMu       sync.Mutex
	control       *ControlExecutor
	asyncCh       chan *domain.RoutedMessage
	config        *OwnerConfig
	tokenIssuer   token.Issuer
	tokenVerifier token.Verifier
}

func NewGatewayService(
	sessions *sessionmanager.Manager,
	control *ControlExecutor,
	config *OwnerConfig,
	issuer token.Issuer,
	verifier token.Verifier,
) *GatewayService {
	return &GatewayService{
		sessions:      sessions,
		mediaCaches:   map[string]domain.MediaCache{},
		lastSequences: map[string]uint64{},
		control:       control,
		asyncCh:       make(chan *domain.RoutedMessage, 64),
		config:        config,
		tokenIssuer:   issuer,
		tokenVerifier: verifier,
	}
}

func (s *GatewayService) AsyncMessages() <-chan *domain.RoutedMessage {
	return s.asyncCh
}

// HandleCompletion processes a control completion: refreshes the snapshot cache
// if requested, constructs the routed message, and delivers it to the async
// output channel. Returns true if the message was sent, false if the context
// was cancelled.
func (s *GatewayService) HandleCompletion(ctx context.Context, comp domain.ControlCompletion) bool {
	logs.Info(ctx, "gateway: handling control completion",
		event.String(logFieldSessionID, comp.SessionID),
		event.String(logFieldRequesterConnID, comp.RequesterConnID),
	)

	if comp.FlashSnapshot {
		cache := s.getOrCreateMediaCache(comp.SessionID)
		if snap, err := cache.RefreshSnapshot(); err == nil && snap != nil {
			rt := s.sessions.GetRuntime(comp.SessionID)
			if rt != nil {
				rt.LatestSnapshot = snap
			}
		}
	}

	msg := &domain.RoutedMessage{
		Message: domain.Message{
			SessionID: comp.SessionID,
			Payload:   comp.Result,
		},
		TargetKind:   domain.RouteTargetConn,
		TargetConnID: comp.RequesterConnID,
	}
	select {
	case s.asyncCh <- msg:
		return true
	case <-ctx.Done():
		return false
	}
}

// StartCompletionWorker returns a WorkerBuilder that creates a completion
// worker consuming the control executor's completion channel.
func (s *GatewayService) StartCompletionWorker() bootstrap.WorkerBuilder {
	return bootstrap.WorkerBuilderFunc(func(_ context.Context) (bootstrap.Worker, error) {
		return NewCompletionWorker(s.control.Completions(), func(ctx context.Context, comp domain.ControlCompletion) {
			s.HandleCompletion(ctx, comp)
		}), nil
	})
}

// ConnectSession validates the connection token and returns the session runtime
// and embedded claims. It verifies the token signature and expiry, confirms the
// gateway ID matches this instance, and ensures the session ID in the token
// matches the path parameter.
func (s *GatewayService) ConnectSession(ctx context.Context, pathSessionID, tokenStr string) (*domain.SessionRuntime, *token.Claims, error) {
	claims, err := s.tokenVerifier.Verify(tokenStr)
	if err != nil {
		return nil, nil, fmt.Errorf("verify token: %w", err)
	}

	if err := claims.ValidateOwnerEpoch(); err != nil {
		return nil, nil, fmt.Errorf("validate owner epoch: %w", err)
	}

	if claims.OwnerGatewayID != s.config.GatewayID {
		return nil, nil, errGatewayMismatch
	}

	if claims.SessionID != pathSessionID {
		return nil, nil, errSessionMismatch
	}

	rt := s.sessions.GetOrCreateRuntime(pathSessionID)

	logs.Info(ctx, "gateway: session connected",
		event.String(logFieldSessionID, pathSessionID),
	)

	return rt, claims, nil
}

// ProcessHello handles the hello message after WebSocket upgrade. For agent
// connections, it registers the agent on the session runtime. For web
// connections, it adds the web connection and returns catch-up messages
// (cached media_init and segments from the last random-access segment).
func (s *GatewayService) ProcessHello(rt *domain.SessionRuntime, _ *token.Claims, role domain.ClientRole, connID string) ([]*domain.RoutedMessage, error) {
	switch role {
	case domain.ClientRoleWindowsAgent:
		if err := s.sessions.RegisterAgent(rt.SessionID, &domain.AgentConnection{ConnID: connID}); err != nil {
			return nil, err
		}
		return nil, nil

	case domain.ClientRoleWeb:
		if err := s.sessions.AddWebConn(rt.SessionID, &domain.WebConnection{ConnID: connID}); err != nil {
			return nil, err
		}
		return s.buildCatchUpMessages(rt.SessionID), nil

	default:
		return nil, fmt.Errorf("unsupported client role: %v", role)
	}
}

// buildCatchUpMessages returns cached media_init and segments from the last
// random access point for a late-joining web client. All messages use
// TargetConnID="" (broadcast to all web connections).
func (s *GatewayService) buildCatchUpMessages(sessionID string) []*domain.RoutedMessage {
	cache := s.getOrCreateMediaCache(sessionID)
	var msgs []*domain.RoutedMessage

	if init, ok := cache.GetInitSegment(); ok {
		msgs = append(msgs, &domain.RoutedMessage{
			TargetKind: domain.RouteTargetWebBroadcast,
			Message: domain.Message{
				SessionID: sessionID,
				Payload: domain.MediaInitPayload{
					StreamID: init.StreamID,
					InitID:   init.InitID,
					MimeType: init.MimeType,
					Codec:    init.Codec,
					Segment:  init.Data,
				},
			},
		})
	}

	for _, seg := range cache.GetSegmentsFromLastRandomAccess() {
		msgs = append(msgs, &domain.RoutedMessage{
			TargetKind: domain.RouteTargetWebBroadcast,
			Message: domain.Message{
				SessionID: sessionID,
				Payload: domain.MediaSegmentPayload{
					StreamID:      seg.StreamID,
					InitID:        seg.InitID,
					Sequence:      seg.Sequence,
					Segment:       seg.Data,
					RandomAccess:  seg.RandomAccess,
					DurationMS:    seg.DurationMS,
					Discontinuity: seg.Discontinuity,
				},
			},
		})
	}

	return msgs
}

// HandleAgentMessage processes messages received from the agent connection.
// Media messages are cached and broadcast to all web connections. Control
// responses are routed to the requesting web connection.
func (s *GatewayService) HandleAgentMessage(_ context.Context, sessionID string, msg *domain.Message) ([]*domain.RoutedMessage, error) {
	switch p := msg.Payload.(type) {
	case domain.MediaInitPayload:
		return s.handleMediaInit(sessionID, p)
	case domain.MediaSegmentPayload:
		return s.handleMediaSegment(sessionID, p)
	case domain.ControlAckPayload:
		return s.handleControlAck(sessionID, p)
	case domain.ControlResultPayload:
		return s.handleControlResult(sessionID, p)
	default:
		return nil, nil
	}
}

// handleMediaInit stores the init segment in the media cache and broadcasts
// it to all web connections.
func (s *GatewayService) handleMediaInit(sessionID string, init domain.MediaInitPayload) ([]*domain.RoutedMessage, error) {
	cache := s.getOrCreateMediaCache(sessionID)
	ref := &domain.InitSegmentRef{
		StreamID: init.StreamID,
		InitID:   init.InitID,
		MimeType: init.MimeType,
		Codec:    init.Codec,
		Data:     init.Segment,
	}
	if err := cache.StoreInitSegment(ref); err != nil {
		return nil, err
	}
	s.mediaMu.Lock()
	s.lastSequences[sessionID] = 0
	s.mediaMu.Unlock()
	return []*domain.RoutedMessage{
		{
			TargetKind: domain.RouteTargetWebBroadcast,
			Message: domain.Message{
				SessionID: sessionID,
				Payload:   init,
			},
		},
	}, nil
}

// handleMediaSegment adds the segment to the media cache and broadcasts it
// to all web connections.
func (s *GatewayService) handleMediaSegment(sessionID string, seg domain.MediaSegmentPayload) ([]*domain.RoutedMessage, error) {
	cache := s.getOrCreateMediaCache(sessionID)
	activeInitID := cache.GetActiveInitID()
	if activeInitID == "" {
		return nil, fmt.Errorf("%w: no init segment received", domain.ErrUnknownInitID)
	}
	if seg.InitID != activeInitID {
		return nil, fmt.Errorf("%w: segment init_id %s != active %s", domain.ErrUnknownInitID, seg.InitID, activeInitID)
	}
	s.mediaMu.Lock()
	lastSeq := s.lastSequences[sessionID]
	if seg.Sequence <= lastSeq {
		s.mediaMu.Unlock()
		return nil, fmt.Errorf("%w: got %d, last was %d", domain.ErrSequenceNotIncreasing, seg.Sequence, lastSeq)
	}
	s.lastSequences[sessionID] = seg.Sequence
	s.mediaMu.Unlock()
	ref := &domain.SegmentRef{
		StreamID:      seg.StreamID,
		InitID:        seg.InitID,
		Sequence:      seg.Sequence,
		Data:          seg.Segment,
		RandomAccess:  seg.RandomAccess,
		DurationMS:    seg.DurationMS,
		Discontinuity: seg.Discontinuity,
		MediaTime:     time.Now(),
	}
	if err := cache.AddSegment(ref); err != nil {
		return nil, err
	}
	return []*domain.RoutedMessage{
		{
			TargetKind: domain.RouteTargetWebBroadcast,
			Message: domain.Message{
				SessionID: sessionID,
				Payload:   seg,
			},
		},
	}, nil
}

// handleControlAck routes the control acknowledgment to the inflight
// operation's requesting web connection.
func (s *GatewayService) handleControlAck(sessionID string, ack domain.ControlAckPayload) ([]*domain.RoutedMessage, error) {
	requesterConnID, err := s.control.HandleAgentAck(sessionID)
	if err != nil {
		return nil, err
	}

	return []*domain.RoutedMessage{
		{
			TargetKind:   domain.RouteTargetWebBroadcast,
			TargetConnID: requesterConnID,
			Message: domain.Message{
				SessionID: sessionID,
				Payload:   ack,
			},
		},
	}, nil
}

// handleControlResult routes the control result to the inflight operation's
// requesting web connection, clears the inflight state, and optionally
// refreshes the snapshot when flash_snapshot was requested.
func (s *GatewayService) handleControlResult(sessionID string, result domain.ControlResultPayload) ([]*domain.RoutedMessage, error) {
	requesterConnID, flashSnapshot, err := s.control.HandleAgentResult(sessionID)
	if err != nil {
		return nil, err
	}

	if flashSnapshot {
		cache := s.getOrCreateMediaCache(sessionID)
		if snap, snapErr := cache.RefreshSnapshot(); snapErr == nil && snap != nil {
			rt := s.sessions.GetRuntime(sessionID)
			if rt != nil {
				rt.LatestSnapshot = snap
			}
		}
	}

	return []*domain.RoutedMessage{
		{
			TargetKind:   domain.RouteTargetWebBroadcast,
			TargetConnID: requesterConnID,
			Message: domain.Message{
				SessionID: sessionID,
				Payload:   result,
			},
		},
	}, nil
}

// HandleWebMessage processes messages received from a web connection. Control
// requests are validated and forwarded to the agent. Ping messages receive a
// pong response directed to the sender.
func (s *GatewayService) HandleWebMessage(_ context.Context, sessionID string, connID string, msg *domain.Message) ([]*domain.RoutedMessage, error) {
	switch p := msg.Payload.(type) {
	case domain.ControlRequestPayload:
		return s.handleControlRequest(sessionID, connID, p)
	case domain.PingPayload:
		return s.handlePing(sessionID, connID, p)
	case domain.ErrorPayload:
		// Validation/protocol errors (e.g., old kind/mouse format rejected).
		return []*domain.RoutedMessage{{
			TargetKind:   domain.RouteTargetConn,
			TargetConnID: connID,
			Message: domain.Message{
				SessionID: sessionID,
				Payload:   p,
			},
		}}, nil
	default:
		return nil, nil
	}
}

// handleControlRequest validates the control request via the ControlExecutor
// and forwards it to the agent connection (TargetConnID="" for the single
// agent).
func (s *GatewayService) handleControlRequest(sessionID, connID string, req domain.ControlRequestPayload) ([]*domain.RoutedMessage, error) {
	if _, err := s.control.SubmitOperation(sessionID, req, connID); err != nil {
		return nil, err
	}

	return []*domain.RoutedMessage{
		{
			TargetKind: domain.RouteTargetAgent,
			Message: domain.Message{
				SessionID: sessionID,
				Payload:   req,
			},
		},
	}, nil
}

// handlePing returns a pong message directed to the sender connection.
func (s *GatewayService) handlePing(sessionID, connID string, ping domain.PingPayload) ([]*domain.RoutedMessage, error) {
	return []*domain.RoutedMessage{
		{
			TargetKind:   domain.RouteTargetConn,
			TargetConnID: connID,
			Message: domain.Message{
				SessionID: sessionID,
				Payload: domain.PongPayload{
					Nonce: ping.Nonce,
				},
			},
		},
	}, nil
}

// GetSnapshot returns the latest snapshot for a session. If the cached snapshot
// is stale, it refreshes from the latest random-access segment.
func (s *GatewayService) GetSnapshot(_ context.Context, sessionID string) (*domain.SnapshotRef, error) {
	rt := s.sessions.GetRuntime(sessionID)
	if rt == nil {
		return nil, domain.ErrSessionNotFound
	}

	cache := s.getOrCreateMediaCache(sessionID)

	if snap, ok := cache.GetLatestSnapshot(); ok {
		return snap, nil
	}

	snap, err := cache.RefreshSnapshot()
	if err != nil {
		return nil, err
	}

	rt.LatestSnapshot = snap
	return snap, nil
}

// DisconnectAgent removes the agent connection for the session and cancels any
// inflight control operation. It is called by the WebSocket handler when an
// agent connection closes.
func (s *GatewayService) DisconnectAgent(sessionID string) {
	s.sessions.UnregisterAgent(sessionID)
	s.control.HandleAgentDisconnect(sessionID)
}

// DisconnectWeb removes a web viewer connection from the session. It is called
// by the WebSocket handler when a web connection closes.
func (s *GatewayService) DisconnectWeb(sessionID, connID string) {
	s.sessions.RemoveWebConn(sessionID, connID)
}

// TouchSession refreshes the idle TTL for a session by updating its
// LastTrafficTime.
func (s *GatewayService) TouchSession(sessionID string) error {
	return s.sessions.TouchSession(sessionID)
}

// GetRuntime returns the current session runtime state.
func (s *GatewayService) GetRuntime(_ context.Context, sessionID string) (*domain.SessionRuntime, error) {
	rt := s.sessions.GetRuntime(sessionID)
	if rt == nil {
		return nil, domain.ErrSessionNotFound
	}

	return rt, nil
}

// CreateGameRuntime creates an in-memory game runtime for the session, stamps
// ownership fields with per-session epoch, initializes the idle timer, and
// issues a routing token.
func (s *GatewayService) CreateGameRuntime(_ context.Context, sessionID string, reconnectGeneration int64) (*domain.SessionRuntime, string, error) {
	rt := s.sessions.GetOrCreateRuntime(sessionID)

	if rt.OwnerGatewayID == s.config.GatewayID && rt.ReconnectGeneration > 0 {
		// Runtime already exists on this pod (reconnect to same pod).
		if reconnectGeneration > rt.ReconnectGeneration {
			rt.ReconnectGeneration = reconnectGeneration
		}
		rt.OwnerEpoch++
	} else {
		// New runtime or takeover from another pod.
		rt.OwnerGatewayID = s.config.GatewayID
		rt.OwnerEpoch = 1
		rt.ReconnectGeneration = reconnectGeneration
	}

	s.sessions.TouchSession(sessionID)

	tokenStr, err := s.tokenIssuer.Issue(sessionID, s.config.GatewayID, rt.OwnerEpoch, token.TokenAudienceInternal, rt.ReconnectGeneration)
	if err != nil {
		return nil, "", fmt.Errorf("issue token: %w", err)
	}

	return rt, tokenStr, nil
}

// RefreshGameRuntime refreshes the token for an existing runtime on this
// gateway. Takeover is NOT supported — the old token must belong to this
// gateway. Old tokens with the same generation and a lower or equal epoch
// are accepted (allows concurrent re-issuers to converge).
func (s *GatewayService) RefreshGameRuntime(_ context.Context, sessionID, oldToken string) (*domain.SessionRuntime, string, error) {
	claims, err := s.tokenVerifier.VerifyWithGrace(oldToken, s.config.TokenRefreshGrace)
	if err != nil {
		return nil, "", fmt.Errorf("verify old token: %w", err)
	}

	if err := claims.ValidateOwnerEpoch(); err != nil {
		return nil, "", fmt.Errorf("validate owner epoch: %w", err)
	}

	if err := claims.ValidateAudience(token.TokenAudienceInternal); err != nil {
		return nil, "", fmt.Errorf("validate audience: %w", err)
	}

	if claims.SessionID != sessionID {
		return nil, "", errSessionMismatch
	}

	if claims.OwnerGatewayID != s.config.GatewayID {
		return nil, "", fmt.Errorf("token owned by %q, this gateway is %q — takeover not supported via refresh", claims.OwnerGatewayID, s.config.GatewayID)
	}

	rt := s.sessions.GetRuntime(sessionID)
	if rt == nil {
		return nil, "", fmt.Errorf("runtime not found: %w", domain.ErrSessionNotFound)
	}

	if claims.ReconnectGeneration != rt.ReconnectGeneration {
		return nil, "", fmt.Errorf("generation mismatch: token=%d runtime=%d", claims.ReconnectGeneration, rt.ReconnectGeneration)
	}
	if claims.OwnerEpoch > rt.OwnerEpoch {
		return nil, "", fmt.Errorf("token epoch %d > current epoch %d", claims.OwnerEpoch, rt.OwnerEpoch)
	}

	rt.OwnerEpoch++
	s.sessions.TouchSession(sessionID)

	tokenStr, err := s.tokenIssuer.Issue(sessionID, s.config.GatewayID, rt.OwnerEpoch, token.TokenAudienceInternal, rt.ReconnectGeneration)
	if err != nil {
		return nil, "", fmt.Errorf("issue token: %w", err)
	}

	return rt, tokenStr, nil
}

// getOrCreateMediaCache returns the media cache for the given session, creating
// one on demand if it does not exist.
func (s *GatewayService) getOrCreateMediaCache(sessionID string) domain.MediaCache {
	s.mediaMu.Lock()
	defer s.mediaMu.Unlock()

	if c, ok := s.mediaCaches[sessionID]; ok {
		return c
	}

	c := mediacache.NewCache()
	s.mediaCaches[sessionID] = c
	return c
}
