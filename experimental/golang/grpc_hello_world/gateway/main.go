package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	pgrpc "dominion/common/gopkg/grpc"
	"dominion/common/gopkg/grpc/solver"
	phttp "dominion/common/gopkg/http"
	"dominion/common/gopkg/otel"
	"dominion/experimental/golang/grpc_hello_world"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
)

var port = flag.String("port", "80", "Port to listen on")

func main() {
	flag.Parse()

	shutdown, err := otel.Init(context.Background())
	if err != nil {
		log.Fatalf("failed to initialize otel: %v", err)
	}

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
		Handler: phttp.Handler(mux),
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("gRPC hello world gateway listening :%s", *port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("failed to serve: %v", err)
		}
	}()

	<-sigCh
	log.Println("shutting down gracefully...")

	if err := srv.Shutdown(context.Background()); err != nil {
		log.Printf("http shutdown error: %v", err)
	}
	conn.Close()
	if err := shutdown(context.Background()); err != nil {
		log.Printf("otel shutdown error: %v", err)
	}
}
