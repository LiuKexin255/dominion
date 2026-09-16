// Package domain defines the memory domain model and repository contract.
package domain

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

// MemoryPageCursor is the composite continuation cursor of the
// ListMemoriesOrderUpdateTimeDesc listing: the sort key of the last entry
// returned on the page (update_time, then memory_id as the tie-break).
type MemoryPageCursor struct {
	UpdateTime time.Time
	MemoryID   string
}

// memoryCursorJSON is the JSON intermediate representation for memory page
// token serialization.
type memoryCursorJSON struct {
	UpdateTime string `json:"update_time"`
	MemoryID   string `json:"memory_id"`
}

// EncodeMemoryPageToken serializes a MemoryPageCursor into a base64url
// (NoPadding) JSON token (AIP-158 opacity:
// https://google.aip.dev/158;
// specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md §1
// item 3).
func EncodeMemoryPageToken(cursor *MemoryPageCursor) (string, error) {
	cj := memoryCursorJSON{
		UpdateTime: cursor.UpdateTime.UTC().Format(time.RFC3339Nano),
		MemoryID:   cursor.MemoryID,
	}
	b, err := json.Marshal(cj)
	if err != nil {
		return "", fmt.Errorf("encode memory page token: %w", err)
	}
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(b), nil
}

// DecodeMemoryPageToken deserializes a base64url (NoPadding) JSON token into a
// MemoryPageCursor. An empty token, invalid base64, invalid JSON, a missing
// field, or an unparseable update_time returns an error.
func DecodeMemoryPageToken(token string) (*MemoryPageCursor, error) {
	if token == "" {
		return nil, fmt.Errorf("decode memory page token: empty token")
	}

	b, err := base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(token)
	if err != nil {
		return nil, fmt.Errorf("decode memory page token: invalid base64: %w", err)
	}

	var cj memoryCursorJSON
	if err := json.Unmarshal(b, &cj); err != nil {
		return nil, fmt.Errorf("decode memory page token: invalid json: %w", err)
	}

	if cj.UpdateTime == "" {
		return nil, fmt.Errorf("decode memory page token: missing update_time")
	}
	if cj.MemoryID == "" {
		return nil, fmt.Errorf("decode memory page token: missing memory_id")
	}

	updateTime, err := time.Parse(time.RFC3339Nano, cj.UpdateTime)
	if err != nil {
		return nil, fmt.Errorf("decode memory page token: invalid update_time: %w", err)
	}

	cursor := &MemoryPageCursor{
		UpdateTime: updateTime,
		MemoryID:   cj.MemoryID,
	}
	return cursor, nil
}
