package grpc

import (
	"time"

	"dominion/pkg/grpc/solver"
	grpctls "dominion/pkg/grpc/tls"

	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
)

const (
	defaultKeepaliveTime    = 30 * time.Second
	defaultKeepaliveTimeout = 10 * time.Second
)

// ServiceDefault returns the default grpc server options for dominion services.
func ServiceDefault() []grpcgo.ServerOption {
	opts := []grpcgo.ServerOption{}

	if cred := grpctls.ServerTransportCredentials(); cred != nil {
		opts = append(opts, grpcgo.Creds(grpctls.ServerTransportCredentials()))
	}
	return opts
}

// ClientDefault returns the default grpc dial options for dominion clients.
func ClientDefault() []grpcgo.DialOption {

	opts := []grpcgo.DialOption{
		grpcgo.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                defaultKeepaliveTime,
			Timeout:             defaultKeepaliveTimeout,
			PermitWithoutStream: true,
		}),
		grpcgo.WithDefaultServiceConfig(`{"loadBalancingConfig":[{"round_robin":{}}]}`),
		grpcgo.WithResolvers(solver.NewBuilder()),
	}

	if cred := grpctls.ClientTransportCredentials(); cred != nil {
		opts = append(opts, grpcgo.WithTransportCredentials(cred))
	}
	return opts
}
