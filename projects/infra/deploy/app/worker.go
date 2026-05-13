package app

import (
	"context"
	"errors"
	"fmt"

	"dominion/common/gopkg/bootstrap"
	"dominion/common/gopkg/logs"
	"dominion/projects/infra/deploy/domain"
	"dominion/projects/infra/deploy/service"
)

// DeployWorkerBuilder implements bootstrap.WorkerBuilder for the deploy worker.
type DeployWorkerBuilder struct {
	Repo    domain.Repository
	Runtime domain.EnvironmentRuntime
	Queue   *domain.Queue
}

// Build creates a new Worker instance. Recover is called on each build to
// rehydrate the persistent queue from the repository.
func (b *DeployWorkerBuilder) Build(ctx context.Context) (bootstrap.Worker, error) {
	if err := domain.Recover(ctx, b.Repo, b.Queue); err != nil {
		return nil, fmt.Errorf("build deploy worker: %w", err)
	}
	logs.Info(ctx, "deploy worker built, recovery complete")
	reconciler := service.NewReconcileService(b.Repo, b.Runtime)
	w := domain.NewWorker(b.Queue, reconciler)
	return &workerAdapter{worker: w}, nil
}

// ClassifyError maps domain errors to daemon decisions.
func (b *DeployWorkerBuilder) ClassifyError(_ context.Context, err error) bootstrap.DaemonDecision {
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
