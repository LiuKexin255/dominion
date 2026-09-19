package domain

import "errors"

var (
	// ErrNotFound indicates the requested resource does not exist.
	ErrNotFound = errors.New("resource not found")
	// ErrAlreadyExists indicates a resource with the given name already exists.
	ErrAlreadyExists = errors.New("resource already exists")
	// ErrInvalidPageToken indicates a page token was rejected: its envelope
	// could not be decoded, its carried request context (the resolved
	// order_by key, or a search's normalized query) does not match the
	// request, or its cursor payload is inconsistent with the implementing
	// cursor
	// (specs/068-wiki-round-pause/contracts/memory-page-token-context.md §1-2,
	// revising specs/065-agent-v2-team-refine/contracts/
	// memory-snapshot-recency.md §1 item 4).
	ErrInvalidPageToken = errors.New("invalid page token")
)
