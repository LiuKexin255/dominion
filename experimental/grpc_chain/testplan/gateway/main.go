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
	"dominion/common/gopkg/otel"
	"dominion/experimental/grpc_chain"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
)

var port = flag.String("port", "80", "Port to listen on")

func main() {
	flag.Parse()

	conn, err := grpc.NewClient(solver.URI("grpc-chain/mid:grpc"), pgrpc.ClientDefault()...)
	if err != nil {
		log.Fatalf("failed to dial mid service: %v", err)
	}

	mux := runtime.NewServeMux(pgrpc.GatewayDefault()...)
	err = grpc_chain.RegisterEchoHandler(context.Background(), mux, conn)
	if err != nil {
		log.Fatalf("failed to register handler: %v", err)
	}

	srv := &http.Server{
		Addr:    ":" + *port,
		Handler: phttp.Handler(mux, "grpc-chain-gateway"),
	}

	log.Printf("grpc-chain gateway listening :%s", *port)

	b := bootstrap.New()
	b.Register(otel.Component())
	b.Register(bootstrap.GRPCConn("mid", conn))
	b.Register(bootstrap.HTTPServer("http", srv))
	log.Fatal(b.Run(context.Background()))
}
