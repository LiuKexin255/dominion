// Package gameconst provides shared constants and helpers for the game services.
package gameconst

import (
	"errors"

	game "dominion/projects/game"
)

// gRPC target constants
const (
	SessionTarget = "game/session:grpc"
	// ProxyTarget is the gRPC target of the proxy service: the proxy hosts
	// the v2 session face (AgentService + DesktopBridgeService) with owner
	// affinity for the stateful agent_v2 instances
	// (specs/051-agent-v2-dsh-migration/research.md D9).
	ProxyTarget = "game/proxy:grpc"
	// AgentV2Target is the service discovery target of the agent-v2 gRPC
	// service, the stateful dsh-hosted game agent. Two consumers dial it:
	// the proxy's stateful-instance resolver, giving the session face
	// (AgentService/DesktopBridgeService) owner affinity, and the gateway's
	// presetConn, which dials agent-v2 directly for the stateless
	// configuration face (PresetService — preset state lives in Mongo and
	// the model catalog is static, so any instance serves and no proxy hop
	// is needed; specs/051-agent-v2-dsh-migration/revisions/
	// directive-2026-09-01.md §3). The discovery name is
	// "agent-v2" (hyphens — the deploy API's service-name constraint,
	// specs/049-agent-v2-dsh-init/research.md D13) while the project
	// directory and bazel targets keep agent_v2.
	AgentV2Target = "game/agent-v2:grpc"
	// MemoryTarget is the gRPC target of the MemoryService (spec 039
	// planner-memory-calibration, contracts/memory-service-contract.md §5).
	MemoryTarget = "game/memory:grpc"

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
