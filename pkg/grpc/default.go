package grpc

import (
	"time"

	"dominion/pkg/grpc/solver"

	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

const (
	defaultKeepaliveTime    = 30 * time.Second
	defaultKeepaliveTimeout = 10 * time.Second
)

func init() {
	solver.SetDefaultDialOptionsProvider(DefaultDialOptions)
}

// ServiceDefault returns the default grpc server options for dominion services.
func ServiceDefault() []grpcgo.ServerOption {
	return nil
}

// DefaultDialOptions returns the standard dominion grpc dial-option bundle.
func DefaultDialOptions() []grpcgo.DialOption {
	return []grpcgo.DialOption{
		grpcgo.WithTransportCredentials(insecure.NewCredentials()),
		grpcgo.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                defaultKeepaliveTime,
			Timeout:             defaultKeepaliveTimeout,
			PermitWithoutStream: true,
		}),
		grpcgo.WithDefaultServiceConfig(`{"loadBalancingConfig":[{"round_robin":{}}]}`),
		grpcgo.WithResolvers(solver.NewBuilder()),
	}
}

// ClientDefault returns the default grpc dial options for dominion clients.
// It also installs the dominion resolver before returning the option slice.
func ClientDefault() []grpcgo.DialOption {
	solver.Register()
	return DefaultDialOptions()
}
