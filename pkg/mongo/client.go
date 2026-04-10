package mongo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"

	mongodriver "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var (
	defaultEnvLoader EnvLoader = new(OSEnvLoader)
	nonDNSLabel                = regexp.MustCompile(`[^a-z0-9-]+`)
	newMongoClient             = func(uri string) (*mongodriver.Client, error) {
		return mongodriver.Connect(context.Background(), options.Client().ApplyURI(uri))
	}
	newK8sClient = func() (K8sClient, error) {
		return NewInClusterClient()
	}
)

const (
	serviceAppLabelKey                 = "app.kubernetes.io/name"
	serviceComponentLabelKey           = "app.kubernetes.io/component"
	serviceDominionAppLabelKey         = "dominion.io/app"
	serviceDominionEnvironmentLabelKey = "dominion.io/environment"
)

// K8sClient is the kubernetes lookup client used by the mongo helper.
type K8sClient interface {
	Resolve(ctx context.Context, target *Target, env *Environment) (string, error)
}

// inClusterConfig loads the runtime pod service-account configuration.
var inClusterConfig = rest.InClusterConfig

// newClientsetForConfig constructs a kubernetes client from the runtime config.
var newClientsetForConfig = func(config *rest.Config) (kubernetes.Interface, error) {
	return kubernetes.NewForConfig(config)
}

// RuntimeK8sClient is the in-cluster kubernetes client used by the mongo helper.
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

// NewClient creates a MongoDB client for the given dominion target.
func NewClient(rawTarget string) (*mongodriver.Client, error) {
	target, err := ParseTarget(rawTarget)
	if err != nil {
		return nil, err
	}

	env, err := defaultEnvLoader.Load(target)
	if err != nil {
		return nil, err
	}
	if env == nil {
		return nil, fmt.Errorf("load environment for %q: environment is nil", rawTarget)
	}

	k8sClient, err := newK8sClient()
	if err != nil {
		return nil, err
	}

	address, err := k8sClient.Resolve(context.Background(), target, env)
	if err != nil {
		return nil, err
	}

	uri := buildMongoURI(target, env, address)
	client, err := newMongoClient(uri)
	if err != nil {
		return nil, fmt.Errorf("create mongo client for %q: %w", rawTarget, err)
	}

	return client, nil
}

func buildMongoURI(target *Target, env *Environment, address string) string {
	password := generateStablePassword(target.App, env.Name, target.Name)

	return fmt.Sprintf(
		"mongodb://%s:%s@%s/%s?authSource=%s",
		defaultMongoUsername,
		password,
		address,
		defaultMongoAuthDatabase,
		defaultMongoAuthDatabase,
	)
}

// buildServiceSelector returns the stable Service label selector for a mongo target.
func buildServiceSelector(target *Target, env *Environment) string {
	return labels.SelectorFromSet(labels.Set{
		serviceAppLabelKey:                 target.App,
		serviceComponentLabelKey:           target.Name,
		serviceDominionAppLabelKey:         env.App,
		serviceDominionEnvironmentLabelKey: env.Name,
	}).String()
}

// Resolve lists EndpointSlices for the resolved Service and returns the first ready ip:port address.
func (c *RuntimeK8sClient) Resolve(ctx context.Context, target *Target, env *Environment) (string, error) {
	serviceName, err := c.lookup(ctx, target, env)
	if err != nil {
		return "", err
	}

	selector := labels.SelectorFromSet(labels.Set{discoveryv1.LabelServiceName: serviceName}).String()
	endpointSlices, err := c.clientset.DiscoveryV1().EndpointSlices(env.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) {
			return "", fmt.Errorf("list EndpointSlices in namespace %q with selector %q: permission denied: %w", env.Namespace, selector, err)
		}
		return "", fmt.Errorf("list EndpointSlices in namespace %q with selector %q: %w", env.Namespace, selector, err)
	}

	for _, endpointSlice := range endpointSlices.Items {
		for _, endpoint := range endpointSlice.Endpoints {
			if !includeEndpoint(endpoint) {
				continue
			}

			for _, address := range endpoint.Addresses {
				address = strings.TrimSpace(address)
				if address == "" {
					continue
				}
				return net.JoinHostPort(address, strconv.Itoa(defaultMongoPort)), nil
			}
		}
	}

	return "", fmt.Errorf("resolve mongo endpoint for target %q/%q in namespace %q: no ready endpoints found for Service %q", target.App, target.Name, env.Namespace, serviceName)
}

func (c *RuntimeK8sClient) lookup(ctx context.Context, target *Target, env *Environment) (string, error) {
	selector := buildServiceSelector(target, env)
	services, err := c.clientset.CoreV1().Services(env.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) {
			return "", fmt.Errorf("list services in namespace %q with selector %q: permission denied: %w", env.Namespace, selector, err)
		}
		return "", fmt.Errorf("list services in namespace %q with selector %q: %w", env.Namespace, selector, err)
	}

	if len(services.Items) == 0 {
		return "", fmt.Errorf("resolve service for target %q/%q in namespace %q: no Services matched selector %q", target.App, target.Name, env.Namespace, selector)
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
			target.Name,
			env.Namespace,
			selector,
			len(names),
			strings.Join(names, ", "),
		)
	}

	serviceName := services.Items[0].Name
	derivedServiceName := deriveServiceName(target, env)
	if serviceName != derivedServiceName {
		return "", fmt.Errorf("resolve service for target %q/%q in namespace %q: service name %q does not match expected derived name %q", target.App, target.Name, env.Namespace, serviceName, derivedServiceName)
	}

	return serviceName, nil
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

func deriveServiceName(target *Target, env *Environment) string {
	return newObjectName(serviceWorkloadKind, target.App, env.App, target.Name, env.Name)
}

func newObjectName(kind string, app string, dominionApp string, serviceName string, environmentName string) string {
	if kind == "" {
		kind = "unknown"
	}

	parts := []string{kind, environmentName, serviceName, shortNameHash(app, dominionApp)}
	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		part = sanitizeNamePart(part)
		if part != "" {
			normalized = append(normalized, part)
		}
	}

	return strings.Join(normalized, "-")
}

func sanitizeNamePart(part string) string {
	part = strings.TrimSpace(strings.ToLower(part))
	part = nonDNSLabel.ReplaceAllString(part, "-")
	part = strings.Trim(part, "-")
	return part
}

func shortNameHash(app string, dominionApp string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(app) + "\x00" + strings.TrimSpace(dominionApp)))
	return hex.EncodeToString(sum[:4])
}
