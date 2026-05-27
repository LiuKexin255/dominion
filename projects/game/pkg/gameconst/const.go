// Package gameconst provides shared constants and helpers for the game services.
package gameconst

// gRPC target constants
const (
	SessionTarget = "game/session:grpc"
	ProxyTarget   = "game/proxy:grpc"
	AgentTarget   = "game/agent:grpc"

	SessionNamePrefix = "sessions/"

	// Log field constants
	LogFieldName       = "name"
	LogFieldSessionID  = "session_id"
	LogFieldOwner      = "owner"
	LogFieldAgentIndex = "agent_index"
)

// SessionName returns the resource name for a session.
func SessionName(sessionID string) string {
	return SessionNamePrefix + sessionID
}

// AgentName returns the resource name for an agent.
func AgentName(sessionID string) string {
	return SessionNamePrefix + sessionID + "/agent"
}
