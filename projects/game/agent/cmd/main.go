package main

import (
	"context"
	"flag"
	"log"
	"net"

	"dominion/common/gopkg/bootstrap"
	pgrpc "dominion/common/gopkg/grpc"
	"dominion/common/gopkg/otel"
	game "dominion/projects/game"
	"dominion/projects/game/agent/handler"
	"dominion/projects/game/agent/runtime"

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

	rt := runtime.NewSimpleRuntime()
	h := handler.NewAgentHandler(rt)

	grpcServer := grpcgo.NewServer(pgrpc.ServiceDefault()...)
	game.RegisterAgentServiceServer(grpcServer, h)
	reflection.Register(grpcServer)

	log.Printf("agent gRPC server listening on :%s", *port)

	b := bootstrap.New()
	b.Register(otel.Component())
	b.Register(bootstrap.GRPCServer("grpc", grpcServer, listener))
	log.Fatal(b.Run(context.Background()))
}
