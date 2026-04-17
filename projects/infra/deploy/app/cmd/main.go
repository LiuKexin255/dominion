package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"dominion/pkg/mongo"
	"dominion/projects/infra/deploy/app"
	"dominion/projects/infra/deploy/domain"
	"dominion/projects/infra/deploy/runtime/k8s"
	"dominion/projects/infra/deploy/storage"

	mongodriver "go.mongodb.org/mongo-driver/mongo"
)

const (
	defaultGRPCListenAddr = ":8080"
	defaultHTTPListenAddr = ":8081"
	deployMongoTarget     = "deploy/mongo"
)

type mongoClientFactory func(target string, opts ...mongo.ClientOption) (*mongodriver.Client, error)

type repositoryFactory func(client *mongodriver.Client) (domain.Repository, error)

var (
	httpPort = flag.String("http-port", listenAddrFromEnv("HTTP_PORT", defaultHTTPListenAddr), "HTTP port or listen address")
)

func main() {
	flag.Parse()

	repo, err := newRepository(mongo.NewClient, storage.NewMongoRepository)
	if err != nil {
		log.Fatalf("create deploy repository: %v", err)
	}
	runtimeClient, err := k8s.NewRuntimeClient()
	if err != nil {
		log.Fatalf("create deploy runtime client: %v", err)
	}
	runtimeImpl := k8s.NewK8sRuntime(runtimeClient)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	bootstrap, err := app.NewBootstrap(ctx, repo, runtimeImpl)
	if err != nil {
		log.Fatalf("bootstrap deploy service: %v", err)
	}
	if err := bootstrap.Start(ctx); err != nil {
		log.Fatalf("start deploy bootstrap: %v", err)
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- app.Serve(ctx, bootstrap.Handler, normalizeListenAddr(*httpPort))
	}()

	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("serve deploy service: %v", err)
	}
}

func newRepository(newClient mongoClientFactory, newMongoRepository repositoryFactory) (domain.Repository, error) {
	client, err := newClient(deployMongoTarget)
	if err != nil {
		return nil, err
	}

	repo, err := newMongoRepository(client)
	if err != nil {
		return nil, err
	}

	return repo, nil
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
