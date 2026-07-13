// Package agentclient provides the gRPC client wrapper for the AgentService.
package agentclient

import (
	"context"

	game "dominion/projects/game"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

// compile-time interface check
var _ Client = (*AgentClient)(nil)

// Client is the interface for agent service operations.
type Client interface {
	GetAgent(ctx context.Context, req *game.AgentGetRequest) (*game.Agent, error)
	ListMessages(ctx context.Context, req *game.ListMessagesRequest) (*game.ListMessagesResponse, error)
	Connect(ctx context.Context, opts ...grpc.CallOption) (game.AgentService_ConnectClient, error)
	RefreshAgent(ctx context.Context, req *game.RefreshAgentRequest) (*emptypb.Empty, error)
}

// ConnRef is a reference to an agent connection with its owner metadata.
type ConnRef struct {
	OwnerIndex int
	Owner      string
	Conn       *grpc.ClientConn
}

// AgentClient is a gRPC client wrapper for the AgentService.
type AgentClient struct {
	client game.AgentServiceClient
}

// NewAgentClient creates a new gRPC client to the agent service using the given connection.
var NewAgentClient = func(conn *grpc.ClientConn) Client {
	return &AgentClient{client: game.NewAgentServiceClient(conn)}
}

// GetAgent returns the current agent in a session.
func (c *AgentClient) GetAgent(ctx context.Context, req *game.AgentGetRequest) (*game.Agent, error) {
	return c.client.GetAgent(ctx, req)
}

// ListMessages lists messages for an agent.
func (c *AgentClient) ListMessages(ctx context.Context, req *game.ListMessagesRequest) (*game.ListMessagesResponse, error) {
	return c.client.ListMessages(ctx, req)
}

// Connect establishes a bidirectional stream to the agent service.
func (c *AgentClient) Connect(ctx context.Context, opts ...grpc.CallOption) (game.AgentService_ConnectClient, error) {
	return c.client.Connect(ctx, opts...)
}

// RefreshAgent forwards a RefreshAgent call to the agent service.
func (c *AgentClient) RefreshAgent(ctx context.Context, req *game.RefreshAgentRequest) (*emptypb.Empty, error) {
	return c.client.RefreshAgent(ctx, req)
}
