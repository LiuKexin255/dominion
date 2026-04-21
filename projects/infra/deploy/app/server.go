// Package app provides shared bootstrap logic for the deploy service.
package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"dominion/pkg/grpc"
	"dominion/projects/infra/deploy"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	grpcgo "google.golang.org/grpc"
)

const shutdownTimeout = 5 * time.Second

// Serve starts the deploy gRPC and HTTP gateway servers.
func Serve(ctx context.Context, handler *deploy.Handler, httpAddr string) error {
	grpcServer := grpcgo.NewServer(grpc.ServiceDefault()...)
	deploy.RegisterDeployServiceServer(grpcServer, handler)

	httpMux := runtime.NewServeMux()
	if err := deploy.RegisterDeployServiceHandlerServer(context.Background(), httpMux, handler); err != nil {
		return fmt.Errorf("register HTTP gateway: %w", err)
	}
	httpServer := &http.Server{Addr: httpAddr, Handler: httpMux}

	errCh := make(chan error, 2)
	go func() {
		<-ctx.Done()
		grpcServer.GracefulStop()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("shutdown HTTP gateway: %w", err)
		}
	}()

	go func() {
		errCh <- httpServer.ListenAndServe()
	}()

	for i := 0; i < cap(errCh); i++ {
		if err := <-errCh; err != nil {
			return err
		}
	}

	return nil
}
