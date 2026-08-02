// Package mongo provides the MongoDB-backed OwnerStore, hash-based
// OwnerPicker, and gRPC agent client implementations for the proxy service.
package mongo

import (
	"time"

	"dominion/projects/game/proxy/domain"
)

// agentOwnerDocument stores AgentOwner documents in MongoDB.
type agentOwnerDocument struct {
	TemplateID string    `bson:"template_id"`
	SessionID  string    `bson:"session_id"`
	OwnerIndex int       `bson:"owner_index"`
	Owner      string    `bson:"owner"`
	CreateTime time.Time `bson:"create_time"`
}

// toDomain converts a MongoDB document into its domain representation.
func (d *agentOwnerDocument) toDomain() *domain.AgentOwner {
	if d == nil {
		return nil
	}

	return &domain.AgentOwner{
		TemplateID: d.TemplateID,
		SessionID:  d.SessionID,
		OwnerIndex: d.OwnerIndex,
		Owner:      d.Owner,
		CreateTime: d.CreateTime,
	}
}

// agentOwnerDocumentFromDomain converts a domain AgentOwner into its MongoDB representation.
func agentOwnerDocumentFromDomain(a *domain.AgentOwner) *agentOwnerDocument {
	if a == nil {
		return nil
	}

	return &agentOwnerDocument{
		TemplateID: a.TemplateID,
		SessionID:  a.SessionID,
		OwnerIndex: a.OwnerIndex,
		Owner:      a.Owner,
		CreateTime: a.CreateTime,
	}
}
