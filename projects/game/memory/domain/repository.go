package domain

import "context"

// MemoryRepository defines storage operations for Memory entities.
type MemoryRepository interface {
	// CreateMemory stores a new Memory. It returns ErrAlreadyExists if a
	// memory with the same (template, session_id, memory_id) already exists.
	CreateMemory(ctx context.Context, memory *Memory) error
	// UpdateMemory replaces the stored Memory identified by
	// memory.MemoryID under (template, session_id). It returns ErrNotFound
	// if no memory with the given id exists.
	UpdateMemory(ctx context.Context, memory *Memory) (*Memory, error)
	// DeleteMemory removes a Memory by template, session and memory id.
	// It returns ErrNotFound if no memory with the given id exists.
	DeleteMemory(ctx context.Context, template, session, memoryID string) error
	// ListMemories retrieves a page of Memories under a session in the given
	// sort key order (AIP-132: https://google.aip.dev/132). sort and cursor
	// are already validated inputs: sort is a ParseMemoryOrderBy product
	// (whitelisted fields completed with the tie-breaker) and cursor is a
	// DecodeMemoryPageToken product; a nil cursor is the first page (the only
	// criterion), and a non-nil cursor always carries non-empty typed-ready
	// entries (its product contract). pageSize controls the maximum number of
	// results; the returned token is empty on the last page
	// (specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md §1
	// item 5).
	ListMemories(ctx context.Context, template, session string, sort []*MemorySortTerm, cursor *MemoryPageCursor, pageSize int) ([]*Memory, string, error)
}
