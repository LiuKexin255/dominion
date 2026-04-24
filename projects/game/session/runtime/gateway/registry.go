package gateway

import (
	"context"
	"errors"
	"math/rand"
)

// ErrNoGatewayAvailable is returned when no gateway IDs are registered.
var ErrNoGatewayAvailable = errors.New("no gateway available")

// Assignment represents a gateway instance selected for session routing.
type Assignment struct {
	// GatewayID is the unique identifier of the gateway instance.
	GatewayID string
	// Index is the ordinal index of the gateway instance.
	Index int
	// PublicHost is the public address clients use to reach this gateway.
	PublicHost string
}

// Registry picks gateway assignments for session routing.
type Registry interface {
	// PickRandom returns a random gateway assignment from the registry.
	PickRandom(ctx context.Context) (*Assignment, error)
	// PickRandomExcluding returns a random gateway assignment excluding the given gatewayID.
	// When only one assignment exists, it falls back to returning that assignment.
	PickRandomExcluding(ctx context.Context, gatewayID string) (*Assignment, error)
}

// StaticRegistry is a fixed registry backed by an in-memory list.
type StaticRegistry struct {
	assignments []*Assignment
}

// NewStaticRegistry creates a StaticRegistry from gateway assignments.
// It performs a defensive copy of the input slice and its elements.
func NewStaticRegistry(assignments []*Assignment) *StaticRegistry {
	copied := make([]*Assignment, len(assignments))
	for i, a := range assignments {
		copied[i] = &Assignment{
			GatewayID:  a.GatewayID,
			Index:      a.Index,
			PublicHost: a.PublicHost,
		}
	}
	return &StaticRegistry{assignments: copied}
}

// PickRandom returns a random gateway assignment from the registry.
func (r *StaticRegistry) PickRandom(_ context.Context) (*Assignment, error) {
	if len(r.assignments) == 0 {
		return nil, ErrNoGatewayAvailable
	}

	return r.assignments[rand.Intn(len(r.assignments))], nil
}

// PickRandomExcluding returns a random gateway assignment excluding the given gatewayID.
// When all assignments are excluded (i.e. only one instance), it falls back to
// returning the excluded gateway's assignment if it exists in the registry.
func (r *StaticRegistry) PickRandomExcluding(_ context.Context, gatewayID string) (*Assignment, error) {
	if len(r.assignments) == 0 {
		return nil, ErrNoGatewayAvailable
	}

	var filtered []*Assignment
	for _, a := range r.assignments {
		if a.GatewayID == gatewayID {
			continue
		}
		filtered = append(filtered, a)
	}

	if len(filtered) == 0 {
		for _, a := range r.assignments {
			if a.GatewayID == gatewayID {
				return a, nil
			}
		}
	}

	return filtered[rand.Intn(len(filtered))], nil
}
