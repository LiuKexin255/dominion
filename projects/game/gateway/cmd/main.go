// Package main is the game gateway binary that serves as the HTTP entry point
// for the game services. It is a grpc-gateway server: every route is served
// from the generated HTTP annotations.
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
//     §3.4); the DesktopBridgeService session downlink/uplink
//     (`:stream` chunked NDJSON / `:sendFrame` octet-stream) also rides the
//     proxy connection
//     (specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md §1).
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
	gameconst "dominion/projects/game/pkg/gameconst"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
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
	// owns owner affinity for the stateful agent_v2 instances, so the
	// AgentService team face (UpdateTeam/GetTeam/GetTeamMember/
	// ListTeamMessages/ListMemberMessages/Send/Cancel) and the
	// DesktopBridgeService face (the `:stream` downlink and the `:sendFrame`
	// uplink) route through it
	// (specs/051-agent-v2-dsh-migration/research.md D9,
	// specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md §3).
	// The downlink/heartbeat stream is long-lived, so this conn opts into
	// keepalive pings (paired with the proxy's
	// WithLongLivedServerKeepalive); session/memory stay unary →
	// default. The team* names describe the connection's payload — the
	// /api/v2 team session surface (the Team singleton and its streams) —
	// while the dial target itself is gameconst.ProxyTarget.
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
	// The mux carries the octet-stream proto marshaler alongside the default
	// JSON one: SendFrame's uplink body is raw binary proto, while every JSON
	// route keeps the default marshaler
	// (specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md §3.1).
	gwmux := newGatewayMux()

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
	// The DesktopBridgeService handler rides the same proxy connection: the
	// session downlink (Stream) is server-streaming and grpc-gateway serves
	// it as chunked NDJSON, while SendFrame is a unary octet-stream POST.
	// teamConn's long-lived keepalive and 8MB hop limit are paired with the
	// proxy's settings
	// (specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md §3).
	if err := game.RegisterDesktopBridgeServiceHandler(ctx, gwmux, teamConn); err != nil {
		log.Fatalf("register desktop bridge handler: %v", err)
	}
	// The PresetService handler rides the direct agent_v2 connection: preset
	// state lives in Mongo and the model catalog is static, so no proxy hop
	// (specs/051-agent-v2-dsh-migration/revisions/directive-2026-09-01.md
	// §3.4). Its /api/v2 paths (preset CRUD + /api/v2/models) are disjoint
	// from the AgentService handler's team paths.
	if err := game.RegisterPresetServiceHandler(ctx, gwmux, presetConn); err != nil {
		log.Fatalf("register preset handler: %v", err)
	}

	// 3. Create HTTP server. grpc-gateway serves every route from the
	// generated HTTP annotations; the SendFrame uplink body guard wraps the
	// mux (it only acts on the :sendFrame POST path).
	srv := &http.Server{
		Addr:    ":" + *port,
		Handler: phttp.Handler(sendFrameBodyLimit(gwmux), "game-gateway"),
	}

	log.Printf("game gateway listening :%s", *port)

	// 4. Bootstrap with all components.
	b := bootstrap.New()
	b.Register(otel.Component())
	b.Register(bootstrap.GRPCConn("session", sessionConn))
	b.Register(bootstrap.GRPCConn("team", teamConn))
	b.Register(bootstrap.GRPCConn("memory", memoryConn))
	b.Register(bootstrap.GRPCConn("agent-v2", presetConn))
	b.Register(bootstrap.HTTPServer("http", srv))
	log.Fatal(b.Run(context.Background()))
}

// newGatewayMux builds the grpc-gateway mux with the octet-stream proto
// marshaler registered alongside the defaults. SendFrame's uplink body is raw
// binary proto — no base64 inflation — while every JSON route keeps the
// default marshaler
// (specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md §3.1/§3.2).
// MIME matching is exact, so the wildcard JSON marshaler still serves
// application/json.
func newGatewayMux() *runtime.ServeMux {
	return runtime.NewServeMux(append(
		pgrpc.GatewayDefault(),
		runtime.WithMarshalerOption("application/octet-stream", &runtime.ProtoMarshaller{}),
	)...)
}

// maxSendFrameBodyBytes is the hard cap on one SendFrame request body (8MiB),
// aligned with the gRPC hop's MaxCallRecvMsgSize/MaxCallSendMsgSize: a
// screenshot up to ~5MiB plus the request envelope fits, and an oversized
// upload is attributed at the HTTP edge instead of deep in the gRPC hop
// (specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md §3.3).
const maxSendFrameBodyBytes = 8 * 1024 * 1024

// sendFrameBodyLimit wraps the next handler with the SendFrame uplink body
// guard: a POST carrying the :sendFrame custom method is rejected with 413
// before reading when its declared Content-Length exceeds the cap, and
// otherwise has its body bounded by http.MaxBytesReader so a chunked or
// under-declared payload surfaces as the gateway decoder's clear "request
// body too large" error. Oversized payloads are never silently truncated and
// the session is unaffected
// (specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md §3.3;
// specs/025-desktop-image-state-refine/contracts/image-transport-contract.md §4).
func sendFrameBodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isSendFrameRequest(r) {
			next.ServeHTTP(w, r)
			return
		}
		if r.ContentLength > maxSendFrameBodyBytes {
			http.Error(w, "sendFrame request body exceeds the 8MiB limit", http.StatusRequestEntityTooLarge)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxSendFrameBodyBytes)
		next.ServeHTTP(w, r)
	})
}

// isSendFrameRequest reports whether the request is the SendFrame uplink POST
// (custom method suffix :sendFrame, AIP-136:
// https://google.aip.dev/136). Other POST routes (e.g. :send) and the
// :stream GET are left untouched.
func isSendFrameRequest(r *http.Request) bool {
	return r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, ":sendFrame")
}
