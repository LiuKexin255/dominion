// Package gameconst provides shared constants and helpers for the game services.
package gameconst

import (
	"errors"

	game "dominion/projects/game"
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

// ErrInvalidTemplate is returned when a template resource name or path
// segment is malformed or references an unknown template.
var ErrInvalidTemplate = errors.New("invalid template")

// SaoleiTemplate is the saolei template's resource name. Template values are a
// fixed set, declared here as AIP-generated resource-name objects rather than
// a proto enum (spec 031 FR-001: Template is a resource with no CRUD).
var SaoleiTemplate = game.TemplateName{TemplateID: "saolei"}

// knownTemplateIDs is the set of recognized template path segments.
var knownTemplateIDs = map[string]bool{SaoleiTemplate.TemplateID: true}

// ValidateTemplateName reports whether name refers to a known template.
func ValidateTemplateName(name game.TemplateName) error {
	if !knownTemplateIDs[name.TemplateID] {
		return ErrInvalidTemplate
	}
	return nil
}

// IsKnownTemplateID reports whether segment is a known template path segment.
func IsKnownTemplateID(segment string) bool {
	return knownTemplateIDs[segment]
}
