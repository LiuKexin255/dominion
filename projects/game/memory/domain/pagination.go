// Package domain defines the memory domain model and repository contract.
package domain

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// MemoryCursorEntry is one final-sort-key value pair of a page token: the API
// field name and its string-encoded value. Time values are UTC RFC3339Nano.
// The struct is the wire form itself — no conversion layer; the unexported
// typed field carries the value's typed form (produced once by
// MemorySortTerm.CursorValue during decoding) and is invisible to
// encoding/json, so the wire shape stays {field, value}
// (specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md §1
// items 4/9).
type MemoryCursorEntry struct {
	Field string `json:"field"`
	Value string `json:"value"`

	typed any
}

// Typed returns the entry's typed value (string or time.Time): the value a
// query built from this cursor position uses directly, with no further
// conversion (specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md
// §1 item 9).
func (e *MemoryCursorEntry) Typed() any {
	return e.typed
}

// MemoryPageCursor is a decoded ListMemories page token: the page's last entry
// position on the final sort key, one entry per key element. A nil pointer is
// the first page (the only criterion); a non-nil cursor always carries
// non-empty, typed-ready entries because that is the DecodeMemoryPageToken
// product contract — hand-building one with empty Entries violates it. Future
// token parameters extend the struct without changing any signature
// (specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md §1
// item 4).
type MemoryPageCursor struct {
	Entries []*MemoryCursorEntry `json:"entries"`
}

// EncodeMemoryPageToken serializes a cursor into a base64url (NoPadding) JSON
// object {"entries":[...]}. It is a total function: the wire form is all
// strings, so encoding cannot fail; a nil cursor or an empty entries list
// yields an empty token (specs/065-agent-v2-team-refine/contracts/
// memory-snapshot-recency.md §1 item 4; AIP-158 opacity:
// https://google.aip.dev/158).
func EncodeMemoryPageToken(cursor *MemoryPageCursor) string {
	if cursor == nil || len(cursor.Entries) == 0 {
		return ""
	}
	// json.Marshal cannot fail for an all-string structure
	// (https://google.github.io/styleguide/go/decisions#handle-errors).
	b, _ := json.Marshal(cursor)
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(b)
}

// DecodeMemoryPageToken decodes a page token and validates it against the
// request's final sort key, returning the resume cursor (always non-nil on
// success). It rejects empty tokens, invalid base64/JSON (including a
// non-object top level), a missing, non-array or empty entries list, null
// entries, empty fields or values, a field sequence that does not match sort
// element for element (length or name — covering unknown fields and
// cross-order replay), and values that do not parse as their field's type.
// Each entry's wire value is validated and converted exactly once via the
// matching term's CursorValue, and the typed value is stored on the entry so
// the storage layer never converts again; every failure wraps
// ErrInvalidPageToken (specs/065-agent-v2-team-refine/contracts/
// memory-snapshot-recency.md §1 item 4; AIP-158 "must match":
// https://google.aip.dev/158).
func DecodeMemoryPageToken(token string, sort []*MemorySortTerm) (*MemoryPageCursor, error) {
	if token == "" {
		return nil, fmt.Errorf("%w: empty token", ErrInvalidPageToken)
	}
	b, err := base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(token)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid base64: %v", ErrInvalidPageToken, err)
	}
	var cursor MemoryPageCursor
	if err := json.Unmarshal(b, &cursor); err != nil {
		return nil, fmt.Errorf("%w: invalid json: %v", ErrInvalidPageToken, err)
	}
	if len(cursor.Entries) == 0 {
		return nil, fmt.Errorf("%w: empty cursor", ErrInvalidPageToken)
	}
	if len(cursor.Entries) != len(sort) {
		return nil, fmt.Errorf("%w: cursor has %d keys, request sorts by %d", ErrInvalidPageToken, len(cursor.Entries), len(sort))
	}
	for i, entry := range cursor.Entries {
		if entry == nil {
			return nil, fmt.Errorf("%w: key %d is null", ErrInvalidPageToken, i)
		}
		if entry.Field == "" {
			return nil, fmt.Errorf("%w: key %d is missing field", ErrInvalidPageToken, i)
		}
		if entry.Value == "" {
			return nil, fmt.Errorf("%w: key %d is missing value", ErrInvalidPageToken, i)
		}
		if entry.Field != sort[i].Field {
			return nil, fmt.Errorf("%w: key %d is %q, request sorts by %q", ErrInvalidPageToken, i, entry.Field, sort[i].Field)
		}
		typed, err := sort[i].CursorValue(entry.Value)
		if err != nil {
			return nil, fmt.Errorf("%w: key %d (%s): %v", ErrInvalidPageToken, i, entry.Field, err)
		}
		entry.typed = typed
	}
	return &cursor, nil
}
