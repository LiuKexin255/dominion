package service

import (
	"context"
	"errors"
	"fmt"

	"dominion/projects/infra/deploy/domain"
)

// Enqueuer enqueues environment reconciliation with user source semantics.
type Enqueuer interface {
	Enqueue(ctx context.Context, envName domain.EnvironmentName) error
}

// EnvironmentCommandService handles CRUD operations for environments.
type EnvironmentCommandService struct {
	repo    domain.Repository
	queue   Enqueuer
	runtime domain.EnvironmentRuntime
}

// NewEnvironmentCommandService constructs an EnvironmentCommandService backed by
// repo, queue, and runtime.
func NewEnvironmentCommandService(repo domain.Repository, queue Enqueuer, runtime domain.EnvironmentRuntime) *EnvironmentCommandService {
	return &EnvironmentCommandService{
		repo:    repo,
		queue:   queue,
		runtime: runtime,
	}
}

// Create creates a new environment and enqueues it for reconciliation.
// Returns domain.ErrAlreadyExists if the environment name is taken.
func (s *EnvironmentCommandService) Create(ctx context.Context, envName domain.EnvironmentName, envType domain.EnvironmentType, description string, desiredState *domain.DesiredState) (*domain.Environment, error) {
	if _, err := s.repo.Get(ctx, envName); err == nil {
		return nil, domain.ErrAlreadyExists
	} else if !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}

	env, err := domain.NewEnvironment(envName, envType, description, desiredState)
	if err != nil {
		return nil, err
	}

	reservedEnvVars, err := s.runtime.ReservedEnvironmentVariableNames(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取保留环境变量失败: %w", err)
	}
	if err := env.ValidateEnvConflict(reservedEnvVars); err != nil {
		return nil, err
	}

	if err := s.repo.Create(ctx, env); err != nil {
		return nil, err
	}

	if err := s.queue.Enqueue(ctx, envName); err != nil {
		return nil, err
	}

	return env, nil
}

// Update changes the desired state of an existing environment and enqueues it
// for reconciliation. Returns domain.ErrInvalidState when the environment is
// in the Deleting state.
func (s *EnvironmentCommandService) Update(ctx context.Context, envName domain.EnvironmentName, desiredState *domain.DesiredState) (*domain.Environment, error) {
	env, err := s.repo.Get(ctx, envName)
	if err != nil {
		return nil, err
	}

	if env.Status() != nil && env.Status().State == domain.StateDeleting {
		return nil, domain.ErrInvalidState
	}

	generation := env.Generation()

	if err := env.SetDesiredPresent(desiredState); err != nil {
		return nil, err
	}

	reservedEnvVars, err := s.runtime.ReservedEnvironmentVariableNames(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取保留环境变量失败: %w", err)
	}
	if err := env.ValidateEnvConflict(reservedEnvVars); err != nil {
		return nil, err
	}

	if err := s.repo.UpdateDesired(ctx, envName, generation, env.DesiredState(), domain.DesiredPresent); err != nil {
		return nil, err
	}

	if err := s.queue.Enqueue(ctx, envName); err != nil {
		return nil, err
	}

	return env, nil
}

// Delete marks an environment for deletion and enqueues it for reconciliation.
func (s *EnvironmentCommandService) Delete(ctx context.Context, envName domain.EnvironmentName) error {
	env, err := s.repo.Get(ctx, envName)
	if err != nil {
		return err
	}

	generation := env.Generation()

	if err := env.SetDesiredAbsent(); err != nil {
		return err
	}

	if err := s.repo.UpdateDesired(ctx, envName, generation, nil, domain.DesiredAbsent); err != nil {
		return err
	}

	return s.queue.Enqueue(ctx, envName)
}
