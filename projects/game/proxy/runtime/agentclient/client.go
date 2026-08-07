// Package agentclient provides the gRPC client wrapper for the TeamService
// hosted by the agent service (specs/031-team-template-mode/: ProxyService and
// AgentService merged into TeamService).
package agentclient

import (
	"context"

	game "dominion/projects/game"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

// compile-time interface check
var _ Client = (*AgentClient)(nil)

// Client is the interface for downstream agent (TeamService) operations.
type Client interface {
	UpdateTeam(ctx context.Context, req *game.UpdateTeamRequest) (*game.Team, error)
	GetTeam(ctx context.Context, req *game.GetTeamRequest) (*game.Team, error)
	ListMessages(ctx context.Context, req *game.ListMessagesRequest) (*game.ListMessagesResponse, error)
	Connect(ctx context.Context, opts ...grpc.CallOption) (game.TeamService_ConnectClient, error)
	RefreshTeam(ctx context.Context, req *game.RefreshTeamRequest) (*emptypb.Empty, error)
}

// ConnRef is a reference to an agent connection with its owner metadata.
type ConnRef struct {
	OwnerIndex int
	Owner      string
	Conn       *grpc.ClientConn
}

// AgentClient is a gRPC client wrapper for the TeamService.
type AgentClient struct {
	client game.TeamServiceClient
}

// NewAgentClient creates a new gRPC client to the agent service using the given connection.
var NewAgentClient = func(conn *grpc.ClientConn) Client {
	return &AgentClient{client: game.NewTeamServiceClient(conn)}
}

// UpdateTeam forwards an UpdateTeam call to the agent service (the agent
// materializes or mutates the session's team graph from the requested
// TeamProfile; specs/040-team-singleton-conformance/contracts/api-contract.md
// §2 — replacing the former CreateTeam).
func (c *AgentClient) UpdateTeam(ctx context.Context, req *game.UpdateTeamRequest) (*game.Team, error) {
	return c.client.UpdateTeam(ctx, req)
}

// GetTeam returns the Team of a session.
func (c *AgentClient) GetTeam(ctx context.Context, req *game.GetTeamRequest) (*game.Team, error) {
	return c.client.GetTeam(ctx, req)
}

// ListMessages lists messages for a team agent's partition.
func (c *AgentClient) ListMessages(ctx context.Context, req *game.ListMessagesRequest) (*game.ListMessagesResponse, error) {
	return c.client.ListMessages(ctx, req)
}

// Connect establishes a bidirectional stream to the agent service.
func (c *AgentClient) Connect(ctx context.Context, opts ...grpc.CallOption) (game.TeamService_ConnectClient, error) {
	return c.client.Connect(ctx, opts...)
}

// RefreshTeam forwards a RefreshTeam call to the agent service.
func (c *AgentClient) RefreshTeam(ctx context.Context, req *game.RefreshTeamRequest) (*emptypb.Empty, error) {
	return c.client.RefreshTeam(ctx, req)
}
