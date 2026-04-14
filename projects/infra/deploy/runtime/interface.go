// Package runtime defines the contract between the deploy domain and the
// Kubernetes runtime implementation.
package runtime

import (
	"context"

	"dominion/projects/infra/deploy/domain"
)

// EnvironmentRuntime reconciles domain environments with the runtime.
type EnvironmentRuntime interface {
	Apply(ctx context.Context, env *domain.Environment) error
	Delete(ctx context.Context, envName domain.EnvironmentName) error
}
