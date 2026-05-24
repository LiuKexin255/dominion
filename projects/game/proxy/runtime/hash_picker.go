package runtime

import (
	"context"
	"hash/fnv"

	"dominion/common/gopkg/solver"
	"dominion/projects/game/proxy/domain"
)

// hashPicker selects an agent instance using fnv hash of the session ID.
type hashPicker struct{}

// NewHashPicker creates a hash-based OwnerPicker.
func NewHashPicker() domain.OwnerPicker {
	return new(hashPicker)
}

// Pick returns the agent instance index for the given session ID.
func (p *hashPicker) Pick(_ context.Context, sessionID string, instances []*solver.StatefulInstance) (int, error) {
	if len(instances) == 0 {
		return 0, domain.ErrNoAgentInstances
	}

	h := fnv.New32a()
	if _, err := h.Write([]byte(sessionID)); err != nil {
		return 0, err
	}

	return int(h.Sum32()) % len(instances), nil
}
