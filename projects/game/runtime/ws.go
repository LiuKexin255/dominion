package runtime

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"dominion/common/gopkg/bootstrap"
	"dominion/common/gopkg/logs"
	"dominion/common/gopkg/logs/event"
	"dominion/common/gopkg/otel"
	"dominion/projects/game/runtime/domain"
	"dominion/projects/game/pkg/token"

	"github.com/coder/websocket"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/protobuf/encoding/protojson"
)

var (
	protojsonMarshaler   = protojson.MarshalOptions{}
	protojsonUnmarshaler = protojson.UnmarshalOptions{DiscardUnknown: true}
)

var connIDSeq int64

func nextConnID() string {
	return fmt.Sprintf("ws-%d", atomic.AddInt64(&connIDSeq, 1))
}

const (
	wsPathPrefix = "/v1/sessions/"
	wsPathSuffix = "/game/connect"
	helloTimeout = 10 * time.Second

	spanConnect = "runtime.ws.connect"
	spanHello   = "runtime.ws.hello"

	logFieldSessionID  = "session_id"
	logFieldConnID     = "conn_id"
	logFieldClientRole = "client_role"

	// Protocol-level WebSocket error codes.
	ErrCodeSessionMismatch       = "session_mismatch"
	ErrCodeMissingPayload        = "missing_payload"
	ErrCodeUnsupportedCodec      = "unsupported_codec"
	ErrCodeInitHashMismatch      = "init_hash_mismatch"
	ErrCodeStreamMismatch        = "stream_mismatch"
	ErrCodeUnknownInitID         = "unknown_init_id"
	ErrCodeSequenceNonIncreasing = "sequence_not_increasing"
	ErrCodeRandomAccessMissing   = "random_access_missing"
	ErrCodeSegmentTooLarge       = "segment_too_large"
)

// WebSocketHandler handles WebSocket upgrade and message routing for the game
// gateway. It implements http.Handler.
type WebSocketHandler struct {
	svc runtimeService

	mu          sync.RWMutex
	conns       map[string]*wsConn
	webConns    map[string]map[string]struct{}
	agentConnID map[string]string
}

func NewWebSocketHandler(svc runtimeService) *WebSocketHandler {
	return &WebSocketHandler{
		svc:         svc,
		conns:       make(map[string]*wsConn),
		webConns:    make(map[string]map[string]struct{}),
		agentConnID: make(map[string]string),
	}
}

// StartRoutingWorker returns a WorkerBuilder that creates a routing worker
// consuming the async message channel and delivering messages to WebSocket
// connections.
func (h *WebSocketHandler) StartRoutingWorker() bootstrap.WorkerBuilder {
	return bootstrap.WorkerBuilderFunc(func(_ context.Context) (bootstrap.Worker, error) {
		return NewRoutingWorker(h.svc.AsyncMessages(), h.RouteRoutedMessage), nil
	})
}

type wsConn struct {
	conn   *websocket.Conn
	mu     sync.Mutex
	connID string
	role   domain.ClientRole
}

func (c *wsConn) write(ctx context.Context, env *GameWebSocketEnvelope) error {
	data, err := protojsonMarshaler.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshal envelope: %w", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.Write(ctx, websocket.MessageText, data)
}

func ParseSessionID(path string) (string, error) {
	if !strings.HasPrefix(path, wsPathPrefix) || !strings.HasSuffix(path, wsPathSuffix) {
		return "", fmt.Errorf("invalid WebSocket path: %s", path)
	}
	id := strings.TrimPrefix(path, wsPathPrefix)
	id = strings.TrimSuffix(id, wsPathSuffix)
	if id == "" || strings.Contains(id, "/") {
		return "", fmt.Errorf("invalid session ID in path: %s", path)
	}
	return id, nil
}

