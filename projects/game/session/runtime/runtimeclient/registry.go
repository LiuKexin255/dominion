package runtimeclient

import (
	"context"
	"errors"
	"math/rand"
)

// ErrNoRuntimeAvailable is returned when no runtime instances are registered.
var ErrNoRuntimeAvailable = errors.New("no runtime available")

// Assignment represents a runtime instance selected for session routing.
type Assignment struct {
	// RuntimeID is the unique identifier of the runtime instance.
	RuntimeID string
	// Index is the ordinal index of the runtime instance.
	Index int
	// PublicHost is the public address clients use to reach this runtime.
	PublicHost string
}

// Registry picks runtime assignments for session routing.
type Registry interface {
	// PickRandom returns a random runtime assignment from the registry.
	PickRandom(ctx context.Context) (*Assignment, error)
	// PickRandomExcluding returns a random runtime assignment excluding the given runtimeID.
	// When only one assignment exists, it falls back to returning that assignment.
	PickRandomExcluding(ctx context.Context, runtimeID string) (*Assignment, error)
	// PublicHost returns the public host address of the runtime identified by runtimeID.
	// Returns ErrNoRuntimeAvailable if the runtime is not found in the registry.
	PublicHost(ctx context.Context, runtimeID string) (string, error)
}

// StaticRegistry is a fixed registry backed by an in-memory list.
type StaticRegistry struct {
	assignments []*Assignment
}

// NewStaticRegistry creates a StaticRegistry from runtime assignments.
// It performs a defensive copy of the input slice and its elements.
func NewStaticRegistry(assignments []*Assignment) *StaticRegistry {
	copied := make([]*Assignment, len(assignments))
	for i, a := range assignments {
		copied[i] = &Assignment{
			RuntimeID:  a.RuntimeID,
			Index:      a.Index,
			PublicHost: a.PublicHost,
		}
	}
	return &StaticRegistry{assignments: copied}
}

// PickRandom returns a random runtime assignment from the registry.
func (r *StaticRegistry) PickRandom(_ context.Context) (*Assignment, error) {
	if len(r.assignments) == 0 {
		return nil, ErrNoRuntimeAvailable
	}

	return r.assignments[rand.Intn(len(r.assignments))], nil
}

// PickRandomExcluding returns a random runtime assignment excluding the given runtimeID.
// When all assignments are excluded (i.e. only one instance), it falls back to
// returning the excluded runtime's assignment if it exists in the registry.
func (r *StaticRegistry) PickRandomExcluding(_ context.Context, runtimeID string) (*Assignment, error) {
	if len(r.assignments) == 0 {
		return nil, ErrNoRuntimeAvailable
	}

	var filtered []*Assignment
	for _, a := range r.assignments {
		if a.RuntimeID == runtimeID {
			continue
		}
		filtered = append(filtered, a)
	}

	if len(filtered) == 0 {
		for _, a := range r.assignments {
			if a.RuntimeID == runtimeID {
				return a, nil
			}
		}
	}

	return filtered[rand.Intn(len(filtered))], nil
}

// PublicHost returns the public host address of the runtime identified by runtimeID.
func (r *StaticRegistry) PublicHost(_ context.Context, runtimeID string) (string, error) {
	for _, a := range r.assignments {
		if a.RuntimeID == runtimeID {
			return a.PublicHost, nil
		}
	}

	return "", ErrNoRuntimeAvailable
}
