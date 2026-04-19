package domain

import (
	"context"
	"errors"
)

// EnvironmentRuntime reconciles domain environments with the runtime.
type EnvironmentRuntime interface {
	Apply(ctx context.Context, env *Environment, progress func(msg string)) error
	Delete(ctx context.Context, envName EnvironmentName) error
	QueryServiceEndpoints(ctx context.Context, envLabel string, app string, service string) (*ServiceQueryResult, error)
	QueryStatefulServiceEndpoints(ctx context.Context, envLabel string, app string, service string) (*ServiceQueryResult, error)
}

// Worker drains the queue and reconciles the latest environment snapshot.
type Worker struct {
	repo    Repository
	queue   *Queue
	runtime EnvironmentRuntime
}

// NewWorker constructs a worker backed by the repository, queue, and runtime.
func NewWorker(repo Repository, queue *Queue, runtime EnvironmentRuntime) *Worker {
	return &Worker{
		repo:    repo,
		queue:   queue,
		runtime: runtime,
	}
}

// Run drains queued environment names until the context is cancelled or the
// queue is stopped.
func (w *Worker) Run(ctx context.Context) error {
	for {
		envName, ok := w.queue.Dequeue(ctx)
		if !ok {
			return nil
		}

		if err := w.process(ctx, envName); err != nil {
			return err
		}
	}
}

func (w *Worker) process(ctx context.Context, envName EnvironmentName) error {
	env, err := w.repo.Get(ctx, envName)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}

	switch env.Status().State {
	case StateDeleting:
		return w.handleDelete(ctx, env)
	case StateReconciling:
		return w.handleApply(ctx, env)
	default:
		return nil
	}
}

func (w *Worker) handleDelete(ctx context.Context, env *Environment) error {
	if err := w.runtime.Delete(ctx, env.Name()); err != nil {
		if setErr := env.SetStatusMessage(err.Error()); setErr != nil {
			return setErr
		}
		return w.repo.Save(ctx, env)
	}

	return w.repo.Delete(ctx, env.Name())
}

func (w *Worker) handleApply(ctx context.Context, env *Environment) error {
	progress := func(msg string) {
		if err := env.SetReconcilingMessage(msg); err != nil {
			return
		}
		if err := w.repo.Save(ctx, env); err != nil {
			return
		}
	}

	if err := w.runtime.Apply(ctx, env, progress); err != nil {
		if ctx.Err() != nil {
			return err
		}
		if markErr := env.MarkFailed(err.Error()); markErr != nil {
			return markErr
		}
		return w.repo.Save(ctx, env)
	}

	if err := env.MarkReady(); err != nil {
		return err
	}

	return w.repo.Save(ctx, env)
}