func (h *WebSocketHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sessionID, err := ParseSessionID(r.URL.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx, connectSpan := otel.Tracer().Start(r.Context(), spanConnect)
	defer connectSpan.End()
	connectSpan.SetAttributes(attribute.String(logFieldSessionID, sessionID))

	logs.Info(ctx, "runtime: ws connect started", event.String(logFieldSessionID, sessionID))

	tokenStr := r.URL.Query().Get("token")
	if tokenStr == "" {
		logs.Warn(ctx, "runtime: ws token missing", event.String(logFieldSessionID, sessionID))
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}

	rt, claims, err := h.svc.ConnectSession(ctx, sessionID, tokenStr)
	if err != nil {
		logs.Warn(ctx, "runtime: ws connect session failed", event.String(logFieldSessionID, sessionID), event.Err(err))
		http.Error(w, fmt.Sprintf("invalid token: %v", err), http.StatusUnauthorized)
		return
	}

	_ = h.svc.TouchSession(sessionID)

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}

	conn.SetReadLimit(int64(domain.MaxSegmentSize)*2 + 4096)

	connID := nextConnID()
	connectSpan.SetAttributes(attribute.String(logFieldConnID, connID))

	logs.Info(ctx, "runtime: ws connected", event.String(logFieldSessionID, sessionID), event.String(logFieldConnID, connID))

	wc := &wsConn{conn: conn, connID: connID}

	h.registerConn(wc)
	defer h.unregisterConn(connID)

	h.serveConn(ctx, wc, sessionID, rt, claims)
}

func (h *WebSocketHandler) serveConn(ctx context.Context, wc *wsConn, sessionID string, rt *domain.SessionRuntime, claims *token.Claims) {
	helloCtx, helloCancel := context.WithTimeout(ctx, helloTimeout)
	defer helloCancel()

	_, helloSpan := otel.Tracer().Start(helloCtx, spanHello)
	helloSpan.SetAttributes(
		attribute.String(logFieldSessionID, sessionID),
		attribute.String(logFieldConnID, wc.connID),
	)

	env, err := readEnvelope(helloCtx, wc.conn)
	if err != nil {
		helloSpan.End()
		wc.conn.Close(websocket.StatusPolicyViolation, "hello timeout or read error")
		return
	}

	if err := ValidateHello(env); err != nil {
		helloSpan.End()
		sendErrorAndClose(wc, ctx, ErrCodeProtocolError, err.Error())
		return
	}
	wc.role = toDomainClientRole(env.GetHello().GetRole())

	initMsgs, svcErr := h.svc.ProcessHello(rt, claims, wc.role, wc.connID)
	if svcErr != nil {
		helloSpan.End()
		sendErrorAndClose(wc, ctx, "runtime_error", svcErr.Error())
		return
	}

	helloSpan.SetAttributes(attribute.String(logFieldClientRole, clientRoleString(wc.role)))
	helloSpan.End()

	logs.Info(ctx, "runtime: ws hello completed",
		event.String(logFieldSessionID, sessionID),
		event.String(logFieldConnID, wc.connID),
		event.String(logFieldClientRole, clientRoleString(wc.role)),
	)

	h.trackConn(sessionID, wc)
	defer h.cleanupDisconnect(sessionID, wc)
	defer wc.conn.Close(websocket.StatusNormalClosure, "")
	defer logs.Info(ctx, "runtime: ws disconnect",
		event.String(logFieldSessionID, sessionID),
		event.String(logFieldConnID, wc.connID),
	)

	for _, r := range initMsgs {
		if writeErr := wc.write(ctx, toProtoMessage(&r.Message)); writeErr != nil {
			return
		}
	}

	for {
		msgEnv, readErr := readEnvelope(ctx, wc.conn)
		if readErr != nil {
			return
		}

		if err := ValidateWebSocketEnvelope(msgEnv); err != nil {
			sendErrorAndClose(wc, ctx, ErrCodeProtocolError, err.Error())
			return
		}
		if msgEnv.GetSessionId() != sessionID {
			sendErrorAndClose(wc, ctx, ErrCodeSessionMismatch, "session_id mismatch")
			return
		}
		if err := ValidateRolePayload(toProtoClientRole(wc.role), msgEnv); err != nil {
			sendErrorAndClose(wc, ctx, ErrCodeProtocolError, err.Error())
			return
		}

		if err := validateMediaPayload(msgEnv); err != nil {
			sendErrorAndClose(wc, ctx, ErrCodeProtocolError, err.Error())
			return
		}

		msg := toDomainMessage(msgEnv)

		var routed []*domain.RoutedMessage
		if wc.role == domain.ClientRoleWindowsAgent {
			routed, svcErr = h.svc.HandleAgentMessage(ctx, sessionID, msg)
		} else {
			routed, svcErr = h.svc.HandleWebMessage(ctx, sessionID, wc.connID, msg)
		}
		if svcErr != nil {
			if isFatalAgentError(svcErr) {
				logs.Warn(ctx, "runtime: ws fatal protocol error",
					event.String(logFieldSessionID, sessionID),
					event.String(logFieldConnID, wc.connID),
					event.Err(svcErr),
				)
				sendErrorAndClose(wc, ctx, ErrCodeProtocolError, svcErr.Error())
				return
			}
			logs.Warn(ctx, "runtime: ws discarding segment",
				event.String(logFieldSessionID, sessionID),
				event.String(logFieldConnID, wc.connID),
				event.Err(svcErr),
			)
			continue
		}

		h.routeMessages(ctx, sessionID, wc, routed)
	}
}

