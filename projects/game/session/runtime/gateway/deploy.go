package gateway

import (
	"context"
	"fmt"
	"math/rand"

	"dominion/common/gopkg/logs"
	"dominion/common/gopkg/logs/event"
	"dominion/common/gopkg/otel"
	"dominion/common/gopkg/solver"

	"go.opentelemetry.io/otel/attribute"
)

const (
	spanPickRandom = "session.gateway.pick_random"

	logFieldReadyCount      = "ready_count"
	logFieldSelectedGateway = "selected_gateway_id"
	logFieldExcludedGateway = "excluded_gateway_id"
	logFieldError           = "error"
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
	ctx, span := otel.Tracer().Start(ctx, spanPickRandom)
	defer span.End()

	instances, err := r.resolver.Resolve(ctx, r.target)
	if err != nil {
		return nil, err
	}

	ready := filterReady(instances)
	span.SetAttributes(attribute.Int(logFieldReadyCount, len(ready)))
	if len(ready) == 0 {
		logs.Warn(ctx, "no gateways available")
		return nil, ErrNoGatewayAvailable
	}

	instance := ready[rand.Intn(len(ready))]
	assignment := &Assignment{
		GatewayID:  instance.Hostname,
		Index:      instance.Index,
		PublicHost: fmt.Sprintf(r.hostPattern, instance.Index),
	}
	span.SetAttributes(attribute.String(logFieldSelectedGateway, assignment.GatewayID))
	logs.Info(ctx, "gateway picked", event.String(logFieldSelectedGateway, assignment.GatewayID), event.Int(logFieldReadyCount, len(ready)))
	return assignment, nil
}

// PickRandomExcluding returns a random gateway assignment excluding the
// given gatewayID. When only one ready instance exists and it is the
// excluded one, it falls back to returning that instance.
func (r *DeployRegistry) PickRandomExcluding(ctx context.Context, gatewayID string) (*Assignment, error) {
	ctx, span := otel.Tracer().Start(ctx, spanPickRandom)
	defer span.End()
	span.SetAttributes(attribute.String(logFieldExcludedGateway, gatewayID))

	instances, err := r.resolver.Resolve(ctx, r.target)
	if err != nil {
		return nil, err
	}

	ready := filterReady(instances)
	span.SetAttributes(attribute.Int(logFieldReadyCount, len(ready)))
	if len(ready) == 0 {
		logs.Warn(ctx, "no gateways available", event.String(logFieldExcludedGateway, gatewayID))
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
		assignment := &Assignment{
			GatewayID:  ready[0].Hostname,
			Index:      ready[0].Index,
			PublicHost: fmt.Sprintf(r.hostPattern, ready[0].Index),
		}
		span.SetAttributes(attribute.String(logFieldSelectedGateway, assignment.GatewayID))
		logs.Info(ctx, "gateway picked", event.String(logFieldSelectedGateway, assignment.GatewayID), event.Int(logFieldReadyCount, len(ready)), event.String(logFieldExcludedGateway, gatewayID))
		return assignment, nil
	}

	instance := filtered[rand.Intn(len(filtered))]
	assignment := &Assignment{
		GatewayID:  instance.Hostname,
		Index:      instance.Index,
		PublicHost: fmt.Sprintf(r.hostPattern, instance.Index),
	}
	span.SetAttributes(attribute.String(logFieldSelectedGateway, assignment.GatewayID))
	logs.Info(ctx, "gateway picked", event.String(logFieldSelectedGateway, assignment.GatewayID), event.Int(logFieldReadyCount, len(ready)), event.String(logFieldExcludedGateway, gatewayID))
	return assignment, nil
}

// PublicHost returns the public host address of the gateway identified by gatewayID.
func (r *DeployRegistry) PublicHost(ctx context.Context, gatewayID string) (string, error) {
	instances, err := r.resolver.Resolve(ctx, r.target)
	if err != nil {
		return "", err
	}

	for _, inst := range filterReady(instances) {
		if inst.Hostname == gatewayID {
			return fmt.Sprintf(r.hostPattern, inst.Index), nil
		}
	}

	return "", ErrNoGatewayAvailable
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
