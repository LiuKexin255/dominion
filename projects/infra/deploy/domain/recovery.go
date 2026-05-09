package domain

import (
	"context"
	"dominion/common/gopkg/logs"
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

	logs.InfoContext(ctx, "recovery: found in-flight environments", "count", len(envs))
	for _, env := range envs {
		if env == nil || env.Status() == nil {
			continue
		}

		name := env.Name()
		if err := queue.Enqueue(ctx, name); err != nil {
			return err
		}
		logs.InfoContext(ctx, "recovery: requeued environment", "env_name", name.String())
	}

	return nil
}
