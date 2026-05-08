package main

import (
	"context"
	"flag"
	"log"
	"os"
	"strings"
	"time"

	"dominion/common/gopkg/bootstrap"
	"dominion/common/gopkg/mongo"
	"dominion/projects/infra/deploy/app"
	"dominion/projects/infra/deploy/runtime/k8s"
	"dominion/projects/infra/deploy/storage"
)

const (
	defaultHTTPListenAddr   = ":8081"
	deployMongoTarget       = "deploy/mongo"
	defaultShutdownDeadline = 5 * time.Second
)

var httpPort = flag.String("http-port", listenAddrFromEnv("HTTP_PORT", defaultHTTPListenAddr), "HTTP port or listen address")

func main() {
	flag.Parse()

	client, err := mongo.NewClient(deployMongoTarget, mongo.WithK8sResolver())
	if err != nil {
		log.Fatalf("create mongo client: %v", err)
	}

	repo, err := storage.NewMongoRepository(client)
	if err != nil {
		log.Fatalf("create deploy repository: %v", err)
	}

	runtimeClient, err := k8s.NewRuntimeClient()
	if err != nil {
		log.Fatalf("create deploy runtime client: %v", err)
	}
	runtimeImpl := k8s.NewK8sRuntime(runtimeClient)

	b := app.NewBootstrap(repo, runtimeImpl)
	components, err := b.Components(normalizeListenAddr(*httpPort))
	if err != nil {
		log.Fatalf("create deploy components: %v", err)
	}

	bs := bootstrap.New(bootstrap.WithShutdownTimeout(defaultShutdownDeadline))
	bs.Register(bootstrap.MongoClient("mongo", client))
	for _, c := range components {
		bs.Register(c)
	}
	if err := bs.Run(context.Background()); err != nil {
		log.Fatalf("run deploy: %v", err)
	}
}

func listenAddrFromEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return normalizeListenAddr(value)
	}

	return fallback
}

func normalizeListenAddr(value string) string {
	if strings.HasPrefix(value, ":") {
		return value
	}

	return ":" + value
}
