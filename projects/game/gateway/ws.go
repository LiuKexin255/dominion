package gateway

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"dominion/projects/game/gateway/domain"
	"dominion/projects/game/pkg/token"

	"github.com/coder/websocket"
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
)

// WebSocketHandler handles WebSocket upgrade and message routing for the game
// gateway. It implements http.Handler.
type WebSocketHandler struct {
	svc gatewayService

	mu          sync.RWMutex
	conns       map[string]*wsConn
	webConns    map[string]map[string]struct{}
	agentConnID map[string]string
}

// NewWebSocketHandler creates a WebSocketHandler backed by svc.
func NewWebSocketHandler(svc gatewayService) *WebSocketHandler {
	return &WebSocketHandler{
		svc:         svc,
		conns:       make(map[string]*wsConn),
		webConns:    make(map[string]map[string]struct{}),
		agentConnID: make(map[string]string),
	}
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

// ServeHTTP implements http.Handler.
func (h *WebSocketHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sessionID, err := ParseSessionID(r.URL.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	tokenStr := r.URL.Query().Get("token")
	if tokenStr == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}

	rt, claims, err := h.svc.ConnectSession(r.Context(), sessionID, tokenStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("connect session: %v", err), http.StatusUnauthorized)
		return
	}

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}

	conn.SetReadLimit(int64(domain.MaxSegmentSize)*2 + 4096)

	connID := nextConnID()
	wc := &wsConn{conn: conn, connID: connID}

	h.registerConn(wc)
	defer h.unregisterConn(connID)

	h.serveConn(r.Context(), wc, sessionID, rt, claims)
}

