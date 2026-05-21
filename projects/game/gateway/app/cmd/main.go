package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"dominion/common/gopkg/bootstrap"
	grpcpkg "dominion/common/gopkg/grpc"
	phttp "dominion/common/gopkg/http"
	"dominion/common/gopkg/otel"
	gateway "dominion/projects/game/gateway"
	"dominion/projects/game/gateway/app"
	"dominion/projects/game/gateway/domain/sessionmanager"
	"dominion/projects/game/gateway/owner"
	"dominion/projects/game/gateway/service"
	"dominion/projects/game/gateway/token"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
)

const (
	envHTTPPort = "HTTP_PORT"

	defaultHTTPListenAddr = ":8080"
)

func main() {
	httpPort := envOrDefault(envHTTPPort, defaultHTTPListenAddr)

	config, err := service.NewOwnerConfig()
	if err != nil {
		log.Fatalf("create owner config: %v", err)
	}

	signer := token.NewHMACSigner(config.TokenSecret, config.TokenTTL)
	sessions := sessionmanager.NewManager(config.GatewayID, sessionmanager.WithIdleTTL(config.IdleTTL))
	control := service.NewControlExecutor()
	svc := service.NewGatewayService(sessions, control, config, signer, signer)
	handler := gateway.NewHandler(svc, signer)
	runtimeHandler := gateway.NewGameRuntimeHandler(svc)

	publicMux := runtime.NewServeMux(grpcpkg.GatewayDefault()...)
	gateway.RegisterGameGatewayServiceHandlerServer(context.Background(), publicMux, handler)

	ownerRouter, err := owner.NewRouter(config.GatewayID, phttp.Handler(publicMux, "gateway-http"))
	if err != nil {
		log.Fatalf("create owner router: %v", err)
	}

	wsHandler := gateway.NewWebSocketHandler(svc, ownerRouter, signer)

	router := &app.Router{
		WSHandler:     phttp.Handler(wsHandler, "GET /v1/sessions/{id}/game/connect"),
		GRPCMux:       phttp.Handler(publicMux, "gateway-http"),
		OwnerRouter:   ownerRouter,
		TokenVerifier: signer,
	}
	httpServer := &http.Server{Addr: normalizeListenAddr(httpPort), Handler: router}

	internalGRPCServer := grpc.NewServer(grpcpkg.ServiceDefault()...)
	gateway.RegisterGameRuntimeServiceServer(internalGRPCServer, runtimeHandler)

	internalListener, err := net.Listen("tcp", normalizeListenAddr(config.InternalGRPCPort))
	if err != nil {
		log.Fatalf("create internal gRPC listener: %v", err)
	}

	cleanupWorker := sessions.StartCleanup()
	completionWorker := svc.StartCompletionWorker()
	routingWorker := wsHandler.StartRoutingWorker()

	bs := bootstrap.New(bootstrap.WithShutdownTimeout(5 * time.Second))
	if err := bs.Register(otel.Component(otel.WithLoggerName("dominion/projects/game/gateway"))); err != nil {
		log.Fatalf("register otel: %v", err)
	}
	if err := bs.Register(bootstrap.HTTPServer("gateway-http", httpServer)); err != nil {
		log.Fatalf("register gateway server: %v", err)
	}
	if err := bs.Register(bootstrap.GRPCServer("gateway-internal-grpc", internalGRPCServer, internalListener)); err != nil {
		log.Fatalf("register internal gRPC server: %v", err)
	}
	if err := bs.Register(bootstrap.Daemon("gateway-idle-cleanup", cleanupWorker)); err != nil {
		log.Fatalf("register cleanup worker: %v", err)
	}
	if err := bs.Register(bootstrap.Daemon("gateway-completion", completionWorker)); err != nil {
		log.Fatalf("register completion worker: %v", err)
	}
	if err := bs.Register(bootstrap.Daemon("gateway-routing", routingWorker)); err != nil {
		log.Fatalf("register routing worker: %v", err)
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
