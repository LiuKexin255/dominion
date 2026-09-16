// Package domain defines the memory domain model and repository contract.
package domain

import (
	"fmt"
	"strings"
	"time"
)

// Memory sort field names: the API field vocabulary of ListMemories.order_by
// (specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md §1).
const (
	// MemorySortFieldMemoryID is the memory identity field, unique within a
	// (template, session) scope.
	MemorySortFieldMemoryID = "memory_id"
	// MemorySortFieldUpdateTime is the last-update timestamp field.
	MemorySortFieldUpdateTime = "update_time"
)

// memorySortValueKind is the value type of a whitelisted sort field. The
// cursor codec interprets wire values by this kind, so no per-field branch
// exists outside the whitelist and the term's symmetric CursorValue/CursorEntry
// methods (specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md
// §1 item 4).
type memorySortValueKind int

const (
	// memorySortValueString is a string-valued sort field. It is the zero
	// kind, so hand-built test terms (zero kind) carry string semantics.
	memorySortValueString memorySortValueKind = iota
	// memorySortValueTime is a time.Time-valued sort field whose wire form is
	// UTC RFC3339Nano.
	memorySortValueTime
)

// memorySortFieldSpec is one whitelist entry: the API sort field, its MongoDB
// document field, and the value kind its wire values carry.
type memorySortFieldSpec struct {
	Field      string
	MongoField string
	kind       memorySortValueKind
}

// memorySortTieBreaker is the designated unique tie-breaker: a final sort key
// that contains no memory_id gets it appended, making the order a total order
// (specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md §1
// item 2).
var memorySortTieBreaker = &memorySortFieldSpec{
	Field:      MemorySortFieldMemoryID,
	MongoField: "memory_id",
	kind:       memorySortValueString,
}

// memorySortFieldSpecs is the single source of truth for sortable fields
// (specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md §1
// item 1). Adding a sortable field means adding one row here, one
// memoryDocument.sortValue case, and the compound index backing the consumed
// direction — no other per-field branches exist.
var memorySortFieldSpecs = []*memorySortFieldSpec{
	memorySortTieBreaker,
	{
		Field:      MemorySortFieldUpdateTime,
		MongoField: "update_time",
		kind:       memorySortValueTime,
	},
}

// memorySortFieldSpecByField returns the whitelist entry for an API sort
// field.
func memorySortFieldSpecByField(field string) (*memorySortFieldSpec, bool) {
	for _, spec := range memorySortFieldSpecs {
		if spec.Field == field {
			return spec, true
		}
	}
	return nil, false
}

// memorySortFieldNames lists the whitelist's API field names for error
// messages.
func memorySortFieldNames() string {
	names := make([]string, 0, len(memorySortFieldSpecs))
	for _, spec := range memorySortFieldSpecs {
		names = append(names, fmt.Sprintf("%q", spec.Field))
	}
	return strings.Join(names, ", ")
}

// memoryOrderByHelp is appended to every order_by parse error so the message
// always states the supported syntax and fields (specs/065-agent-v2-team-
// refine/contracts/memory-snapshot-recency.md §1 item 1; AIP-193:
// https://google.aip.dev/193).
var memoryOrderByHelp = fmt.Sprintf(
	`supported syntax: comma-separated "{field}" or "{field} desc" terms over supported fields: %s`,
	memorySortFieldNames(),
)

// MemorySortTerm is one element of the final ListMemories sort key. It is
// self-contained: the API field name, the Mongo field it maps to and the
// direction travel together, so the storage layer can translate the key
// without looking the whitelist up again. It is also the only source of
// knowledge for cursor value conversion, carrying the symmetric pair
// CursorValue (wire → typed) and CursorEntry (typed → wire)
// (specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md §1
// items 5/9).
type MemorySortTerm struct {
	Field      string
	MongoField string
	Descending bool

	kind memorySortValueKind
}

// term joins a whitelist spec with a direction into one final-key term.
func (s *memorySortFieldSpec) term(descending bool) *MemorySortTerm {
	return &MemorySortTerm{
		Field:      s.Field,
		MongoField: s.MongoField,
		Descending: descending,
		kind:       s.kind,
	}
}

