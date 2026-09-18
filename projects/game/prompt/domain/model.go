// Package domain defines the prompt domain model and repository contract.
package domain

import "time"

// TeamProfile is the per-template team configuration entity (spec
// 031-team-template-mode FR-006). It replaces the former AgentProfile/Skill
// resources (clean break): the configuration is a typed, template-specialized
// spec — the saolei template only carries the player/planner LLM model
// choices (FR-027), while tools/MCP are fixed by the template itself (FR-028).
type TeamProfile struct {
	// TeamProfileName is the business identifier for this profile.
	TeamProfileName string
	// Template is the template path segment this profile belongs to
	// (e.g. "saolei").
	Template string
	// SaoleiPlayerModel is the saolei template's player LLM model choice
	// (spec.saolei.player_model, {provider}/{model} format).
	SaoleiPlayerModel string
	// SaoleiPlannerModel is the saolei template's planner LLM model choice
	// (spec.saolei.planner_model).
	SaoleiPlannerModel string
	// SaoleiPlayerPrompt is the saolei player's base prompt (spec.saolei.player_prompt;
	// empty = unset = agent falls back to the template default base,
	// specs/031-team-template-mode/spec.md FR-034).
	SaoleiPlayerPrompt string
	// SaoleiPlannerPrompt is the saolei planner's base prompt (spec.saolei.planner_prompt;
	// empty = unset = agent falls back to the template default base,
	// specs/031-team-template-mode/spec.md FR-034).
	SaoleiPlannerPrompt string
	// CreateTime is the timestamp when this profile was created.
	CreateTime time.Time
	// UpdateTime is the timestamp when this profile was last updated.
	UpdateTime time.Time
}

// DefaultListTeamProfilesPageSize is the default page size when listing team profiles.
const DefaultListTeamProfilesPageSize = 100

// MaxListTeamProfilesPageSize is the maximum allowed page size when listing team profiles.
const MaxListTeamProfilesPageSize = 1000
