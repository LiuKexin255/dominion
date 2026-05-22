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
	"dominion/common/gopkg/otel"
	"dominion/projects/game/pkg/token"
	rt "dominion/projects/game/runtime"
	"dominion/projects/game/runtime/config"
	"dominion/projects/game/runtime/domain/sessionmanager"
	"dominion/projects/game/runtime/service"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
)

const (
	envHTTPPort           = "HTTP_PORT"
	defaultHTTPListenAddr = ":8080"
)

func main() {
	httpPort := envOrDefault(envHTTPPort, defaultHTTPListenAddr)

	runtimeID := strings.TrimSpace(os.Getenv("HOSTNAME"))
	if runtimeID == "" {
		host, err := os.Hostname()
		if err != nil {
			log.Fatalf("get hostname: %v", err)
		}
		runtimeID = host
	}

	cfg := loadRuntimeConfig(runtimeID)

	signer := token.NewHMACSigner(cfg.TokenSecret, cfg.TokenTTL)
	sessions := sessionmanager.NewManager(cfg.RuntimeID, sessionmanager.WithIdleTTL(cfg.IdleTTL))
	control := service.NewControlExecutor()
	svc := service.NewRuntimeService(sessions, control, cfg, signer, signer)
	handler := rt.NewHandler(svc, signer)
	runtimeLifecycleHandler := rt.NewRuntimeHandler(svc, signer)

	publicMux := runtime.NewServeMux(grpcpkg.GatewayDefault()...)
	rt.RegisterGameGatewayServiceHandlerServer(context.Background(), publicMux, handler)

	wsHandler := rt.NewWebSocketHandler(svc)

	// Simple HTTP router (no owner router needed — owner resolution happens in gateway)
	httpMux := http.NewServeMux()
	httpMux.Handle("/", publicMux)
	httpMux.Handle("/v1/sessions/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check for WebSocket upgrade
		if strings.HasSuffix(r.URL.Path, "/game/connect") &&
			strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") {
			wsHandler.ServeHTTP(w, r)
			return
		}
		publicMux.ServeHTTP(w, r)
	}))

	httpServer := &http.Server{Addr: normalizeListenAddr(httpPort), Handler: httpMux}

	internalGRPCServer := grpc.NewServer(grpcpkg.ServiceDefault()...)
	rt.RegisterGameRuntimeServiceServer(internalGRPCServer, runtimeLifecycleHandler)

	internalListener, err := net.Listen("tcp", normalizeListenAddr(cfg.GRPCPort))
	if err != nil {
		log.Fatalf("create internal gRPC listener: %v", err)
	}

	cleanupWorker := sessions.StartCleanup()
	completionWorker := svc.StartCompletionWorker()
	routingWorker := wsHandler.StartRoutingWorker()

	bs := bootstrap.New(bootstrap.WithShutdownTimeout(5 * time.Second))
	if err := bs.Register(otel.Component(otel.WithLoggerName("dominion/projects/game/runtime"))); err != nil {
		log.Fatalf("register otel: %v", err)
	}
	if err := bs.Register(bootstrap.HTTPServer("runtime-http", httpServer)); err != nil {
		log.Fatalf("register http server: %v", err)
	}
	if err := bs.Register(bootstrap.GRPCServer("runtime-grpc", internalGRPCServer, internalListener)); err != nil {
		log.Fatalf("register gRPC server: %v", err)
	}
	if err := bs.Register(bootstrap.Daemon("runtime-idle-cleanup", cleanupWorker)); err != nil {
		log.Fatalf("register cleanup worker: %v", err)
	}
	if err := bs.Register(bootstrap.Daemon("runtime-completion", completionWorker)); err != nil {
		log.Fatalf("register completion worker: %v", err)
	}
	if err := bs.Register(bootstrap.Daemon("runtime-routing", routingWorker)); err != nil {
		log.Fatalf("register routing worker: %v", err)
	}
	if err := bs.Run(context.Background()); err != nil {
		log.Fatalf("run runtime: %v", err)
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

func loadRuntimeConfig(runtimeID string) *config.RuntimeConfig {
	tokenSecret := strings.TrimSpace(os.Getenv("SESSION_TOKEN_SECRET"))
	if tokenSecret == "" {
		log.Fatal("missing required environment variable SESSION_TOKEN_SECRET")
	}

	cfg := config.NewRuntimeConfig(runtimeID, tokenSecret)

	if v := os.Getenv("SESSION_TOKEN_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.TokenTTL = d
		}
	}
	if v := os.Getenv("SESSION_TOKEN_REFRESH_GRACE"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.TokenRefreshGrace = d
		}
	}
	if v := os.Getenv("SESSION_IDLE_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.IdleTTL = d
		}
	}
	if v := os.Getenv("HTTP_PORT"); v != "" {
		cfg.HTTPPort = v
	}
	if v := os.Getenv("GRPC_PORT"); v != "" {
		cfg.GRPCPort = v
	}

	return cfg
}
