// Package domain defines the prompt domain model and repository contract.
package domain

import "time"

// AgentProfile describes the configuration for creating an agent.
type AgentProfile struct {
	// Name is the resource name, e.g. "agentProfiles/my-profile".
	Name string
	// AgentProfileName is the business identifier for this profile.
	AgentProfileName string
	// Model is the model name to use.
	Model string
	// SystemPrompt is the system prompt for the agent.
	SystemPrompt string
	// SkillNames are names of skills referenced by this profile.
	SkillNames []string
	// MCPNames are names of MCP servers referenced by this profile.
	MCPNames []string
	// Enabled indicates whether this profile is enabled.
	Enabled bool
	// CreateTime is the timestamp when this profile was created.
	CreateTime time.Time
	// UpdateTime is the timestamp when this profile was last updated.
	UpdateTime time.Time
}

// Skill represents a tool-independent skill definition.
type Skill struct {
	// Name is the resource name, e.g. "skills/my-skill".
	Name string
	// SkillName is the business identifier for this skill.
	SkillName string
	// Content is the skill content (text).
	Content string
	// Enabled indicates whether this skill is enabled.
	Enabled bool
	// CreateTime is the timestamp when this skill was created.
	CreateTime time.Time
	// UpdateTime is the timestamp when this skill was last updated.
	UpdateTime time.Time
}
