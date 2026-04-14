package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"dominion/pkg/grpc"
	"dominion/pkg/mongo"
	"dominion/projects/infra/deploy"
	"dominion/projects/infra/deploy/domain"
	"dominion/projects/infra/deploy/runtime/k8s"
	"dominion/projects/infra/deploy/storage"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	mongodriver "go.mongodb.org/mongo-driver/mongo"
	grpcgo "google.golang.org/grpc"
)

const (
	defaultGRPCListenAddr = ":8080"
	defaultHTTPListenAddr = ":8081"
	deployMongoTarget     = "deploy/mongo"
)

type mongoClientFactory func(target string, opts ...mongo.ClientOption) (*mongodriver.Client, error)

type repositoryFactory func(client *mongodriver.Client) (domain.Repository, error)

var (
	grpcPort = flag.String("grpc-port", listenAddrFromEnv("PORT", defaultGRPCListenAddr), "gRPC port or listen address")
	httpPort = flag.String("http-port", listenAddrFromEnv("HTTP_PORT", defaultHTTPListenAddr), "HTTP port or listen address")
)

func main() {
	flag.Parse()

	repo, err := newRepository(mongo.NewClient, storage.NewMongoRepository)
	if err != nil {
		log.Fatalf("create deploy repository: %v", err)
	}
	queue := domain.NewQueue()
	handler := deploy.NewHandler(repo, queue)
	runtimeClient, err := k8s.NewRuntimeClient()
	if err != nil {
		log.Fatalf("create deploy runtime client: %v", err)
	}
	runtimeImpl := k8s.NewK8sRuntime(runtimeClient)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 3)

	if err := domain.Recover(ctx, repo, queue); err != nil {
		log.Fatalf("recover deploy environments: %v", err)
	}
	worker := domain.NewWorker(repo, queue, runtimeImpl)
	go func() {
		<-ctx.Done()
		queue.Stop()
	}()
	go func() {
		if err := worker.Run(ctx); err != nil {
			errCh <- err
		}
	}()

	grpcListener, err := net.Listen("tcp", normalizeListenAddr(*grpcPort))
	if err != nil {
		log.Fatalf("listen on %s: %v", *grpcPort, err)
	}

	grpcServer := grpcgo.NewServer(grpc.ServiceDefault()...)
	deploy.RegisterDeployServiceServer(grpcServer, handler)

	httpMux := runtime.NewServeMux()
	if err := deploy.RegisterDeployServiceHandlerServer(context.Background(), httpMux, handler); err != nil {
		log.Fatalf("register HTTP gateway: %v", err)
	}
	httpServer := &http.Server{
		Addr:    normalizeListenAddr(*httpPort),
		Handler: httpMux,
	}

	go func() {
		<-ctx.Done()
		grpcServer.GracefulStop()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("shutdown HTTP gateway: %v", err)
		}
	}()

	go func() {
		log.Printf("deploy gRPC server listening on %s", normalizeListenAddr(*grpcPort))
		errCh <- grpcServer.Serve(grpcListener)
	}()

	go func() {
		log.Printf("deploy HTTP gateway listening on %s", normalizeListenAddr(*httpPort))
		errCh <- httpServer.ListenAndServe()
	}()

	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("serve deploy service: %v", err)
	}
}

const shutdownTimeout = 5 * time.Second

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
