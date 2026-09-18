package domain

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestEncodeMemoryPageToken(t *testing.T) {
	tests := []struct {
		name      string
		cursor    *MemoryPageCursor
		wantEmpty bool
	}{
		{
			name: "mixed time and string cursor yields a token",
			cursor: &MemoryPageCursor{Entries: []*MemoryCursorEntry{
				{Field: MemorySortFieldUpdateTime, Value: "2026-05-29T12:34:56.789123456Z"},
				{Field: MemorySortFieldMemoryID, Value: "abc123"},
			}},
		},
		{
			name:      "nil cursor yields an empty token",
			cursor:    nil,
			wantEmpty: true,
		},
		{
			name:      "empty entries yield an empty token",
			cursor:    &MemoryPageCursor{},
			wantEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			token := EncodeMemoryPageToken(tt.cursor)

			// then
			if tt.wantEmpty {
				if token != "" {
					t.Fatalf("EncodeMemoryPageToken() = %q, want empty", token)
				}
				return
			}
			if token == "" {
				t.Fatal("EncodeMemoryPageToken() returned empty token")
			}
		})
	}
}

// TestEncodeMemoryPageToken_jsonShape pins the token's wire shape: a JSON
// object {"entries":[{"field":...,"value":...}]} with values as their string
// form and no serialized `typed` field
// (specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md §1
// items 4/9).
func TestEncodeMemoryPageToken_jsonShape(t *testing.T) {
	// given
	sort := mustParseMemoryOrderBy(t, "update_time desc")
	cursor := &MemoryPageCursor{Entries: []*MemoryCursorEntry{
		sort[0].CursorEntry(time.Date(2026, 5, 29, 12, 34, 56, 789123456, time.UTC)),
		sort[1].CursorEntry("m-1"),
	}}

	// when
	token := EncodeMemoryPageToken(cursor)

	// then
	b, err := base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(token)
	if err != nil {
		t.Fatalf("base64 decode error: %v", err)
	}
	var raw struct {
		Entries []map[string]json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("token is not a {\"entries\":[...]} object: %v", err)
	}
	if len(raw.Entries) != 2 {
		t.Fatalf("token entries = %v, want 2", raw.Entries)
	}
	for i, entry := range raw.Entries {
		if len(entry) != 2 {
			t.Fatalf("entry %d serializes %d JSON fields (%v), want field/value only", i, len(entry), entry)
		}
		for _, key := range []string{"field", "value"} {
			if _, ok := entry[key]; !ok {
				t.Fatalf("entry %d is missing %q: %v", i, key, entry)
			}
		}
	}
	var wire struct {
		Entries []struct {
			Field string `json:"field"`
			Value string `json:"value"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(b, &wire); err != nil {
		t.Fatalf("json unmarshal error: %v", err)
	}
	if wire.Entries[0].Field != "update_time" || wire.Entries[1].Field != "memory_id" {
		t.Fatalf("token fields = %v, want [update_time memory_id]", wire.Entries)
	}
	if wire.Entries[0].Value != "2026-05-29T12:34:56.789123456Z" {
		t.Fatalf("token time value = %q, want the RFC3339Nano string", wire.Entries[0].Value)
	}
	if wire.Entries[1].Value != "m-1" {
		t.Fatalf("token string value = %q, want %q", wire.Entries[1].Value, "m-1")
	}
}

func TestDecodeMemoryPageToken(t *testing.T) {
	mustB64 := func(v string) string {
		return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte(v))
	}

	tests := []struct {
		name    string
		token   string
		sortBy  string
		want    []*MemoryCursorEntry
		wantErr bool
	}{
		{
			name: "round-trip: mixed time and string keys, nanoseconds preserved",
			token: EncodeMemoryPageToken(&MemoryPageCursor{Entries: []*MemoryCursorEntry{
				{Field: MemorySortFieldUpdateTime, Value: "2026-05-29T12:34:56.789123456Z"},
				{Field: MemorySortFieldMemoryID, Value: "abc123"},
			}}),
			sortBy: "update_time desc",
			want: []*MemoryCursorEntry{
				{Field: MemorySortFieldUpdateTime, Value: "2026-05-29T12:34:56.789123456Z"},
				{Field: MemorySortFieldMemoryID, Value: "abc123"},
			},
		},
		{
			name:   "round-trip: single memory_id key",
			token:  EncodeMemoryPageToken(&MemoryPageCursor{Entries: []*MemoryCursorEntry{{Field: MemorySortFieldMemoryID, Value: "xyz789"}}}),
			sortBy: "",
			want:   []*MemoryCursorEntry{{Field: MemorySortFieldMemoryID, Value: "xyz789"}},
		},
		{
			name: "round-trip: ascending business key",
			token: EncodeMemoryPageToken(&MemoryPageCursor{Entries: []*MemoryCursorEntry{
				{Field: MemorySortFieldUpdateTime, Value: "2026-05-29T12:35:00.000000001Z"},
				{Field: MemorySortFieldMemoryID, Value: "m-2"},
			}}),
			sortBy: "update_time",
			want: []*MemoryCursorEntry{
				{Field: MemorySortFieldUpdateTime, Value: "2026-05-29T12:35:00.000000001Z"},
				{Field: MemorySortFieldMemoryID, Value: "m-2"},
			},
		},
		{
			name:    "empty token returns error",
			token:   "",
			sortBy:  "",
			wantErr: true,
		},
		{
			name:    "invalid base64 token returns error",
			token:   "!!!not-base64!!!",
			sortBy:  "",
			wantErr: true,
		},
		{
			name:    "valid base64 but invalid JSON returns error",
			token:   mustB64("not-json"),
			sortBy:  "",
			wantErr: true,
		},
		{
			name:    "top-level JSON array is not an object",
			token:   mustB64(`[{"field":"memory_id","value":"m-1"}]`),
			sortBy:  "",
			wantErr: true,
		},
		{
			name:    "entries key is missing",
			token:   mustB64(`{}`),
			sortBy:  "",
			wantErr: true,
		},
		{
			name:    "entries is not an array",
			token:   mustB64(`{"entries":{}}`),
			sortBy:  "",
			wantErr: true,
		},
		{
			name:    "entries is null",
			token:   mustB64(`{"entries":null}`),
			sortBy:  "",
			wantErr: true,
		},
		{
			name:    "entries is empty",
			token:   mustB64(`{"entries":[]}`),
			sortBy:  "",
			wantErr: true,
		},
		{
			name:    "element is null",
			token:   mustB64(`{"entries":[null]}`),
			sortBy:  "",
			wantErr: true,
		},
		{
			name:    "entry missing field returns error",
			token:   mustB64(`{"entries":[{"value":"m-1"}]}`),
			sortBy:  "",
			wantErr: true,
		},
		{
			name:    "entry missing value returns error",
			token:   mustB64(`{"entries":[{"field":"memory_id"}]}`),
			sortBy:  "",
			wantErr: true,
		},
		{
			name:    "empty entry object returns error",
			token:   mustB64(`{"entries":[{}]}`),
			sortBy:  "",
			wantErr: true,
		},
		{
			name:    "unknown field in token returns error",
			token:   mustB64(`{"entries":[{"field":"content","value":"x"}]}`),
			sortBy:  "memory_id",
			wantErr: true,
		},
		{
			name: "length mismatch: two-key token against the default key",
			token: EncodeMemoryPageToken(&MemoryPageCursor{Entries: []*MemoryCursorEntry{
				{Field: MemorySortFieldUpdateTime, Value: "2026-09-16T10:00:00Z"},
				{Field: MemorySortFieldMemoryID, Value: "m-1"},
			}}),
			sortBy:  "",
			wantErr: true,
		},
		{
			name:    "length mismatch: single-key token against a two-key order",
			token:   EncodeMemoryPageToken(&MemoryPageCursor{Entries: []*MemoryCursorEntry{{Field: MemorySortFieldUpdateTime, Value: "2026-09-16T10:00:00Z"}}}),
			sortBy:  "update_time desc",
			wantErr: true,
		},
		{
			name:    "cross-order replay: update_time token against a memory_id order",
			token:   EncodeMemoryPageToken(&MemoryPageCursor{Entries: []*MemoryCursorEntry{{Field: MemorySortFieldUpdateTime, Value: "2026-09-16T10:00:00Z"}}}),
			sortBy:  "memory_id desc",
			wantErr: true,
		},
		{
			name: "unparsable time value returns error",
			token: EncodeMemoryPageToken(&MemoryPageCursor{Entries: []*MemoryCursorEntry{
				{Field: MemorySortFieldUpdateTime, Value: "not-a-time"},
				{Field: MemorySortFieldMemoryID, Value: "m-1"},
			}}),
			sortBy:  "update_time desc",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			sort := mustParseMemoryOrderBy(t, tt.sortBy)

			// when
			got, err := DecodeMemoryPageToken(tt.token, sort)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("DecodeMemoryPageToken() expected error but got nil, result: %+v", got)
				}
				if !errors.Is(err, ErrInvalidPageToken) {
					t.Fatalf("DecodeMemoryPageToken() error = %v, want ErrInvalidPageToken", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("DecodeMemoryPageToken() unexpected error: %v", err)
			}
			if got == nil {
				t.Fatal("DecodeMemoryPageToken() returned nil cursor on success")
			}
			assertMemoryPageCursor(t, got, tt.want)
		})
	}
}

// TestDecodeMemoryPageToken_typedValues pins the single-shot conversion
// behavior: Decode stores each entry's typed value (time.Time for time fields,
// string for string fields) exactly once, so consumers never convert again
// (specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md §1
// items 4/9).
func TestDecodeMemoryPageToken_typedValues(t *testing.T) {
	// given
	sort := mustParseMemoryOrderBy(t, "update_time desc")
	now := time.Date(2026, 9, 16, 10, 0, 0, 1, time.UTC)
	token := EncodeMemoryPageToken(&MemoryPageCursor{Entries: []*MemoryCursorEntry{
		{Field: MemorySortFieldUpdateTime, Value: "2026-09-16T10:00:00.000000001Z"},
		{Field: MemorySortFieldMemoryID, Value: "m-1"},
	}})

	// when
	cursor, err := DecodeMemoryPageToken(token, sort)

	// then
	if err != nil {
		t.Fatalf("DecodeMemoryPageToken() unexpected error: %v", err)
	}
	gotTime, ok := cursor.Entries[0].Typed().(time.Time)
	if !ok || !gotTime.Equal(now) {
		t.Fatalf("entry 0 typed = %v (%T), want %v", cursor.Entries[0].Typed(), cursor.Entries[0].Typed(), now)
	}
	if got := cursor.Entries[1].Typed(); got != "m-1" {
		t.Fatalf("entry 1 typed = %v (%T), want %q", got, got, "m-1")
	}
}

// TestDecodeMemoryPageToken_equivalentSpellings verifies a token is
// interchangeable across order_by spellings that normalize to the same final
// key (specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md §1
// item 4).
func TestDecodeMemoryPageToken_equivalentSpellings(t *testing.T) {
	// given
	implicit := mustParseMemoryOrderBy(t, "update_time desc")
	explicit := mustParseMemoryOrderBy(t, "update_time desc, memory_id")
	want := []*MemoryCursorEntry{
		{Field: MemorySortFieldUpdateTime, Value: "2026-09-16T10:00:00.000000001Z"},
		{Field: MemorySortFieldMemoryID, Value: "m-1"},
	}
	token := EncodeMemoryPageToken(&MemoryPageCursor{Entries: want})

	// when
	fromImplicit, err := DecodeMemoryPageToken(token, implicit)
	if err != nil {
		t.Fatalf("DecodeMemoryPageToken() with the single-field spelling: %v", err)
	}
	fromExplicit, err := DecodeMemoryPageToken(token, explicit)
	if err != nil {
		t.Fatalf("DecodeMemoryPageToken() with the explicit tie-breaker spelling: %v", err)
	}

	// then
	assertMemoryPageCursor(t, fromImplicit, want)
	assertMemoryPageCursor(t, fromExplicit, want)
}

// mustParseMemoryOrderBy parses order_by into the final sort key, failing the
// test on error.
func mustParseMemoryOrderBy(t *testing.T, orderBy string) []*MemorySortTerm {
	t.Helper()
	sort, err := ParseMemoryOrderBy(orderBy)
	if err != nil {
		t.Fatalf("ParseMemoryOrderBy(%q) setup error: %v", orderBy, err)
	}
	return sort
}

// assertMemoryPageCursor asserts the decoded cursor equals want entry by
// entry.
func assertMemoryPageCursor(t *testing.T, got *MemoryPageCursor, want []*MemoryCursorEntry) {
	t.Helper()
	if len(got.Entries) != len(want) {
		t.Fatalf("cursor = %+v, want %+v", got, want)
	}
	for i := range want {
		if got.Entries[i] == nil || got.Entries[i].Field != want[i].Field || got.Entries[i].Value != want[i].Value {
			t.Fatalf("cursor[%d] = %+v, want %+v", i, got.Entries[i], want[i])
		}
	}
}
