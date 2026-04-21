package domain

import (
	"context"
	"log"
)

type recoveryRepository interface {
	ListNeedingReconcile(context.Context) ([]*Environment, error)
}

type recoveryQueue interface {
	Enqueue(context.Context, EnvironmentName) error
}

// Recover reloads in-flight environments and requeues them at startup.
func Recover(ctx context.Context, repo recoveryRepository, queue recoveryQueue) error {
	envs, err := repo.ListNeedingReconcile(ctx)
	if err != nil {
		return err
	}

	log.Printf("recovery: found %d in-flight environments", len(envs))
	for _, env := range envs {
		if env == nil || env.Status() == nil {
			continue
		}

		name := env.Name()
		if err := queue.Enqueue(ctx, name); err != nil {
			return err
		}
		log.Printf("recovery: requeued %s", name.String())
	}

	return nil
}