// validateMediaPayload validates media-specific proto fields before domain
// conversion. Returns nil for non-media payloads.
func validateMediaPayload(env *GameWebSocketEnvelope) error {
	switch p := env.Payload.(type) {
	case *GameWebSocketEnvelope_MediaInit:
		return ValidateMediaInit(p.MediaInit)
	case *GameWebSocketEnvelope_MediaSegment:
		return ValidateMediaSegment(p.MediaSegment)
	default:
		return nil
	}
}

// isFatalAgentError returns true for sentinel errors that indicate a protocol
// violation requiring disconnection (unknown init, stream mismatch, init hash
// mismatch, unsupported codec). Non-fatal errors (sequence not increasing,
// random access missing) are logged and the segment is discarded.
func isFatalAgentError(err error) bool {
	return errors.Is(err, domain.ErrUnknownInitID) ||
		errors.Is(err, domain.ErrStreamMismatch) ||
		errors.Is(err, domain.ErrInitHashMismatch) ||
		errors.Is(err, domain.ErrUnsupportedCodec)
}

func (h *WebSocketHandler) registerConn(wc *wsConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.conns[wc.connID] = wc
}

func (h *WebSocketHandler) unregisterConn(connID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.conns, connID)
}

func (h *WebSocketHandler) trackConn(sessionID string, wc *wsConn) {
	h.mu.Lock()
	defer h.mu.Unlock()

	switch wc.role {
	case domain.ClientRoleWindowsAgent:
		h.agentConnID[sessionID] = wc.connID
	case domain.ClientRoleWeb:
		if h.webConns[sessionID] == nil {
			h.webConns[sessionID] = make(map[string]struct{})
		}
		h.webConns[sessionID][wc.connID] = struct{}{}
	}
}

func (h *WebSocketHandler) untrackConn(sessionID string, wc *wsConn) {
	h.mu.Lock()
	defer h.mu.Unlock()

	switch wc.role {
	case domain.ClientRoleWindowsAgent:
		delete(h.agentConnID, sessionID)
	case domain.ClientRoleWeb:
		if set, ok := h.webConns[sessionID]; ok {
			delete(set, wc.connID)
			if len(set) == 0 {
				delete(h.webConns, sessionID)
			}
		}
	}
}

