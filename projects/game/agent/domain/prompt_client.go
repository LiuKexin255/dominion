package domain

import "context"

// ProfileInfo holds the profile data needed by the agent runtime.
type ProfileInfo struct {
	AgentProfileName string
	Model            string
	SystemPrompt     string
	SkillNames       []string
	MCPNames         []string
	Enabled          bool
}

// SkillInfo holds the skill data needed by the agent runtime.
type SkillInfo struct {
	SkillName string
	Content   string
	Enabled   bool
}

// PromptServiceClient defines the interface for the agent to interact with
// the prompt service for profile and skill lookups.
type PromptServiceClient interface {
	// GetProfile retrieves an agent profile by name.
	GetProfile(ctx context.Context, profileName string) (*ProfileInfo, error)
	// GetSkill retrieves a skill by name.
	GetSkill(ctx context.Context, skillName string) (*SkillInfo, error)
}
