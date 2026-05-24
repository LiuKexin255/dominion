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
	"dominion/projects/game/proxy/handler"
	"dominion/projects/game/proxy/runtime"

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
	mongoOwnerStore := runtime.NewMongoOwnerStore(mongoClient)

	// StatefulResolver discovers agent service instances.
	statefulResolver, err := solver.NewDeployStatefulResolver()
	if err != nil {
		log.Fatalf("failed to create stateful resolver: %v", err)
	}

	// Hash-based owner picker.
	hashPicker := runtime.NewHashPicker()

	// Proxy handler implements the ProxyService gRPC server interface.
	h := handler.NewProxyHandler(mongoOwnerStore, hashPicker, statefulResolver)

	// gRPC server with default service options (OTel tracing, TLS).
	grpcServer := grpcgo.NewServer(pgrpc.ServiceDefault()...)
	game.RegisterProxyServiceServer(grpcServer, h)
	reflection.Register(grpcServer)

	// Bootstrap lifecycle: OTEL → Mongo client → gRPC server.
	b := bootstrap.New()
	b.Register(otel.Component())
	b.Register(bootstrap.MongoClient("mongo", mongoClient))
	b.Register(bootstrap.GRPCServer("grpc", grpcServer, listener))
	log.Fatal(b.Run(context.Background()))
}
