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
	"dominion/projects/game/session/handler"
	"dominion/projects/game/session/runtime"

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

	sessionRepo := runtime.NewSessionRepository(mongoClient, "game_session", "sessions")

	h := handler.NewSessionHandler(sessionRepo)

	grpcServer := grpcgo.NewServer(pgrpc.ServiceDefault()...)
	game.RegisterSessionServiceServer(grpcServer, h)
	reflection.Register(grpcServer)

	b := bootstrap.New()
	b.Register(otel.Component())
	b.Register(bootstrap.MongoClient("mongo", mongoClient))
	b.Register(bootstrap.GRPCServer("grpc", grpcServer, listener))
	log.Fatal(b.Run(context.Background()))
}
