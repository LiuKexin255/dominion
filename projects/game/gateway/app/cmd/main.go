package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"dominion/common/gopkg/bootstrap"
	"dominion/common/gopkg/grpc"
	phttp "dominion/common/gopkg/http"
	"dominion/common/gopkg/otel"
	gateway "dominion/projects/game/gateway"
	"dominion/projects/game/gateway/app"
	"dominion/projects/game/gateway/domain/sessionmanager"
	"dominion/projects/game/gateway/service"
	"dominion/projects/game/pkg/token"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
)

const (
	envHTTPPort           = "HTTP_PORT"
	envSessionTokenSecret = "SESSION_TOKEN_SECRET"

	defaultHTTPListenAddr = ":8080"
)

func main() {
	httpPort := envOrDefault(envHTTPPort, defaultHTTPListenAddr)
	tokenSecret := strings.TrimSpace(os.Getenv(envSessionTokenSecret))
	if tokenSecret == "" {
		log.Fatalf("missing required environment variable %s", envSessionTokenSecret)
	}
	gatewayID := os.Getenv("HOSTNAME")
	if gatewayID == "" {
		gatewayID = "game-gateway-0"
	}

	verifier := token.NewHMACSigner(tokenSecret, 0)
	sessions := sessionmanager.NewManager(gatewayID)
	control := service.NewControlExecutor()
	svc, completionWorker := service.NewGatewayService(sessions, control, gatewayID, verifier)
	wsHandler, routingWorker := gateway.NewWebSocketHandler(svc)
	handler := gateway.NewHandler(svc)

	httpMux := runtime.NewServeMux(grpc.GatewayDefault()...)
	_ = gateway.RegisterGameGatewayServiceHandlerServer(context.Background(), httpMux, handler)
	router := &app.Router{
		WSHandler: phttp.Handler(wsHandler, "GET /v1/sessions/{id}/game/connect"),
		GRPCMux: phttp.Handler(httpMux, "gateway-http"),
	}
	httpServer := &http.Server{Addr: normalizeListenAddr(httpPort), Handler: router}

	bs := bootstrap.New(bootstrap.WithShutdownTimeout(5 * time.Second))
	if err := bs.Register(otel.Component()); err != nil {
		log.Fatalf("register otel: %v", err)
	}
	if err := bs.Register(bootstrap.HTTPServer("gateway-http", httpServer)); err != nil {
		log.Fatalf("register gateway server: %v", err)
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
