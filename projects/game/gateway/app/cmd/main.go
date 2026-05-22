// Package main is the entry point for the game gateway edge proxy.
//
// The gateway is a pure edge aggregation layer that:
//  1. Registers grpc-gateway handlers for session proto services,
//     forwarding to the session gRPC backend.
//  2. Proxies WebSocket upgrade requests to the owner runtime instance based
//     on the owner_runtime_id extracted from the session token.
//
// The gateway does NOT hold any runtime domain, service, or WebSocket handler
// code. It does NOT verify tokens, issue tokens, cache auth results,
// or make routing decisions based on business semantics.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"dominion/common/gopkg/bootstrap"
	grpcpkg "dominion/common/gopkg/grpc"
	solver "dominion/common/gopkg/grpc/solver"
	phttp "dominion/common/gopkg/http"
	"dominion/common/gopkg/otel"
	"dominion/projects/game/gateway/app"
	"dominion/projects/game/gateway/runtime/owner"
	gameconst "dominion/projects/game/pkg/const"
	"dominion/projects/game/pkg/token"
	runtimepb "dominion/projects/game/runtime"
	sessionpb "dominion/projects/game/session"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
)

const (
	envHTTPPort = "HTTP_PORT"

	defaultHTTPListenAddr = ":8080"
)

func main() {
	httpPort := envOrDefault(envHTTPPort, defaultHTTPListenAddr)

	// Create gRPC client connection to session backend.
	sessionConn, err := grpc.NewClient(solver.URI(gameconst.TargetSessionGRPC), grpcpkg.ClientDefault()...)
	if err != nil {
		log.Fatalf("create session gRPC client: %v", err)
	}

	// Create gRPC client connection to runtime backend.
	runtimeConn, err := grpc.NewClient(solver.URI(gameconst.TargetRuntimeGRPC), grpcpkg.ClientDefault()...)
	if err != nil {
		log.Fatalf("create runtime gRPC client: %v", err)
	}

	// Create grpc-gateway mux and register session and runtime backend
	// handlers. The mux translates HTTP+JSON requests to gRPC and forwards
	// them to the appropriate gRPC backend.
	publicMux := runtime.NewServeMux(grpcpkg.GatewayDefault()...)
	if err := sessionpb.RegisterSessionServiceHandler(context.Background(), publicMux, sessionConn); err != nil {
		log.Fatalf("register session service handler: %v", err)
	}
	if err := runtimepb.RegisterGameRuntimeReaderHandler(context.Background(), publicMux, runtimeConn); err != nil {
		log.Fatalf("register runtime reader handler: %v", err)
	}

	// Create owner resolver for WebSocket proxy routing.
	ownerResolver, err := owner.NewResolver()
	if err != nil {
		log.Fatalf("create owner resolver: %v", err)
	}

	// OwnerExtractor parses routing claims from tokens without verification.
	ownerExtractor := token.NewParser()

	router := &app.Router{
		GRPCMux:        phttp.Handler(publicMux, "gateway-http"),
		OwnerResolver:  ownerResolver,
		OwnerExtractor: ownerExtractor,
	}

	httpServer := &http.Server{Addr: normalizeListenAddr(httpPort), Handler: router}

	bs := bootstrap.New(bootstrap.WithShutdownTimeout(5 * time.Second))
	if err := bs.Register(otel.Component(otel.WithLoggerName("dominion/projects/game/gateway"))); err != nil {
		log.Fatalf("register otel: %v", err)
	}
	if err := bs.Register(bootstrap.HTTPServer("gateway-http", httpServer)); err != nil {
		log.Fatalf("register gateway http server: %v", err)
	}
	if err := bs.Register(bootstrap.GRPCConn("session-grpc-client", sessionConn)); err != nil {
		log.Fatalf("register session gRPC client: %v", err)
	}
	if err := bs.Register(bootstrap.GRPCConn("runtime-grpc-client", runtimeConn)); err != nil {
		log.Fatalf("register runtime gRPC client: %v", err)
	}
	if err := bs.Run(context.Background()); err != nil {
		log.Fatalf("run gateway: %v", err)
	}
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}

	return fallback
}

func normalizeListenAddr(value string) string {
	if strings.HasPrefix(value, ":") {
		return value
	}

	return ":" + value
}
