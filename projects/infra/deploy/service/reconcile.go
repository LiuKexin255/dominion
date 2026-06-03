// Package service implements the deploy service application layer.
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"dominion/projects/infra/deploy/domain"
)

const (
	// rolloutPollInterval is the delay between rollout status checks.
	rolloutPollInterval = 5 * time.Second
)

// ReconcileService reconciles environments toward their desired state one step
// at a time. Each call to ProcessOne advances at most one persistent state
// transition.
type ReconcileService struct {
	repo    domain.Repository
	runtime domain.EnvironmentRuntime
}

// NewReconcileService constructs a ReconcileService backed by repo and runtime.
func NewReconcileService(repo domain.Repository, runtime domain.EnvironmentRuntime) *ReconcileService {
	return &ReconcileService{repo: repo, runtime: runtime}
}

// ProcessOne loads the environment and advances its state machine by at most one
// persistent step. It returns a ProcessResult describing what happened, or an
// error when processing cannot proceed.
//
// Error semantics:
//   - error wrapping domain.ErrRetryCounted signals the worker to retry with backoff.
//   - other errors are treated as transient and may be retried.
//   - on error, ProcessResult is zero-valued and should be ignored.
//
// Result semantics:
//   - ProcessResult.Changed is true when a persistent state transition occurred.
//   - ProcessResult.Terminal is true when no further processing is needed.
//   - ProcessResult.RequeueAfter specifies a delay before the next ProcessOne call.
func (s *ReconcileService) ProcessOne(ctx context.Context, envName domain.EnvironmentName) (*domain.ProcessResult, error) {
	env, err := s.repo.Get(ctx, envName)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return &domain.ProcessResult{Terminal: true}, nil
		}
		return nil, fmt.Errorf("load %s: %w", envName, err)
	}

	switch env.Status().Desired {
	case domain.DesiredPresent:
		return s.processPresent(ctx, env)
	case domain.DesiredAbsent:
		return s.processAbsent(ctx, env)
	default:
		return nil, fmt.Errorf("unsupported desired state %v for %s", env.Status().Desired, envName)
	}
}

// MarkRetryExhausted transitions an environment to Failed after the worker's
// max retry count has been exceeded. It returns Terminal=true so the worker
// stops re-enqueuing the item.
func (s *ReconcileService) MarkRetryExhausted(ctx context.Context, envName domain.EnvironmentName) (*domain.ProcessResult, error) {
	env, err := s.repo.Get(ctx, envName)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return &domain.ProcessResult{Terminal: true}, nil
		}
		return nil, fmt.Errorf("load %s for retry exhausted: %w", envName, err)
	}

	if err := s.repo.TransitionStatus(ctx, envName, env.Generation(), env.Status().State, &domain.EnvironmentStatus{
		State:              domain.StateFailed,
		ObservedGeneration: env.Generation(),
		Message:            "retry count exhausted",
	}); err != nil {
		if errors.Is(err, domain.ErrStaleState) || errors.Is(err, domain.ErrStaleGeneration) {
			return &domain.ProcessResult{RequeueAfter: 1}, nil
		}
		return nil, err
	}
	return &domain.ProcessResult{Changed: true, Terminal: true}, nil
}

// processPresent handles environments that should exist.
func (s *ReconcileService) processPresent(ctx context.Context, env *domain.Environment) (*domain.ProcessResult, error) {
	gen := env.Generation()

	switch env.Status().State {
	case domain.StatePending:
		return s.transitionToReconciling(ctx, env)
	case domain.StateReady:
		if env.Status().ObservedGeneration == gen {
			return &domain.ProcessResult{Terminal: true}, nil
		}
		return s.transitionToReconciling(ctx, env)
	case domain.StateFailed:
		return s.transitionToReconciling(ctx, env)
	case domain.StateReconciling:
		return s.applyAndWait(ctx, env)
	case domain.StateWaitingRollout:
		return s.checkRollout(ctx, env)
	default:
		return nil, fmt.Errorf("unsupported present state %v for %s", env.Status().State, env.Name())
	}
}

// transitionToReconciling advances the environment from Pending/Ready/Failed to
// Reconciling.
func (s *ReconcileService) transitionToReconciling(ctx context.Context, env *domain.Environment) (*domain.ProcessResult, error) {
	if err := s.repo.TransitionStatus(ctx, env.Name(), env.Generation(), env.Status().State, &domain.EnvironmentStatus{State: domain.StateReconciling, Desired: env.Status().Desired}); err != nil {
		if errors.Is(err, domain.ErrStaleState) || errors.Is(err, domain.ErrStaleGeneration) {
			return &domain.ProcessResult{RequeueAfter: 1}, nil
		}
		return nil, err
	}
	return &domain.ProcessResult{Changed: true}, nil
}

// applyAndWait applies resources and, on success, transitions to
// WaitingRollout.
func (s *ReconcileService) applyAndWait(ctx context.Context, env *domain.Environment) (*domain.ProcessResult, error) {
	if err := s.runtime.ApplyResources(ctx, env); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		// apply failure does not transition to Failed; state stays Reconciling.
		return nil, fmt.Errorf("%w: apply: %v", domain.ErrRetryCounted, err)
	}

	if err := s.repo.TransitionStatus(ctx, env.Name(), env.Generation(), domain.StateReconciling, &domain.EnvironmentStatus{
		State: domain.StateWaitingRollout, Desired: domain.DesiredPresent,
		ObservedGeneration: env.Generation(),
	}); err != nil {
		if errors.Is(err, domain.ErrStaleState) || errors.Is(err, domain.ErrStaleGeneration) {
			return &domain.ProcessResult{RequeueAfter: 1}, nil
		}
		return nil, err
	}
	return &domain.ProcessResult{Changed: true, RequeueAfter: rolloutPollInterval, Source: domain.WorkItemSourcePoll}, nil
}

