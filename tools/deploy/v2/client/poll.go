package client

import (
	"context"
	"errors"
	"fmt"
	"time"

	deploy "dominion/projects/infra/deploy"
)

// PollUntilReady polls until an environment becomes ready, fails, or times out.
func PollUntilReady(ctx context.Context, client *Client, name string, interval, timeout time.Duration) (*deploy.Environment, error) {
	pollCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		env, err := client.GetEnvironment(pollCtx, name)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return nil, fmt.Errorf("poll until ready for %q: %w", name, err)
			}
			return nil, err
		}

		if env != nil && env.Status != nil {
			switch env.Status.State {
			case deploy.EnvironmentState_ENVIRONMENT_STATE_READY:
				return env, nil
			case deploy.EnvironmentState_ENVIRONMENT_STATE_FAILED:
				return nil, fmt.Errorf("poll until ready for %q: %w: %s", name, ErrFailed, env.Status.Message)
			}
		}

		if err := waitForNextPoll(pollCtx, interval); err != nil {
			return nil, fmt.Errorf("poll until ready for %q: %w", name, err)
		}
	}
}

// PollUntilDeleted polls until an environment disappears or times out.
func PollUntilDeleted(ctx context.Context, client *Client, name string, interval, timeout time.Duration) error {
	pollCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		_, err := client.GetEnvironment(pollCtx, name)
		switch {
		case err == nil:
		case errors.Is(err, ErrNotFound):
			return nil
		case errors.Is(err, context.DeadlineExceeded):
			return fmt.Errorf("poll until deleted for %q: %w", name, err)
		default:
			return err
		}

		if err := waitForNextPoll(pollCtx, interval); err != nil {
			return fmt.Errorf("poll until deleted for %q: %w", name, err)
		}
	}
}

func waitForNextPoll(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
