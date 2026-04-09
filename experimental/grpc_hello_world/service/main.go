package main

import (
	"context"
	"flag"
	"log"
	"net"

	"dominion/experimental/grpc_hello_world"
	pgrpc "dominion/pkg/grpc"

	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var port = flag.String("port", "50051", "Port to listen on")

type greeterServer struct {
	grpc_hello_world.UnimplementedGreeterServer
}

func (s *greeterServer) GetHello(ctx context.Context, req *grpc_hello_world.HelloRequest) (*grpc_hello_world.Hello, error) {
	name := req.GetName()
	if name == "" {
		name = "world"
	}

	return &grpc_hello_world.Hello{Name: name, Message: "Hello, " + name + "!"}, nil
}

func main() {
	flag.Parse()

	listener, err := net.Listen("tcp", ":"+*port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpcgo.NewServer(pgrpc.ServiceDefault()...)

	grpc_hello_world.RegisterGreeterServer(grpcServer, &greeterServer{})
	reflection.Register(grpcServer)

	log.Printf("gRPC hello world server listening: %s", *port)
	if err := grpcServer.Serve(listener); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
