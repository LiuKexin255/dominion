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
	"dominion/experimental/redis_demo"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
)

var port = flag.String("port", "80", "Port to listen on")

// One sub-prefix per language implementation. The HTTPRoute owns the shared
// /experimental/redis-demo prefix, so the gateway mounts each mux below its own
// sub-prefix and strips it before routing; the proto annotations themselves are
// rooted at /v1 (specs/067-redis-infra-support/contracts/redis-demo.md §1).
const (
	goPathPrefix = "/experimental/redis-demo/go"
	jsPathPrefix = "/experimental/redis-demo/js"
)

func main() {
	flag.Parse()

	goConn, err := grpc.NewClient(solver.URI("redis-demo/go-service:grpc"), pgrpc.ClientDefault()...)
	if err != nil {
		log.Fatalf("failed to dial go-service: %v", err)
	}

	jsConn, err := grpc.NewClient(solver.URI("redis-demo/js-service:grpc"), pgrpc.ClientDefault()...)
	if err != nil {
		log.Fatalf("failed to dial js-service: %v", err)
	}

	goMux := runtime.NewServeMux(pgrpc.GatewayDefault()...)
	if err := redis_demo.RegisterKvServiceHandler(context.Background(), goMux, goConn); err != nil {
		log.Fatalf("failed to register go-service handler: %v", err)
	}

	jsMux := runtime.NewServeMux(pgrpc.GatewayDefault()...)
	if err := redis_demo.RegisterKvServiceHandler(context.Background(), jsMux, jsConn); err != nil {
		log.Fatalf("failed to register js-service handler: %v", err)
	}

	rootMux := http.NewServeMux()
	rootMux.Handle(goPathPrefix+"/", http.StripPrefix(goPathPrefix, goMux))
	rootMux.Handle(jsPathPrefix+"/", http.StripPrefix(jsPathPrefix, jsMux))

	srv := &http.Server{
		Addr:    ":" + *port,
		Handler: phttp.Handler(rootMux, "redis-demo-gateway"),
	}

	log.Printf("redis-demo gateway listening :%s", *port)

	b := bootstrap.New()
	b.Register(otel.Component())
	b.Register(bootstrap.GRPCConn("go-service", goConn))
	b.Register(bootstrap.GRPCConn("js-service", jsConn))
	b.Register(bootstrap.HTTPServer("http", srv))
	log.Fatal(b.Run(context.Background()))
}
