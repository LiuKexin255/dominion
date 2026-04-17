package solver

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	// ServiceAppLabelKey is the standard Kubernetes app name label.
	ServiceAppLabelKey = "app.kubernetes.io/name"
	// ServiceComponentLabelKey is the standard Kubernetes component label.
	ServiceComponentLabelKey = "app.kubernetes.io/component"
	// ServiceDominionEnvironmentLabelKey stores the dominion environment name.
	ServiceDominionEnvironmentLabelKey = "dominion.io/environment"
)

// Resolver resolves dominion targets to services and endpoints.
type Resolver interface {
	Resolve(ctx context.Context, target *Target) ([]string, error)
}

// inClusterConfig loads the runtime pod service-account configuration.
var inClusterConfig = rest.InClusterConfig

// newClientsetForConfig constructs a kubernetes client from the runtime config.
var newClientsetForConfig = func(config *rest.Config) (kubernetes.Interface, error) {
	return kubernetes.NewForConfig(config)
}

// K8sResolver is the in-cluster kubernetes-backed resolver implementation.
type K8sResolver struct {
	clientset kubernetes.Interface
}

// NewK8sResolver constructs the runtime kubernetes resolver from pod credentials.
func NewK8sResolver() (Resolver, error) {
	config, err := inClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("build in-cluster kubernetes config: %w", err)
	}

	clientset, err := newClientsetForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("build kubernetes clientset: %w", err)
	}

	return &K8sResolver{clientset: clientset}, nil
}

// buildServiceSelector returns the stable Service label selector for a target.
func buildServiceSelector(target *Target, env *environment) string {
	return labels.SelectorFromSet(labels.Set{
		ServiceAppLabelKey:                 target.App,
		ServiceComponentLabelKey:           target.Service,
		ServiceDominionEnvironmentLabelKey: env.Name,
	}).String()
}

// Resolve resolves the target to ready endpoint addresses.
func (c *K8sResolver) Resolve(ctx context.Context, target *Target) ([]string, error) {
	env, err := loadEnvironment(target)
	if err != nil {
		return nil, err
	}

	selector := buildServiceSelector(target, env)
	services, err := c.clientset.CoreV1().Services(env.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) {
			return nil, fmt.Errorf("list services in namespace %q with selector %q: permission denied: %w", env.Namespace, selector, err)
		}
		return nil, fmt.Errorf("list services in namespace %q with selector %q: %w", env.Namespace, selector, err)
	}

	if len(services.Items) == 0 {
		return nil, fmt.Errorf("resolve service for target %q/%q in namespace %q: no Services matched selector %q", target.App, target.Service, env.Namespace, selector)
	}
	if len(services.Items) > 1 {
		names := make([]string, 0, len(services.Items))
		for _, service := range services.Items {
			names = append(names, service.Name)
		}
		sort.Strings(names)
		return nil, fmt.Errorf(
			"resolve service for target %q/%q in namespace %q: expected exactly one Service for selector %q, found %d (%s)",
			target.App,
			target.Service,
			env.Namespace,
			selector,
			len(names),
			strings.Join(names, ", "),
		)
	}

	serviceName := services.Items[0].Name
	epsSelector := labels.SelectorFromSet(labels.Set{discoveryv1.LabelServiceName: serviceName}).String()
	endpointSlices, err := c.clientset.DiscoveryV1().EndpointSlices(env.Namespace).List(ctx, metav1.ListOptions{LabelSelector: epsSelector})
	if err != nil {
		if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) {
			return nil, fmt.Errorf("list EndpointSlices in namespace %q with selector %q: permission denied: %w", env.Namespace, epsSelector, err)
		}
		return nil, fmt.Errorf("list EndpointSlices in namespace %q with selector %q: %w", env.Namespace, epsSelector, err)
	}

	return expandEndpointSliceAddresses(endpointSlices.Items, target.PortSelector), nil
}

func expandEndpointSliceAddresses(endpointSlices []discoveryv1.EndpointSlice, portSelector PortSelector) []string {
	if len(endpointSlices) == 0 {
		return nil
	}

	addresses := make(map[string]struct{})
	for _, endpointSlice := range endpointSlices {
		ports := endpointSlicePorts(endpointSlice, portSelector)
		if len(ports) == 0 {
			continue
		}

		for _, endpoint := range endpointSlice.Endpoints {
			if !includeEndpoint(endpoint) {
				continue
			}

			for _, ip := range endpoint.Addresses {
				ip = strings.TrimSpace(ip)
				if ip == "" {
					continue
				}

				for _, port := range ports {
					addresses[net.JoinHostPort(ip, strconv.Itoa(port))] = struct{}{}
				}
			}
		}
	}

	if len(addresses) == 0 {
		return nil
	}

	expanded := make([]string, 0, len(addresses))
	for address := range addresses {
		expanded = append(expanded, address)
	}
	sort.Strings(expanded)

	return expanded
}

func endpointSlicePorts(endpointSlice discoveryv1.EndpointSlice, portSelector PortSelector) []int {
	if portSelector.IsNumeric() {
		return []int{portSelector.Numeric()}
	}

	if portSelector.IsNamed() {
		name := portSelector.Name()
		for _, endpointPort := range endpointSlice.Ports {
			if endpointPort.Port == nil || endpointPort.Name == nil {
				continue
			}
			if *endpointPort.Name == name {
				return []int{int(*endpointPort.Port)}
			}
		}
		return nil
	}

	var ports []int
	for _, endpointPort := range endpointSlice.Ports {
		if endpointPort.Port == nil {
			continue
		}
		ports = append(ports, int(*endpointPort.Port))
	}

	return ports
}

func includeEndpoint(endpoint discoveryv1.Endpoint) bool {
	if endpoint.Conditions.Ready != nil && !*endpoint.Conditions.Ready {
		return false
	}
	if endpoint.Conditions.Terminating != nil && *endpoint.Conditions.Terminating {
		return false
	}
	return true
}
