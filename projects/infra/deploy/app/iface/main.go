package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"dominion/pkg/mongo"
	"dominion/projects/infra/deploy/app"
	"dominion/projects/infra/deploy/domain"
	"dominion/projects/infra/deploy/runtime"
	"dominion/projects/infra/deploy/storage"

	mongodriver "go.mongodb.org/mongo-driver/mongo"
)

const (
	defaultIfaceGRPCListenAddr = ":8080"
	defaultIfaceHTTPListenAddr = ":8081"
	deployIfaceMongoTarget     = "deploy/mongo"
)

type ifaceMongoClientFactory func(target string, opts ...mongo.ClientOption) (*mongodriver.Client, error)

type ifaceRepositoryFactory func(client *mongodriver.Client) (domain.Repository, error)

var (
	ifaceGRPCPort = flag.String("grpc-port", listenAddrFromEnv("PORT", defaultIfaceGRPCListenAddr), "gRPC port or listen address")
	ifaceHTTPPort = flag.String("http-port", listenAddrFromEnv("HTTP_PORT", defaultIfaceHTTPListenAddr), "HTTP port or listen address")
)

func main() {
	flag.Parse()

	repo, err := newIfaceRepository(mongo.NewClient, storage.NewMongoRepository)
	if err != nil {
		log.Fatalf("create deploy repository: %v", err)
	}

	fakeRuntime := runtime.NewFakeRuntime()
	configureFakeRuntime(fakeRuntime)
	app.SetAdminHTTPHandler(fakeRuntime.AdminHandler())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	bootstrap, err := app.NewBootstrap(ctx, repo, fakeRuntime)
	if err != nil {
		log.Fatalf("bootstrap deploy service: %v", err)
	}
	if err := bootstrap.Start(ctx); err != nil {
		log.Fatalf("start deploy bootstrap: %v", err)
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- app.Serve(ctx, bootstrap.Handler, normalizeListenAddr(*ifaceGRPCPort), normalizeListenAddr(*ifaceHTTPPort))
	}()

	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("serve deploy service: %v", err)
	}
}

func newIfaceRepository(newClient ifaceMongoClientFactory, newMongoRepository ifaceRepositoryFactory) (domain.Repository, error) {
	client, err := newClient(deployIfaceMongoTarget)
	if err != nil {
		return nil, err
	}

	repo, err := newMongoRepository(client)
	if err != nil {
		return nil, err
	}

	return repo, nil
}

func configureFakeRuntime(fakeRuntime *runtime.FakeRuntime) {
	if value := os.Getenv("APPLY_ERROR"); value != "" {
		fakeRuntime.SetApplyError(errors.New(value))
	}
	if value := os.Getenv("DELETE_ERROR"); value != "" {
		fakeRuntime.SetDeleteError(errors.New(value))
	}
	if value := os.Getenv("FAIL_APPLY"); value != "" {
		if err := failureError("apply", value); err != nil {
			fakeRuntime.SetApplyError(err)
		}
	}
	if value := os.Getenv("FAIL_DELETE"); value != "" {
		if err := failureError("delete", value); err != nil {
			fakeRuntime.SetDeleteError(err)
		}
	}
}

func failureError(kind, value string) error {
	parsed, err := strconv.ParseBool(value)
	if err == nil {
		if !parsed {
			return nil
		}
		return errors.New(kind + " failed")
	}
	if strings.EqualFold(value, "") || strings.EqualFold(value, "false") {
		return nil
	}
	return errors.New(value)
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
