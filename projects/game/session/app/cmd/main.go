package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"dominion/pkg/mongo"
	"dominion/pkg/solver"
	"dominion/projects/game/pkg/token"
	"dominion/projects/game/session/app"
	"dominion/projects/game/session/runtime/gateway"
	"dominion/projects/game/session/runtime/storage"
)

const (
	envHTTPPort           = "HTTP_PORT"
	envSessionTokenSecret = "SESSION_TOKEN_SECRET"
	envSessionTokenTTL    = "SESSION_TOKEN_TTL"
	envSessionMongoTarget = "SESSION_MONGO_TARGET"

	defaultHTTPListenAddr   = ":8081"
	defaultMongoTarget      = "game/mongo"
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

	mongoTarget := envOrDefault(envSessionMongoTarget, defaultMongoTarget)
	httpAddr := normalizeListenAddr(*httpPort)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client, err := mongo.NewClient(mongoTarget)
	if err != nil {
		log.Fatalf("create mongo client: %v", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), defaultShutdownDeadline)
		defer cancel()
		if err := client.Disconnect(shutdownCtx); err != nil {
			log.Printf("disconnect mongo client: %v", err)
		}
	}()

	coll := storage.NewMongoCollection(client.Database(storage.DatabaseName).Collection(storage.CollectionName))

	repo, err := storage.NewMongoRepository(ctx, coll)
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
	bootstrap := app.NewBootstrap(repo, tokenIssuer, gatewayReg)

	if err := app.Serve(ctx, bootstrap.Handler, httpAddr); err != nil {
		log.Fatalf("serve session service: %v", err)
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
