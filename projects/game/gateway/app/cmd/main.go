package main

import (
	"context"
	"log"
	"os"
	"strings"
	"time"

	"dominion/common/gopkg/bootstrap"
	"dominion/projects/game/gateway/app"
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

	b := app.NewBootstrap(tokenSecret, gatewayID)

	bs := bootstrap.New(bootstrap.WithShutdownTimeout(5 * time.Second))
	if err := bs.Register(b.Component(normalizeListenAddr(httpPort))); err != nil {
		log.Fatalf("register gateway server: %v", err)
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
