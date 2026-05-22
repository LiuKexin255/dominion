// Package owner provides types and interfaces for owner runtime routing.
//
// The owner package defines the core abstractions for resolving which runtime
// instance owns a particular game session. The gateway edge proxy uses owner
// resolution to forward WebSocket upgrade requests to the correct runtime.
package owner

import (
	"context"
	"fmt"

	"dominion/common/gopkg/solver"
	gameconst "dominion/projects/game/pkg/const"
)

// OwnerResolver resolves an owner runtime ID to its internal HTTP endpoint URL.
type OwnerResolver interface {
	// Resolve returns the internal HTTP endpoint URL for the given owner
	// runtime ID. The returned URL has the form "http://10.0.0.5:8080".
	Resolve(ctx context.Context, ownerRuntimeID string) (string, error)
}

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

// Resolver wraps an OwnerResolver with a convenience constructor that
// creates a DeployOwnerResolver targeting the game runtime HTTP endpoint.
type Resolver struct {
	OwnerResolver
}

// NewResolver creates a Resolver that resolves owner runtime endpoints
// via the deploy service, targeting the game runtime HTTP port.
func NewResolver() (*Resolver, error) {
	statefulResolver, err := solver.NewDeployStatefulResolver()
	if err != nil {
		return nil, fmt.Errorf("owner: create stateful resolver: %w", err)
	}
	target := solver.MustParseTarget(gameconst.TargetRuntimeHTTP)
	return &Resolver{
		OwnerResolver: NewDeployOwnerResolver(statefulResolver, target),
	}, nil
}
