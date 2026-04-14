package domain

import (
	"context"
	"log"
)

type recoveryRepository interface {
	ListByStates(context.Context, ...EnvironmentState) ([]*Environment, error)
}

type recoveryQueue interface {
	Enqueue(context.Context, EnvironmentName) error
	EnqueueWithPriority(context.Context, EnvironmentName) error
}

// Recover reloads in-flight environments and requeues them at startup.
func Recover(ctx context.Context, repo recoveryRepository, queue recoveryQueue) error {
	envs, err := repo.ListByStates(ctx, StateReconciling, StateDeleting)
	if err != nil {
		return err
	}

	log.Printf("recovery: found %d in-flight environments", len(envs))
	for _, env := range envs {
		if env == nil || env.Status() == nil {
			continue
		}

		name := env.Name()
		switch env.Status().State {
		case StateReconciling:
			if err := queue.Enqueue(ctx, name); err != nil {
				return err
			}
			log.Printf("recovery: requeued %s for reconciliation", name.String())
		case StateDeleting:
			if err := queue.EnqueueWithPriority(ctx, name); err != nil {
				return err
			}
			log.Printf("recovery: requeued %s for deletion", name.String())
		}
	}

	return nil
}
