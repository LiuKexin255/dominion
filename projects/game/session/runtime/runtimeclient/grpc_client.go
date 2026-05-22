package runtimeclient

import (
	"context"
	"errors"
	"fmt"

	"dominion/common/gopkg/grpc/solver"
	pgrpc "dominion/common/gopkg/grpc"
	runtimepb "dominion/projects/game/runtime"

	"google.golang.org/grpc"
)

// ErrNoRuntimeAvailable is returned when no runtime instances are registered.
var ErrNoRuntimeAvailable = errors.New("no runtime available")

// GRPCRuntimeClient implements RuntimeClient using the proto-generated gRPC stub.
type GRPCRuntimeClient struct {
	client runtimepb.GameRuntimeManagerClient
	conn   *grpc.ClientConn
	target string
}

// NewGRPCRuntimeClient creates a GRPCRuntimeClient connected to the given target URI.
// The target should be in the format "game/runtime:internal-grpc".
func NewGRPCRuntimeClient(ctx context.Context, target string) (*GRPCRuntimeClient, error) {
	conn, err := grpc.NewClient(solver.URI(target), pgrpc.ClientDefault()...)
	if err != nil {
		return nil, fmt.Errorf("create grpc client for %s: %w", target, err)
	}

	return &GRPCRuntimeClient{
		client: runtimepb.NewGameRuntimeManagerClient(conn),
		conn:   conn,
		target: target,
	}, nil
}

// Conn returns the underlying gRPC connection for lifecycle management.
func (c *GRPCRuntimeClient) Conn() *grpc.ClientConn {
	return c.conn
}

// InitGameRuntime creates a game runtime on a runtime instance for the given session.
func (c *GRPCRuntimeClient) InitGameRuntime(ctx context.Context, sessionID string, reconnectGeneration int64) (*InitResult, error) {
	req := &runtimepb.CreateGameRuntimeRequest{
		Parent:              "sessions/" + sessionID,
		ReconnectGeneration: reconnectGeneration,
	}

	resp, err := c.client.CreateGameRuntime(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("init game runtime: %w", err)
	}

	return &InitResult{
		OwnerRuntimeID: resp.GetOwnerRuntimeId(),
		OwnerEpoch:     resp.GetOwnerEpoch(),
		Token:          resp.GetToken(),
		ExpiresAt:      resp.GetExpiresAt().AsTime(),
	}, nil
}

// RefreshGameRuntime refreshes a game runtime, typically during reconnect.
func (c *GRPCRuntimeClient) RefreshGameRuntime(ctx context.Context, sessionID string, oldToken string) (*RefreshResult, error) {
	req := &runtimepb.RefreshGameRuntimeRequest{
		Name:     "sessions/" + sessionID + "/game/runtime",
		OldToken: oldToken,
	}

	resp, err := c.client.RefreshGameRuntime(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("refresh game runtime: %w", err)
	}

	return &RefreshResult{
		OwnerRuntimeID:      resp.GetOwnerRuntimeId(),
		OwnerEpoch:          resp.GetOwnerEpoch(),
		ReconnectGeneration: resp.GetReconnectGeneration(),
		Token:               resp.GetToken(),
		ExpiresAt:           resp.GetExpiresAt().AsTime(),
	}, nil
}
