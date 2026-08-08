// Package domain defines the memory domain model and repository contract.
package domain

import "time"

// Memory is one planner long-term memory entry of a session (spec
// 039-planner-memory-calibration FR-006;
// specs/039-planner-memory-calibration/contracts/memory-service-contract.md
// §1). It is scoped to (template, session); memory_id is the resource id
// segment {memory} of the resource pattern
// templates/{template}/sessions/{session}/memories/{memory} (FR-012) — the
// agent generates it internally on add, the LLM never sees it (Session
// 2026-08-08 / FR-008).
type Memory struct {
	// Template is the template path segment this memory belongs to.
	Template string
	// SessionID is the session path segment this memory belongs to.
	SessionID string
	// MemoryID is the business identifier for this memory entry.
	MemoryID string
	// Content is the memory entry text.
	Content string
	// CreateTime is the timestamp when this memory was created.
	CreateTime time.Time
	// UpdateTime is the timestamp when this memory was last updated.
	UpdateTime time.Time
}

// DefaultListMemoriesPageSize is the default page size when listing memories.
const DefaultListMemoriesPageSize = 100

// MaxListMemoriesPageSize is the maximum allowed page size when listing memories.
const MaxListMemoriesPageSize = 1000
