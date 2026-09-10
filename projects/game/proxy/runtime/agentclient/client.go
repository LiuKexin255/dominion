// Package agentclient provides the gRPC connection reference and manager for
// the agent_v2 instance pool the proxy routes stateful session RPCs to
// (specs/051-agent-v2-dsh-migration/research.md D9). The v2 session face
// itself is dialed through the generated game.AgentServiceClient (see
// projects/game/proxy/handler/agent.go).
package agentclient

import (
	"google.golang.org/grpc"
)

// ConnRef is a reference to an agent connection with its owner metadata.
type ConnRef struct {
	OwnerIndex int
	Owner      string
	Conn       *grpc.ClientConn
}
