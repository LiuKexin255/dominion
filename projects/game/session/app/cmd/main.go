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
	"dominion/common/gopkg/solver"
	"dominion/projects/game/pkg/token"
	"dominion/projects/game/session"
	"dominion/projects/game/session/runtime/gateway"
	"dominion/projects/game/session/runtime/storage"
	"dominion/projects/game/session/service"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
)

const (
	envHTTPPort           = "HTTP_PORT"
	envSessionTokenSecret = "SESSION_TOKEN_SECRET"
	envSessionTokenTTL    = "SESSION_TOKEN_TTL"

	defaultHTTPListenAddr   = ":8081"
	defaultMongoTarget      = "game/mongo"
	defaultMongoDatabase    = "game"
	defaultSessionTokenTTL  = "1h"
	defaultShutdownDeadline = 5 * time.Second
	publicHostPattern       = "gateway-%d-game.liukexin.com"
)

var httpPort = flag.String("http-port", envOrDefault(envHTTPPort, defaultHTTPListenAddr), "HTTP listen address")

func main() {
	flag.Parse()

	tokenSecret := strings.TrimSpace(os.Getenv(envSessionTokenSecret))
	if tokenSecret == "" {
		log.Fatalf("missing required environment variable %s", envSessionTokenSecret)
	}

	tokenTTL, err := time.ParseDuration(envOrDefault(envSessionTokenTTL, defaultSessionTokenTTL))
	if err != nil {
		log.Fatalf("parse %s: %v", envSessionTokenTTL, err)
	}

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

	resolver, err := solver.NewDeployStatefulResolver()
	if err != nil {
		log.Fatalf("create deploy stateful resolver: %v", err)
	}
	target, err := solver.ParseTarget("game/gateway:http")
	if err != nil {
		log.Fatalf("parse gateway target: %v", err)
	}
	gatewayReg := gateway.NewDeployRegistry(resolver, target, publicHostPattern)
	tokenIssuer := token.NewHMACSigner(tokenSecret, tokenTTL)

	// Create handler.
	svc := service.NewSessionService(repo, tokenIssuer, gatewayReg)
	handler := session.NewHandler(svc)

	// Server component.
	httpMux := runtime.NewServeMux(grpc.GatewayDefault()...)
	session.RegisterSessionServiceHandlerServer(context.Background(), httpMux, handler)
	httpServer := &http.Server{Addr: httpAddr, Handler: phttp.Handler(httpMux, "session-http")}

	bs := bootstrap.New(bootstrap.WithShutdownTimeout(defaultShutdownDeadline))
	bs.Register(bootstrap.OTel())
	bs.Register(bootstrap.MongoClient("mongo", client))
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
