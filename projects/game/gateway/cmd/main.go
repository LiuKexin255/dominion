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
	// Longer /api/v1/sessions/ pattern takes priority over /api/v1/ prefix.
	rootMux := http.NewServeMux()

	// grpc-gateway handles all /api/v1/ unary HTTP/JSON requests.
	rootMux.Handle("/api/v1/", gwmux)

	// Sessions handler intercepts /api/v1/sessions/* to dispatch between
	// grpc-gateway (unary agent CRUD) and WebSocket (ConnectAgent stream).
	rootMux.HandleFunc("/api/v1/sessions/", func(w http.ResponseWriter, r *http.Request) {
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
// done carries the terminal Recv error so that the peer stream can
// unblock with the same error and allow the binder to shut down cleanly.
type wsStream struct {
	conn      *websocket.Conn
	sessionID string
	done      chan error
}

// Recv reads a text frame from the WebSocket, unmarshals it as an
// AgentFrame (protojson, DiscardUnknown), and injects the sessionID
// from the URL path. On error it sends the error through done to
// unblock the peer.
func (s *wsStream) Recv() (*game.AgentFrame, error) {
	_, data, err := s.conn.Read(context.Background())
	if err != nil {
		s.done <- err
		close(s.done)
		return nil, err
	}
	var frame game.AgentFrame
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(data, &frame); err != nil {
		s.done <- errors.Join(errProtocol, err)
		close(s.done)
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
	return s.conn.Write(context.Background(), websocket.MessageText, data)
}

// gRPCStream wraps a bind.AgentFrameStream and checks the wsStream's
// done channel concurrently with Recv, so that the binder can cleanly
// shut down when the wsStream side encounters a terminal error.
// The done channel carries the wsStream error, ensuring both binder
// goroutines return the same error and eliminating a reporting race.
type gRPCStream struct {
	inner bind.AgentFrameStream
	done  <-chan error
}

func (s *gRPCStream) Recv() (*game.AgentFrame, error) {
	select {
	case err := <-s.done:
		return nil, err
	default:
	}

	type recvResult struct {
		frame *game.AgentFrame
		err   error
	}
	ch := make(chan recvResult, 1)
	go func() {
		f, err := s.inner.Recv()
		ch <- recvResult{f, err}
	}()

	select {
	case err := <-s.done:
		return nil, err
	case r := <-ch:
		return r.frame, r.err
	}
}

func (s *gRPCStream) Send(frame *game.AgentFrame) error {
	return s.inner.Send(frame)
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
		log.Printf("ws accept: %v", err)
		return
	}
	defer conn.CloseNow()

	proxyClient := game.NewProxyServiceClient(proxyConn)
	stream, err := proxyClient.ConnectAgent(r.Context())
	if err != nil {
		log.Printf("proxy ConnectAgent: %v", err)
		return
	}

	ws := &wsStream{conn: conn, sessionID: sessionID, done: make(chan error, 1)}
	grs := &gRPCStream{inner: stream, done: ws.done}
	b := bind.NewBinder()
	err = b.Bind(r.Context(), ws, grs)

	if err == nil {
		conn.Close(websocket.StatusNormalClosure, "")
		return
	}
	if isCleanClose(err) {
		conn.Close(websocket.StatusNormalClosure, "")
		return
	}
	if isProtocolError(err) {
		conn.Close(websocket.StatusInvalidFramePayloadData, "invalid AgentFrame JSON")
		return
	}
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
