// Package gameconst provides shared constants and helpers for the game services.
package gameconst

import (
	"errors"
	"strings"
)

// gRPC target constants
const (
	SessionTarget = "game/session:grpc"
	// TeamTarget is the gRPC target of the TeamService (hosted by the proxy
	// service, which replaced ProxyService per spec 031-team-template-mode).
	TeamTarget   = "game/proxy:grpc"
	AgentTarget  = "game/agent:grpc"
	PromptTarget = "game/prompt:grpc"

	// Log field constants
	LogFieldName       = "name"
	LogFieldSessionID  = "session_id"
	LogFieldOwner      = "owner"
	LogFieldAgentIndex = "agent_index"
)

// Resource name path segments (AIP-122 https://google.aip.dev/122 collection
// identifiers). templatesPrefix is the leading segment of every resource name
// under a template (spec 031-team-template-mode FR-002).
const (
	templatesPrefix = "templates/"
	sessionsSegment = "sessions"
	teamSegment     = "team"
	agentsSegment   = "agents"
	messagesSegment = "messages"
	profilesSegment = "profiles"
)

// ErrInvalidSessionName is returned when a session name cannot be parsed.
var ErrInvalidSessionName = errors.New("invalid session name")

// ErrInvalidTeamName is returned when a team name cannot be parsed.
var ErrInvalidTeamName = errors.New("invalid team name")

// ErrInvalidTeamProfileName is returned when a team profile name cannot be parsed.
var ErrInvalidTeamProfileName = errors.New("invalid team profile name")

// SessionName returns the resource name for a session under a template:
// "templates/{template}/sessions/{session}".
func SessionName(template, sessionID string) string {
	return templatesPrefix + template + "/" + sessionsSegment + "/" + sessionID
}

// SessionID extracts the template and session ID from a session resource name
// of the form "templates/{template}/sessions/{session}". It returns
// ErrInvalidSessionName if the name is malformed.
func SessionID(name string) (template, sessionID string, err error) {
	segments := strings.Split(name, "/")
	if len(segments) != 4 || segments[0] != "templates" || segments[2] != sessionsSegment ||
		segments[1] == "" || segments[3] == "" {
		return "", "", ErrInvalidSessionName
	}
	return segments[1], segments[3], nil
}

// TeamName returns the resource name for a session's team:
// "templates/{template}/sessions/{session}/team".
func TeamName(template, sessionID string) string {
	return SessionName(template, sessionID) + "/" + teamSegment
}

// TeamSessionID extracts the template and session ID from a team resource name
// of the form "templates/{template}/sessions/{session}/team". It returns
// ErrInvalidTeamName if the name is malformed.
func TeamSessionID(name string) (template, sessionID string, err error) {
	segments := strings.Split(name, "/")
	if len(segments) != 5 || segments[0] != "templates" || segments[2] != sessionsSegment ||
		segments[4] != teamSegment || segments[1] == "" || segments[3] == "" {
		return "", "", ErrInvalidTeamName
	}
	return segments[1], segments[3], nil
}

// MessageAgentName returns the resource name for a message in a team agent's
// message partition (FR-005):
// "templates/{template}/sessions/{session}/team/agents/{agent}/messages/{message}".
func MessageAgentName(template, sessionID, agent, messageID string) string {
	return TeamName(template, sessionID) + "/" + agentsSegment + "/" + agent +
		"/" + messagesSegment + "/" + messageID
}

// MessageAgentParse extracts the template, session ID and agent name from a
// message resource name of the form
// "templates/{template}/sessions/{session}/team/agents/{agent}/messages/{message}".
// A message name is a team resource name with a partition suffix, so a
// malformed name is reported as ErrInvalidTeamName.
func MessageAgentParse(name string) (template, sessionID, agent string, err error) {
	segments := strings.Split(name, "/")
	if len(segments) != 9 || segments[0] != "templates" || segments[2] != sessionsSegment ||
		segments[4] != teamSegment || segments[5] != agentsSegment || segments[7] != messagesSegment ||
		segments[1] == "" || segments[3] == "" || segments[6] == "" || segments[8] == "" {
		return "", "", "", ErrInvalidTeamName
	}
	return segments[1], segments[3], segments[6], nil
}

// TeamProfileName returns the resource name for a team profile under a
// template: "templates/{template}/profiles/{profile}".
func TeamProfileName(template, profileID string) string {
	return templatesPrefix + template + "/" + profilesSegment + "/" + profileID
}

// TeamProfileID extracts the template and profile ID from a team profile
// resource name of the form "templates/{template}/profiles/{profile}". It
// returns ErrInvalidTeamProfileName if the name is malformed.
func TeamProfileID(name string) (template, profileID string, err error) {
	segments := strings.Split(name, "/")
	if len(segments) != 4 || segments[0] != "templates" || segments[2] != profilesSegment ||
		segments[1] == "" || segments[3] == "" {
		return "", "", ErrInvalidTeamProfileName
	}
	return segments[1], segments[3], nil
}
