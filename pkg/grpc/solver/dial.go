package solver

import (
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	grpcresolver "google.golang.org/grpc/resolver"
)

const (
	defaultKeepaliveTime    = 30 * time.Second
	defaultKeepaliveTimeout = 10 * time.Second
)

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
	// newClientConn bridges grpc client creation for runtime and tests.
	newClientConn = grpc.NewClient
	// defaultDialOptionsProvider returns the default dominion grpc dial options.
	defaultDialOptionsProvider = defaultDialOptions
)

// SetDefaultDialOptionsProvider overrides the default grpc dial-option bundle.
func SetDefaultDialOptionsProvider(provider func() []grpc.DialOption) {
	if provider == nil {
		defaultDialOptionsProvider = defaultDialOptions
		return
	}

	defaultDialOptionsProvider = provider
}

// Register installs the dominion grpc resolver exactly once.
func Register() {
	registerOnce.Do(func() {
		registerResolver(newResolverBuilder())
	})
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

func URI(raw string) string {
	target, err := ParseTarget(raw)
	if err != nil {
		fmt.Printf("%s format error\n", raw)
		return raw
	}

	return fmt.Sprintf("%s:///%s/%s:%d", Scheme, target.App, target.Service, target.Port)
}
