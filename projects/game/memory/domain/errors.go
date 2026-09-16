package domain

import "errors"

var (
	// ErrNotFound indicates the requested resource does not exist.
	ErrNotFound = errors.New("resource not found")
	// ErrAlreadyExists indicates a resource with the given name already exists.
	ErrAlreadyExists = errors.New("resource already exists")
	// ErrInvalidPageToken indicates a page token could not be decoded or does
	// not match the request's final sort key
	// (specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md §1
	// item 4).
	ErrInvalidPageToken = errors.New("invalid page token")
)
