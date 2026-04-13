// Package deploy contains the deploy service implementation.
package deploy

import (
	"context"

	"dominion/projects/infra/deploy/domain"
)

// Reconciler drives environment status transitions to READY.
type Reconciler struct {
	repo domain.Repository
}

// NewReconciler creates a Reconciler backed by the provided repository.
func NewReconciler(repo domain.Repository) *Reconciler {
	return &Reconciler{repo: repo}
}

// Reconcile moves the environment to READY through the domain state machine.
func (r *Reconciler) Reconcile(ctx context.Context, envName domain.EnvironmentName) error {
	env, err := r.repo.Get(ctx, envName)
	if err != nil {
		return err
	}

	if env.Status().State != domain.StateReconciling {
		if err := env.MarkReconciling(); err != nil {
			return err
		}

		if err := r.repo.Save(ctx, env); err != nil {
			return err
		}
	}

	if err := env.MarkReady(); err != nil {
		return err
	}

	return r.repo.Save(ctx, env)
}
