package main

import (
	"context"
	"flag"
	"log"
	"net"

	"dominion/common/gopkg/bootstrap"
	pgrpc "dominion/common/gopkg/grpc"
	"dominion/common/gopkg/logs"
	"dominion/common/gopkg/logs/event"
	"dominion/common/gopkg/otel"
	"dominion/experimental/grpc_chain"

	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var port = flag.String("port", "50051", "Port to listen on")

type echoServer struct {
	grpc_chain.UnimplementedEchoServer
}

func (s *echoServer) Say(ctx context.Context, req *grpc_chain.EchoRequest) (*grpc_chain.EchoResponse, error) {
	msg := req.GetMessage()
	logs.Info(ctx, "handle Say", event.String("message", msg))

	return &grpc_chain.EchoResponse{
		Message: "backend:" + msg,
		Chain:   "backend",
	}, nil
}

func main() {
	flag.Parse()

	listener, err := net.Listen("tcp", ":"+*port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpcgo.NewServer(pgrpc.ServiceDefault()...)

	grpc_chain.RegisterEchoServer(grpcServer, &echoServer{})
	reflection.Register(grpcServer)

	log.Printf("grpc-chain backend server listening: %s", *port)

	b := bootstrap.New()
	b.Register(otel.Component())
	b.Register(bootstrap.GRPCServer("grpc", grpcServer, listener))
	log.Fatal(b.Run(context.Background()))
}
