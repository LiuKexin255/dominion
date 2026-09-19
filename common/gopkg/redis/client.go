package redis

import (
	"context"
	"fmt"
	"os"
	"strings"

	"dominion/common/gopkg/logs"
	"dominion/common/gopkg/logs/event"
	"dominion/common/gopkg/solver"

	goredis "github.com/redis/go-redis/v9"
)

var (
	lookupEnv = os.LookupEnv
	// newRedisClient is a variable so tests can capture the resolved options
	// without dialing a server (style/golang.md 单测：不访问外部依赖).
	newRedisClient = func(options *goredis.Options) *goredis.Client {
		return goredis.NewClient(options)
	}

	logFieldApp          = "app"
	logFieldService      = "service"
	logFieldAddress      = "address"
	logFieldAddressCount = "address_count"
)

const dominionEnvironmentEnvKey = "DOMINION_ENVIRONMENT"

// ClientOption configures NewClient.
type ClientOption func(opts *ClientOptions)

// ClientOptions holds the resolved NewClient configuration.
type ClientOptions struct {
	resolverBuilder func() (solver.Resolver, error)
}

func defaultOptions() *ClientOptions {
	return &ClientOptions{
		resolverBuilder: solver.NewDeployResolver,
	}
}

// WithK8sResolver configures NewClient to use the legacy Kubernetes resolver.
func WithK8sResolver() ClientOption {
	return func(opts *ClientOptions) {
		opts.resolverBuilder = solver.NewK8sResolver
	}
}

// NewClient creates a Redis client for the given dominion target.
//
// The target is "app/name"; the default redis port is applied before endpoint
// resolution. The client connects lazily: no Ping is issued here, so a server
// that is down surfaces on the first command instead of at construction.
// Closing the client is the caller's responsibility.
func NewClient(rawTarget string, opts ...ClientOption) (*goredis.Client, error) {
	options := defaultOptions()
	for _, opt := range opts {
		opt(options)
	}

	target, err := parseRedisTarget(rawTarget)
	if err != nil {
		return nil, fmt.Errorf("invalid target %q: want app/name", rawTarget)
	}
	logs.Info(context.Background(), "redis client initializing", event.String(logFieldApp, target.App), event.String(logFieldService, target.Service))

	resolver, err := options.resolverBuilder()
	if err != nil {
		return nil, err
	}

	addresses, err := resolver.Resolve(context.Background(), target)
	if err != nil {
		return nil, err
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("resolve redis endpoint for target %q/%q: no ready endpoints found", target.App, target.Service)
	}
	logs.Info(context.Background(), "redis endpoints resolved", event.Int(logFieldAddressCount, len(addresses)))
	address := addresses[0]
	logs.Info(context.Background(), "redis endpoint selected", event.String(logFieldAddress, address))

	client := newRedisClient(&goredis.Options{
		Addr:     address,
		Password: generateStablePassword(target.App, environmentName(), target.Service),
	})

	return client, nil
}

func parseRedisTarget(rawTarget string) (*solver.Target, error) {
	trimmed := strings.TrimSpace(rawTarget)
	app, service, ok := strings.Cut(trimmed, "/")
	if !ok {
		return nil, fmt.Errorf("split target %q", rawTarget)
	}

	app = strings.TrimSpace(app)
	service = strings.TrimSpace(service)
	if app == "" || service == "" || strings.Contains(service, "/") {
		return nil, fmt.Errorf("invalid target %q", rawTarget)
	}

	return &solver.Target{
		App:          app,
		Service:      service,
		PortSelector: solver.NumericPort(defaultRedisPort),
	}, nil
}

func environmentName() string {
	if value, ok := lookupEnv(dominionEnvironmentEnvKey); ok {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}

	return defaultEnvironmentName
}
