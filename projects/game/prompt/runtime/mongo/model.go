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
	fieldPlayerModel     = "player_model"
	fieldPlannerModel    = "planner_model"
	fieldPlayerPrompt    = "player_prompt"
	fieldPlannerPrompt   = "planner_prompt"
	fieldCreateTime      = "create_time"
	fieldUpdateTime      = "update_time"
)

// teamProfileDocument stores TeamProfile documents in MongoDB.
// The saolei oneof spec is flattened into the player_model/planner_model and
// player_prompt/planner_prompt fields; other templates add their own spec
// fields (typed, no blobs).
type teamProfileDocument struct {
	ID                  interface{} `bson:"_id,omitempty"`
	TeamProfileName     string      `bson:"team_profile_name"`
	Template            string      `bson:"template"`
	SaoleiPlayerModel   string      `bson:"player_model"`
	SaoleiPlannerModel  string      `bson:"planner_model"`
	SaoleiPlayerPrompt  string      `bson:"player_prompt"`
	SaoleiPlannerPrompt string      `bson:"planner_prompt"`
	CreateTime          time.Time   `bson:"create_time"`
	UpdateTime          time.Time   `bson:"update_time"`
}

// toDomain converts a MongoDB document into its domain representation.
func (d *teamProfileDocument) toDomain() *domain.TeamProfile {
	if d == nil {
		return nil
	}

	return &domain.TeamProfile{
		TeamProfileName:     d.TeamProfileName,
		Template:            d.Template,
		SaoleiPlayerModel:   d.SaoleiPlayerModel,
		SaoleiPlannerModel:  d.SaoleiPlannerModel,
		SaoleiPlayerPrompt:  d.SaoleiPlayerPrompt,
		SaoleiPlannerPrompt: d.SaoleiPlannerPrompt,
		CreateTime:          d.CreateTime,
		UpdateTime:          d.UpdateTime,
	}
}

// teamProfileDocumentFromDomain converts a domain TeamProfile into its MongoDB representation.
func teamProfileDocumentFromDomain(p *domain.TeamProfile) *teamProfileDocument {
	if p == nil {
		return nil
	}

	return &teamProfileDocument{
		TeamProfileName:     p.TeamProfileName,
		Template:            p.Template,
		SaoleiPlayerModel:   p.SaoleiPlayerModel,
		SaoleiPlannerModel:  p.SaoleiPlannerModel,
		SaoleiPlayerPrompt:  p.SaoleiPlayerPrompt,
		SaoleiPlannerPrompt: p.SaoleiPlannerPrompt,
		CreateTime:          p.CreateTime,
		UpdateTime:          p.UpdateTime,
	}
}

// teamProfileFilter is a concrete BSON filter struct for querying by
// team_profile_name under a template.
type teamProfileFilter struct {
	TeamProfileName string `bson:"team_profile_name"`
	Template        string `bson:"template"`
}
