package domain

import (
	"context"
	"errors"
	"time"

	"dominion/common/gopkg/logs"
	"dominion/common/gopkg/logs/event"
	"dominion/common/gopkg/otel"
)

const (
	defaultIterTimeout = 5 * time.Minute
	maxRetryDelay      = 30 * time.Second
	defaultMaxRetries  = 15

	spanProcess = "deploy.worker.process"

	logFieldEnvName    = "env_name"
	logFieldRetryCount = "retry_count"
)

// EnvironmentRuntime reconciles domain environments with the runtime.
type EnvironmentRuntime interface {
	// ApplyResources submits the environment's desired state to the runtime (Kubernetes)
	// by creating/updating/pruning resources. It does NOT wait for rollout.
	ApplyResources(ctx context.Context, env *Environment) error
	// CheckRollout queries the runtime for rollout status of all workloads in the environment.
	// Returns a tri-state result: Ready, Waiting(message), or Failed(message).
	CheckRollout(ctx context.Context, env *Environment) (*RolloutStatus, error)
	Delete(ctx context.Context, envName EnvironmentName) error
	QueryServiceEndpoints(ctx context.Context, envLabel string, app string, service string) (*ServiceQueryResult, error)
	QueryStatefulServiceEndpoints(ctx context.Context, envLabel string, app string, service string) (*ServiceQueryResult, error)
	ReservedEnvironmentVariableNames(ctx context.Context) ([]string, error)
}

// ReconcileService handles single-step environment reconciliation.
type ReconcileService interface {
	ProcessOne(ctx context.Context, envName EnvironmentName) (*ProcessResult, error)
	MarkRetryExhausted(ctx context.Context, envName EnvironmentName) (*ProcessResult, error)
}

// Worker drains the queue and calls ProcessOne for each work item.
type Worker struct {
	queue      *Queue
	reconciler ReconcileService
	maxRetries int

	iterTimeout time.Duration
	after       func(time.Duration) <-chan time.Time
}

// NewWorker constructs a worker backed by the queue and reconciler.
func NewWorker(queue *Queue, reconciler ReconcileService) *Worker {
	return &Worker{
		queue:       queue,
		reconciler:  reconciler,
		maxRetries:  defaultMaxRetries,
		iterTimeout: defaultIterTimeout,
		after:       time.After,
	}
}

// Run drains queued environment names until the queue is stopped or ctx is
// cancelled.
//
// Each dequeued item is processed with its own short-lived timeout context
// derived from ctx. Iteration errors are handled internally so the daemon keeps
// running; only a panic from processing terminates the goroutine naturally.
func (w *Worker) Run(ctx context.Context) error {
	for {
		item, ok := w.queue.Dequeue(ctx)
		if !ok {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return nil
		}

		iterCtx, cancel := context.WithTimeout(ctx, w.iterTimeout)

		logs.Info(iterCtx, "dequeue environment",
			event.String(logFieldEnvName, item.EnvName.String()),
			event.Int(logFieldRetryCount, item.RetryCount),
		)

		iterCtx, processSpan := otel.Tracer().Start(iterCtx, spanProcess)
		result, processErr := w.reconciler.ProcessOne(iterCtx, item.EnvName)
		processSpan.End()
		cancel()
		w.queue.Complete(item.EnvName)

		switch {
		case processErr == nil:
			w.maybeRequeue(ctx, item, result)
			continue
		case errors.Is(processErr, ErrRetryCounted):
			if item.RetryCount >= w.maxRetries {
				logs.Error(ctx, "max retry exceeded, marking environment as failed",
					event.String(logFieldEnvName, item.EnvName.String()),
					event.Int(logFieldRetryCount, item.RetryCount),
				)
				result, err := w.reconciler.MarkRetryExhausted(ctx, item.EnvName)
				if err != nil {
					logs.Error(ctx, "failed to mark retry exhausted",
						event.String(logFieldEnvName, item.EnvName.String()),
						event.Err(err),
					)
				}
				w.maybeRequeue(ctx, item, result)
				continue
			}
			logs.Warn(ctx, "process failed, scheduling retry",
				event.String(logFieldEnvName, item.EnvName.String()),
				event.Int(logFieldRetryCount, item.RetryCount),
			)
			_ = w.queue.EnqueueAfter(ctx, &WorkItem{
				EnvName:    item.EnvName,
				RetryCount: item.RetryCount + 1,
				Source:     WorkItemSourceRetry,
			}, retryBackoff(item.RetryCount))
		case errors.Is(processErr, context.Canceled) || errors.Is(processErr, context.DeadlineExceeded):
			continue
		default:
			logs.Error(ctx, "worker process error",
				event.String(logFieldEnvName, item.EnvName.String()),
				event.Err(processErr),
			)
			continue
		}
	}
}

// maybeRequeue re-enqueues the work item if the result indicates it should continue.
func (w *Worker) maybeRequeue(ctx context.Context, item *WorkItem, result *ProcessResult) {
	if result == nil || result.Terminal {
		return
	}
	if result.Changed || result.RequeueAfter > 0 {
		_ = w.queue.EnqueueAfter(ctx, &WorkItem{
			EnvName:    item.EnvName,
			RetryCount: item.RetryCount,
			Source:     result.Source,
		}, result.RequeueAfter)
	}
}

func retryBackoff(retryCount int) time.Duration {
	delay := time.Second * time.Duration(1<<retryCount)
	if delay > maxRetryDelay {
		return maxRetryDelay
	}
	return delay
}
