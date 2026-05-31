// Package main is the game gateway binary that serves as the HTTP entry point
// for the game services. It combines grpc-gateway for unary HTTP/JSON requests
// and a WebSocket handler for bidirectional streaming RPCs.
//
// Routes:
//   - /api/v1/* → grpc-gateway (SessionService + ProxyService unary RPCs)
//   - /api/v1/sessions/{session_id}/agent/connect → WebSocket (ProxyService.ConnectAgent stream)
package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log"
	"net/http"
	"strings"

	"dominion/common/gopkg/bootstrap"
	pgrpc "dominion/common/gopkg/grpc"
	"dominion/common/gopkg/grpc/solver"
	phttp "dominion/common/gopkg/http"
	"dominion/common/gopkg/logs"
	"dominion/common/gopkg/logs/event"
	"dominion/common/gopkg/otel"
	game "dominion/projects/game"
	"dominion/projects/game/pkg/bind"
	gameconst "dominion/projects/game/pkg/gameconst"

	"github.com/coder/websocket"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"
)

var (
	port = flag.String("port", "80", "Port to listen on")
)

func main() {
	flag.Parse()

	// 1. Create gRPC connections to backend services.
	sessionConn, err := grpc.NewClient(solver.URI(gameconst.SessionTarget), pgrpc.ClientDefault()...)
	if err != nil {
		log.Fatalf("session dial: %v", err)
	}

	proxyConn, err := grpc.NewClient(solver.URI(gameconst.ProxyTarget), pgrpc.ClientDefault()...)
	if err != nil {
		log.Fatalf("proxy dial: %v", err)
	}

	// 2. Create grpc-gateway mux and register handlers for unary RPCs.
	gwmux := runtime.NewServeMux(pgrpc.GatewayDefault()...)

	ctx := context.Background()
	if err := game.RegisterSessionServiceHandler(ctx, gwmux, sessionConn); err != nil {
		log.Fatalf("register session handler: %v", err)
	}
	if err := game.RegisterProxyServiceHandler(ctx, gwmux, proxyConn); err != nil {
		log.Fatalf("register proxy handler: %v", err)
	}

	// 3. Create root HTTP mux with path-based routing.
	// All /api/v1/ requests flow through a single handler that dispatches
	// WebSocket upgrades before falling through to grpc-gateway.
	// A single subtree pattern avoids Go's ServeMux 307 redirect when
	// both "/api/v1/" and "/api/v1/sessions/" are registered separately.
	rootMux := http.NewServeMux()

	rootMux.HandleFunc("/api/v1/", func(w http.ResponseWriter, r *http.Request) {
		if isWebSocketConnectPath(r.URL.Path) {
			handleWebSocketConnect(w, r, proxyConn)
			return
		}
		gwmux.ServeHTTP(w, r)
	})

	// 5. Create HTTP server.
	srv := &http.Server{
		Addr:    ":" + *port,
		Handler: phttp.Handler(rootMux, "game-gateway"),
	}

	log.Printf("game gateway listening :%s", *port)

	// 6. Bootstrap with all components.
	b := bootstrap.New()
	b.Register(otel.Component())
	b.Register(bootstrap.GRPCConn("session", sessionConn))
	b.Register(bootstrap.GRPCConn("proxy", proxyConn))
	b.Register(bootstrap.HTTPServer("http", srv))
	log.Fatal(b.Run(context.Background()))
}

// isWebSocketConnectPath reports whether the request path matches the
// WebSocket agent connect pattern: /api/v1/sessions/{session_id}/agent/connect
func isWebSocketConnectPath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	return len(parts) >= 6 &&
		parts[0] == "api" && parts[1] == "v1" && parts[2] == "sessions" &&
		parts[3] != "" && parts[4] == "agent" && parts[5] == "connect"
}

// extractSessionID extracts the session_id segment from a path matching
// /api/v1/sessions/{session_id}/agent/connect
func extractSessionID(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 4 && parts[2] == "sessions" {
		return parts[3]
	}
	return ""
}

// wsStream adapts a WebSocket connection to bind.AgentFrameStream.
// It handles protojson serialization/deserialization and injects the
// sessionID from the URL path into every received frame, overwriting
// any client-supplied value.
//
// ctx holds the HTTP request context so that Read/Write respect request
// cancellation (e.g. client disconnect).
type wsStream struct {
	ctx       context.Context
	conn      *websocket.Conn
	sessionID string
}

