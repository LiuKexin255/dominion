// Package main is the game gateway binary that serves as the HTTP entry point
// for the game services. It combines grpc-gateway for unary HTTP/JSON requests
// and a WebSocket handler for bidirectional streaming RPCs.
//
// Routes:
//   - /api/v1/* → grpc-gateway (SessionService + TeamService unary RPCs:
//     UpdateTeam/GetTeam/ListMessages/RefreshTeam per
//     projects/game/game.proto HTTP annotations, AIP-127)
//   - /api/v1/templates/{template}/sessions/{session}/connect → WebSocket
//     (TeamService.Connect stream; the WebSocket endpoint mirrors the Team
//     resource hierarchy per spec 031-team-template-mode FR-004)
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
	"google.golang.org/protobuf/proto"
)

var (
	port = flag.String("port", "80", "Port to listen on")
)

func main() {
	flag.Parse()

	// 1. Create gRPC connections to backend services.
	// MaxRecvMsgSize/MaxSendMsgSize are bumped to 8MB so screenshots and
	// multimodal frames fit comfortably within the gRPC hop.
	clientOpts := append(
		pgrpc.ClientDefault(),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(8*1024*1024),
			grpc.MaxCallSendMsgSize(8*1024*1024),
		),
	)

	sessionConn, err := grpc.NewClient(solver.URI(gameconst.SessionTarget), clientOpts...)
	if err != nil {
		log.Fatalf("session dial: %v", err)
	}

	// teamConn hosts the TeamService — implemented by the proxy service,
	// which replaced the former ProxyService (clean break, spec
	// 031-team-template-mode). TeamTarget resolves to "game/proxy:grpc".
	// The TeamService.Connect bidi stream is long-lived, so this conn opts
	// into keepalive pings (paired with the proxy's
	// WithLongLivedServerKeepalive); session/prompt stay unary → default.
	teamClientOpts := append(
		clientOpts,
		pgrpc.WithLongLivedClientKeepalive(),
	)
	teamConn, err := grpc.NewClient(solver.URI(gameconst.TeamTarget), teamClientOpts...)
	if err != nil {
		log.Fatalf("team dial: %v", err)
	}

	promptConn, err := grpc.NewClient(solver.URI(gameconst.PromptTarget), clientOpts...)
	if err != nil {
		log.Fatalf("prompt dial: %v", err)
	}

	// memoryConn hosts the MemoryService — the planner's long-term memory
	// (spec 039-planner-memory-calibration FR-006). Registered on the gateway
	// so the /api/v1/templates/{template}/sessions/{session}/memories surface
	// is reachable through the public HTTP entry (the 039 large tests verify
	// memory persistence/pagination through it — spec quickstart.md 场景 2).
	memoryConn, err := grpc.NewClient(solver.URI(gameconst.MemoryTarget), clientOpts...)
	if err != nil {
		log.Fatalf("memory dial: %v", err)
	}

	// 2. Create grpc-gateway mux and register handlers for unary RPCs.
	gwmux := runtime.NewServeMux(pgrpc.GatewayDefault()...)

	ctx := context.Background()
	if err := game.RegisterSessionServiceHandler(ctx, gwmux, sessionConn); err != nil {
		log.Fatalf("register session handler: %v", err)
	}
	if err := game.RegisterTeamServiceHandler(ctx, gwmux, teamConn); err != nil {
		log.Fatalf("register team handler: %v", err)
	}
	if err := game.RegisterPromptServiceHandler(ctx, gwmux, promptConn); err != nil {
		log.Fatalf("register prompt handler: %v", err)
	}
	if err := game.RegisterMemoryServiceHandler(ctx, gwmux, memoryConn); err != nil {
		log.Fatalf("register memory handler: %v", err)
	}

	// 3. Create root HTTP mux with path-based routing.
	// All /api/v1/ requests flow through a single handler that dispatches
	// WebSocket upgrades before falling through to grpc-gateway.
	// A single subtree pattern avoids Go's ServeMux 307 redirect when
	// both "/api/v1/" and "/api/v1/sessions/" are registered separately.
	rootMux := http.NewServeMux()

	rootMux.HandleFunc("/api/v1/", func(w http.ResponseWriter, r *http.Request) {
		if isWebSocketConnectPath(r.URL.Path) {
			handleWebSocketConnect(w, r, teamConn)
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
	b.Register(bootstrap.GRPCConn("team", teamConn))
	b.Register(bootstrap.GRPCConn("prompt", promptConn))
	b.Register(bootstrap.GRPCConn("memory", memoryConn))
	b.Register(bootstrap.HTTPServer("http", srv))
	log.Fatal(b.Run(context.Background()))
}

// isWebSocketConnectPath reports whether the request path matches the
// WebSocket connect pattern: /api/v1/templates/{template}/sessions/{session}/connect
// (spec 031-team-template-mode FR-004).
func isWebSocketConnectPath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	return len(parts) == 7 &&
		parts[0] == "api" && parts[1] == "v1" && parts[2] == "templates" &&
		parts[3] != "" && parts[4] == "sessions" &&
		parts[5] != "" && parts[6] == "connect"
}

// extractConnectIdentity extracts the template and session segments from a
// path matching /api/v1/templates/{template}/sessions/{session}/connect. It
// only accepts the full connect path shape (delegating to
// isWebSocketConnectPath) so a foreign path such as
// .../sessions/{id}/team never yields a template/session id.
func extractConnectIdentity(path string) (template, session string) {
	if !isWebSocketConnectPath(path) {
		return "", ""
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	return parts[3], parts[5]
}

// wsStream adapts a WebSocket connection to bind.UserFrameStream.
// It handles binary-protobuf serialization/deserialization and injects the
// templateID/sessionID from the URL path into every received UserFrame,
// overwriting any client-supplied value. The proxy reconstructs the session
// resource name from this pair without parsing (spec
// 031-team-template-mode contracts/api-contract.md §2.2).
//
// ctx holds the HTTP request context so that Read/Write respect request
// cancellation (e.g. client disconnect).
type wsStream struct {
	ctx        context.Context
	conn       *websocket.Conn
	templateID string
	sessionID  string
}

// Recv reads a binary frame from the WebSocket, unmarshals it as a UserFrame
// (protobuf wire format), and injects the templateID/sessionID from the URL
// path. proto.Unmarshal preserves unknown fields per the proto spec,
// maintaining the forward-compatibility that protojson's DiscardUnknown
// previously provided
// (specs/025-desktop-image-state-refine/contracts/image-transport-contract.md §2).
func (s *wsStream) Recv() (*game.UserFrame, error) {
	_, data, err := s.conn.Read(s.ctx)
	if err != nil {
		return nil, err
	}
	var frame game.UserFrame
	if err := proto.Unmarshal(data, &frame); err != nil {
		return nil, errors.Join(errProtocol, err)
	}
	// CRITICAL: inject templateID/sessionID from URL path — always wins over
	// client-supplied values.
	frame.TemplateId = s.templateID
	frame.SessionId = s.sessionID
	return &frame, nil
}

// Send marshals the TeamFrame as binary protobuf and writes it as a binary
// frame to the WebSocket connection.
func (s *wsStream) Send(frame *game.TeamFrame) error {
	data, err := proto.Marshal(frame)
	if err != nil {
		return err
	}
	return s.conn.Write(s.ctx, websocket.MessageBinary, data)
}

// errProtocol is a sentinel error for protocol-level errors (e.g. invalid
// frame protobuf) that should result in a WebSocket
// InvalidFramePayloadData close code.
var errProtocol = errors.New("protocol error")

// isProtocolError reports whether err is a protocol-level error (invalid
// frame protobuf) from the WebSocket adapter.
func isProtocolError(err error) bool {
	return errors.Is(err, errProtocol)
}

// handleWebSocketConnect upgrades an HTTP connection to WebSocket and
// establishes a bidirectional forwarding bridge between the WebSocket
// and the underlying TeamService.Connect gRPC stream.
//
// Messages are serialized as binary protobuf over WebSocket binary frames in
// both directions: UserFrame inbound (desktop → server), TeamFrame outbound
// (server → desktop). proto.Unmarshal preserves unknown fields for forward
// compatibility.
func handleWebSocketConnect(w http.ResponseWriter, r *http.Request, teamConn *grpc.ClientConn) {
	templateID, sessionID := extractConnectIdentity(r.URL.Path)
	if templateID == "" || sessionID == "" {
		http.Error(w, "missing template_id or session_id", http.StatusBadRequest)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"},
	})
	if err != nil {
		logs.Error(r.Context(), "ws accept failed",
			event.String("template_id", templateID),
			event.String("session_id", sessionID),
			event.Err(err),
		)
		return
	}
	defer conn.CloseNow()
	logs.Info(r.Context(), "ws connected",
		event.String("template_id", templateID),
		event.String("session_id", sessionID),
	)

	// Allow up to 10MB per frame to support PNG screenshot uploads.
	conn.SetReadLimit(10 << 20)

	teamClient := game.NewTeamServiceClient(teamConn)
	stream, err := teamClient.Connect(r.Context())
	if err != nil {
		logs.Error(r.Context(), "team Connect: stream creation failed",
			event.String("template_id", templateID),
			event.String("session_id", sessionID),
			event.Err(err),
		)
		return
	}

	ws := &wsStream{ctx: r.Context(), conn: conn, templateID: templateID, sessionID: sessionID}
	b := bind.NewBinder()
	err = b.Bind(ws, stream)

	if err == nil {
		logs.Info(r.Context(), "agent connect stream closed",
			event.String("template_id", templateID),
			event.String("session_id", sessionID),
		)
		conn.Close(websocket.StatusNormalClosure, "")
		return
	}
	if isCleanClose(err) {
		logs.Info(r.Context(), "agent connect stream closed (clean)",
			event.String("template_id", templateID),
			event.String("session_id", sessionID),
		)
		conn.Close(websocket.StatusNormalClosure, "")
		return
	}
	if isProtocolError(err) {
		logs.Warn(r.Context(), "agent connect: protocol error",
			event.String("template_id", templateID),
			event.String("session_id", sessionID),
			event.Err(err),
		)
		conn.Close(websocket.StatusInvalidFramePayloadData, "invalid frame protobuf")
		return
	}
	logs.Error(r.Context(), "agent connect: internal error",
		event.String("template_id", templateID),
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
