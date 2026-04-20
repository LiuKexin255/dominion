package domain

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	defaultMaxRetries  = 5
	defaultIterTimeout = 5 * time.Minute
	maxRetryDelay      = 30 * time.Second
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

	maxRetries  int
	iterTimeout time.Duration
	after       func(time.Duration) <-chan time.Time
}

// NewWorker constructs a worker backed by the repository, queue, and runtime.
func NewWorker(repo Repository, queue *Queue, runtime EnvironmentRuntime) *Worker {
	return &Worker{
		repo:        repo,
		queue:       queue,
		runtime:     runtime,
		maxRetries:  defaultMaxRetries,
		iterTimeout: defaultIterTimeout,
		after:       time.After,
	}
}

// Run drains queued environment names until the queue is stopped.
//
// Each dequeued item is processed with its own short-lived timeout context.
// Iteration errors are handled internally so the daemon keeps running; only a
// panic from processing terminates the goroutine naturally.
func (w *Worker) Run() {
	for {
		item, ok := w.queue.Dequeue(context.Background())
		if !ok {
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), w.iterTimeout)
		err := w.process(ctx, item)
		cancel()
		w.queue.Complete(item.EnvName)

		switch {
		case err == nil:
			continue
		case errors.Is(err, ErrRetryCounted):
			if item.RetryCount >= w.maxRetries {
				continue
			}
			w.scheduleRetry(context.Background(), &WorkItem{EnvName: item.EnvName, RetryCount: item.RetryCount + 1}, retryBackoff(item.RetryCount))
		default:
			continue
		}
	}
}

func retryBackoff(retryCount int) time.Duration {
	delay := time.Second * time.Duration(1<<retryCount)
	if delay > maxRetryDelay {
		return maxRetryDelay
	}
	return delay
}

func (w *Worker) scheduleRetry(ctx context.Context, item *WorkItem, delay time.Duration) {
	go func() {
		if delay > 0 {
			select {
			case <-ctx.Done():
				return
			case <-w.after(delay):
			}
		}

		if ctx.Err() != nil {
			return
		}

		_ = w.queue.EnqueueRetry(ctx, item)
	}()
}

func (w *Worker) process(ctx context.Context, item *WorkItem) error {
	env, err := w.repo.Get(ctx, item.EnvName)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return fmt.Errorf("%w: load environment %s: %v", ErrWorkerFatal, item.EnvName, err)
	}
	processedGeneration := env.Generation()

	var processErr error
	switch env.Status().Desired {
	case DesiredPresent:
		processErr = w.processPresent(ctx, env, processedGeneration)
	case DesiredAbsent:
		processErr = w.processAbsent(ctx, env)
	default:
		return fmt.Errorf("%w: unsupported desired state %v for %s", ErrWorkerFatal, env.Status().Desired, env.Name())
	}

	return processErr
}

func (w *Worker) processPresent(ctx context.Context, env *Environment, processedGeneration int64) error {
	switch env.Status().State {
	case StatePending, StateReady, StateFailed:
		if env.Status().State == StateReady && env.Status().ObservedGeneration == processedGeneration {
			return nil
		}
		if err := env.MarkReconciling(); err != nil {
			return fmt.Errorf("%w: mark reconciling %s: %v", ErrWorkerFatal, env.Name(), err)
		}
		if err := w.repo.Save(ctx, env); err != nil {
			return fmt.Errorf("%w: save reconciling %s: %v", ErrWorkerFatal, env.Name(), err)
		}
		return w.applyPresent(ctx, env, processedGeneration)
	case StateReconciling:
		return w.applyPresent(ctx, env, processedGeneration)
	default:
		return fmt.Errorf("%w: unsupported present state %v for %s", ErrWorkerFatal, env.Status().State, env.Name())
	}
}

func (w *Worker) applyPresent(ctx context.Context, env *Environment, processedGeneration int64) error {
	progress := func(msg string) {
		_ = env.SetReconcilingMessage(msg)
	}

	if err := w.runtime.Apply(ctx, env, progress); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		if markErr := env.MarkFailed(processedGeneration, err.Error()); markErr != nil {
			return fmt.Errorf("%w: mark failed %s: %v", ErrWorkerFatal, env.Name(), markErr)
		}
		if saveErr := w.repo.Save(ctx, env); saveErr != nil {
			return fmt.Errorf("%w: save failed status %s: %v", ErrWorkerFatal, env.Name(), saveErr)
		}
		return fmt.Errorf("%w: apply %s: %v", ErrRetryCounted, env.Name(), err)
	}

	if err := env.MarkReady(processedGeneration); err != nil {
		return fmt.Errorf("%w: mark ready %s: %v", ErrWorkerFatal, env.Name(), err)
	}

	if err := w.repo.Save(ctx, env); err != nil {
		return fmt.Errorf("%w: save ready %s: %v", ErrWorkerFatal, env.Name(), err)
	}

	return nil
}

func (w *Worker) processAbsent(ctx context.Context, env *Environment) error {
	switch env.Status().State {
	case StatePending, StateReady, StateReconciling, StateFailed:
		if err := env.MarkDeleting(); err != nil {
			return fmt.Errorf("%w: mark deleting %s: %v", ErrWorkerFatal, env.Name(), err)
		}
		if err := w.repo.Save(ctx, env); err != nil {
			return fmt.Errorf("%w: save deleting %s: %v", ErrWorkerFatal, env.Name(), err)
		}
		return w.deleteAbsent(ctx, env)
	case StateDeleting:
		return w.deleteAbsent(ctx, env)
	default:
		return fmt.Errorf("%w: unsupported absent state %v for %s", ErrWorkerFatal, env.Status().State, env.Name())
	}
}

func (w *Worker) deleteAbsent(ctx context.Context, env *Environment) error {
	if err := w.runtime.Delete(ctx, env.Name()); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		if setErr := env.SetStatusMessage(err.Error()); setErr != nil {
			return fmt.Errorf("%w: set deleting message %s: %v", ErrWorkerFatal, env.Name(), setErr)
		}
		if saveErr := w.repo.Save(ctx, env); saveErr != nil {
			return fmt.Errorf("%w: save deleting failure %s: %v", ErrWorkerFatal, env.Name(), saveErr)
		}
		return fmt.Errorf("%w: delete %s: %v", ErrRetryCounted, env.Name(), err)
	}

	if err := w.repo.Delete(ctx, env.Name()); err != nil {
		return fmt.Errorf("%w: delete environment %s: %v", ErrWorkerFatal, env.Name(), err)
	}

	return nil
}