// CursorValue parses a cursor wire value as this term's field type: string
// values pass through unchanged, time values are parsed as RFC3339Nano. It is
// called exactly once per entry by DecodeMemoryPageToken, which stores the
// result as the entry's typed value
// (specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md §1
// item 4).
func (t *MemorySortTerm) CursorValue(value string) (any, error) {
	switch t.kind {
	case memorySortValueString:
		return value, nil
	case memorySortValueTime:
		parsed, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return nil, fmt.Errorf("invalid time value %q: %w", value, err)
		}
		return parsed, nil
	default:
		return nil, fmt.Errorf("unknown sort value kind %d", t.kind)
	}
}

// CursorEntry builds a cursor entry from a typed value: the unique place where
// a typed cursor value becomes its wire form (times as UTC RFC3339Nano). A
// value whose dynamic type does not match this term's kind panics — a
// programming-time invariant violation, like memoryDocument.sortValue's
// unknown field (specs/065-agent-v2-team-refine/contracts/
// memory-snapshot-recency.md §1 items 4/9).
func (t *MemorySortTerm) CursorEntry(value any) *MemoryCursorEntry {
	switch t.kind {
	case memorySortValueString:
		s, ok := value.(string)
		if !ok {
			panic(fmt.Sprintf("MemorySortTerm.CursorEntry: field %q got %T, want string", t.Field, value))
		}
		return &MemoryCursorEntry{Field: t.Field, Value: s, typed: value}
	case memorySortValueTime:
		ts, ok := value.(time.Time)
		if !ok {
			panic(fmt.Sprintf("MemorySortTerm.CursorEntry: field %q got %T, want time.Time", t.Field, value))
		}
		return &MemoryCursorEntry{
			Field: t.Field,
			Value: ts.UTC().Format(time.RFC3339Nano),
			typed: value,
		}
	default:
		panic(fmt.Sprintf("MemorySortTerm.CursorEntry: field %q has unknown kind %d", t.Field, t.kind))
	}
}

// ParseMemoryOrderBy parses and validates an AIP-132 order_by value into the
// final ListMemories sort key: comma-separated "{field}" / "{field} desc"
// terms over the whitelist, completed with the designated memory_id
// tie-breaker when the list contains no memory_id. A blank value returns the
// fixed default [memory_id asc]. The non-blank input is processed in two
// passes — the validation pass completes every check (syntax, whitelist,
// duplicates) before the produce pass replays the input to restore the request
// order (map iteration order is unspecified: https://go.dev/blog/maps). Every
// error states the supported syntax and fields
// (specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md §1
// items 1-2/5; AIP-193: https://google.aip.dev/193).
func ParseMemoryOrderBy(orderBy string) ([]*MemorySortTerm, error) {
	if strings.TrimSpace(orderBy) == "" {
		return []*MemorySortTerm{memorySortTieBreaker.term(false)}, nil
	}
	items := strings.Split(orderBy, ",")

	// Validation pass: check each item and store its term in the map, the
	// only validated store — it serves the duplicate check, the memory_id
	// presence test and the term lookup.
	validated := make(map[string]*MemorySortTerm)
	for _, item := range items {
		fields := strings.Fields(item)
		if len(fields) == 0 || len(fields) > 2 || (len(fields) == 2 && fields[1] != "desc") {
			return nil, fmt.Errorf("order_by term %q is invalid: %s", strings.TrimSpace(item), memoryOrderByHelp)
		}
		spec, ok := memorySortFieldSpecByField(fields[0])
		if !ok {
			return nil, fmt.Errorf("order_by field %q is not sortable: %s", fields[0], memoryOrderByHelp)
		}
		if _, dup := validated[spec.Field]; dup {
			return nil, fmt.Errorf("order_by field %q is repeated: %s", spec.Field, memoryOrderByHelp)
		}
		validated[spec.Field] = spec.term(len(fields) == 2)
	}

	// Produce pass: replay the input to restore the requested order (map
	// iteration order is unspecified), then append the tie-breaker at the end
	// unless the key already contains memory_id anywhere.
	var terms []*MemorySortTerm
	for _, item := range items {
		terms = append(terms, validated[strings.Fields(item)[0]])
	}
	if _, ok := validated[memorySortTieBreaker.Field]; !ok {
		terms = append(terms, memorySortTieBreaker.term(false))
	}
	return terms, nil
}
