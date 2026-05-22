// Package owner provides types and interfaces for owner runtime routing.
//
// The owner package defines the core abstractions for resolving which runtime
// instance owns a particular game session. The gateway edge proxy uses owner
// resolution to forward WebSocket upgrade requests to the correct runtime.
package owner

import (
	"fmt"

	"dominion/common/gopkg/solver"
	gameconst "dominion/projects/game/pkg/const"
)

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
