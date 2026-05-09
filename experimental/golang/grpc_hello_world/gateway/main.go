package main

import (
	"context"
	"flag"
	"log"
	"net/http"

	"dominion/common/gopkg/bootstrap"
	pgrpc "dominion/common/gopkg/grpc"
	"dominion/common/gopkg/grpc/solver"
	phttp "dominion/common/gopkg/http"
	"dominion/experimental/golang/grpc_hello_world"

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

	mux := runtime.NewServeMux(pgrpc.GatewayDefault()...)
	err = grpc_hello_world.RegisterGreeterHandler(context.Background(), mux, conn)
	if err != nil {
		log.Fatalf("failed to register handler: %v", err)
	}

	srv := &http.Server{
		Addr:    ":" + *port,
		Handler: phttp.Handler(mux, "grpc-hello-world-gateway"),
	}

	log.Printf("gRPC hello world gateway listening :%s", *port)

	b := bootstrap.New()
	b.Register(bootstrap.OTel())
	b.Register(bootstrap.GRPCConn("backend", conn))
	b.Register(bootstrap.HTTPServer("http", srv))
	log.Fatal(b.Run(context.Background()))
}
