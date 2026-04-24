package gateway

import (
	"context"
	"fmt"
	"math/rand"

	"dominion/pkg/solver"
)

// DeployRegistry resolves gateway instances via a StatefulResolver.
// It filters instances by ready endpoints and generates Assignment
// with PublicHost derived from the host pattern.
type DeployRegistry struct {
	resolver    solver.StatefulResolver
	target      *solver.Target
	hostPattern string
}

// NewDeployRegistry creates a DeployRegistry that discovers gateway
// instances using the given StatefulResolver for the specified target.
// The hostPattern is used with fmt.Sprintf(hostPattern, instance.Index)
// to generate PublicHost values.
func NewDeployRegistry(resolver solver.StatefulResolver, target *solver.Target, hostPattern string) *DeployRegistry {
	return &DeployRegistry{
		resolver:    resolver,
		target:      target,
		hostPattern: hostPattern,
	}
}

// PickRandom returns a random gateway assignment from ready instances.
// Only instances with non-empty Endpoints are considered ready.
func (r *DeployRegistry) PickRandom(ctx context.Context) (*Assignment, error) {
	instances, err := r.resolver.Resolve(ctx, r.target)
	if err != nil {
		return nil, err
	}

	ready := filterReady(instances)
	if len(ready) == 0 {
		return nil, ErrNoGatewayAvailable
	}

	instance := ready[rand.Intn(len(ready))]
	return &Assignment{
		GatewayID:  instance.Hostname,
		Index:      instance.Index,
		PublicHost: fmt.Sprintf(r.hostPattern, instance.Index),
	}, nil
}

// PickRandomExcluding returns a random gateway assignment excluding the
// given gatewayID. When only one ready instance exists and it is the
// excluded one, it falls back to returning that instance.
func (r *DeployRegistry) PickRandomExcluding(ctx context.Context, gatewayID string) (*Assignment, error) {
	instances, err := r.resolver.Resolve(ctx, r.target)
	if err != nil {
		return nil, err
	}

	ready := filterReady(instances)
	if len(ready) == 0 {
		return nil, ErrNoGatewayAvailable
	}

	var filtered []*solver.StatefulInstance
	for _, inst := range ready {
		if inst.Hostname == gatewayID {
			continue
		}
		filtered = append(filtered, inst)
	}

	if len(filtered) == 0 {
		return &Assignment{
			GatewayID:  ready[0].Hostname,
			Index:      ready[0].Index,
			PublicHost: fmt.Sprintf(r.hostPattern, ready[0].Index),
		}, nil
	}

	instance := filtered[rand.Intn(len(filtered))]
	return &Assignment{
		GatewayID:  instance.Hostname,
		Index:      instance.Index,
		PublicHost: fmt.Sprintf(r.hostPattern, instance.Index),
	}, nil
}

func filterReady(instances []*solver.StatefulInstance) []*solver.StatefulInstance {
	var ready []*solver.StatefulInstance
	for _, inst := range instances {
		if len(inst.Endpoints) > 0 {
			ready = append(ready, inst)
		}
	}
	return ready
}
