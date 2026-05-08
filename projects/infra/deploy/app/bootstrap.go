// Package app provides shared bootstrap logic for the deploy service.
package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"dominion/common/gopkg/bootstrap"
	"dominion/common/gopkg/grpc"
	"dominion/projects/infra/deploy"
	"dominion/projects/infra/deploy/domain"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	grpcgo "google.golang.org/grpc"
)

// Bootstrap holds the shared components for the deploy service.
type Bootstrap struct {
	Repo    domain.Repository
	Runtime domain.EnvironmentRuntime
	Handler *deploy.Handler
	queue   *domain.Queue
}

// NewBootstrap creates the deploy service handler with a persistent Queue.
// The Queue is shared between Handler (enqueue) and Worker (dequeue).
func NewBootstrap(repo domain.Repository, runtime domain.EnvironmentRuntime) *Bootstrap {
	queue := domain.NewQueue()
	handler := deploy.NewHandler(repo, queue, runtime)
	return &Bootstrap{Repo: repo, Runtime: runtime, Handler: handler, queue: queue}
}

// Components returns bootstrap components assembled from existing bootstrap types.
func (b *Bootstrap) Components(httpAddr string) ([]bootstrap.Component, error) {
	// gRPC server for handler registration only (gRPC-gateway in-process).
	grpcServer := grpcgo.NewServer(grpc.ServiceDefault()...)
	deploy.RegisterDeployServiceServer(grpcServer, b.Handler)

	httpMux := runtime.NewServeMux()
	_ = deploy.RegisterDeployServiceHandlerServer(context.Background(), httpMux, b.Handler)
	httpServer := &http.Server{Addr: httpAddr, Handler: httpMux}

	server := bootstrap.HTTPServer("deploy-http", httpServer)

	builder := &deployWorkerBuilder{
		repo:    b.Repo,
		runtime: b.Runtime,
		queue:   b.queue,
	}
	daemon := bootstrap.Daemon("deploy-worker", builder,
		bootstrap.WithDaemonErrorClassifier(builder.classifyError),
	)

	return []bootstrap.Component{server, daemon}, nil
}

// deployWorkerBuilder implements bootstrap.WorkerBuilder.
type deployWorkerBuilder struct {
	repo    domain.Repository
	runtime domain.EnvironmentRuntime
	queue   *domain.Queue
}

// Build creates a new Worker instance. Recover is called on each build to
// rehydrate the persistent queue from the repository.
func (b *deployWorkerBuilder) Build(ctx context.Context) (bootstrap.Worker, error) {
	if err := domain.Recover(ctx, b.repo, b.queue); err != nil {
		return nil, fmt.Errorf("build deploy worker: %w", err)
	}
	worker := domain.NewWorker(b.repo, b.queue, b.runtime)
	return &workerAdapter{worker: worker}, nil
}

// classifyError maps domain errors to daemon decisions.
func (b *deployWorkerBuilder) classifyError(_ context.Context, err error) bootstrap.DaemonDecision {
	if err == nil {
		return bootstrap.DaemonStop
	}
	if errors.Is(err, domain.ErrWorkerFatal) {
		return bootstrap.DaemonFatal
	}
	return bootstrap.DaemonRestart
}

// workerAdapter adapts domain.Worker to the bootstrap.Worker interface.
type workerAdapter struct {
	worker *domain.Worker
}

// Start blocks until the worker exits.
func (a *workerAdapter) Start(ctx context.Context) error {
	return a.worker.Run(ctx)
}

// Stop is a no-op. The daemon cancels its context on shutdown, which causes
// Worker.Run to exit via Queue.Dequeue's context check.
func (a *workerAdapter) Stop(_ context.Context) error {
	return nil
}
