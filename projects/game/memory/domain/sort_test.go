package domain

import (
	"strings"
	"testing"
	"time"
)

func TestParseMemoryOrderBy(t *testing.T) {
	tests := []struct {
		name    string
		orderBy string
		want    []*MemorySortTerm
		wantErr bool
	}{
		{
			name:    "empty order_by returns the default memory_id ascending key",
			orderBy: "",
			want:    []*MemorySortTerm{{Field: MemorySortFieldMemoryID, MongoField: "memory_id"}},
		},
		{
			name:    "space-only order_by returns the default key",
			orderBy: "   ",
			want:    []*MemorySortTerm{{Field: MemorySortFieldMemoryID, MongoField: "memory_id"}},
		},
		{
			name:    "tab-only order_by returns the default key",
			orderBy: "\t",
			want:    []*MemorySortTerm{{Field: MemorySortFieldMemoryID, MongoField: "memory_id"}},
		},
		{
			name:    "descending field appends the designated tie-breaker",
			orderBy: "update_time desc",
			want: []*MemorySortTerm{
				{Field: MemorySortFieldUpdateTime, MongoField: "update_time", Descending: true},
				{Field: MemorySortFieldMemoryID, MongoField: "memory_id"},
			},
		},
		{
			name:    "bare field is ascending and appends the tie-breaker",
			orderBy: "update_time",
			want: []*MemorySortTerm{
				{Field: MemorySortFieldUpdateTime, MongoField: "update_time"},
				{Field: MemorySortFieldMemoryID, MongoField: "memory_id"},
			},
		},
		{
			name:    "redundant whitespace is insignificant",
			orderBy: "  update_time \t desc  ",
			want: []*MemorySortTerm{
				{Field: MemorySortFieldUpdateTime, MongoField: "update_time", Descending: true},
				{Field: MemorySortFieldMemoryID, MongoField: "memory_id"},
			},
		},
		{
			name:    "explicit tie-breaker is not appended twice",
			orderBy: "update_time desc, memory_id",
			want: []*MemorySortTerm{
				{Field: MemorySortFieldUpdateTime, MongoField: "update_time", Descending: true},
				{Field: MemorySortFieldMemoryID, MongoField: "memory_id"},
			},
		},
		{
			name:    "whitespace around the separator is insignificant",
			orderBy: "update_time desc , memory_id",
			want: []*MemorySortTerm{
				{Field: MemorySortFieldUpdateTime, MongoField: "update_time", Descending: true},
				{Field: MemorySortFieldMemoryID, MongoField: "memory_id"},
			},
		},
		{
			name:    "tie-breaker descending terminates the key",
			orderBy: "memory_id desc",
			want:    []*MemorySortTerm{{Field: MemorySortFieldMemoryID, MongoField: "memory_id", Descending: true}},
		},
		{
			name:    "bare tie-breaker is ascending",
			orderBy: "memory_id",
			want:    []*MemorySortTerm{{Field: MemorySortFieldMemoryID, MongoField: "memory_id"}},
		},
		{
			name:    "ascending tie-breaker terminates the key",
			orderBy: "update_time, memory_id",
			want: []*MemorySortTerm{
				{Field: MemorySortFieldUpdateTime, MongoField: "update_time"},
				{Field: MemorySortFieldMemoryID, MongoField: "memory_id"},
			},
		},
		{
			name:    "tie-breaker in the middle is legal and appends nothing",
			orderBy: "memory_id, update_time desc",
			want: []*MemorySortTerm{
				{Field: MemorySortFieldMemoryID, MongoField: "memory_id"},
				{Field: MemorySortFieldUpdateTime, MongoField: "update_time", Descending: true},
			},
		},
		{
			name:    "descending tie-breaker in the middle is legal and appends nothing",
			orderBy: "memory_id desc, update_time",
			want: []*MemorySortTerm{
				{Field: MemorySortFieldMemoryID, MongoField: "memory_id", Descending: true},
				{Field: MemorySortFieldUpdateTime, MongoField: "update_time"},
			},
		},
		{
			name:    "unknown field is rejected",
			orderBy: "foo",
			wantErr: true,
		},
		{
			name:    "non-sortable resource field is rejected",
			orderBy: "content",
			wantErr: true,
		},
		{
			name:    "subfield path is rejected",
			orderBy: "content.foo",
			wantErr: true,
		},
		{
			name:    "asc suffix is rejected (AIP-132 only defines desc)",
			orderBy: "update_time asc",
			wantErr: true,
		},
		{
			name:    "desc is case-sensitive",
			orderBy: "update_time DESC",
			wantErr: true,
		},
		{
			name:    "extra token is rejected",
			orderBy: "update_time desc desc",
			wantErr: true,
		},
		{
			name:    "repeated field is rejected",
			orderBy: "update_time, update_time",
			wantErr: true,
		},
		{
			name:    "repeated field with directions is rejected",
			orderBy: "update_time desc, update_time",
			wantErr: true,
		},
		{
			name:    "repeated tie-breaker is rejected",
			orderBy: "memory_id, memory_id",
			wantErr: true,
		},
		{
			name:    "empty term between separators is rejected",
			orderBy: "update_time, ",
			wantErr: true,
		},
		{
			name:    "comma-only order_by is rejected",
			orderBy: ",",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got, err := ParseMemoryOrderBy(tt.orderBy)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseMemoryOrderBy(%q) expected error, got %+v", tt.orderBy, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseMemoryOrderBy(%q) unexpected error: %v", tt.orderBy, err)
			}
			assertMemorySortTerms(t, got, tt.want)
		})
	}
}

