package solver

import (
	"context"
	"errors"
)

// StatefulInstance represents a single instance of a stateful service,
// identified by its ordinal index within the StatefulSet.
type StatefulInstance struct {
	// Index is the 0-based ordinal of this instance within the StatefulSet.
	Index int
	// Endpoints holds the ready endpoint addresses for this instance.
	Endpoints []string
	// Hostname is the hostname of the stateful pod, matching the HOSTNAME
	// environment variable on the pod.
	Hostname string
}

// StatefulResolver discovers all instances of a stateful service.
// Unlike Resolver which returns aggregate addresses, StatefulResolver
// returns individual instances with their own endpoint sets.
type StatefulResolver interface {
	// Resolve resolves all instances of a stateful service target.
	Resolve(ctx context.Context, target *Target) ([]*StatefulInstance, error)
}

var (
	// ErrServiceNotStateful indicates that the target service is not a stateful service.
	ErrServiceNotStateful = errors.New("service is not stateful")
	// ErrInstanceNotFound indicates that the requested instance index does not exist.
	ErrInstanceNotFound = errors.New("stateful instance not found")
	// ErrInstanceNoReadyEndpoints indicates that the requested instance has no ready endpoints.
	ErrInstanceNoReadyEndpoints = errors.New("stateful instance has no ready endpoints")
)
