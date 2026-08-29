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
	gamev2 "dominion/projects/game/v2"

	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var port = flag.String("port", "50051", "Port to listen on")

func main() {
	flag.Parse()

	listener, err := net.Listen("tcp", ":"+*port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	// MongoDB-backed owner stores. The v1 team-owner store and the agent_v2
	// conversation-owner store live in dedicated collections: the two
	// stateful instance pools are independent, and the same game session may
	// hold a v1 team owner and a v2 conversation owner at once
	// (specs/049-agent-v2-dsh-init/data-model.md §2.9).
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
	// server-streaming pump (ConversationService.Send relay).
	binder := bind.NewBinder()

	// Team handler implements the TeamService gRPC server interface directly:
	// owner resolution, agent-client routing, and stream binding live here.
	// (spec 031-team-template-mode: ProxyService/AgentService merged into TeamService.)
	grpcHandler := handler.NewTeamHandler(mongoOwnerStore, hashPicker, manager, binder)

	// Conversation handler forwards the /api/v2 surface (Send/ListHistory/
	// Dispose) to the agent_v2 instance owning the (template, session) pair —
	// owner affinity keeps the in-memory sessions from drifting across
	// instances (specs/049-agent-v2-dsh-init/research.md D4).
	conversationHandler := handler.NewConversationHandler(
		agentV2OwnerStore,
		hashPicker,
		agentV2Manager,
		bind.NewServerStreamBinder[gamev2.ChatEvent](),
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
	game.RegisterTeamServiceServer(grpcServer, grpcHandler)
	gamev2.RegisterConversationServiceServer(grpcServer, conversationHandler)
	reflection.Register(grpcServer)

	// Bootstrap lifecycle: OTEL → Mongo client → Agent client managers → gRPC server.
	b := bootstrap.New()
	b.Register(otel.Component())
	b.Register(bootstrap.MongoClient("mongo", mongoClient))
	b.Register(agentclient.NewDaemon(agentclient.DefaultDaemonName, manager, agentclient.DefaultRefreshInterval))
	b.Register(agentclient.NewDaemon("agentclient-manager-v2", agentV2Manager, agentclient.DefaultRefreshInterval))
	b.Register(bootstrap.GRPCServer("grpc", grpcServer, listener))
	log.Fatal(b.Run(context.Background()))
}
