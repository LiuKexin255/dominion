package solver

import (
	"fmt"
	"sync"
	"time"

	"dominion/common/gopkg/solver"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	grpcresolver "google.golang.org/grpc/resolver"
)

const (
	defaultKeepaliveTime    = 30 * time.Second
	defaultKeepaliveTimeout = 10 * time.Second
)

// StatefulScheme is the grpc resolver scheme for stateful service instances.
const StatefulScheme = "dominion-stateful"

var (
	// registerOnce keeps dominion resolver registration idempotent.
	registerOnce sync.Once
	// registerResolver bridges grpc resolver registration for runtime and tests.
	registerResolver = func(builder grpcresolver.Builder) {
		grpcresolver.Register(builder)
	}
	// newResolverBuilder constructs the dominion resolver builder.
	newResolverBuilder = func() grpcresolver.Builder {
		return NewBuilder()
	}
	// newStatefulResolverBuilder constructs the dominion-stateful resolver builder.
	newStatefulResolverBuilder = func() grpcresolver.Builder {
		return NewStatefulBuilder()
	}
	// newClientConn bridges grpc client creation for runtime and tests.
	newClientConn = grpc.NewClient
)

func init() {
	Register()
}

// Register installs the dominion grpc resolvers exactly once.
func Register() {
	registerOnce.Do(func() {
		registerResolver(newResolverBuilder())
		registerResolver(newStatefulResolverBuilder())
	})
}

// URIOption configures URI generation options.
type URIOption func(*uriConfig)

// uriConfig holds URI generation settings.
type uriConfig struct {
	instance *int
}

// WithInstance specifies the stateful service instance index (0-based).
func WithInstance(index int) URIOption {
	return func(c *uriConfig) {
		c.instance = &index
	}
}

func defaultDialOptions() []grpc.DialOption {
	return []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                defaultKeepaliveTime,
			Timeout:             defaultKeepaliveTimeout,
			PermitWithoutStream: true,
		}),
		grpc.WithResolvers(newResolverBuilder()),
	}
}

func URI(raw string, opts ...URIOption) string {
	target, err := solver.ParseTarget(raw)
	if err != nil {
		return raw
	}

	config := new(uriConfig)
	for _, opt := range opts {
		if opt != nil {
			opt(config)
		}
	}

	if config.instance != nil {
		return fmt.Sprintf("%s:///%s/%s:%s?%s=%d", StatefulScheme, target.App, target.Service, target.PortSelector.String(), instanceQueryParam, *config.instance)
	}

	return fmt.Sprintf("%s:///%s/%s:%s", Scheme, target.App, target.Service, target.PortSelector.String())
}
