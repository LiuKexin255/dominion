package domain

import "context"

// AgentProfileRepository defines storage operations for AgentProfile entities.
type AgentProfileRepository interface {
	// Create stores a new AgentProfile.
	Create(ctx context.Context, profile *AgentProfile) error
	// Get retrieves an AgentProfile by its profile name.
	Get(ctx context.Context, profileName string) (*AgentProfile, error)
	// List retrieves a page of AgentProfiles.
	// pageSize controls the maximum number of results; pageToken is the cursor
	// for the next page. Pass empty string for the first page.
	List(ctx context.Context, pageSize int, pageToken string) ([]*AgentProfile, string, error)
	// Delete removes an AgentProfile by its profile name.
	Delete(ctx context.Context, profileName string) error
}

// SkillRepository defines storage operations for Skill entities.
type SkillRepository interface {
	// Create stores a new Skill.
	Create(ctx context.Context, skill *Skill) error
	// Get retrieves a Skill by its skill name.
	Get(ctx context.Context, skillName string) (*Skill, error)
	// List retrieves a page of Skills.
	// pageSize controls the maximum number of results; pageToken is the cursor
	// for the next page. Pass empty string for the first page.
	List(ctx context.Context, pageSize int, pageToken string) ([]*Skill, string, error)
	// Delete removes a Skill by its skill name.
	Delete(ctx context.Context, skillName string) error
}
