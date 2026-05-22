// Package owner provides types and interfaces for owner runtime routing.
//
// The owner package defines the core abstractions for resolving which runtime
// instance owns a particular game session. The gateway edge proxy uses owner
// resolution to forward WebSocket upgrade requests to the correct runtime.
package owner

import "context"

// OwnerResolver resolves an owner runtime ID to its internal HTTP endpoint URL.
type OwnerResolver interface {
	// Resolve returns the internal HTTP endpoint URL for the given owner
	// runtime ID. The returned URL has the form "http://10.0.0.5:8080".
	Resolve(ctx context.Context, ownerRuntimeID string) (string, error)
}
