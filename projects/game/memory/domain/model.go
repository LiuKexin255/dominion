// Package domain defines the memory domain model and repository contract.
package domain

import (
	"fmt"
	"regexp"
	"time"
)

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

// memoryIDPattern is the allowed character set for the memory resource id (the
// {memory} name segment,
// specs/039-planner-memory-calibration/contracts/memory-service-contract.md
// §2/§6). The agent generates memory_id internally on add (spec 039 Session
// 2026-08-08 / FR-008); the service rejects anything outside [a-z0-9_-].
var memoryIDPattern = regexp.MustCompile(`^[a-z0-9_-]+$`)

// ValidateMemoryID reports whether id is a valid memory resource id. Like the
// wiki validators, it owns the resource validation invariant, so the handler
// only maps its error to a status
// (specs/039-planner-memory-calibration/contracts/memory-service-contract.md
// §2/§6).
func ValidateMemoryID(id string) error {
	if !memoryIDPattern.MatchString(id) {
		return fmt.Errorf("memory_id %q must match %s", id, memoryIDPattern.String())
	}
	return nil
}