func (h *WebSocketHandler) cleanupDisconnect(sessionID string, wc *wsConn) {
	h.untrackConn(sessionID, wc)

	switch wc.role {
	case domain.ClientRoleWindowsAgent:
		h.svc.DisconnectAgent(sessionID)
	case domain.ClientRoleWeb:
		h.svc.DisconnectWeb(sessionID, wc.connID)
	}
}

func (h *WebSocketHandler) routeMessages(ctx context.Context, sessionID string, _ *wsConn, msgs []*domain.RoutedMessage) {
	for _, msg := range msgs {
		protoMsg := toProtoMessage(&msg.Message)
		switch msg.TargetKind {
		case domain.RouteTargetAgent:
			h.sendToAgentConn(ctx, sessionID, protoMsg)
		case domain.RouteTargetWebBroadcast:
			h.broadcastToWebConns(ctx, sessionID, protoMsg)
		case domain.RouteTargetConn:
			h.sendToConn(ctx, msg.TargetConnID, protoMsg)
		}
	}
}

func (h *WebSocketHandler) broadcastToWebConns(ctx context.Context, sessionID string, env *GameWebSocketEnvelope) {
	h.mu.RLock()
	set := h.webConns[sessionID]
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	h.mu.RUnlock()

	for _, id := range ids {
		h.sendToConn(ctx, id, env)
	}
}

func (h *WebSocketHandler) sendToAgentConn(ctx context.Context, sessionID string, env *GameWebSocketEnvelope) {
	h.mu.RLock()
	agentID := h.agentConnID[sessionID]
	h.mu.RUnlock()

	if agentID != "" {
		h.sendToConn(ctx, agentID, env)
	}
}

func (h *WebSocketHandler) sendToConn(ctx context.Context, connID string, env *GameWebSocketEnvelope) {
	h.mu.RLock()
	wc := h.conns[connID]
	h.mu.RUnlock()

	if wc != nil {
		_ = wc.write(ctx, env)
	}
}

// RouteRoutedMessage converts a domain RoutedMessage to proto and delivers it
// to the target connection.
func (h *WebSocketHandler) RouteRoutedMessage(ctx context.Context, msg *domain.RoutedMessage) {
	if msg == nil {
		return
	}
	protoMsg := toProtoMessage(&msg.Message)
	switch msg.TargetKind {
	case domain.RouteTargetAgent:
		h.sendToAgentConn(ctx, msg.Message.SessionID, protoMsg)
	case domain.RouteTargetWebBroadcast:
		h.broadcastToWebConns(ctx, msg.Message.SessionID, protoMsg)
	case domain.RouteTargetConn:
		h.sendToConn(ctx, msg.TargetConnID, protoMsg)
	}
}

func readEnvelope(ctx context.Context, conn *websocket.Conn) (*GameWebSocketEnvelope, error) {
	_, data, err := conn.Read(ctx)
	if err != nil {
		return nil, fmt.Errorf("read message: %w", err)
	}

	env := new(GameWebSocketEnvelope)
	if err := protojsonUnmarshaler.Unmarshal(data, env); err != nil {
		return nil, fmt.Errorf("unmarshal envelope: %w", err)
	}
	return env, nil
}

func sendErrorAndClose(wc *wsConn, ctx context.Context, code, message string) {
	env := &GameWebSocketEnvelope{
		Payload: &GameWebSocketEnvelope_Error{
			Error: &GameError{
				Code:    code,
				Message: message,
			},
		},
	}
	_ = wc.write(ctx, env)
	wc.conn.Close(websocket.StatusPolicyViolation, message)
}

func toDomainClientRole(role GameClientRole) domain.ClientRole {
	switch role {
	case GameClientRole_GAME_CLIENT_ROLE_WINDOWS_AGENT:
		return domain.ClientRoleWindowsAgent
	case GameClientRole_GAME_CLIENT_ROLE_WEB:
		return domain.ClientRoleWeb
	default:
		return domain.ClientRoleUnspecified
	}
}

