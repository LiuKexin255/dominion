package domain

import "context"

// AgentProfileRepository defines storage operations for AgentProfile entities.
type AgentProfileRepository interface {
	// CreateAgentProfile stores a new AgentProfile.
	CreateAgentProfile(ctx context.Context, profile *AgentProfile) error
	// GetAgentProfile retrieves an AgentProfile by its profile name.
	GetAgentProfile(ctx context.Context, profileName string) (*AgentProfile, error)
	// ListAgentProfiles retrieves a page of AgentProfiles.
	// pageSize controls the maximum number of results; pageToken is the cursor
	// for the next page. Pass empty string for the first page.
	ListAgentProfiles(ctx context.Context, pageSize int, pageToken string) ([]*AgentProfile, string, error)
	// DeleteAgentProfile removes an AgentProfile by its profile name.
	DeleteAgentProfile(ctx context.Context, profileName string) error
}

// SkillRepository defines storage operations for Skill entities.
type SkillRepository interface {
	// CreateSkill stores a new Skill.
	CreateSkill(ctx context.Context, skill *Skill) error
	// GetSkill retrieves a Skill by its skill name.
	GetSkill(ctx context.Context, skillName string) (*Skill, error)
	// ListSkills retrieves a page of Skills.
	// pageSize controls the maximum number of results; pageToken is the cursor
	// for the next page. Pass empty string for the first page.
	ListSkills(ctx context.Context, pageSize int, pageToken string) ([]*Skill, string, error)
	// DeleteSkill removes a Skill by its skill name.
	DeleteSkill(ctx context.Context, skillName string) error
}
