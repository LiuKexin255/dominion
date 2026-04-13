//go:build cmd

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
	deploy "dominion/projects/infra/deploy"
	"dominion/projects/infra/deploy/storage"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	grpcgo "google.golang.org/grpc"
)

const (
	defaultGRPCListenAddr = ":8080"
	defaultHTTPListenAddr = ":8081"
)

var (
	grpcPort = flag.String("grpc-port", listenAddrFromEnv("PORT", defaultGRPCListenAddr), "gRPC port or listen address")
	httpPort = flag.String("http-port", listenAddrFromEnv("HTTP_PORT", defaultHTTPListenAddr), "HTTP port or listen address")
)

func main() {
	flag.Parse()

	repo := storage.NewMemoryRepository()
	reconciler := deploy.NewReconciler(repo)
	handler := deploy.NewHandler(repo, reconciler)

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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 2)

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
