package main

import (
	"context"
	"flag"
	"log"
	"net"

	"dominion/experimental/grpc_hello_world"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var port = flag.String("port", "50051", "Port to listen on")
var tlsCertFile = flag.String("tls_cert_file", "/etc/tls/tls.crt", "Path to TLS certificate file")
var tlsKeyFile = flag.String("tls_key_file", "/etc/tls/tls.key", "Path to TLS private key file")

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

	// creds, err := credentials.NewServerTLSFromFile(*tlsCertFile, *tlsKeyFile)
	// if err != nil {
	// 	log.Fatalf("failed to load TLS key pair from cert=%s key=%s: %v", *tlsCertFile, *tlsKeyFile, err)
	// }

	// grpcServer := grpc.NewServer(grpc.Creds(creds))
	grpcServer := grpc.NewServer()

	grpc_hello_world.RegisterGreeterServer(grpcServer, &greeterServer{})
	reflection.Register(grpcServer)

	log.Printf("gRPC hello world server listening: %s", *port)
	if err := grpcServer.Serve(listener); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
