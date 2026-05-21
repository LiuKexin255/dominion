// Package owner provides types and interfaces for owner gateway routing.
//
// The owner package defines the core abstractions for resolving which gateway
// instance owns a particular game session. Owner resolution determines whether
// a request should be handled locally or forwarded to a remote gateway.
package owner

import (
	"fmt"

	"dominion/common/gopkg/solver"
)

// Router identifies which gateway instance owns a particular game session.
// It provides the Decide method to determine if a request should be handled
// locally or forwarded to a remote gateway.
type Router struct {
	Resolver  OwnerResolver
	gatewayID string
}

// NewRouter creates a Router that identifies the local gateway by gatewayID.
// It internally creates a DeployStatefulResolver and DeployOwnerResolver
// for resolving owner gateway endpoints. Returns an error if resolver
// creation fails.
func NewRouter(gatewayID string) (*Router, error) {
	statefulResolver, err := solver.NewDeployStatefulResolver()
	if err != nil {
		return nil, fmt.Errorf("owner: create stateful resolver: %w", err)
	}
	target := solver.MustParseTarget("game/gateway:internal-grpc")
	ownerResolver := NewDeployOwnerResolver(statefulResolver, target)
	return &Router{
		Resolver:  ownerResolver,
		gatewayID: gatewayID,
	}, nil
}

// GatewayID returns the gateway ID of this router instance.
func (r *Router) GatewayID() string {
	return r.gatewayID
}

// Decide returns the routing target for the given ownerGatewayID.
// If ownerGatewayID matches the router's gatewayID, the request is local;
// otherwise it should be forwarded to a remote gateway.
func (r *Router) Decide(ownerGatewayID string) Target {
	if ownerGatewayID == r.gatewayID {
		return TargetLocal
	}
	return TargetRemote
}
