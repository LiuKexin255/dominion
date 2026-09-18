// Package main is the game gateway binary that serves as the HTTP entry point
// for the game services. It combines grpc-gateway for unary HTTP/JSON requests
// and a WebSocket handler for bidirectional streaming RPCs.
//
// Routes:
//   - /api/v1/* → grpc-gateway (SessionService + MemoryService unary RPCs per
//     projects/game/game.proto HTTP annotations, AIP-127)
//   - /api/v2/* → grpc-gateway, split by where the RPC's state lives
//     (specs/059-agent-v2-team-mode/contracts/team-api.md §1): the
//     session-scoped AgentService team face (UpdateTeam/GetTeam/
//     GetTeamMember/ListTeamMessages/ListMemberMessages/Send/Cancel,
//     including the Send team server-streaming RPC served as chunked NDJSON)
//     rides the proxy connection — the proxy owns owner affinity for the
//     stateful agent_v2 instances
//     (specs/051-agent-v2-dsh-migration/research.md D9); the stateless
//     PresetService face (preset CRUD + ListModels) is registered on a
//     direct agent_v2 connection — preset state lives in Mongo and the
//     model catalog is static, so no proxy hop
//     (specs/051-agent-v2-dsh-migration/revisions/directive-2026-09-01.md
//     §3.4) — except the desktop-bridge connect path:
//     /api/v2/templates/{template}/sessions/{session}/connect → WebSocket
//     (DesktopBridgeService.Connect stream relayed over the proxy
//     connection; specs/051-agent-v2-dsh-migration/contracts/
//     desktop-bridge.md §3).
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

	// teamConn hosts the stateful-routing services on the proxy: the proxy
	// owns owner affinity for the stateful agent_v2 instances, so both the
	// AgentService team face (UpdateTeam/GetTeam/GetTeamMember/
	// ListTeamMessages/ListMemberMessages/Send/Cancel) and the
	// DesktopBridgeService bidi stream route through it
	// (specs/051-agent-v2-dsh-migration/research.md D9). The
	// DesktopBridgeService.Connect bidi stream and the AgentService.Send
	// team stream are long-lived, so this conn opts into keepalive pings
	// (paired with the proxy's WithLongLivedServerKeepalive); session/memory
	// stay unary → default. The team* names describe the connection's
	// payload — the /api/v2 team session surface (the Team singleton and its
	// streams) — while the dial target itself is gameconst.ProxyTarget.
	teamClientOpts := append(
		clientOpts,
		pgrpc.WithLongLivedClientKeepalive(),
	)
	teamConn, err := grpc.NewClient(solver.URI(gameconst.ProxyTarget), teamClientOpts...)
	if err != nil {
		log.Fatalf("team dial: %v", err)
	}

	// memoryConn hosts the MemoryService — the planner's long-term memory
	// (spec 039-planner-memory-calibration FR-006). Registered on the gateway
	// so the /api/v1/templates/{template}/sessions/{session}/memories surface
	// is reachable through the public HTTP entry.
	memoryConn, err := grpc.NewClient(solver.URI(gameconst.MemoryTarget), clientOpts...)
	if err != nil {
		log.Fatalf("memory dial: %v", err)
	}

	// presetConn dials agent_v2 directly for the stateless configuration
	// surface (PresetService): preset state lives in Mongo and the model
	// catalog is static plugin configuration, so any live instance serves —
	// the resolver returns every ready endpoint and the gRPC client LB
	// spreads the load, with no proxy owner affinity
	// (specs/051-agent-v2-dsh-migration/revisions/directive-2026-09-01.md
	// §3.4). Unary RPCs only → default keepalive, same as
	// sessionConn/memoryConn.
	presetConn, err := grpc.NewClient(solver.URI(gameconst.AgentV2Target), clientOpts...)
	if err != nil {
		log.Fatalf("preset dial: %v", err)
	}

	// 2. Create grpc-gateway mux and register handlers for unary RPCs.
	gwmux := runtime.NewServeMux(pgrpc.GatewayDefault()...)

	ctx := context.Background()
	if err := game.RegisterSessionServiceHandler(ctx, gwmux, sessionConn); err != nil {
		log.Fatalf("register session handler: %v", err)
	}
	if err := game.RegisterMemoryServiceHandler(ctx, gwmux, memoryConn); err != nil {
		log.Fatalf("register memory handler: %v", err)
	}
	// The AgentService handler rides the proxy connection: the proxy
	// forwards the session-scoped team RPCs to the agent_v2 stateful
	// instance owning the session (owner affinity — agent_v2 keeps the team,
	// queue, and game state in process memory,
	// specs/059-agent-v2-team-mode/contracts/team-api.md §1).
	if err := game.RegisterAgentServiceHandler(ctx, gwmux, teamConn); err != nil {
		log.Fatalf("register agent_v2 team handler: %v", err)
	}
	// The PresetService handler rides the direct agent_v2 connection: preset
	// state lives in Mongo and the model catalog is static, so no proxy hop
	// (specs/051-agent-v2-dsh-migration/revisions/directive-2026-09-01.md
	// §3.4). Its /api/v2 paths (preset CRUD + /api/v2/models) are disjoint
	// from the AgentService handler's team paths.
	if err := game.RegisterPresetServiceHandler(ctx, gwmux, presetConn); err != nil {
		log.Fatalf("register preset handler: %v", err)
	}

	// 3. Create root HTTP mux with path-based routing.
	// The /api/v1/ subtree falls through to grpc-gateway (session + memory
	// faces); the /api/v2/ subtree dispatches WebSocket upgrades before
	// falling through to grpc-gateway. Single subtree patterns avoid Go's
	// ServeMux 307 redirect when both "/api/v2/" and "/api/v2/templates/"
	// would otherwise be registered separately.
	rootMux := newRootMux(gwmux, teamConn)

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
	b.Register(bootstrap.GRPCConn("memory", memoryConn))
	b.Register(bootstrap.GRPCConn("agent-v2", presetConn))
	b.Register(bootstrap.HTTPServer("http", srv))
	log.Fatal(b.Run(context.Background()))
}

