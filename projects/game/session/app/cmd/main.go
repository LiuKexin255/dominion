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
	"dominion/projects/game/pkg/token"
	"dominion/projects/game/session/app"
	"dominion/projects/game/session/runtime/gateway"
	"dominion/projects/game/session/runtime/storage"
)

const (
	envHTTPPort            = "HTTP_PORT"
	envSessionTokenSecret  = "SESSION_TOKEN_SECRET"
	envSessionTokenTTL     = "SESSION_TOKEN_TTL"
	envGameGatewayIDs      = "GAME_GATEWAY_IDS"
	envGameGatewayDomain   = "GAME_GATEWAY_DOMAIN"
	envSessionMongoTarget  = "SESSION_MONGO_TARGET"

	defaultHTTPListenAddr      = ":8081"
	defaultMongoTarget        = "game/mongo"
	defaultTokenSecret        = "dev-session-token-secret"
	defaultSessionTokenTTL    = "1h"
	defaultGatewayIDs         = "game-gateway-0"
	defaultGatewayDomain      = "gw.liukexin.com"
	defaultShutdownDeadline   = 5 * time.Second
)

var httpPort = flag.String("http-port", envOrDefault(envHTTPPort, defaultHTTPListenAddr), "HTTP listen address")

func main() {
	flag.Parse()

	tokenSecret := envOrDefault(envSessionTokenSecret, defaultTokenSecret)

	tokenTTL, err := time.ParseDuration(envOrDefault(envSessionTokenTTL, defaultSessionTokenTTL))
	if err != nil {
		log.Fatalf("parse %s: %v", envSessionTokenTTL, err)
	}

	gatewayIDs := parseCSVEnvOrDefault(envGameGatewayIDs, defaultGatewayIDs)
	gatewayDomain := envOrDefault(envGameGatewayDomain, defaultGatewayDomain)
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

	gatewayReg := gateway.NewStaticRegistry(gatewayIDs)
	tokenIssuer := token.NewHMACSigner(tokenSecret, tokenTTL)
	bootstrap := app.NewBootstrap(repo, tokenIssuer, gatewayReg, gatewayDomain)

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

func parseCSVEnvOrDefault(key, fallback string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return strings.Split(fallback, ",")
	}

	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			values = append(values, value)
		}
	}

	return values
}

func parseCSVEnv(key string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			values = append(values, value)
		}
	}

	return values
}

func normalizeListenAddr(value string) string {
	if strings.HasPrefix(value, ":") {
		return value
	}

	return ":" + value
}
