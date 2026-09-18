// Package mongo provides the MongoDB-backed repository implementation for
// TeamProfile entities (spec 031-team-template-mode: TeamProfile replaces the
// former AgentProfile/Skill entities, clean break).
package mongo

import (
	"time"

	"dominion/projects/game/prompt/domain"
)

// BSON field name constants for MongoDB documents.
const (
	fieldTeamProfileName = "team_profile_name"
	fieldTemplate        = "template"
)

// teamProfileDocument stores TeamProfile documents in MongoDB. Each template
// variant of the oneof spec has its own nested document struct with omitempty
// tags so that inactive variants produce no stored fields.
type teamProfileDocument struct {
	ID              interface{}         `bson:"_id,omitempty"`
	TeamProfileName string              `bson:"team_profile_name"`
	Template        string              `bson:"template"`
	Saolei          *saoleiSpecDocument `bson:"saolei,omitempty"`
	CreateTime      time.Time           `bson:"create_time"`
	UpdateTime      time.Time           `bson:"update_time"`
}

// saoleiSpecDocument is the saolei-template oneof variant (proto SaoleiProfile).
type saoleiSpecDocument struct {
	PlayerModel   string `bson:"player_model,omitempty"`
	PlannerModel  string `bson:"planner_model,omitempty"`
	PlayerPrompt  string `bson:"player_prompt,omitempty"`
	PlannerPrompt string `bson:"planner_prompt,omitempty"`
}

// toDomain converts a MongoDB document into its domain representation.
func (d *teamProfileDocument) toDomain() *domain.TeamProfile {
	if d == nil {
		return nil
	}

	tp := &domain.TeamProfile{
		TeamProfileName: d.TeamProfileName,
		Template:        d.Template,
		CreateTime:      d.CreateTime,
		UpdateTime:      d.UpdateTime,
	}
	if d.Saolei != nil {
		tp.SaoleiPlayerModel = d.Saolei.PlayerModel
		tp.SaoleiPlannerModel = d.Saolei.PlannerModel
		tp.SaoleiPlayerPrompt = d.Saolei.PlayerPrompt
		tp.SaoleiPlannerPrompt = d.Saolei.PlannerPrompt
	}
	return tp
}

// teamProfileDocumentFromDomain converts a domain TeamProfile into its MongoDB representation.
func teamProfileDocumentFromDomain(p *domain.TeamProfile) *teamProfileDocument {
	if p == nil {
		return nil
	}

	return &teamProfileDocument{
		TeamProfileName: p.TeamProfileName,
		Template:        p.Template,
		Saolei: &saoleiSpecDocument{
			PlayerModel:   p.SaoleiPlayerModel,
			PlannerModel:  p.SaoleiPlannerModel,
			PlayerPrompt:  p.SaoleiPlayerPrompt,
			PlannerPrompt: p.SaoleiPlannerPrompt,
		},
		CreateTime: p.CreateTime,
		UpdateTime: p.UpdateTime,
	}
}

// teamProfileFilter is a concrete BSON filter struct for querying by
// team_profile_name under a template.
type teamProfileFilter struct {
	TeamProfileName string `bson:"team_profile_name"`
	Template        string `bson:"template"`
}
