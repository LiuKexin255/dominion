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

// registerServices wires the proxy's gRPC surface onto one server: the v1
// TeamService forwarding face plus the agent_v2 AgentService (the /api/v2
// session-scoped agent face) and DesktopBridgeService (the desktop flow
// stream) faces, all dispatched to the agent_v2 stateful pool through the
// owner store. The agent_v2 stateless configuration face (PresetService) is
// not registered here — preset state lives in Mongo and the model catalog is
// static, so the gateway dials agent_v2 directly for it
// (specs/051-agent-v2-dsh-migration/revisions/directive-2026-09-01.md §3.4).
// Registration only — callers keep ownership of the handlers' lifecycle.
func registerServices(srv *grpcgo.Server, teamHandler game.TeamServiceServer, agentHandler game.AgentServiceServer, bridgeHandler game.DesktopBridgeServiceServer) {
	game.RegisterTeamServiceServer(srv, teamHandler)
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

	// MongoDB-backed owner stores. The v1 team-owner store and the agent_v2
	// owner store live in dedicated collections: the two
	// stateful instance pools are independent, and the same game session may
	// hold a v1 team owner and a v2 agent owner at once
	// (specs/051-agent-v2-dsh-migration/data-model.md §2.9).
	mongoClient, err := mongo.NewClient("game/mongo")
	if err != nil {
		log.Fatalf("failed to create mongo client: %v", err)
	}
	mongoOwnerStore := proxymongo.NewAgentOwnerStore(mongoClient)
	agentV2OwnerStore := proxymongo.NewAgentV2OwnerStore(mongoClient)

	// StatefulResolver discovers agent service instances.
	statefulResolver, err := solver.NewDeployStatefulResolver()
	if err != nil {
		log.Fatalf("failed to create stateful resolver: %v", err)
	}

	// Hash-based owner picker.
	hashPicker := picker.NewHashPicker()

	// Agent client managers with periodic refresh via Daemon: one per
	// stateful pool (the v1 agent pool and the agent_v2 conversation pool).
	agentTarget := solver.MustParseTarget(gameconst.AgentTarget)
	manager := agentclient.NewManager(statefulResolver, agentTarget, agentclient.DefaultRefreshInterval)
	agentV2Target := solver.MustParseTarget(gameconst.AgentV2Target)
	agentV2Manager := agentclient.NewManager(statefulResolver, agentV2Target, agentclient.DefaultRefreshInterval)

	// Bidirectional stream binder (v1 TeamService.Connect) and the generic
	// server-streaming pump (AgentService.Send relay).
	binder := bind.NewBinder()

	// Team handler implements the TeamService gRPC server interface directly:
	// owner resolution, agent-client routing, and stream binding live here.
	// (spec 031-team-template-mode: ProxyService/AgentService merged into TeamService.)
	grpcHandler := handler.NewTeamHandler(mongoOwnerStore, hashPicker, manager, binder)

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
	// The gateway's TeamService.Connect client pings every 30s
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
	registerServices(grpcServer, grpcHandler, agentHandler, bridgeHandler)

	// Bootstrap lifecycle: OTEL → Mongo client → Agent client managers → gRPC server.
	b := bootstrap.New()
	b.Register(otel.Component())
	b.Register(bootstrap.MongoClient("mongo", mongoClient))
	b.Register(agentclient.NewDaemon(agentclient.DefaultDaemonName, manager, agentclient.DefaultRefreshInterval))
	b.Register(agentclient.NewDaemon("agentclient-manager-v2", agentV2Manager, agentclient.DefaultRefreshInterval))
	b.Register(bootstrap.GRPCServer("grpc", grpcServer, listener))
	log.Fatal(b.Run(context.Background()))
}
