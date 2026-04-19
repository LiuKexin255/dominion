package gateway

import (
	"errors"
	"math/rand"
)

// ErrNoGatewayAvailable is returned when no gateway IDs are registered.
var ErrNoGatewayAvailable = errors.New("no gateway available")

// Registry picks gateway IDs for session assignment.
type Registry interface {
	PickRandom() (string, error)
	PickRandomExcluding(gatewayID string) (string, error)
}

// StaticRegistry is a fixed registry backed by an in-memory list.
type StaticRegistry struct {
	gatewayIDs []string
}

// NewStaticRegistry creates a StaticRegistry from gateway IDs.
func NewStaticRegistry(gatewayIDs []string) *StaticRegistry {
	return &StaticRegistry{gatewayIDs: append([]string(nil), gatewayIDs...)}
}

// PickRandom returns a random gateway ID from the registry.
func (r *StaticRegistry) PickRandom() (string, error) {
	if len(r.gatewayIDs) == 0 {
		return "", ErrNoGatewayAvailable
	}

	return r.gatewayIDs[rand.Intn(len(r.gatewayIDs))], nil
}

// PickRandomExcluding returns a random gateway ID excluding gatewayID.
func (r *StaticRegistry) PickRandomExcluding(gatewayID string) (string, error) {
	if len(r.gatewayIDs) == 0 {
		return "", ErrNoGatewayAvailable
	}

	var filtered []string
	for _, id := range r.gatewayIDs {
		if id == gatewayID {
			continue
		}
		filtered = append(filtered, id)
	}

	if len(filtered) == 0 {
		return gatewayID, nil
	}

	return filtered[rand.Intn(len(filtered))], nil
}
