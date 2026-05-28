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

// Pick returns the agent connection for the given session ID.
func (p *hashPicker) Pick(_ context.Context, sessionID string, conns []*agentclient.ConnRef) (*agentclient.ConnRef, error) {
	if len(conns) == 0 {
		return nil, domain.ErrNoAgentInstances
	}

	h := fnv.New32a()
	if _, err := h.Write([]byte(sessionID)); err != nil {
		return nil, err
	}

	return conns[int(h.Sum32())%len(conns)], nil
}
