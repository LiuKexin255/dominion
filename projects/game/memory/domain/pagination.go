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

// MemoryPageCursor is a decoded ListMemories page token: the order_by context
// the token was issued under (Sort, the canonical spelling of the final sort
// key) and the page's last entry position on that key (Entries, one per key
// element). A nil pointer is the first page (the only criterion); a non-nil
// cursor always carries a non-empty Sort and non-empty, typed-ready Entries
// because that is the DecodeMemoryPageToken product contract — hand-building
// one with empty fields violates it. The struct is the token's single
// evolution point (specs/068-wiki-round-pause/contracts/
// memory-page-token-context.md §1).
type MemoryPageCursor struct {
	Sort    []string             `json:"sort"`
	Entries []*MemoryCursorEntry `json:"entries"`
}

// EncodeMemoryPageToken serializes a cursor into a base64url (NoPadding) JSON
// object {"sort":[...],"entries":[...]}. It is a total function: the wire form
// is all strings, so encoding cannot fail; a nil cursor, an empty entries list
// or an empty sort context yields an empty token — a hand-built half cursor
// fails closed instead of minting a token that can never decode
// (specs/068-wiki-round-pause/contracts/memory-page-token-context.md §1-2;
// AIP-158 opacity: https://google.aip.dev/158).
func EncodeMemoryPageToken(cursor *MemoryPageCursor) string {
	if cursor == nil || len(cursor.Entries) == 0 || len(cursor.Sort) == 0 {
		return ""
	}
	// json.Marshal cannot fail for an all-string structure
	// (https://google.github.io/styleguide/go/decisions#handle-errors).
	b, _ := json.Marshal(cursor)
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(b)
}

// DecodeMemoryPageToken decodes a page token, validates it against the
// request's final sort key and returns the resume cursor (always non-nil on
// success). It has two ownership layers
// (specs/068-wiki-round-pause/contracts/memory-page-token-context.md §1-2):
//
//   - page token layer (this function): envelope decoding — empty token,
//     invalid base64/JSON (including a non-object top level), a missing or
//     non-array entries list — and order_by context consistency: the token's
//     carried Sort is compared element for element (length and spelling, so
//     the direction participates) against the request's resolved final key.
//     This is the only order_by consistency check in the token contract; a
//     legacy token without a sort context fails closed on the length
//     comparison, and a direction flip (e.g. "update_time" vs
//     "update_time desc") is rejected.
//   - cursor layer (newMemoryCursor): the carried entries must be consistent
//     with the current cursor implementation — one entry per sort term, a
//     non-null element with a non-empty field and value, the field label
//     aligned with the term at that position, and a value parseable as that
//     term's kind. Each entry's wire value is validated and converted exactly
//     once via the matching term's CursorValue, and the typed value is stored
//     on the entry so the storage layer never converts again.
//
// Every failure wraps ErrInvalidPageToken (AIP-158 "must match":
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
	if cursor.Entries == nil {
		return nil, fmt.Errorf("%w: missing entries", ErrInvalidPageToken)
	}
	if err := validateMemoryPageTokenContext(cursor.Sort, sort); err != nil {
		return nil, err
	}
	return newMemoryCursor(sort, cursor.Entries)
}

// validateMemoryPageTokenContext checks the token's carried order_by context
// against the request's resolved final sort key: same length and same spelling
// element for element. The spelling source is memorySortTermWire — the same
// function cursor construction uses — so the two can never disagree, and a
// direction change is a mismatch like any other
// (specs/068-wiki-round-pause/contracts/memory-page-token-context.md §1-2).
func validateMemoryPageTokenContext(tokenSort []string, sort []*MemorySortTerm) error {
	if len(tokenSort) != len(sort) {
		return fmt.Errorf("%w: token carries %d sort keys, request sorts by %d", ErrInvalidPageToken, len(tokenSort), len(sort))
	}
	for i, term := range sort {
		want := memorySortTermWire(term)
		if tokenSort[i] != want {
			return fmt.Errorf("%w: sort key %d is %q, request sorts by %q", ErrInvalidPageToken, i, tokenSort[i], want)
		}
	}
	return nil
}

// newMemoryCursor builds a cursor from a token's carried entries, checking that
// they are consistent with the current cursor implementation: one entry per
// sort term, no null elements, non-empty field/value, field labels aligned
// with the terms by position, and values parseable as their term's kind. The
// typed value is produced exactly once per entry via CursorValue. This is
// cursor-internal logic, not part of the token contract, so the payload shape
// and its checks can evolve without changing the contract
// (specs/068-wiki-round-pause/contracts/memory-page-token-context.md §1-2).
func newMemoryCursor(sort []*MemorySortTerm, entries []*MemoryCursorEntry) (*MemoryPageCursor, error) {
	if len(entries) == 0 {
		return nil, fmt.Errorf("%w: empty cursor", ErrInvalidPageToken)
	}
	if len(entries) != len(sort) {
		return nil, fmt.Errorf("%w: cursor has %d keys, request sorts by %d", ErrInvalidPageToken, len(entries), len(sort))
	}
	for i, entry := range entries {
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
	return &MemoryPageCursor{Sort: memorySortWire(sort), Entries: entries}, nil
}

// NewMemoryPageCursor builds a page cursor from a final sort key and the
// page's last-entry typed values, one value per term. It is the single cursor
// construction entry point: the order_by context and the entries are filled
// together, so a cursor can never carry a context that disagrees with its own
// key. A value count that does not match the key length panics — a
// programming-time invariant violation, like CursorEntry's kind mismatch
// (specs/068-wiki-round-pause/contracts/memory-page-token-context.md §1).
func NewMemoryPageCursor(sort []*MemorySortTerm, values []any) *MemoryPageCursor {
	if len(values) != len(sort) {
		panic(fmt.Sprintf("NewMemoryPageCursor: %d values for %d sort keys", len(values), len(sort)))
	}
	var entries []*MemoryCursorEntry
	for i, term := range sort {
		entries = append(entries, term.CursorEntry(values[i]))
	}
	return &MemoryPageCursor{Sort: memorySortWire(sort), Entries: entries}
}
