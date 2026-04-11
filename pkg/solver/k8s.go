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
	// ServiceDominionAppLabelKey stores the dominion app name.
	ServiceDominionAppLabelKey = "dominion.io/app"
	// ServiceDominionEnvironmentLabelKey stores the dominion environment name.
	ServiceDominionEnvironmentLabelKey = "dominion.io/environment"
)

// K8sClient is the k8s lookup client used by the resolver.
type K8sClient interface {
	Lookup(ctx context.Context, target *Target, env *Environment) (string, error)
	ResolveEndpoints(ctx context.Context, target *Target, env *Environment, serviceName string) ([]string, error)
	Resolve(ctx context.Context, target *Target, env *Environment) ([]string, error)
}

// inClusterConfig loads the runtime pod service-account configuration.
var inClusterConfig = rest.InClusterConfig

// newClientsetForConfig constructs a kubernetes client from the runtime config.
var newClientsetForConfig = func(config *rest.Config) (kubernetes.Interface, error) {
	return kubernetes.NewForConfig(config)
}

// RuntimeK8sClient is the in-cluster kubernetes client used by the resolver.
type RuntimeK8sClient struct {
	clientset kubernetes.Interface
}

// NewInClusterClient constructs the runtime kubernetes client from pod credentials.
func NewInClusterClient() (*RuntimeK8sClient, error) {
	config, err := inClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("build in-cluster kubernetes config: %w", err)
	}

	clientset, err := newClientsetForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("build kubernetes clientset: %w", err)
	}

	return &RuntimeK8sClient{clientset: clientset}, nil
}

// BuildServiceSelector returns the stable Service label selector for a target.
func BuildServiceSelector(target *Target, env *Environment) string {
	return labels.SelectorFromSet(labels.Set{
		ServiceAppLabelKey:                 target.App,
		ServiceComponentLabelKey:           target.Service,
		ServiceDominionAppLabelKey:         env.App,
		ServiceDominionEnvironmentLabelKey: env.Name,
	}).String()
}

// Lookup resolves the target to exactly one Service name in the pod namespace.
func (c *RuntimeK8sClient) Lookup(ctx context.Context, target *Target, env *Environment) (string, error) {
	selector := BuildServiceSelector(target, env)
	services, err := c.clientset.CoreV1().Services(env.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) {
			return "", fmt.Errorf("list services in namespace %q with selector %q: permission denied: %w", env.Namespace, selector, err)
		}
		return "", fmt.Errorf("list services in namespace %q with selector %q: %w", env.Namespace, selector, err)
	}

	if len(services.Items) == 0 {
		return "", fmt.Errorf("resolve service for target %q/%q in namespace %q: no Services matched selector %q", target.App, target.Service, env.Namespace, selector)
	}
	if len(services.Items) > 1 {
		names := make([]string, 0, len(services.Items))
		for _, service := range services.Items {
			names = append(names, service.Name)
		}
		sort.Strings(names)
		return "", fmt.Errorf(
			"resolve service for target %q/%q in namespace %q: expected exactly one Service for selector %q, found %d (%s)",
			target.App,
			target.Service,
			env.Namespace,
			selector,
			len(names),
			strings.Join(names, ", "),
		)
	}

	return services.Items[0].Name, nil
}

// ResolveEndpoints lists EndpointSlices for a Service and expands them into stable ip:port addresses.
func (c *RuntimeK8sClient) ResolveEndpoints(ctx context.Context, target *Target, env *Environment, serviceName string) ([]string, error) {
	selector := labels.SelectorFromSet(labels.Set{discoveryv1.LabelServiceName: serviceName}).String()
	endpointSlices, err := c.clientset.DiscoveryV1().EndpointSlices(env.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) {
			return nil, fmt.Errorf("list EndpointSlices in namespace %q with selector %q: permission denied: %w", env.Namespace, selector, err)
		}
		return nil, fmt.Errorf("list EndpointSlices in namespace %q with selector %q: %w", env.Namespace, selector, err)
	}

	return expandEndpointSliceAddresses(endpointSlices.Items, target.Port), nil
}

// Resolve resolves the target to ready endpoint addresses.
func (c *RuntimeK8sClient) Resolve(ctx context.Context, target *Target, env *Environment) ([]string, error) {
	serviceName, err := c.Lookup(ctx, target, env)
	if err != nil {
		return nil, err
	}

	return c.ResolveEndpoints(ctx, target, env, serviceName)
}

func expandEndpointSliceAddresses(endpointSlices []discoveryv1.EndpointSlice, targetPort int) []string {
	if len(endpointSlices) == 0 {
		return nil
	}

	addresses := make(map[string]struct{})
	for _, endpointSlice := range endpointSlices {
		ports := endpointSlicePorts(endpointSlice, targetPort)
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

func endpointSlicePorts(endpointSlice discoveryv1.EndpointSlice, targetPort int) []int {
	if targetPort > 0 {
		return []int{targetPort}
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
