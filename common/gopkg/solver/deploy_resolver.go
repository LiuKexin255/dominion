package solver

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
)

// ServiceEndpointsInfo holds endpoint data resolved from the deploy service.
type ServiceEndpointsInfo struct {
	// Endpoints is the list of resolved endpoint addresses in "host:port" format.
	Endpoints []string
	// Ports maps named ports to their numeric port values.
	Ports map[string]int32
	// StatefulInstances holds per-instance endpoints for stateful services.
	StatefulInstances []*StatefulInstance
	// IsStateful indicates whether the service is a stateful service.
	IsStateful bool
}

// DeployEndpointClient fetches service endpoints from the deploy service.
type DeployEndpointClient interface {
	GetServiceEndpoints(ctx context.Context, name string) (*ServiceEndpointsInfo, error)
}

// DeployResolver resolves dominion targets via the deploy service.
type DeployResolver struct {
	client  DeployEndpointClient
	scope   string
	envName string
}

// NewDeployResolver creates a DeployResolver from the given client and environment variables.
//
// It reads DOMINION_ENVIRONMENT (format: scope.envName) from the
// process environment. Use WithDeployResolverEnvLookup to override the lookup for testing.
func NewDeployResolver() (Resolver, error) {
	r := &DeployResolver{
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

// Resolve resolves the target to ready endpoint addresses, applying port selection.
func (r *DeployResolver) Resolve(ctx context.Context, target *Target) ([]string, error) {
	name := r.buildResourceName(target)
	info, err := r.client.GetServiceEndpoints(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("deploy GetServiceEndpoints(%q): %w", name, err)
	}
	if info == nil {
		return nil, nil
	}
	return filterEndpoints(info.Endpoints, info.Ports, target.PortSelector)
}

// buildResourceName constructs the deploy resource name for a target.
func (r *DeployResolver) buildResourceName(target *Target) string {
	return fmt.Sprintf(deployEndpointsNameFormat, r.scope, r.envName, target.App, target.Service)
}

// filterEndpoints applies port selection to the resolved endpoints.
func filterEndpoints(endpoints []string, ports map[string]int32, ps PortSelector) ([]string, error) {
	if len(endpoints) == 0 {
		return nil, nil
	}

	if ps.IsNumeric() {
		return filterByNumericPort(endpoints, ps.Numeric()), nil
	}

	if ps.IsNamed() {
		portValue, ok := ports[ps.Name()]
		if !ok {
			return nil, fmt.Errorf("named port %q not found in service endpoints", ps.Name())
		}
		return filterByNamedPort(endpoints, int(portValue)), nil
	}

	return endpoints, nil
}

// filterByNumericPort returns endpoints matching the given numeric port.
func filterByNumericPort(endpoints []string, port int) []string {
	portStr := strconv.Itoa(port)
	seen := make(map[string]struct{})
	for _, ep := range endpoints {
		host, p, err := net.SplitHostPort(ep)
		if err != nil {
			continue
		}
		if p == portStr {
			seen[net.JoinHostPort(host, portStr)] = struct{}{}
		}
	}
	return sortUnique(seen)
}

// filterByNamedPort returns endpoints with the host from each endpoint combined with the resolved port.
func filterByNamedPort(endpoints []string, port int) []string {
	portStr := strconv.Itoa(port)
	seen := make(map[string]struct{})
	for _, ep := range endpoints {
		host, _, err := net.SplitHostPort(ep)
		if err != nil {
			continue
		}
		seen[net.JoinHostPort(host, portStr)] = struct{}{}
	}
	return sortUnique(seen)
}

// sortUnique converts a set of strings to a sorted slice.
func sortUnique(seen map[string]struct{}) []string {
	if len(seen) == 0 {
		return nil
	}
	result := make([]string, 0, len(seen))
	for addr := range seen {
		result = append(result, addr)
	}
	sort.Strings(result)
	return result
}
