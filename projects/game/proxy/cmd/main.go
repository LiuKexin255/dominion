// Package main is the bootstrap entrypoint for the game proxy service.
package main

import (
	"context"
	"flag"
	"log"
	"net"

	"dominion/common/gopkg/bootstrap"
	pgrpc "dominion/common/gopkg/grpc"
	"dominion/common/gopkg/mongo"
	"dominion/common/gopkg/otel"
	"dominion/common/gopkg/solver"
	game "dominion/projects/game"
	"dominion/projects/game/pkg/bind"
	gameconst "dominion/projects/game/pkg/gameconst"
	"dominion/projects/game/proxy/handler"
	"dominion/projects/game/proxy/runtime/agentclient"
	proxymongo "dominion/projects/game/proxy/runtime/mongo"
	"dominion/projects/game/proxy/runtime/picker"

	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var port = flag.String("port", "50051", "Port to listen on")

// registerServices wires the proxy's gRPC surface onto one server: the
// agent_v2 AgentService face (the /api/v2 session-scoped agent face) and the
// DesktopBridgeService face (the desktop flow stream), both dispatched to the
// agent_v2 stateful pool through the owner store. The agent_v2 stateless
// configuration face (PresetService) is not registered here — preset state
// lives in Mongo and the model catalog is static, so the gateway dials
// agent_v2 directly for it
// (specs/051-agent-v2-dsh-migration/revisions/directive-2026-09-01.md §3.4).
// Registration only — callers keep ownership of the handlers' lifecycle.
func registerServices(srv *grpcgo.Server, agentHandler game.AgentServiceServer, bridgeHandler game.DesktopBridgeServiceServer) {
	game.RegisterAgentServiceServer(srv, agentHandler)
	game.RegisterDesktopBridgeServiceServer(srv, bridgeHandler)
	reflection.Register(srv)
}

func main() {
	flag.Parse()

	listener, err := net.Listen("tcp", ":"+*port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	// MongoDB-backed owner store for the agent_v2 stateful instance pool.
	mongoClient, err := mongo.NewClient("game/mongo")
	if err != nil {
		log.Fatalf("failed to create mongo client: %v", err)
	}
	agentV2OwnerStore := proxymongo.NewAgentV2OwnerStore(mongoClient)

	// StatefulResolver discovers agent service instances.
	statefulResolver, err := solver.NewDeployStatefulResolver()
	if err != nil {
		log.Fatalf("failed to create stateful resolver: %v", err)
	}

	// Hash-based owner picker.
	hashPicker := picker.NewHashPicker()

	// Agent client manager with periodic refresh via Daemon: the agent_v2
	// conversation pool.
	agentV2Target := solver.MustParseTarget(gameconst.AgentV2Target)
	agentV2Manager := agentclient.NewManager(statefulResolver, agentV2Target, agentclient.DefaultRefreshInterval)

	// Bidirectional stream binder (DesktopBridgeService.Connect) and the
	// generic server-streaming pump (AgentService.Send relay).
	binder := bind.NewBinder()

	// Agent handler forwards the /api/v2 session-scoped agent RPCs
	// (UpdateAgent/GetAgent/ListAgentMessages/Send) to the agent_v2 instance
	// owning the (template, session) pair — owner affinity keeps the
	// in-memory sessions from drifting across instances. The stateless
	// configuration face (PresetService) bypasses the proxy entirely: the
	// gateway dials agent_v2 directly for it
	// (specs/051-agent-v2-dsh-migration/revisions/directive-2026-09-01.md §3.4).
	agentHandler := handler.NewAgentHandler(
		agentV2OwnerStore,
		hashPicker,
		agentV2Manager,
		bind.NewServerStreamBinder[game.ChatEvent](),
	)

	// Bridge handler relays the desktop flow-control WebSocket stream
	// (gateway /api/v2 connect) to the agent_v2 instance owning the session
	// — the owner is allocated get-or-create so the flow stream and the
	// conversation share one instance
	// (specs/051-agent-v2-dsh-migration/contracts/desktop-bridge.md §4).
	bridgeHandler := handler.NewDesktopBridgeHandler(
		agentV2OwnerStore,
		hashPicker,
		agentV2Manager,
		binder,
	)

	// gRPC server with default service options (OTel tracing, TLS).
	// The gateway's DesktopBridgeService.Connect client pings every 30s
	// (WithLongLivedClientKeepalive); without a relaxed enforcement policy
	// the grpc-go server default MinTime (5min) would GOAWAY the long-lived
	// bidi stream with "too_many_pings" during idle gaps.
	serverOpts := append(
		pgrpc.ServiceDefault(),
		grpcgo.MaxRecvMsgSize(8*1024*1024),
		grpcgo.MaxSendMsgSize(8*1024*1024),
		pgrpc.WithLongLivedServerKeepalive(),
	)
	grpcServer := grpcgo.NewServer(serverOpts...)
	registerServices(grpcServer, agentHandler, bridgeHandler)

	// Bootstrap lifecycle: OTEL → Mongo client → Agent client manager → gRPC server.
	b := bootstrap.New()
	b.Register(otel.Component())
	b.Register(bootstrap.MongoClient("mongo", mongoClient))
	b.Register(agentclient.NewDaemon("agentclient-manager-v2", agentV2Manager, agentclient.DefaultRefreshInterval))
	b.Register(bootstrap.GRPCServer("grpc", grpcServer, listener))
	log.Fatal(b.Run(context.Background()))
}
