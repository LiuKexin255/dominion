package gateway

import (
	"context"
	"fmt"

	"dominion/common/gopkg/grpc/solver"
	pgrpc "dominion/common/gopkg/grpc"
	gwproto "dominion/projects/game/gateway"

	"google.golang.org/grpc"
)

// GRPCGatewayClient implements GatewayClient using the proto-generated gRPC stub.
type GRPCGatewayClient struct {
	client gwproto.GameRuntimeServiceClient
	conn   *grpc.ClientConn
	target string
}

// NewGRPCGatewayClient creates a GRPCGatewayClient connected to the given target URI.
// The target should be in the format "game/gateway:internal-grpc".
func NewGRPCGatewayClient(ctx context.Context, target string) (*GRPCGatewayClient, error) {
	conn, err := grpc.NewClient(solver.URI(target), pgrpc.ClientDefault()...)
	if err != nil {
		return nil, fmt.Errorf("create grpc client for %s: %w", target, err)
	}

	return &GRPCGatewayClient{
		client: gwproto.NewGameRuntimeServiceClient(conn),
		conn:   conn,
		target: target,
	}, nil
}

// Conn returns the underlying gRPC connection for lifecycle management.
func (c *GRPCGatewayClient) Conn() *grpc.ClientConn {
	return c.conn
}

// InitGameRuntime creates a game runtime on a gateway for the given session.
func (c *GRPCGatewayClient) InitGameRuntime(ctx context.Context, sessionID string, reconnectGeneration int64) (*InitResult, error) {
	req := &gwproto.CreateGameRuntimeRequest{
		Parent:              "sessions/" + sessionID,
		ReconnectGeneration: reconnectGeneration,
	}

	resp, err := c.client.CreateGameRuntime(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("init game runtime: %w", err)
	}

	return &InitResult{
		OwnerGatewayID: resp.GetOwnerGatewayId(),
		OwnerEpoch:     resp.GetOwnerEpoch(),
		Token:          resp.GetToken(),
		ExpiresAt:      resp.GetExpiresAt().AsTime(),
	}, nil
}

// RefreshGameRuntime refreshes a game runtime, typically during reconnect.
func (c *GRPCGatewayClient) RefreshGameRuntime(ctx context.Context, sessionID string, oldToken string) (*RefreshResult, error) {
	req := &gwproto.RefreshGameRuntimeRequest{
		Name:     "sessions/" + sessionID + "/game/runtime",
		OldToken: oldToken,
	}

	resp, err := c.client.RefreshGameRuntime(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("refresh game runtime: %w", err)
	}

	return &RefreshResult{
		OwnerGatewayID:      resp.GetOwnerGatewayId(),
		OwnerEpoch:          resp.GetOwnerEpoch(),
		ReconnectGeneration: resp.GetReconnectGeneration(),
		Token:               resp.GetToken(),
		ExpiresAt:           resp.GetExpiresAt().AsTime(),
	}, nil
}