// checkRollout inspects rollout status and transitions accordingly.
func (s *ReconcileService) checkRollout(ctx context.Context, env *domain.Environment) (*domain.ProcessResult, error) {
	status, err := s.runtime.CheckRollout(ctx, env)
	if err != nil {
		return nil, err
	}

	switch status.State {
	case domain.RolloutReady:
		return s.markReadyFromRollout(ctx, env)
	case domain.RolloutFailed:
		return s.markFailedFromRollout(ctx, env, status.Message)
	default:
		return s.retainWaitingRollout(ctx, env, status.Message)
	}
}

// markReadyFromRollout transitions WaitingRollout to Ready.
func (s *ReconcileService) markReadyFromRollout(ctx context.Context, env *domain.Environment) (*domain.ProcessResult, error) {
	now := time.Now().UTC()
	if err := s.repo.TransitionStatus(ctx, env.Name(), env.Generation(), domain.StateWaitingRollout, &domain.EnvironmentStatus{
		State: domain.StateReady, Desired: domain.DesiredPresent,
		ObservedGeneration: env.Generation(), LastSuccessTime: now,
		Message: "ready",
	}); err != nil {
		if errors.Is(err, domain.ErrStaleState) || errors.Is(err, domain.ErrStaleGeneration) {
			return &domain.ProcessResult{RequeueAfter: 1}, nil
		}
		return nil, err
	}
	return &domain.ProcessResult{Changed: true, Terminal: true}, nil
}

// markFailedFromRollout transitions WaitingRollout to Failed.
func (s *ReconcileService) markFailedFromRollout(ctx context.Context, env *domain.Environment, message string) (*domain.ProcessResult, error) {
	if err := s.repo.TransitionStatus(ctx, env.Name(), env.Generation(), domain.StateWaitingRollout, &domain.EnvironmentStatus{
		State: domain.StateFailed, Desired: domain.DesiredPresent,
		ObservedGeneration: env.Generation(), Message: message,
	}); err != nil {
		if errors.Is(err, domain.ErrStaleState) || errors.Is(err, domain.ErrStaleGeneration) {
			return &domain.ProcessResult{RequeueAfter: 1}, nil
		}
		return nil, err
	}
	return &domain.ProcessResult{Changed: true, Terminal: true}, nil
}

// retainWaitingRollout keeps the environment in WaitingRollout, updating the
// message only when it differs from the current value.
func (s *ReconcileService) retainWaitingRollout(ctx context.Context, env *domain.Environment, message string) (*domain.ProcessResult, error) {
	if env.Status().Message == message {
		return &domain.ProcessResult{RequeueAfter: rolloutPollInterval, Source: domain.WorkItemSourcePoll}, nil
	}

	if err := s.repo.TransitionStatus(ctx, env.Name(), env.Generation(), domain.StateWaitingRollout, &domain.EnvironmentStatus{
		State: domain.StateWaitingRollout, Desired: domain.DesiredPresent, Message: message,
	}); err != nil {
		if errors.Is(err, domain.ErrStaleState) || errors.Is(err, domain.ErrStaleGeneration) {
			return &domain.ProcessResult{RequeueAfter: 1}, nil
		}
		return nil, err
	}
	return &domain.ProcessResult{RequeueAfter: rolloutPollInterval, Source: domain.WorkItemSourcePoll}, nil
}

// processAbsent handles environments that should be deleted.
func (s *ReconcileService) processAbsent(ctx context.Context, env *domain.Environment) (*domain.ProcessResult, error) {
	if env.Status().State != domain.StateDeleting {
		return s.transitionToDeleting(ctx, env)
	}
	return s.deleteAbsent(ctx, env)
}

// transitionToDeleting advances the environment to Deleting.
func (s *ReconcileService) transitionToDeleting(ctx context.Context, env *domain.Environment) (*domain.ProcessResult, error) {
	if err := s.repo.TransitionStatus(ctx, env.Name(), env.Generation(), env.Status().State, &domain.EnvironmentStatus{State: domain.StateDeleting, Desired: domain.DesiredAbsent}); err != nil {
		if errors.Is(err, domain.ErrStaleState) || errors.Is(err, domain.ErrStaleGeneration) {
			return &domain.ProcessResult{RequeueAfter: 1}, nil
		}
		return nil, err
	}
	return &domain.ProcessResult{Changed: true}, nil
}

// deleteAbsent performs the runtime deletion and removes the environment from
// the repository.
func (s *ReconcileService) deleteAbsent(ctx context.Context, env *domain.Environment) (*domain.ProcessResult, error) {
	if err := s.runtime.Delete(ctx, env.Name()); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: delete %s: %v", domain.ErrRetryCounted, env.Name(), err)
	}

	if err := s.repo.Delete(ctx, env.Name()); err != nil {
		return nil, err
	}
	return &domain.ProcessResult{Changed: true, Terminal: true}, nil
}
