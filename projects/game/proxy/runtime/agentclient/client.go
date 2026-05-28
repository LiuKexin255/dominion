// Package agentclient provides the gRPC client wrapper for the AgentService.
package agentclient

import (
	"context"

	game "dominion/projects/game"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

// Client is the interface for agent service operations.
type Client interface {
	CreateAgent(ctx context.Context, req *game.AgentCreateRequest) (*game.AgentStatus, error)
	DeleteAgent(ctx context.Context, req *game.AgentDeleteRequest) (*emptypb.Empty, error)
	GetAgentStatus(ctx context.Context, req *game.GetAgentStatusRequest) (*game.AgentStatus, error)
	Connect(ctx context.Context, opts ...grpc.CallOption) (game.AgentService_ConnectClient, error)
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

// CreateAgent creates an agent for a given session.
func (c *AgentClient) CreateAgent(ctx context.Context, req *game.AgentCreateRequest) (*game.AgentStatus, error) {
	return c.client.CreateAgent(ctx, req)
}

// DeleteAgent deletes the agent for a given session.
func (c *AgentClient) DeleteAgent(ctx context.Context, req *game.AgentDeleteRequest) (*emptypb.Empty, error) {
	return c.client.DeleteAgent(ctx, req)
}

// GetAgentStatus returns the current status of the agent in a session.
func (c *AgentClient) GetAgentStatus(ctx context.Context, req *game.GetAgentStatusRequest) (*game.AgentStatus, error) {
	return c.client.GetAgentStatus(ctx, req)
}

// Connect establishes a bidirectional stream to the agent service.
func (c *AgentClient) Connect(ctx context.Context, opts ...grpc.CallOption) (game.AgentService_ConnectClient, error) {
	return c.client.Connect(ctx, opts...)
}
