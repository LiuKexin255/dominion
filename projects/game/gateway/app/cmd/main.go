package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"dominion/projects/game/gateway/app"
)

const (
	envHTTPPort           = "HTTP_PORT"
	envSessionTokenSecret = "SESSION_TOKEN_SECRET"

	defaultHTTPListenAddr = ":8080"
	defaultTokenSecret    = "dev-session-token-secret"
)

func main() {
	httpPort := envOrDefault(envHTTPPort, defaultHTTPListenAddr)
	tokenSecret := envOrDefault(envSessionTokenSecret, defaultTokenSecret)
	gatewayID := os.Getenv("HOSTNAME")
	if gatewayID == "" {
		gatewayID = "game-gateway-0"
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	bootstrap := app.NewBootstrap(tokenSecret, gatewayID)
	if err := app.Serve(ctx, bootstrap.Handler, bootstrap.WSHandler, normalizeListenAddr(httpPort)); err != nil {
		log.Fatalf("serve gateway: %v", err)
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
