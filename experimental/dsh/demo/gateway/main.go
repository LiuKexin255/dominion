// The gateway command serves as the public HTTP entry point of the dsh-demo
// chat link: it transcodes the Chat gRPC service (HTTP/JSON via grpc-gateway)
// onto the agent service. The wire contract is defined in
// specs/047-dsh-chat-demo/contracts/chat-api.md.
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
	"dominion/experimental/dsh/demo"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
)

var port = flag.String("port", "80", "Port to listen on")

func main() {
	flag.Parse()

	conn, err := grpc.NewClient(solver.URI("dsh-demo/agent:grpc"), pgrpc.ClientDefault()...)
	if err != nil {
		log.Fatalf("failed to dial agent service: %v", err)
	}

	mux := runtime.NewServeMux(pgrpc.GatewayDefault()...)
	err = demo.RegisterChatHandler(context.Background(), mux, conn)
	if err != nil {
		log.Fatalf("failed to register handler: %v", err)
	}

	srv := &http.Server{
		Addr:    ":" + *port,
		Handler: phttp.Handler(mux, "dsh-demo-gateway"),
	}

	log.Printf("dsh-demo gateway listening :%s", *port)

	b := bootstrap.New()
	b.Register(otel.Component())
	b.Register(bootstrap.GRPCConn("agent", conn))
	b.Register(bootstrap.HTTPServer("http", srv))
	log.Fatal(b.Run(context.Background()))
}
