package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"dominion/projects/game/gateway/domain"
	"dominion/projects/game/gateway/domain/mediacache"
	"dominion/projects/game/gateway/domain/sessionmanager"
	"dominion/projects/game/pkg/token"
)

// AsyncMessageSink receives routed messages outside the synchronous handler
// flow. GatewayService calls it when control operations complete asynchronously
// (timeout or agent disconnect).
type AsyncMessageSink interface {
	RouteRoutedMessage(ctx context.Context, msg *domain.RoutedMessage)
}

var (
	// errGatewayMismatch indicates the token's gateway ID does not match this
	// gateway instance.
	errGatewayMismatch = errors.New("gateway ID mismatch")
	// errSessionMismatch indicates the token's session ID does not match the
	// path parameter.
	errSessionMismatch = errors.New("session ID mismatch")
)

// GatewayService orchestrates the game gateway business logic, coordinating
// session management, media caching, control operations, and token verification
// for a single gateway instance.
type GatewayService struct {
	sessions      *sessionmanager.Manager
	mediaCaches   map[string]domain.MediaCache
	mediaMu       sync.Mutex
	control       *ControlExecutor
	asyncSink     AsyncMessageSink
	gatewayID     string
	tokenVerifier token.Verifier
}

// NewGatewayService creates a GatewayService with the given dependencies.
func NewGatewayService(
	sessions *sessionmanager.Manager,
	control *ControlExecutor,
	gatewayID string,
	verifier token.Verifier,
) *GatewayService {
	svc := &GatewayService{
		sessions:      sessions,
		mediaCaches:   map[string]domain.MediaCache{},
		control:       control,
		gatewayID:     gatewayID,
		tokenVerifier: verifier,
	}
	control.SetOnCompletion(svc.handleAsyncCompletion)
	return svc
}

func (s *GatewayService) SetAsyncSink(sink AsyncMessageSink) {
	s.asyncSink = sink
}

func (s *GatewayService) handleAsyncCompletion(comp domain.ControlCompletion) {
	if comp.FlashSnapshot {
		cache := s.getOrCreateMediaCache(comp.SessionID)
		if snap, err := cache.RefreshSnapshot(); err == nil && snap != nil {
			rt := s.sessions.GetRuntime(comp.SessionID)
			if rt != nil {
				rt.LatestSnapshot = snap
			}
		}
	}

	if s.asyncSink != nil {
		s.asyncSink.RouteRoutedMessage(context.Background(), &domain.RoutedMessage{
			TargetConnID: comp.RequesterConnID,
			Message: &domain.Message{
				SessionID: comp.SessionID,
				Payload:   comp.Result,
			},
		})
	}
}

// ConnectSession validates the connection token and returns the session runtime
// and embedded claims. It verifies the token signature and expiry, confirms the
// gateway ID matches this instance, and ensures the session ID in the token
// matches the path parameter.
func (s *GatewayService) ConnectSession(_ context.Context, pathSessionID, tokenStr string) (*domain.SessionRuntime, *token.Claims, error) {
	claims, err := s.tokenVerifier.Verify(tokenStr)
	if err != nil {
		return nil, nil, fmt.Errorf("verify token: %w", err)
	}

	if claims.GatewayID != s.gatewayID {
		return nil, nil, errGatewayMismatch
	}

	if claims.SessionID != pathSessionID {
		return nil, nil, errSessionMismatch
	}

	rt := s.sessions.GetOrCreateRuntime(pathSessionID)
	return rt, claims, nil
}

// ProcessHello handles the hello message after WebSocket upgrade. For agent
// connections, it registers the agent on the session runtime. For web
// connections, it adds the web connection and returns catch-up messages
// (cached media_init and segments from the last key frame).
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
// key frame for a late-joining web client. All messages use TargetConnID=""
// (broadcast to all web connections).
func (s *GatewayService) buildCatchUpMessages(sessionID string) []*domain.RoutedMessage {
	cache := s.getOrCreateMediaCache(sessionID)
	var msgs []*domain.RoutedMessage

	if init, ok := cache.GetInitSegment(); ok {
		msgs = append(msgs, &domain.RoutedMessage{
			TargetConnID: "",
			Message: &domain.Message{
				SessionID: sessionID,
				Payload: domain.MediaInitPayload{
					MimeType: init.MimeType,
					Segment:  init.Data,
				},
			},
		})
	}

	for _, seg := range cache.GetSegmentsFromLastKeyframe() {
		msgs = append(msgs, &domain.RoutedMessage{
			TargetConnID: "",
			Message: &domain.Message{
				SessionID: sessionID,
				Payload: domain.MediaSegmentPayload{
					SegmentID: seg.SegmentID,
					Segment:   seg.Data,
					KeyFrame:  seg.KeyFrame,
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
	if err := cache.StoreInitSegment(init.MimeType, init.Segment); err != nil {
		return nil, err
	}

	return []*domain.RoutedMessage{
		{
			TargetConnID: "",
			Message: &domain.Message{
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
	if err := cache.AddSegment(&domain.SegmentRef{
		SegmentID: seg.SegmentID,
		Data:      seg.Segment,
		KeyFrame:  seg.KeyFrame,
		MediaTime: time.Now(),
	}); err != nil {
		return nil, err
	}

	return []*domain.RoutedMessage{
		{
			TargetConnID: "",
			Message: &domain.Message{
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
			TargetConnID: requesterConnID,
			Message: &domain.Message{
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
			TargetConnID: requesterConnID,
			Message: &domain.Message{
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
			TargetConnID: "",
			Message: &domain.Message{
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
			TargetConnID: connID,
			Message: &domain.Message{
				SessionID: sessionID,
				Payload: domain.PongPayload{
					Nonce: ping.Nonce,
				},
			},
		},
	}, nil
}

// GetSnapshot returns the latest snapshot for a session. If the cached snapshot
// is stale, it refreshes from the latest key frame segment.
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

// GetRuntime returns the current session runtime state.
func (s *GatewayService) GetRuntime(_ context.Context, sessionID string) (*domain.SessionRuntime, error) {
	rt := s.sessions.GetRuntime(sessionID)
	if rt == nil {
		return nil, domain.ErrSessionNotFound
	}

	return rt, nil
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
