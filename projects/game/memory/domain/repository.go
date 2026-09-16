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
	// order (AIP-132: https://google.aip.dev/132). pageSize controls the
	// maximum number of results; pageToken is the cursor for the next page —
	// pass empty string for the first page. ListMemoriesOrderMemoryIDAsc pages
	// by raw memory_id tokens; ListMemoriesOrderUpdateTimeDesc pages by
	// composite (update_time, memory_id) tokens and returns
	// ErrInvalidPageToken when a token cannot be decoded.
	ListMemories(ctx context.Context, template, session string, pageSize int, pageToken string, order ListMemoriesOrder) ([]*Memory, string, error)
}