func toProtoClientRole(role domain.ClientRole) GameClientRole {
	switch role {
	case domain.ClientRoleWindowsAgent:
		return GameClientRole_GAME_CLIENT_ROLE_WINDOWS_AGENT
	case domain.ClientRoleWeb:
		return GameClientRole_GAME_CLIENT_ROLE_WEB
	default:
		return GameClientRole_GAME_CLIENT_ROLE_UNSPECIFIED
	}
}

func toDomainControlResultStatus(status GameControlResultStatus) domain.ControlResultStatus {
	switch status {
	case GameControlResultStatus_GAME_CONTROL_RESULT_STATUS_SUCCEEDED:
		return domain.ControlResultStatusSucceeded
	case GameControlResultStatus_GAME_CONTROL_RESULT_STATUS_FAILED:
		return domain.ControlResultStatusFailed
	case GameControlResultStatus_GAME_CONTROL_RESULT_STATUS_TIMED_OUT:
		return domain.ControlResultStatusTimedOut
	default:
		return domain.ControlResultStatusFailed
	}
}

func toDomainMessage(env *GameWebSocketEnvelope) *domain.Message {
	if env == nil {
		return nil
	}
	return &domain.Message{
		SessionID: env.GetSessionId(),
		MessageID: env.GetMessageId(),
		Payload:   toDomainPayload(env),
	}
}

func toDomainPayload(env *GameWebSocketEnvelope) domain.MessagePayload {
	switch p := env.Payload.(type) {
	case *GameWebSocketEnvelope_Hello:
		return domain.HelloPayload{Role: toDomainClientRole(p.Hello.GetRole())}
	case *GameWebSocketEnvelope_Ping:
		return domain.PingPayload{Nonce: p.Ping.GetNonce()}
	case *GameWebSocketEnvelope_Pong:
		return domain.PongPayload{Nonce: p.Pong.GetNonce()}
	case *GameWebSocketEnvelope_MediaInit:
		init := p.MediaInit
		return domain.MediaInitPayload{
			StreamID: init.GetStreamId(),
			InitID:   init.GetInitId(),
			MimeType: init.GetMimeType(),
			Codec:    init.GetCodec(),
			Segment:  init.GetSegment(),
		}
	case *GameWebSocketEnvelope_MediaSegment:
		seg := p.MediaSegment
		return domain.MediaSegmentPayload{
			StreamID:      seg.GetStreamId(),
			InitID:        seg.GetInitId(),
			Sequence:      seg.GetSequence(),
			Segment:       seg.GetSegment(),
			RandomAccess:  seg.GetRandomAccess(),
			DurationMS:    seg.GetDurationMs(),
			Discontinuity: seg.GetDiscontinuity(),
		}
	case *GameWebSocketEnvelope_ControlRequest:
		req := p.ControlRequest
		kind, err := ActionKindFromProto(req)
		if err != nil {
			return domain.ErrorPayload{Code: "protocol_error", Message: err.Error()}
		}
		return domain.ControlRequestPayload{
			OperationID:   req.GetOperationId(),
			ActionKind:    kind,
			FlashSnapshot: req.GetFlashSnapshot(),
			RawRequest:    req, // preserve full proto for forwarding
		}
	case *GameWebSocketEnvelope_ControlAck:
		return domain.ControlAckPayload{
			RequestID: p.ControlAck.GetOperationId(),
		}
	case *GameWebSocketEnvelope_ControlResult:
		result := p.ControlResult
		return domain.ControlResultPayload{
			OperationID:  result.GetOperationId(),
			Status:       toDomainControlResultStatus(result.GetStatus()),
			ErrorMessage: result.GetErrorMessage(),
		}
	case *GameWebSocketEnvelope_Error:
		return domain.ErrorPayload{
			Code:    p.Error.GetCode(),
			Message: p.Error.GetMessage(),
		}
	default:
		return domain.ErrorPayload{Code: "unknown_payload", Message: "unrecognized payload type"}
	}
}