func (h *WebSocketHandler) serveConn(ctx context.Context, wc *wsConn, sessionID string, rt *domain.SessionRuntime, claims *token.Claims) {
	helloCtx, helloCancel := context.WithTimeout(ctx, helloTimeout)
	defer helloCancel()

	env, err := readEnvelope(helloCtx, wc.conn)
	if err != nil {
		wc.conn.Close(websocket.StatusPolicyViolation, "hello timeout or read error")
		return
	}

	hello := env.GetHello()
	if hello == nil {
		wc.conn.Close(websocket.StatusPolicyViolation, "expected hello message")
		return
	}
	wc.role = toDomainClientRole(hello.Role)

	initMsgs, svcErr := h.svc.ProcessHello(rt, claims, wc.role, wc.connID)
	if svcErr != nil {
		sendErrorAndClose(wc, ctx, svcErr.Error())
		return
	}

	h.trackConn(sessionID, wc)
	defer h.cleanupDisconnect(sessionID, wc)
	defer wc.conn.Close(websocket.StatusNormalClosure, "")

	for _, r := range initMsgs {
		if writeErr := wc.write(ctx, toProtoMessage(r.Message)); writeErr != nil {
			return
		}
	}

	for {
		msgEnv, readErr := readEnvelope(ctx, wc.conn)
		if readErr != nil {
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
			sendErrorAndClose(wc, ctx, svcErr.Error())
			return
		}

		h.routeMessages(ctx, sessionID, wc, routed)
	}
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

func (h *WebSocketHandler) routeMessages(ctx context.Context, sessionID string, sender *wsConn, msgs []*domain.RoutedMessage) {
	for _, msg := range msgs {
		protoMsg := toProtoMessage(msg.Message)
		if msg.TargetConnID == "" {
			if sender.role == domain.ClientRoleWindowsAgent {
				h.broadcastToWebConns(ctx, sessionID, protoMsg)
			} else {
				h.sendToAgentConn(ctx, sessionID, protoMsg)
			}
		} else {
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
	if msg == nil || msg.Message == nil {
		return
	}
	protoMsg := toProtoMessage(msg.Message)
	if msg.TargetConnID != "" {
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

func sendErrorAndClose(wc *wsConn, ctx context.Context, message string) {
	env := &GameWebSocketEnvelope{
		Payload: &GameWebSocketEnvelope_Error{
			Error: &GameError{
				Code:    "gateway_error",
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

func toDomainOperationKind(kind GameControlOperationKind) domain.OperationKind {
	switch kind {
	case GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_CLICK:
		return domain.OperationKindMouseClick
	case GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_DOUBLE_CLICK:
		return domain.OperationKindMouseDoubleClick
	case GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_DRAG:
		return domain.OperationKindMouseDrag
	case GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_HOVER:
		return domain.OperationKindMouseHover
	case GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_HOLD:
		return domain.OperationKindMouseHold
	default:
		return domain.OperationKindMouseClick
	}
}

func toProtoOperationKindWS(kind domain.OperationKind) GameControlOperationKind {
	switch kind {
	case domain.OperationKindMouseClick:
		return GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_CLICK
	case domain.OperationKindMouseDoubleClick:
		return GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_DOUBLE_CLICK
	case domain.OperationKindMouseDrag:
		return GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_DRAG
	case domain.OperationKindMouseHover:
		return GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_HOVER
	case domain.OperationKindMouseHold:
		return GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_HOLD
	default:
		return GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_UNSPECIFIED
	}
}

func mouseButtonToString(btn GameMouseButton) string {
	switch btn {
	case GameMouseButton_GAME_MOUSE_BUTTON_LEFT:
		return "left"
	case GameMouseButton_GAME_MOUSE_BUTTON_RIGHT:
		return "right"
	case GameMouseButton_GAME_MOUSE_BUTTON_MIDDLE:
		return "middle"
	default:
		return ""
	}
}

func stringToMouseButton(s string) GameMouseButton {
	switch strings.ToLower(s) {
	case "left":
		return GameMouseButton_GAME_MOUSE_BUTTON_LEFT
	case "right":
		return GameMouseButton_GAME_MOUSE_BUTTON_RIGHT
	case "middle":
		return GameMouseButton_GAME_MOUSE_BUTTON_MIDDLE
	default:
		return GameMouseButton_GAME_MOUSE_BUTTON_UNSPECIFIED
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
		return domain.MediaInitPayload{
			MimeType: p.MediaInit.GetMimeType(),
			Segment:  p.MediaInit.GetSegment(),
		}
	case *GameWebSocketEnvelope_MediaSegment:
		return domain.MediaSegmentPayload{
			SegmentID: p.MediaSegment.GetSegmentId(),
			Segment:   p.MediaSegment.GetSegment(),
			KeyFrame:  p.MediaSegment.GetKeyFrame(),
		}
	case *GameWebSocketEnvelope_ControlRequest:
		req := p.ControlRequest
		mouse := req.GetMouse()
		return domain.ControlRequestPayload{
			RequestID:     req.GetOperationId(),
			Kind:          toDomainOperationKind(req.GetKind()),
			Button:        mouseButtonToString(mouse.GetButton()),
			X:             mouse.GetX(),
			Y:             mouse.GetY(),
			FromX:         mouse.GetFromX(),
			FromY:         mouse.GetFromY(),
			ToX:           mouse.GetToX(),
			ToY:           mouse.GetToY(),
			DurationMs:    mouse.GetDurationMs(),
			FlashSnapshot: req.GetFlashSnapshot(),
		}
	case *GameWebSocketEnvelope_ControlAck:
		return domain.ControlAckPayload{
			RequestID: p.ControlAck.GetOperationId(),
		}
	case *GameWebSocketEnvelope_ControlResult:
		result := p.ControlResult
		success := result.GetStatus() == GameControlResultStatus_GAME_CONTROL_RESULT_STATUS_SUCCEEDED
		return domain.ControlResultPayload{
			RequestID: result.GetOperationId(),
			Success:   success,
			Error:     result.GetErrorMessage(),
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
				MimeType: p.MimeType,
				Segment:  p.Segment,
			},
		}
	case domain.MediaSegmentPayload:
		return &GameWebSocketEnvelope_MediaSegment{
			MediaSegment: &GameMediaSegment{
				SegmentId: p.SegmentID,
				Segment:   p.Segment,
				KeyFrame:  p.KeyFrame,
			},
		}
	case domain.ControlRequestPayload:
		return &GameWebSocketEnvelope_ControlRequest{
			ControlRequest: &GameControlRequest{
				OperationId:   p.RequestID,
				Kind:          toProtoOperationKindWS(p.Kind),
				FlashSnapshot: p.FlashSnapshot,
				Mouse: &GameMouseAction{
					Button:     stringToMouseButton(p.Button),
					X:          p.X,
					Y:          p.Y,
					FromX:      p.FromX,
					FromY:      p.FromY,
					ToX:        p.ToX,
					ToY:        p.ToY,
					DurationMs: p.DurationMs,
				},
			},
		}
	case domain.ControlAckPayload:
		return &GameWebSocketEnvelope_ControlAck{
			ControlAck: &GameControlAck{
				OperationId: p.RequestID,
			},
		}
	case domain.ControlResultPayload:
		status := GameControlResultStatus_GAME_CONTROL_RESULT_STATUS_FAILED
		if p.Success {
			status = GameControlResultStatus_GAME_CONTROL_RESULT_STATUS_SUCCEEDED
		} else if p.TimedOut {
			status = GameControlResultStatus_GAME_CONTROL_RESULT_STATUS_TIMED_OUT
		}
		return &GameWebSocketEnvelope_ControlResult{
			ControlResult: &GameControlResult{
				OperationId:  p.RequestID,
				Status:       status,
				ErrorMessage: p.Error,
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
