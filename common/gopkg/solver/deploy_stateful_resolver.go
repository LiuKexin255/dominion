package solver

import (
	"context"
	"fmt"
	"strings"
)

// DeployStatefulResolver resolves stateful service instances via the deploy service API.
type DeployStatefulResolver struct {
	client  DeployEndpointClient
	scope   string
	envName string
}

// NewDeployStatefulResolver creates a DeployStatefulResolver from environment variables.
//
// It reads DOMINION_ENVIRONMENT (format: scope.envName) from the
// process environment. Use WithDeployResolverEnvLookup to override the lookup for testing.
func NewDeployStatefulResolver() (StatefulResolver, error) {
	r := &DeployStatefulResolver{
		client: NewDeployHTTPClient(),
	}

	envValue, ok := lookupEnv(dominionEnvironmentEnvKey)
	if !ok || envValue == "" {
		return nil, fmt.Errorf("missing required env %s", dominionEnvironmentEnvKey)
	}

	scope, envName, found := strings.Cut(envValue, ".")
	if !found || scope == "" || envName == "" {
		return nil, fmt.Errorf("invalid %s format %q: want scope.envName", dominionEnvironmentEnvKey, envValue)
	}
	r.scope = scope
	r.envName = envName

	return r, nil
}

// Resolve resolves all instances of a stateful service target via the deploy service.
func (r *DeployStatefulResolver) Resolve(ctx context.Context, target *Target) ([]*StatefulInstance, error) {
	name := r.buildResourceName(target)
	info, err := r.client.GetServiceEndpoints(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("deploy GetServiceEndpoints(%q): %w", name, err)
	}

	if !info.IsStateful {
		return nil, ErrServiceNotStateful
	}

	instances := info.StatefulInstances
	if len(instances) == 0 {
		return nil, nil
	}

	filtered := make([]*StatefulInstance, 0, len(instances))
	for _, instance := range instances {
		filteredEndpoints, err := filterEndpoints(instance.Endpoints, info.Ports, target.PortSelector)
		if err != nil {
			return nil, err
		}
		filtered = append(filtered, &StatefulInstance{
			Index:     instance.Index,
			Endpoints: filteredEndpoints,
			Hostname:  instance.Hostname,
		})
	}

	return filtered, nil
}

// buildResourceName constructs the deploy resource name for a target.
func (r *DeployStatefulResolver) buildResourceName(target *Target) string {
	return fmt.Sprintf(deployEndpointsNameFormat, r.scope, r.envName, target.App, target.Service)
}
