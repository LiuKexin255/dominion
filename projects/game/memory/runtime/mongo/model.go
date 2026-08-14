// Package mongo provides the MongoDB-backed repository implementation for
// Memory entities (spec 039-planner-memory-calibration FR-006;
// specs/039-planner-memory-calibration/contracts/memory-service-contract.md
// §3).
package mongo

import (
	"time"

	"dominion/projects/game/memory/domain"
)

// BSON field name constants for MongoDB documents.
const (
	fieldMemoryID  = "memory_id"
	fieldTemplate  = "template"
	fieldSessionID = "session_id"
)

// memoryDocument stores Memory documents in MongoDB. The _id is left to the
// database to generate (omitempty on insert) and is never overwritten by the
// repository (style/mongo.md); the unique identity is the
// (template, session_id, memory_id) compound index.
type memoryDocument struct {
	ID         interface{} `bson:"_id,omitempty"`
	Template   string      `bson:"template"`
	SessionID  string      `bson:"session_id"`
	MemoryID   string      `bson:"memory_id"`
	Content    string      `bson:"content"`
	CreateTime time.Time   `bson:"create_time"`
	UpdateTime time.Time   `bson:"update_time"`
}

// toDomain converts a MongoDB document into its domain representation.
func (d *memoryDocument) toDomain() *domain.Memory {
	if d == nil {
		return nil
	}
	return &domain.Memory{
		Template:   d.Template,
		SessionID:  d.SessionID,
		MemoryID:   d.MemoryID,
		Content:    d.Content,
		CreateTime: d.CreateTime,
		UpdateTime: d.UpdateTime,
	}
}

// memoryDocumentFromDomain converts a domain Memory into its MongoDB representation.
func memoryDocumentFromDomain(m *domain.Memory) *memoryDocument {
	if m == nil {
		return nil
	}
	return &memoryDocument{
		Template:   m.Template,
		SessionID:  m.SessionID,
		MemoryID:   m.MemoryID,
		Content:    m.Content,
		CreateTime: m.CreateTime,
		UpdateTime: m.UpdateTime,
	}
}

// memoryFilter is a concrete BSON filter struct for querying by memory_id
// under a (template, session_id) scope.
type memoryFilter struct {
	Template  string `bson:"template"`
	SessionID string `bson:"session_id"`
	MemoryID  string `bson:"memory_id"`
}
