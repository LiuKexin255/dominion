package grpc

import (
	"dominion/common/gopkg/grpc/gateway"
	"dominion/common/gopkg/grpc/solver"
	grpctls "dominion/common/gopkg/grpc/tls"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	otelgrpc "go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	grpcgo "google.golang.org/grpc"
)

// ServiceDefault returns the default grpc server options for dominion services.
func ServiceDefault() []grpcgo.ServerOption {
	opts := []grpcgo.ServerOption{
		grpcgo.StatsHandler(otelgrpc.NewServerHandler()),
	}

	if cred := grpctls.ServerTransportCredentials(); cred != nil {
		opts = append(opts, grpcgo.Creds(grpctls.ServerTransportCredentials()))
	}
	return opts
}

// ClientDefault returns the default grpc dial options for dominion clients.
func ClientDefault() []grpcgo.DialOption {

	opts := []grpcgo.DialOption{
		grpcgo.WithDefaultServiceConfig(`{"loadBalancingConfig":[{"round_robin":{}}]}`),
		grpcgo.WithResolvers(solver.NewBuilder()),
		grpcgo.WithStatsHandler(otelgrpc.NewClientHandler()),
	}

	if cred := grpctls.ClientTransportCredentials(); cred != nil {
		opts = append(opts, grpcgo.WithTransportCredentials(cred))
	}
	return opts
}

// GatewayDefault returns the default ServeMux options for dominion grpc-gateway services.
func GatewayDefault() []runtime.ServeMuxOption {
	return []runtime.ServeMuxOption{
		gateway.WithOTelTracing(),
	}
}
