// The memory command is the MemoryService gRPC server (spec
// 039-planner-memory-calibration FR-006), hosting planner long-term memory
// entries in the dedicated "game_memory" database (style/mongo.md;
// specs/039-planner-memory-calibration/contracts/memory-service-contract.md
// §4).
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

	game "dominion/projects/game"
	"dominion/projects/game/memory/handler"
	mongort "dominion/projects/game/memory/runtime/mongo"

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

	mongoClient, err := mongo.NewClient("game/mongo")
	if err != nil {
		log.Fatalf("failed to create mongo client: %v", err)
	}

	repo := mongort.NewRepository(mongoClient, "game_memory")

	h := handler.NewHandler(repo)

	grpcServer := grpcgo.NewServer(pgrpc.ServiceDefault()...)
	game.RegisterMemoryServiceServer(grpcServer, h)
	reflection.Register(grpcServer)

	b := bootstrap.New()
	b.Register(otel.Component())
	b.Register(bootstrap.MongoClient("mongo", mongoClient))
	b.Register(bootstrap.GRPCServer("grpc", grpcServer, listener))
	log.Fatal(b.Run(context.Background()))
}
