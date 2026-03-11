package main

import (
	"context"
	"flag"
	"log"
	"net/http"

	"dominion/experimental/grpc_hello_world"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var port = flag.String("port", "80", "Port to listen on")

func main() {
	flag.Parse()

	mux := runtime.NewServeMux()

	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	err := grpc_hello_world.RegisterGreeterHandlerFromEndpoint(context.Background(), mux, "grpc-hello-world-service:50051", opts)
	if err != nil {
		log.Fatalf("failed to serve: %v", err)
	}

	log.Printf("gRPC hello world gateway listening :%s", *port)
	if err := http.ListenAndServe(":"+*port, mux); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
