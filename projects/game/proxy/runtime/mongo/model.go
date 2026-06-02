// Package mongo provides the MongoDB-backed OwnerStore, hash-based
// OwnerPicker, and gRPC agent client implementations for the proxy service.
package mongo

import (
	"time"

	"dominion/projects/game/proxy/domain"
)

// agentOwnerDocument stores AgentOwner documents in MongoDB.
type agentOwnerDocument struct {
	SessionID        string    `bson:"session_id"`
	OwnerIndex       int       `bson:"owner_index"`
	Owner            string    `bson:"owner"`
	AgentProfileName string    `bson:"agent_profile_name"`
	CreateTime       time.Time `bson:"create_time"`
}

// toDomain converts a MongoDB document into its domain representation.
func (d *agentOwnerDocument) toDomain() *domain.AgentOwner {
	if d == nil {
		return nil
	}

	return &domain.AgentOwner{
		SessionID:        d.SessionID,
		OwnerIndex:       d.OwnerIndex,
		Owner:            d.Owner,
		AgentProfileName: d.AgentProfileName,
		CreateTime:       d.CreateTime,
	}
}

// agentOwnerDocumentFromDomain converts a domain AgentOwner into its MongoDB representation.
func agentOwnerDocumentFromDomain(a *domain.AgentOwner) *agentOwnerDocument {
	if a == nil {
		return nil
	}

	return &agentOwnerDocument{
		SessionID:        a.SessionID,
		OwnerIndex:       a.OwnerIndex,
		Owner:            a.Owner,
		AgentProfileName: a.AgentProfileName,
		CreateTime:       a.CreateTime,
	}
}
