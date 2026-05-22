// Package main is the entry point for the session apitest shell.
//
// The apitest shell is a minimal HTTP-to-gRPC gateway that forwards
// testplan requests targeting /game/session/* to the session gRPC backend.
// It is intended for testplan use only and does not include business logic,
// auth, or other gateway features.
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
	"dominion/common/gopkg/otel"
	gameconst "dominion/projects/game/pkg/const"
	sessionpb "dominion/projects/game/session"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
)

const (
	envHTTPPort = "HTTP_PORT"

	defaultHTTPListenAddr = ":8080"
	routePrefix           = "/game/session"
	shutdownTimeout       = 5 * time.Second
)

func main() {
	httpPort := envOrDefault(envHTTPPort, defaultHTTPListenAddr)

	// Create gRPC client connection to session backend.
	sessionConn, err := grpc.NewClient(solver.URI(gameconst.TargetSessionGRPC), grpcpkg.ClientDefault()...)
	if err != nil {
		log.Fatalf("create session gRPC client: %v", err)
	}

	// Create grpc-gateway mux and register session handler.
	mux := runtime.NewServeMux(grpcpkg.GatewayDefault()...)
	if err := sessionpb.RegisterSessionServiceHandler(context.Background(), mux, sessionConn); err != nil {
		log.Fatalf("register session service handler: %v", err)
	}

	// Create HTTP router with prefix stripping.
	router := http.NewServeMux()
	handler := http.StripPrefix(routePrefix, mux)
	router.Handle(routePrefix+"/", handler)

	httpServer := &http.Server{Addr: normalizeListenAddr(httpPort), Handler: router}

	bs := bootstrap.New(bootstrap.WithShutdownTimeout(shutdownTimeout))
	if err := bs.Register(otel.Component(otel.WithLoggerName("dominion/projects/game/session/testplan/apitest"))); err != nil {
		log.Fatalf("register otel: %v", err)
	}
	if err := bs.Register(bootstrap.GRPCConn("session-grpc-client", sessionConn)); err != nil {
		log.Fatalf("register session gRPC client: %v", err)
	}
	if err := bs.Register(bootstrap.HTTPServer("apitest-http", httpServer)); err != nil {
		log.Fatalf("register apitest http server: %v", err)
	}
	if err := bs.Run(context.Background()); err != nil {
		log.Fatalf("run apitest: %v", err)
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
