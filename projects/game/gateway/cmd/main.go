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
	"flag"
	"log"
	"net/http"
	"strings"

	"dominion/common/gopkg/bootstrap"
	pgrpc "dominion/common/gopkg/grpc"
	"dominion/common/gopkg/grpc/solver"
	phttp "dominion/common/gopkg/http"
	"dominion/common/gopkg/otel"
	game "dominion/projects/game"

	"github.com/gorilla/websocket"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"
)

var port = flag.String("port", "80", "Port to listen on")

func main() {
	flag.Parse()

	// 1. Create gRPC connections to backend services.
	sessionConn, err := grpc.NewClient(solver.URI("game/session:grpc"), pgrpc.ClientDefault()...)
	if err != nil {
		log.Fatalf("session dial: %v", err)
	}

	proxyConn, err := grpc.NewClient(solver.URI("game/proxy:grpc"), pgrpc.ClientDefault()...)
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

	// 3. Create WebSocket upgrader.
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	// 4. Create root HTTP mux with path-based routing.
	// Longer /api/v1/sessions/ pattern takes priority over /api/v1/ prefix.
	rootMux := http.NewServeMux()

	// grpc-gateway handles all /api/v1/ unary HTTP/JSON requests.
	rootMux.Handle("/api/v1/", gwmux)

	// Sessions handler intercepts /api/v1/sessions/* to dispatch between
	// grpc-gateway (unary agent CRUD) and WebSocket (ConnectAgent stream).
	rootMux.HandleFunc("/api/v1/sessions/", func(w http.ResponseWriter, r *http.Request) {
		if isWebSocketConnectPath(r.URL.Path) {
			handleWebSocketConnect(w, r, upgrader, proxyConn)
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

// handleWebSocketConnect upgrades an HTTP connection to WebSocket and
// establishes a bidirectional forwarding bridge between the WebSocket
// and the underlying ProxyService.ConnectAgent gRPC stream.
//
// Messages are serialized as AgentFrame JSON (protojson) over WebSocket
// text frames on both directions.
func handleWebSocketConnect(w http.ResponseWriter, r *http.Request, upgrader websocket.Upgrader, proxyConn *grpc.ClientConn) {
	sessionID := extractSessionID(r.URL.Path)
	if sessionID == "" {
		http.Error(w, "invalid session_id in path", http.StatusBadRequest)
		return
	}

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade: %v", err)
		return
	}
	defer ws.Close()

	proxyClient := game.NewProxyServiceClient(proxyConn)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	stream, err := proxyClient.ConnectAgent(ctx)
	if err != nil {
		log.Printf("proxy ConnectAgent: %v", err)
		return
	}

	// goroutine: WebSocket read → gRPC stream send
	go func() {
		defer cancel()
		for {
			_, msg, err := ws.ReadMessage()
			if err != nil {
				return
			}
			frame := new(game.AgentFrame)
			if err := protojson.Unmarshal(msg, frame); err != nil {
				// Treat unrecognized JSON as raw payload echo.
				frame = &game.AgentFrame{
					SessionId: sessionID,
					Type:      "echo",
					Payload:   msg,
				}
			}
			frame.SessionId = sessionID
			if err := stream.Send(frame); err != nil {
				return
			}
		}
	}()

	// main goroutine: gRPC stream recv → WebSocket write
	for {
		frame, err := stream.Recv()
		if err != nil {
			break
		}
		msg, err := protojson.Marshal(frame)
		if err != nil {
			break
		}
		if err := ws.WriteMessage(websocket.TextMessage, msg); err != nil {
			break
		}
	}
}
