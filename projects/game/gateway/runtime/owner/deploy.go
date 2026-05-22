package owner

import (
	"context"
	"fmt"

	"dominion/common/gopkg/solver"
)

// DeployOwnerResolver resolves owner runtime endpoints using the deploy service
// StatefulResolver. It resolves a runtime instance by matching its Hostname
// against the ownerRuntimeID extracted from the session token.
type DeployOwnerResolver struct {
	resolver solver.StatefulResolver
	target   *solver.Target
}

// NewDeployOwnerResolver creates a new DeployOwnerResolver.
// The target should be gameconst.TargetRuntimeHTTP to resolve runtime HTTP endpoints.
func NewDeployOwnerResolver(resolver solver.StatefulResolver, target *solver.Target) *DeployOwnerResolver {
	return &DeployOwnerResolver{
		resolver: resolver,
		target:   target,
	}
}

// Resolve resolves the owner runtime ID to an internal HTTP endpoint URL.
//
// It queries the stateful resolver for all instances of the target service,
// finds the instance whose Hostname matches ownerRuntimeID, and returns its
// first ready endpoint.
func (r *DeployOwnerResolver) Resolve(ctx context.Context, ownerRuntimeID string) (string, error) {
	instances, err := r.resolver.Resolve(ctx, r.target)
	if err != nil {
		return "", fmt.Errorf("owner: resolve instances: %w", err)
	}

	for _, instance := range instances {
		if instance.Hostname == ownerRuntimeID {
			if len(instance.Endpoints) == 0 {
				return "", fmt.Errorf("owner: instance %q has no ready endpoints: %w", ownerRuntimeID, solver.ErrInstanceNoReadyEndpoints)
			}
			return instance.Endpoints[0], nil
		}
	}

	return "", fmt.Errorf("owner: instance %q not found: %w", ownerRuntimeID, solver.ErrInstanceNotFound)
}
