// Package owner provides types and interfaces for owner gateway routing.
//
// The owner package defines the core abstractions for resolving which gateway
// instance owns a particular game session. Owner resolution determines whether
// a request should be handled locally or forwarded to a remote gateway.
package owner

import "context"

// Target represents the routing decision for a gateway request.
type Target int

const (
	// TargetLocal indicates the request should be handled by the current gateway.
	TargetLocal Target = iota
	// TargetRemote indicates the request should be forwarded to a remote gateway.
	TargetRemote
)

// OwnerResolver resolves an owner gateway ID to its internal HTTP endpoint URL.
type OwnerResolver interface {
	// Resolve returns the internal HTTP endpoint URL for the given owner
	// gateway ID. The returned URL has the form "http://10.0.0.5:8082".
	Resolve(ctx context.Context, ownerGatewayID string) (string, error)
}

// OwnerRoutingConfig holds the configuration for owner-based routing.
type OwnerRoutingConfig struct {
	// GatewayID identifies the current gateway instance.
	GatewayID string

	// OwnerEpoch identifies the epoch of the owner assignment.
	// Starts at 1; 0 is illegal.
	OwnerEpoch int64
}