// newRootMux builds the path-based routing mux. The /api/v2/ subtree
// dispatches the desktop-bridge WebSocket upgrade before falling through to
// grpc-gateway: /api/v2/templates/{template}/sessions/{session}/connect
// (specs/051-agent-v2-dsh-migration/contracts/desktop-bridge.md §3). The
// /api/v1/ subtree serves grpc-gateway only (session + memory faces). Single
// subtree patterns avoid Go's ServeMux 307 redirect when a subtree prefix
// and a longer path would otherwise be registered separately.
func newRootMux(gwmux *runtime.ServeMux, teamConn *grpc.ClientConn) *http.ServeMux {
	rootMux := http.NewServeMux()

	rootMux.Handle("/api/v1/", gwmux)

	rootMux.HandleFunc("/api/v2/", func(w http.ResponseWriter, r *http.Request) {
		if isWebSocketConnectPathV2(r.URL.Path) {
			handleDesktopBridgeConnect(w, r, teamConn)
			return
		}
		gwmux.ServeHTTP(w, r)
	})
	return rootMux
}

// isWebSocketConnectPathV2 reports whether the request path matches the
// desktop-bridge WebSocket connect pattern:
// /api/v2/templates/{template}/sessions/{session}/connect
// (specs/051-agent-v2-dsh-migration/contracts/desktop-bridge.md §3).
func isWebSocketConnectPathV2(path string) bool {
	return isWebSocketConnectPathIn(apiV2, path)
}

// isWebSocketConnectPathIn reports whether the request path matches the
// WebSocket connect pattern under the given API version.
func isWebSocketConnectPathIn(version, path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	return len(parts) == 7 &&
		parts[0] == "api" && parts[1] == version && parts[2] == "templates" &&
		parts[3] != "" && parts[4] == "sessions" &&
		parts[5] != "" && parts[6] == "connect"
}

// extractConnectIdentityV2 extracts the template and session segments from a
// v2 desktop-bridge connect path. It only accepts the full connect path shape
// (delegating to isWebSocketConnectPathV2) so a foreign path such as
// .../sessions/{id}/agent never yields a template/session id.
func extractConnectIdentityV2(path string) (template, session string) {
	return extractConnectIdentityIn(apiV2, path)
}

// extractConnectIdentityIn extracts the template and session segments from a
// connect path under the given API version.
func extractConnectIdentityIn(version, path string) (template, session string) {
	if !isWebSocketConnectPathIn(version, path) {
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

// streamOpener opens the backend gRPC bidirectional stream for a WebSocket
// connect: the DesktopBridgeService.Connect client structurally satisfies
// bind.TeamFrameStream (Send UserFrame / Recv TeamFrame).
type streamOpener func(ctx context.Context) (bind.TeamFrameStream, error)

// API version path segment of the connect route.
const apiV2 = "v2"

// handleDesktopBridgeConnect upgrades an HTTP connection to WebSocket and
// establishes a bidirectional forwarding bridge between the WebSocket and
// the DesktopBridgeService.Connect gRPC stream on the proxy connection —
// the v2 desktop flow-control face (specs/051-agent-v2-dsh-migration/
// contracts/desktop-bridge.md §3).
func handleDesktopBridgeConnect(w http.ResponseWriter, r *http.Request, teamConn *grpc.ClientConn) {
	pumpWebSocketConnect(w, r, apiV2, func(ctx context.Context) (bind.TeamFrameStream, error) {
		return game.NewDesktopBridgeServiceClient(teamConn).Connect(ctx)
	})
}

// pumpWebSocketConnect is the WebSocket↔gRPC relay of the desktop-bridge
// connect face. Messages are serialized as binary protobuf over WebSocket
// binary frames in both directions: UserFrame inbound (desktop → server),
// TeamFrame outbound (server → desktop). proto.Unmarshal preserves unknown
// fields for forward compatibility. The template/session identity is
// extracted from the URL path and injected into every received frame.
func pumpWebSocketConnect(w http.ResponseWriter, r *http.Request, version string, openStream streamOpener) {
	templateID, sessionID := extractConnectIdentityIn(version, r.URL.Path)
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

	stream, err := openStream(r.Context())
	if err != nil {
		logs.Error(r.Context(), "connect: stream creation failed",
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
		logs.Info(r.Context(), "connect stream closed",
			event.String("template_id", templateID),
			event.String("session_id", sessionID),
		)
		conn.Close(websocket.StatusNormalClosure, "")
		return
	}
	if isCleanClose(err) {
		logs.Info(r.Context(), "connect stream closed (clean)",
			event.String("template_id", templateID),
			event.String("session_id", sessionID),
		)
		conn.Close(websocket.StatusNormalClosure, "")
		return
	}
	if isProtocolError(err) {
		logs.Warn(r.Context(), "connect: protocol error",
			event.String("template_id", templateID),
			event.String("session_id", sessionID),
			event.Err(err),
		)
		conn.Close(websocket.StatusInvalidFramePayloadData, "invalid frame protobuf")
		return
	}
	logs.Error(r.Context(), "connect: internal error",
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
