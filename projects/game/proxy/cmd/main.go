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

func main() {
	flag.Parse()

	listener, err := net.Listen("tcp", ":"+*port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	// MongoDB-backed owner store.
	mongoClient, err := mongo.NewClient("game/mongo")
	if err != nil {
		log.Fatalf("failed to create mongo client: %v", err)
	}
	mongoOwnerStore := proxymongo.NewMongoOwnerStore(mongoClient)

	// StatefulResolver discovers agent service instances.
	statefulResolver, err := solver.NewDeployStatefulResolver()
	if err != nil {
		log.Fatalf("failed to create stateful resolver: %v", err)
	}

	// Hash-based owner picker.
	hashPicker := picker.NewHashPicker()

	// Agent client manager with periodic refresh via Daemon.
	agentTarget := solver.MustParseTarget(gameconst.AgentTarget)
	manager := agentclient.NewManager(statefulResolver, agentTarget, agentclient.DefaultRefreshInterval)

	// Bidirectional stream binder.
	binder := bind.NewBinder()

	// Team handler implements the TeamService gRPC server interface directly:
	// owner resolution, agent-client routing, and stream binding live here.
	// (spec 031-team-template-mode: ProxyService/AgentService merged into TeamService.)
	grpcHandler := handler.NewTeamHandler(mongoOwnerStore, hashPicker, manager, binder)

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
	reflection.Register(grpcServer)

	// Bootstrap lifecycle: OTEL → Mongo client → Agent client manager → gRPC server.
	b := bootstrap.New()
	b.Register(otel.Component())
	b.Register(bootstrap.MongoClient("mongo", mongoClient))
	b.Register(agentclient.NewDaemon(manager, agentclient.DefaultRefreshInterval))
	b.Register(bootstrap.GRPCServer("grpc", grpcServer, listener))
	log.Fatal(b.Run(context.Background()))
}
