package runtime

import (
	"context"

	pgrpc "dominion/common/gopkg/grpc"
	"dominion/common/gopkg/grpc/solver"
	game "dominion/projects/game"

	"google.golang.org/grpc"
)

// AgentClient is a gRPC client wrapper for the AgentService.
type AgentClient struct {
	client game.AgentServiceClient
	conn   *grpc.ClientConn
}

// NewAgentClient creates a new gRPC client to the agent service for a specific instance.
func NewAgentClient(ctx context.Context, instanceIndex int) (*AgentClient, error) {
	uri := solver.URI("game/agent:grpc", solver.WithInstance(instanceIndex))
	dialOpts := pgrpc.ClientDefault()
	conn, err := grpc.NewClient(uri, dialOpts...)
	if err != nil {
		return nil, err
	}

	return &AgentClient{
		client: game.NewAgentServiceClient(conn),
		conn:   conn,
	}, nil
}

// InitAgent initializes the agent for a given session.
func (c *AgentClient) InitAgent(ctx context.Context, req *game.InitAgentRequest) (*game.AgentStatus, error) {
	return c.client.InitAgent(ctx, req)
}

// GetAgentStatus returns the current status of the agent in a session.
func (c *AgentClient) GetAgentStatus(ctx context.Context, req *game.GetAgentStatusRequest) (*game.AgentStatus, error) {
	return c.client.GetAgentStatus(ctx, req)
}

// Connect establishes a bidirectional stream to the agent service.
func (c *AgentClient) Connect(ctx context.Context, opts ...grpc.CallOption) (game.AgentService_ConnectClient, error) {
	return c.client.Connect(ctx, opts...)
}

// Close closes the underlying gRPC connection.
func (c *AgentClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
