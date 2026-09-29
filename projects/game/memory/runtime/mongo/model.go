package mongo

import (
	"fmt"
	"time"

	"dominion/projects/game/memory/domain"
)

// BSON field name constants for MongoDB documents.
const (
	fieldMemoryID   = "memory_id"
	fieldTemplate   = "template"
	fieldPresetID   = "preset_id"
	fieldUpdateTime = "update_time"
)

// memoryDocument stores Memory documents in MongoDB. The _id is left to the
// database to generate (omitempty on insert) and is never overwritten by the
// repository (style/mongo.md); the unique identity is the
// (template, preset_id, memory_id) compound index
// (specs/071-memory-preset-scope/data-model.md §2.1-2.2). There is no session
// field: the memory scope is (template, preset), so a preset's entries persist
// across the sessions it is materialized into.
type memoryDocument struct {
	ID         interface{} `bson:"_id,omitempty"`
	Template   string      `bson:"template"`
	PresetID   string      `bson:"preset_id"`
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
		PresetID:   d.PresetID,
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
		PresetID:   m.PresetID,
		MemoryID:   m.MemoryID,
		Content:    m.Content,
		CreateTime: m.CreateTime,
		UpdateTime: m.UpdateTime,
	}
}

// sortValue returns the document's typed value for a whitelisted API sort
// field: the value a page token carries for that key, with no formatting
// (times stay time.Time — the wire form belongs to
// MemorySortTerm.CursorEntry). Unknown fields fail fast so a whitelist row
// without a matching case surfaces immediately
// (specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md §1
// item 9).
func (d *memoryDocument) sortValue(field string) any {
	switch field {
	case domain.MemorySortFieldMemoryID:
		return d.MemoryID
	case domain.MemorySortFieldUpdateTime:
		return d.UpdateTime
	default:
		panic(fmt.Sprintf("memoryDocument.sortValue: field %q is not a whitelisted sort field", field))
	}
}

// memoryFilter is a concrete BSON filter struct for querying by memory_id
// under a (template, preset_id) scope.
type memoryFilter struct {
	Template string `bson:"template"`
	PresetID string `bson:"preset_id"`
	MemoryID string `bson:"memory_id"`
}
