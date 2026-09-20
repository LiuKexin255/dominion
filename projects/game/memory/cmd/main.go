// The memory command is the MemoryService + WikiService gRPC server
// (specs/039-planner-memory-calibration/spec.md FR-006;
// specs/069-wiki-template-web/contracts/wiki-service.md §1), hosting planner
// long-term memory entries and wiki knowledge-base entries in the dedicated
// "game_memory" database (style/mongo.md;
// specs/039-planner-memory-calibration/contracts/memory-service-contract.md
// §4; specs/069-wiki-template-web/contracts/wiki-service.md §6).
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
	wikiRepo, err := mongort.NewWikiRepository(mongoClient, "game_memory")
	if err != nil {
		log.Fatalf("failed to create wiki repository: %v", err)
	}

	h := handler.NewHandler(repo)
	wikiHandler := handler.NewWikiHandler(wikiRepo)

	grpcServer := grpcgo.NewServer(pgrpc.ServiceDefault()...)
	game.RegisterMemoryServiceServer(grpcServer, h)
	game.RegisterWikiServiceServer(grpcServer, wikiHandler)
	reflection.Register(grpcServer)

	b := bootstrap.New()
	b.Register(otel.Component())
	b.Register(bootstrap.MongoClient("mongo", mongoClient))
	b.Register(bootstrap.GRPCServer("grpc", grpcServer, listener))
	log.Fatal(b.Run(context.Background()))
}