// Recv reads a text frame from the WebSocket, unmarshals it as an
// AgentFrame (protojson, DiscardUnknown), and injects the sessionID
// from the URL path.
func (s *wsStream) Recv() (*game.AgentFrame, error) {
	_, data, err := s.conn.Read(s.ctx)
	if err != nil {
		return nil, err
	}
	var frame game.AgentFrame
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(data, &frame); err != nil {
		return nil, errors.Join(errProtocol, err)
	}
	// CRITICAL: inject sessionID from URL path — always wins over client-supplied value
	frame.SessionId = s.sessionID
	return &frame, nil
}

// Send marshals the AgentFrame as JSON and writes it as a text frame to
// the WebSocket connection.
func (s *wsStream) Send(frame *game.AgentFrame) error {
	data, err := protojson.Marshal(frame)
	if err != nil {
		return err
	}
	return s.conn.Write(s.ctx, websocket.MessageText, data)
}

// errProtocol is a sentinel error for protocol-level errors (e.g. invalid
// AgentFrame JSON) that should result in a WebSocket InvalidFramePayloadData
// close code.
var errProtocol = errors.New("protocol error")

// isProtocolError reports whether err is a protocol-level error (invalid
// AgentFrame JSON) from the WebSocket adapter.
func isProtocolError(err error) bool {
	return errors.Is(err, errProtocol)
}

// handleWebSocketConnect upgrades an HTTP connection to WebSocket and
// establishes a bidirectional forwarding bridge between the WebSocket
// and the underlying ProxyService.ConnectAgent gRPC stream.
//
// Messages are serialized as AgentFrame JSON (protojson) over WebSocket
// text frames on both directions. Unknown JSON fields are silently
// discarded during deserialization (DiscardUnknown).
func handleWebSocketConnect(w http.ResponseWriter, r *http.Request, proxyConn *grpc.ClientConn) {
	sessionID := extractSessionID(r.URL.Path)
	if sessionID == "" {
		http.Error(w, "missing session_id", http.StatusBadRequest)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"},
	})
	if err != nil {
		logs.Error(r.Context(), "ws accept failed",
			event.String("session_id", sessionID),
			event.Err(err),
		)
		return
	}
	defer conn.CloseNow()
	logs.Info(r.Context(), "ws connected",
		event.String("session_id", sessionID),
	)

	// Allow up to 10MB per frame to support PNG screenshot uploads.
	conn.SetReadLimit(10 << 20)

	proxyClient := game.NewProxyServiceClient(proxyConn)
	stream, err := proxyClient.ConnectAgent(r.Context())
	if err != nil {
		logs.Error(r.Context(), "proxy ConnectAgent: stream creation failed",
			event.String("session_id", sessionID),
			event.Err(err),
		)
		return
	}

	ws := &wsStream{ctx: r.Context(), conn: conn, sessionID: sessionID}
	b := bind.NewBinder()
	err = b.Bind(ws, stream)

	if err == nil {
		logs.Info(r.Context(), "agent connect stream closed",
			event.String("session_id", sessionID),
		)
		conn.Close(websocket.StatusNormalClosure, "")
		return
	}
	if isCleanClose(err) {
		logs.Info(r.Context(), "agent connect stream closed (clean)",
			event.String("session_id", sessionID),
		)
		conn.Close(websocket.StatusNormalClosure, "")
		return
	}
	if isProtocolError(err) {
		logs.Warn(r.Context(), "agent connect: protocol error",
			event.String("session_id", sessionID),
			event.Err(err),
		)
		conn.Close(websocket.StatusInvalidFramePayloadData, "invalid AgentFrame JSON")
		return
	}
	logs.Error(r.Context(), "agent connect: internal error",
		event.String("session_id", sessionID),
		event.Err(err),
	)
	conn.Close(websocket.StatusInternalError, "internal error")
}

// isCleanClose reports whether the error represents a normal WebSocket or
// context closure that should not be logged as an error.
func isCleanClose(err error) bool {
	if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
		return true
	}
	// coder/websocket: CloseStatus returns -1 for non-close errors.
	status := websocket.CloseStatus(err)
	return status != -1
}
