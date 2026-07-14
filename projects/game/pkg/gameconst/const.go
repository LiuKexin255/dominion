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
	PromptTarget  = "game/prompt:grpc"

	SessionNamePrefix      = "sessions/"
	AgentProfileNamePrefix = "prompts/agentProfiles/"
	SkillNamePrefix        = "prompts/skills/"

	// PromptsParent is the singleton-namespace parent for AgentProfile and
	// Skill resources (AIP-156 https://google.aip.dev/156). It exists purely
	// as a path-prefix segment; no Prompt resource message exists. Create
	// RPCs under prompts/ carry this literal in their parent field.
	PromptsParent = "prompts"

	// Log field constants
	LogFieldName       = "name"
	LogFieldSessionID  = "session_id"
	LogFieldOwner      = "owner"
	LogFieldAgentIndex = "agent_index"
)

// ErrInvalidSessionName is returned when a session name cannot be parsed.
var ErrInvalidSessionName = errors.New("invalid session name")

// ErrInvalidAgentProfileName is returned when an agent profile name cannot be parsed.
var ErrInvalidAgentProfileName = errors.New("invalid agent profile name")

// ErrInvalidSkillName is returned when a skill name cannot be parsed.
var ErrInvalidSkillName = errors.New("invalid skill name")

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

// ErrInvalidAgentName is returned when an agent name cannot be parsed.
var ErrInvalidAgentName = errors.New("invalid agent name")

const agentNameSuffix = "/agent"

// AgentSessionID extracts the session ID from an agent resource name of the
// form "sessions/{session}/agent". It returns ErrInvalidAgentName if the name
// is malformed.
func AgentSessionID(name string) (string, error) {
	if !strings.HasSuffix(name, agentNameSuffix) {
		return "", ErrInvalidAgentName
	}
	return SessionID(strings.TrimSuffix(name, agentNameSuffix))
}

// AgentProfileName returns the resource name for an agent profile.
func AgentProfileName(profileID string) string {
	return AgentProfileNamePrefix + profileID
}

// AgentProfileID extracts the agent profile ID from a resource name.
// It returns ErrInvalidAgentProfileName if the name is malformed.
func AgentProfileID(name string) (string, error) {
	if !strings.HasPrefix(name, AgentProfileNamePrefix) {
		return "", ErrInvalidAgentProfileName
	}
	id := strings.TrimPrefix(name, AgentProfileNamePrefix)
	if id == "" || strings.Contains(id, "/") {
		return "", ErrInvalidAgentProfileName
	}
	return id, nil
}

// SkillName returns the resource name for a skill.
func SkillName(skillID string) string {
	return SkillNamePrefix + skillID
}

// SkillID extracts the skill ID from a resource name.
// It returns ErrInvalidSkillName if the name is malformed.
func SkillID(name string) (string, error) {
	if !strings.HasPrefix(name, SkillNamePrefix) {
		return "", ErrInvalidSkillName
	}
	id := strings.TrimPrefix(name, SkillNamePrefix)
	if id == "" || strings.Contains(id, "/") {
		return "", ErrInvalidSkillName
	}
	return id, nil
}
