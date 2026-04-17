// Package app provides shared bootstrap logic for the deploy service.
package app

import (
	"context"
	"fmt"

	"dominion/projects/infra/deploy"
	"dominion/projects/infra/deploy/domain"
)

// Bootstrap holds the shared components needed to run the deploy service.
type Bootstrap struct {
	Repo    domain.Repository
	Handler *deploy.Handler
	Queue   *domain.Queue
	Worker  *domain.Worker
}

// NewBootstrap creates all deploy service components: queue, handler, and worker.
// The runtime parameter is the infrastructure runtime implementation (e.g. k8s or fake).
func NewBootstrap(ctx context.Context, repo domain.Repository, runtime domain.EnvironmentRuntime) (*Bootstrap, error) {
	queue := domain.NewQueue()
	handler := deploy.NewHandler(repo, queue, runtime)

	if err := domain.Recover(ctx, repo, queue); err != nil {
		return nil, fmt.Errorf("recover deploy environments: %w", err)
	}

	worker := domain.NewWorker(repo, queue, runtime)

	return &Bootstrap{
		Repo:    repo,
		Handler: handler,
		Queue:   queue,
		Worker:  worker,
	}, nil
}

// Start launches the worker goroutine.
func (b *Bootstrap) Start(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		b.Queue.Stop()
	}()

	go func() {
		if err := b.Worker.Run(ctx); err != nil {
			panic(fmt.Sprintf("worker run: %v", err))
		}
	}()

	return nil
}

// Stop performs graceful shutdown of the queue.
func (b *Bootstrap) Stop() {
	b.Queue.Stop()
}