func toProtoMessage(msg *domain.Message) *GameWebSocketEnvelope {
	if msg == nil {
		return nil
	}
	return &GameWebSocketEnvelope{
		SessionId: msg.SessionID,
		MessageId: msg.MessageID,
		Payload:   toProtoPayload(msg.Payload),
	}
}

func toProtoPayload(payload domain.MessagePayload) isGameWebSocketEnvelope_Payload {
	switch p := payload.(type) {
	case domain.HelloPayload:
		return &GameWebSocketEnvelope_Hello{
			Hello: &GameHello{Role: toProtoClientRole(p.Role)},
		}
	case domain.PingPayload:
		return &GameWebSocketEnvelope_Ping{
			Ping: &GamePing{Nonce: p.Nonce},
		}
	case domain.PongPayload:
		return &GameWebSocketEnvelope_Pong{
			Pong: &GamePong{Nonce: p.Nonce},
		}
	case domain.MediaInitPayload:
		return &GameWebSocketEnvelope_MediaInit{
			MediaInit: &GameMediaInit{
				StreamId: p.StreamID,
				InitId:   p.InitID,
				MimeType: p.MimeType,
				Codec:    p.Codec,
				Segment:  p.Segment,
			},
		}
	case domain.MediaSegmentPayload:
		return &GameWebSocketEnvelope_MediaSegment{
			MediaSegment: &GameMediaSegment{
				StreamId:      p.StreamID,
				InitId:        p.InitID,
				Sequence:      p.Sequence,
				Segment:       p.Segment,
				RandomAccess:  &p.RandomAccess,
				DurationMs:    p.DurationMS,
				Discontinuity: p.Discontinuity,
			},
		}
	case domain.ControlRequestPayload:
		if p.RawRequest != nil {
			if raw, ok := p.RawRequest.(*GameControlRequest); ok {
				return &GameWebSocketEnvelope_ControlRequest{ControlRequest: raw}
			}
		}
		return &GameWebSocketEnvelope_ControlRequest{
			ControlRequest: &GameControlRequest{
				OperationId:   p.OperationID,
				FlashSnapshot: p.FlashSnapshot,
			},
		}
	case domain.ControlAckPayload:
		return &GameWebSocketEnvelope_ControlAck{
			ControlAck: &GameControlAck{
				OperationId: p.RequestID,
			},
		}
	case domain.ControlResultPayload:
		var status GameControlResultStatus
		switch p.Status {
		case domain.ControlResultStatusSucceeded:
			status = GameControlResultStatus_GAME_CONTROL_RESULT_STATUS_SUCCEEDED
		case domain.ControlResultStatusFailed:
			status = GameControlResultStatus_GAME_CONTROL_RESULT_STATUS_FAILED
		case domain.ControlResultStatusTimedOut:
			status = GameControlResultStatus_GAME_CONTROL_RESULT_STATUS_TIMED_OUT
		default:
			status = GameControlResultStatus_GAME_CONTROL_RESULT_STATUS_UNSPECIFIED
		}
		return &GameWebSocketEnvelope_ControlResult{
			ControlResult: &GameControlResult{
				OperationId:  p.OperationID,
				Status:       status,
				ErrorMessage: p.ErrorMessage,
			},
		}
	case domain.ErrorPayload:
		return &GameWebSocketEnvelope_Error{
			Error: &GameError{
				Code:    p.Code,
				Message: p.Message,
			},
		}
	default:
		return &GameWebSocketEnvelope_Error{
			Error: &GameError{
				Code:    "unknown_payload",
				Message: "unrecognized payload type",
			},
		}
	}
}

func clientRoleString(r domain.ClientRole) string {
	switch r {
	case domain.ClientRoleWindowsAgent:
		return "agent"
	case domain.ClientRoleWeb:
		return "web"
	default:
		return "unspecified"
	}
}
