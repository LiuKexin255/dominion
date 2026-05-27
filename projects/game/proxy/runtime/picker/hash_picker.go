// Package picker provides owner selection strategies for the proxy service.
package picker

import (
	"context"
	"hash/fnv"

	"dominion/projects/game/proxy/domain"
	"dominion/projects/game/proxy/runtime/agentclient"
)

// hashPicker selects an agent instance using fnv hash of the session ID.
type hashPicker struct{}

// NewHashPicker creates a hash-based OwnerPicker.
func NewHashPicker() domain.OwnerPicker {
	return new(hashPicker)
}

// Pick returns the agent client for the given session ID.
func (p *hashPicker) Pick(_ context.Context, sessionID string, clients []agentclient.ClientRef) (agentclient.ClientRef, error) {
	if len(clients) == 0 {
		return agentclient.ClientRef{}, domain.ErrNoAgentInstances
	}

	h := fnv.New32a()
	if _, err := h.Write([]byte(sessionID)); err != nil {
		return agentclient.ClientRef{}, err
	}

	return clients[int(h.Sum32())%len(clients)], nil
}
