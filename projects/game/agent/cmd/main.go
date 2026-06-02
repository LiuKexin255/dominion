package main

import (
	"context"
	"flag"
	"log"
	"net"

	"dominion/common/gopkg/bootstrap"
	pgrpc "dominion/common/gopkg/grpc"
	"dominion/common/gopkg/grpc/solver"
	"dominion/common/gopkg/otel"
	game "dominion/projects/game"
	"dominion/projects/game/agent/handler"
	"dominion/projects/game/agent/runtime/invoke"
	"dominion/projects/game/agent/runtime/promptclient"
	gameconst "dominion/projects/game/pkg/gameconst"

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

	promptConn, err := grpcgo.NewClient(solver.URI(gameconst.PromptTarget), pgrpc.ClientDefault()...)
	if err != nil {
		log.Fatalf("prompt dial: %v", err)
	}

	promptClient := &promptclient.Adapter{Client: game.NewPromptServiceClient(promptConn)}
	_ = promptClient // reserved for future use (handler in Task 10)
	rt := invoke.New(promptClient)
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
