package main

import (
	"context"
	"flag"
	"log"
	"net/http"

	"dominion/experimental/golang/grpc_hello_world"
	pgrpc "dominion/common/gopkg/grpc"
	"dominion/common/gopkg/grpc/solver"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
)

var port = flag.String("port", "80", "Port to listen on")

func main() {
	flag.Parse()
	conn, err := grpc.NewClient(solver.URI("grpc-hello-world/service:grpc"), pgrpc.ClientDefault()...)
	if err != nil {
		log.Fatalf("failed to dial backend: %v", err)
	}
	defer conn.Close()

	mux := runtime.NewServeMux()
	err = grpc_hello_world.RegisterGreeterHandler(context.Background(), mux, conn)
	if err != nil {
		log.Fatalf("failed to serve: %v", err)
	}

	log.Printf("gRPC hello world gateway listening :%s", *port)
	if err := http.ListenAndServe(":"+*port, mux); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
