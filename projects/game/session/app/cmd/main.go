package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"dominion/common/gopkg/bootstrap"
	"dominion/common/gopkg/grpc"
	phttp "dominion/common/gopkg/http"
	"dominion/common/gopkg/mongo"
	"dominion/common/gopkg/otel"
	"dominion/projects/game/session"
	"dominion/projects/game/session/runtime/gateway"
	"dominion/projects/game/session/runtime/storage"
	"dominion/projects/game/session/service"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
)

const (
	envHTTPPort = "HTTP_PORT"

	defaultHTTPListenAddr   = ":8081"
	defaultMongoTarget      = "game/mongo"
	defaultMongoDatabase    = "game"
	defaultShutdownDeadline = 5 * time.Second
	defaultGatewayTarget    = "game/gateway:internal-grpc"
)

var httpPort = flag.String("http-port", envOrDefault(envHTTPPort, defaultHTTPListenAddr), "HTTP listen address")

func main() {
	flag.Parse()

	httpAddr := normalizeListenAddr(*httpPort)

	// Create Mongo client and repo.
	client, err := mongo.NewClient(defaultMongoTarget)
	if err != nil {
		log.Fatalf("create mongo client: %v", err)
	}

	ctx := context.Background()
	repo, err := storage.NewMongoRepository(ctx, client.Database(defaultMongoDatabase))
	if err != nil {
		log.Fatalf("create session repository: %v", err)
	}

	// Create gRPC gateway client.
	gatewayClient, err := gateway.NewGRPCGatewayClient(ctx, defaultGatewayTarget)
	if err != nil {
		log.Fatalf("create grpc gateway client: %v", err)
	}

	// Create handler.
	svc := service.NewSessionService(repo, gatewayClient)
	handler := session.NewHandler(svc)

	// Server component.
	httpMux := runtime.NewServeMux(grpc.GatewayDefault()...)
	session.RegisterSessionServiceHandlerServer(context.Background(), httpMux, handler)
	httpServer := &http.Server{Addr: httpAddr, Handler: phttp.Handler(httpMux, "session-http")}

	bs := bootstrap.New(bootstrap.WithShutdownTimeout(defaultShutdownDeadline))
	bs.Register(otel.Component(otel.WithLoggerName("dominion/projects/game/session")))
	bs.Register(bootstrap.MongoClient("mongo", client))
	bs.Register(bootstrap.GRPCConn("gateway-client", gatewayClient.Conn()))
	bs.Register(bootstrap.HTTPServer("session-http", httpServer))
	if err := bs.Run(context.Background()); err != nil {
		log.Fatalf("run session: %v", err)
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