// TestParseMemoryOrderBy_errorMessages verifies every parse error states the
// supported syntax and fields
// (specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md §1
// item 1).
func TestParseMemoryOrderBy_errorMessages(t *testing.T) {
	// given
	orderBy := "update_time asc"

	// when
	_, err := ParseMemoryOrderBy(orderBy)

	// then
	if err == nil {
		t.Fatalf("ParseMemoryOrderBy(%q) expected error, got nil", orderBy)
	}
	for _, want := range []string{"{field}", "{field} desc", `"memory_id"`, `"update_time"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("ParseMemoryOrderBy error %q does not mention %s", err.Error(), want)
		}
	}
}

// TestMemorySortTerm_CursorValue pins the wire → typed direction of the
// symmetric conversion pair (specs/065-agent-v2-team-refine/contracts/
// memory-snapshot-recency.md §1 item 9).
func TestMemorySortTerm_CursorValue(t *testing.T) {
	stringTerm := memorySortTieBreaker.term(false)
	timeTerm := mustParseMemoryOrderBy(t, "update_time desc")[0]
	now := time.Date(2026, 9, 16, 10, 0, 0, 1, time.UTC)

	// when - string kind passes the wire value through
	stringValue, err := stringTerm.CursorValue("m-1")

	// then
	if err != nil {
		t.Fatalf("CursorValue() unexpected error: %v", err)
	}
	if stringValue != "m-1" {
		t.Fatalf("CursorValue() = %v, want %q", stringValue, "m-1")
	}

	// when - time kind parses RFC3339Nano
	timeValue, err := timeTerm.CursorValue("2026-09-16T10:00:00.000000001Z")

	// then
	if err != nil {
		t.Fatalf("CursorValue() unexpected error: %v", err)
	}
	parsed, ok := timeValue.(time.Time)
	if !ok || !parsed.Equal(now) {
		t.Fatalf("CursorValue() = %v (%T), want %v", timeValue, timeValue, now)
	}

	// when - an unparsable time value
	_, err = timeTerm.CursorValue("not-a-time")

	// then
	if err == nil {
		t.Fatal("CursorValue() expected error, got nil")
	}
}

// TestMemorySortTerm_CursorEntry pins the typed → wire direction: the unique
// place where a cursor value becomes its wire form, with times formatted as
// UTC RFC3339Nano (specs/065-agent-v2-team-refine/contracts/
// memory-snapshot-recency.md §1 item 9).
func TestMemorySortTerm_CursorEntry(t *testing.T) {
	stringTerm := memorySortTieBreaker.term(false)
	timeTerm := mustParseMemoryOrderBy(t, "update_time desc")[0]
	now := time.Date(2026, 9, 16, 10, 0, 0, 1, time.UTC)

	// when - a string value
	stringEntry := stringTerm.CursorEntry("m-1")

	// then
	if stringEntry.Field != MemorySortFieldMemoryID || stringEntry.Value != "m-1" {
		t.Fatalf("CursorEntry() = %+v, want {memory_id m-1}", stringEntry)
	}
	if stringEntry.Typed() != "m-1" {
		t.Fatalf("CursorEntry() typed = %v, want %q", stringEntry.Typed(), "m-1")
	}

	// when - a time value in a non-UTC zone
	timeEntry := timeTerm.CursorEntry(now.In(time.FixedZone("plus2", 2*60*60)))

	// then - the wire value is the UTC RFC3339Nano form
	if timeEntry.Field != MemorySortFieldUpdateTime || timeEntry.Value != "2026-09-16T10:00:00.000000001Z" {
		t.Fatalf("CursorEntry() = %+v, want {update_time 2026-09-16T10:00:00.000000001Z}", timeEntry)
	}
	typed, ok := timeEntry.Typed().(time.Time)
	if !ok || !typed.Equal(now) {
		t.Fatalf("CursorEntry() typed = %v (%T), want %v", timeEntry.Typed(), timeEntry.Typed(), now)
	}
}

// TestMemorySortTerm_CursorEntry_kindMismatch verifies the fail-fast panic for
// a value whose dynamic type does not match the term's kind
// (specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md §1
// item 9).
func TestMemorySortTerm_CursorEntry_kindMismatch(t *testing.T) {
	// given
	stringTerm := memorySortTieBreaker.term(false)

	// when
	defer func() {
		// then
		if recover() == nil {
			t.Fatal("CursorEntry() did not panic for a non-string value on a string term")
		}
	}()
	_ = stringTerm.CursorEntry(42)
}

// assertMemorySortTerms asserts the produced key equals want on the exported
// fields (kind is package-private derivation knowledge).
func assertMemorySortTerms(t *testing.T, got, want []*MemorySortTerm) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("sort key = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i].Field != want[i].Field || got[i].MongoField != want[i].MongoField || got[i].Descending != want[i].Descending {
			t.Fatalf("sort key[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}
