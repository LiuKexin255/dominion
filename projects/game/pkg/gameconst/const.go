// Package gameconst provides shared constants and helpers for the game services.
package gameconst

import (
	"errors"
	"strings"
)

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

// ErrInvalidSessionName is returned when a session name cannot be parsed.
var ErrInvalidSessionName = errors.New("invalid session name")

// SessionName returns the resource name for a session.
func SessionName(sessionID string) string {
	return SessionNamePrefix + sessionID
}

// SessionID extracts the session ID from a session resource name.
// It returns ErrInvalidSessionName if the name is malformed.
func SessionID(name string) (string, error) {
	if !strings.HasPrefix(name, SessionNamePrefix) {
		return "", ErrInvalidSessionName
	}
	id := strings.TrimPrefix(name, SessionNamePrefix)
	if id == "" || strings.Contains(id, "/") {
		return "", ErrInvalidSessionName
	}
	return id, nil
}

// AgentName returns the resource name for an agent.
func AgentName(sessionID string) string {
	return SessionNamePrefix + sessionID + "/agent"
}
