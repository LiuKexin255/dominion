// Package mongo provides the MongoDB-backed repository implementations for
// AgentProfile and Skill entities.
package mongo

import (
	"time"

	"dominion/projects/game/prompt/domain"
)

// BSON field name constants for MongoDB documents.
const (
	fieldAgentProfileName = "agent_profile_name"
	fieldSkillName        = "skill_name"
	fieldCreateTime       = "create_time"
)

// agentProfileDocument stores AgentProfile documents in MongoDB.
type agentProfileDocument struct {
	ID               interface{} `bson:"_id,omitempty"`
	Name             string      `bson:"name"`
	AgentProfileName string      `bson:"agent_profile_name"`
	Model            string      `bson:"model"`
	SystemPrompt     string      `bson:"system_prompt"`
	SkillNames       []string    `bson:"skill_names"`
	MCPNames         []string    `bson:"mcp_names"`
	Enabled          bool        `bson:"enabled"`
	CreateTime       time.Time   `bson:"create_time"`
	UpdateTime       time.Time   `bson:"update_time"`
}

// toDomain converts a MongoDB document into its domain representation.
func (d *agentProfileDocument) toDomain() *domain.AgentProfile {
	if d == nil {
		return nil
	}

	return &domain.AgentProfile{
		Name:             d.Name,
		AgentProfileName: d.AgentProfileName,
		Model:            d.Model,
		SystemPrompt:     d.SystemPrompt,
		SkillNames:       d.SkillNames,
		MCPNames:         d.MCPNames,
		Enabled:          d.Enabled,
		CreateTime:       d.CreateTime,
		UpdateTime:       d.UpdateTime,
	}
}

// agentProfileDocumentFromDomain converts a domain AgentProfile into its MongoDB representation.
func agentProfileDocumentFromDomain(p *domain.AgentProfile) *agentProfileDocument {
	if p == nil {
		return nil
	}

	return &agentProfileDocument{
		Name:             p.Name,
		AgentProfileName: p.AgentProfileName,
		Model:            p.Model,
		SystemPrompt:     p.SystemPrompt,
		SkillNames:       p.SkillNames,
		MCPNames:         p.MCPNames,
		Enabled:          p.Enabled,
		CreateTime:       p.CreateTime,
		UpdateTime:       p.UpdateTime,
	}
}

// skillDocument stores Skill documents in MongoDB.
type skillDocument struct {
	ID         interface{} `bson:"_id,omitempty"`
	Name       string      `bson:"name"`
	SkillName  string      `bson:"skill_name"`
	Content    string      `bson:"content"`
	Enabled    bool        `bson:"enabled"`
	CreateTime time.Time   `bson:"create_time"`
	UpdateTime time.Time   `bson:"update_time"`
}

// toDomain converts a MongoDB document into its domain representation.
func (d *skillDocument) toDomain() *domain.Skill {
	if d == nil {
		return nil
	}

	return &domain.Skill{
		Name:       d.Name,
		SkillName:  d.SkillName,
		Content:    d.Content,
		Enabled:    d.Enabled,
		CreateTime: d.CreateTime,
		UpdateTime: d.UpdateTime,
	}
}

// skillDocumentFromDomain converts a domain Skill into its MongoDB representation.
func skillDocumentFromDomain(s *domain.Skill) *skillDocument {
	if s == nil {
		return nil
	}

	return &skillDocument{
		Name:       s.Name,
		SkillName:  s.SkillName,
		Content:    s.Content,
		Enabled:    s.Enabled,
		CreateTime: s.CreateTime,
		UpdateTime: s.UpdateTime,
	}
}

// agentProfileFilter is a concrete BSON filter struct for querying by agent_profile_name.
type agentProfileFilter struct {
	AgentProfileName string `bson:"agent_profile_name"`
}

// skillFilter is a concrete BSON filter struct for querying by skill_name.
type skillFilter struct {
	SkillName string `bson:"skill_name"`
}
