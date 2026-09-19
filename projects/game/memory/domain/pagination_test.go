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
			name: "a full cursor yields a token",
			cursor: &MemoryPageCursor{
				Sort: []string{"update_time desc", "memory_id"},
				Entries: []*MemoryCursorEntry{
					{Field: MemorySortFieldUpdateTime, Value: "2026-05-29T12:34:56.789123456Z"},
					{Field: MemorySortFieldMemoryID, Value: "abc123"},
				},
			},
		},
		{
			name:      "nil cursor yields an empty token",
			cursor:    nil,
			wantEmpty: true,
		},
		{
			name:      "empty entries yield an empty token",
			cursor:    &MemoryPageCursor{Sort: []string{"memory_id"}},
			wantEmpty: true,
		},
		{
			name:      "empty sort context yields an empty token",
			cursor:    &MemoryPageCursor{Entries: []*MemoryCursorEntry{{Field: MemorySortFieldMemoryID, Value: "m-1"}}},
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
// object with exactly the two keys {"sort":[...],"entries":[...]}, sort as the
// canonical key spellings, entry values as their string form and no serialized
// `typed` field (specs/068-wiki-round-pause/contracts/
// memory-page-token-context.md §1).
func TestEncodeMemoryPageToken_jsonShape(t *testing.T) {
	// given
	sort := mustParseMemoryOrderBy(t, "update_time desc")
	cursor := NewMemoryPageCursor(sort, []any{
		time.Date(2026, 5, 29, 12, 34, 56, 789123456, time.UTC),
		"m-1",
	})

	// when
	token := EncodeMemoryPageToken(cursor)

	// then
	b, err := base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(token)
	if err != nil {
		t.Fatalf("base64 decode error: %v", err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err != nil {
		t.Fatalf("token is not a JSON object: %v", err)
	}
	if len(top) != 2 {
		t.Fatalf("token serializes %d top-level fields (%v), want sort/entries only", len(top), top)
	}
	for _, key := range []string{"sort", "entries"} {
		if _, ok := top[key]; !ok {
			t.Fatalf("token is missing top-level %q: %v", key, top)
		}
	}
	var wire struct {
		Sort    []string `json:"sort"`
		Entries []struct {
			Field string `json:"field"`
			Value string `json:"value"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(b, &wire); err != nil {
		t.Fatalf("json unmarshal error: %v", err)
	}
	wantSort := []string{"update_time desc", "memory_id"}
	if len(wire.Sort) != len(wantSort) {
		t.Fatalf("token sort = %v, want %v", wire.Sort, wantSort)
	}
	for i, want := range wantSort {
		if wire.Sort[i] != want {
			t.Fatalf("token sort[%d] = %q, want %q", i, wire.Sort[i], want)
		}
	}
	if len(wire.Entries) != 2 {
		t.Fatalf("token entries = %v, want 2", wire.Entries)
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

// TestDecodeMemoryPageToken covers the page token layer: envelope decoding and
// order_by context consistency (specs/068-wiki-round-pause/contracts/
// memory-page-token-context.md §1-2).
func TestDecodeMemoryPageToken(t *testing.T) {
	mustB64 := func(v string) string {
		return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte(v))
	}
	descSort := mustParseMemoryOrderBy(t, "update_time desc")
	descCursor := NewMemoryPageCursor(descSort, []any{
		time.Date(2026, 9, 16, 10, 0, 0, 1, time.UTC),
		"m-1",
	})

	tests := []struct {
		name    string
		token   string
		sortBy  string
		want    []*MemoryCursorEntry
		wantErr bool
	}{
		{
			name:   "round-trip: mixed time and string keys, nanoseconds preserved",
			token:  EncodeMemoryPageToken(descCursor),
			sortBy: "update_time desc",
			want: []*MemoryCursorEntry{
				{Field: MemorySortFieldUpdateTime, Value: "2026-09-16T10:00:00.000000001Z"},
				{Field: MemorySortFieldMemoryID, Value: "m-1"},
			},
		},
		{
			name:   "round-trip: single memory_id key",
			token:  EncodeMemoryPageToken(NewMemoryPageCursor(mustParseMemoryOrderBy(t, ""), []any{"xyz789"})),
			sortBy: "",
			want:   []*MemoryCursorEntry{{Field: MemorySortFieldMemoryID, Value: "xyz789"}},
		},
		{
			name: "round-trip: ascending business key",
			token: EncodeMemoryPageToken(NewMemoryPageCursor(mustParseMemoryOrderBy(t, "update_time"), []any{
				time.Date(2026, 5, 29, 12, 35, 0, 1, time.UTC),
				"m-2",
			})),
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
			token:   mustB64(`{"sort":["memory_id"]}`),
			sortBy:  "",
			wantErr: true,
		},
		{
			name:    "entries is not an array",
			token:   mustB64(`{"sort":["memory_id"],"entries":{}}`),
			sortBy:  "",
			wantErr: true,
		},
		{
			name:    "entries is null",
			token:   mustB64(`{"sort":["memory_id"],"entries":null}`),
			sortBy:  "",
			wantErr: true,
		},
		{
			name:    "legacy token without a sort context is rejected",
			token:   mustB64(`{"entries":[{"field":"memory_id","value":"m-1"}]}`),
			sortBy:  "",
			wantErr: true,
		},
		{
			name:    "direction flip: desc context against an ascending request",
			token:   EncodeMemoryPageToken(descCursor),
			sortBy:  "update_time",
			wantErr: true,
		},
		{
			name: "direction flip: ascending context against a desc request",
			token: EncodeMemoryPageToken(NewMemoryPageCursor(mustParseMemoryOrderBy(t, "update_time"), []any{
				time.Date(2026, 9, 16, 10, 0, 0, 1, time.UTC),
				"m-1",
			})),
			sortBy:  "update_time desc",
			wantErr: true,
		},
		{
			name: "forged context: one spelling changed",
			token: EncodeMemoryPageToken(&MemoryPageCursor{
				Sort: []string{"update_time", "memory_id"},
				Entries: []*MemoryCursorEntry{
					{Field: MemorySortFieldUpdateTime, Value: "2026-09-16T10:00:00.000000001Z"},
					{Field: MemorySortFieldMemoryID, Value: "m-1"},
				},
			}),
			sortBy:  "update_time desc",
			wantErr: true,
		},
		{
			name:    "context length mismatch: two-key context against the default key",
			token:   EncodeMemoryPageToken(descCursor),
			sortBy:  "",
			wantErr: true,
		},
		{
			name: "context length mismatch: single-key context against a two-key order",
			token: EncodeMemoryPageToken(&MemoryPageCursor{
				Sort:    []string{"update_time desc"},
				Entries: []*MemoryCursorEntry{{Field: MemorySortFieldUpdateTime, Value: "2026-09-16T10:00:00Z"}},
			}),
			sortBy:  "update_time desc",
			wantErr: true,
		},
		{
			name: "cross-order replay: update_time context against a memory_id order",
			token: EncodeMemoryPageToken(NewMemoryPageCursor(mustParseMemoryOrderBy(t, "update_time desc"), []any{
				time.Date(2026, 9, 16, 10, 0, 0, 0, time.UTC),
				"m-1",
			})),
			sortBy:  "memory_id desc",
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

// TestDecodeMemoryPageToken_cursorPayload covers the cursor layer: a token
// whose context matches the request but whose carried payload is broken is
// rejected by the cursor-internal checks (specs/068-wiki-round-pause/contracts/
// memory-page-token-context.md §1-2). Every case crafts the context from the
// request's own key, so the context comparison passes and the cursor check is
// reached.
func TestDecodeMemoryPageToken_cursorPayload(t *testing.T) {
	singleSort := mustParseMemoryOrderBy(t, "")
	doubleSort := mustParseMemoryOrderBy(t, "update_time desc")

	tests := []struct {
		name    string
		sort    []*MemorySortTerm
		entries []*MemoryCursorEntry
	}{
		{
			name:    "entries empty",
			sort:    singleSort,
			entries: []*MemoryCursorEntry{},
		},
		{
			name:    "length misalignment: two entries against a single key",
			sort:    singleSort,
			entries: []*MemoryCursorEntry{{Field: MemorySortFieldMemoryID, Value: "m-1"}, {Field: MemorySortFieldMemoryID, Value: "m-2"}},
		},
		{
			name:    "element is null",
			sort:    singleSort,
			entries: []*MemoryCursorEntry{nil},
		},
		{
			name:    "entry missing field",
			sort:    singleSort,
			entries: []*MemoryCursorEntry{{Value: "m-1"}},
		},
		{
			name:    "entry missing value",
			sort:    singleSort,
			entries: []*MemoryCursorEntry{{Field: MemorySortFieldMemoryID}},
		},
		{
			name:    "field label misaligned with the term at that position",
			sort:    doubleSort,
			entries: []*MemoryCursorEntry{{Field: MemorySortFieldMemoryID, Value: "m-1"}, {Field: MemorySortFieldUpdateTime, Value: "2026-09-16T10:00:00Z"}},
		},
		{
			name:    "unknown field label",
			sort:    singleSort,
			entries: []*MemoryCursorEntry{{Field: "content", Value: "x"}},
		},
		{
			name: "time value is not RFC3339Nano",
			sort: doubleSort,
			entries: []*MemoryCursorEntry{
				{Field: MemorySortFieldUpdateTime, Value: "not-a-time"},
				{Field: MemorySortFieldMemoryID, Value: "m-1"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given - the context is the request's own key, so only the payload
			// can fail
			token := EncodeMemoryPageToken(&MemoryPageCursor{Sort: memorySortWire(tt.sort), Entries: tt.entries})

			// when
			got, err := DecodeMemoryPageToken(token, tt.sort)

			// then
			if err == nil {
				t.Fatalf("DecodeMemoryPageToken() expected error but got nil, result: %+v", got)
			}
			if !errors.Is(err, ErrInvalidPageToken) {
				t.Fatalf("DecodeMemoryPageToken() error = %v, want ErrInvalidPageToken", err)
			}
		})
	}
}

// TestDecodeMemoryPageToken_typedValues pins the single-shot conversion
// behavior: Decode stores each entry's typed value (time.Time for time fields,
// string for string fields) exactly once, so consumers never convert again
// (specs/068-wiki-round-pause/contracts/memory-page-token-context.md §1-2).
func TestDecodeMemoryPageToken_typedValues(t *testing.T) {
	// given
	sort := mustParseMemoryOrderBy(t, "update_time desc")
	now := time.Date(2026, 9, 16, 10, 0, 0, 1, time.UTC)
	token := EncodeMemoryPageToken(NewMemoryPageCursor(sort, []any{now, "m-1"}))

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
// key (specs/068-wiki-round-pause/contracts/memory-page-token-context.md §3).
func TestDecodeMemoryPageToken_equivalentSpellings(t *testing.T) {
	// given
	implicit := mustParseMemoryOrderBy(t, "update_time desc")
	explicit := mustParseMemoryOrderBy(t, "update_time desc, memory_id")
	now := time.Date(2026, 9, 16, 10, 0, 0, 1, time.UTC)
	want := []*MemoryCursorEntry{
		{Field: MemorySortFieldUpdateTime, Value: "2026-09-16T10:00:00.000000001Z"},
		{Field: MemorySortFieldMemoryID, Value: "m-1"},
	}
	token := EncodeMemoryPageToken(NewMemoryPageCursor(implicit, []any{now, "m-1"}))

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

// TestNewMemoryPageCursor pins the single construction entry point: the
// order_by context is the canonical spelling of the key and the entries carry
// their typed values ready for the storage layer
// (specs/068-wiki-round-pause/contracts/memory-page-token-context.md §1).
func TestNewMemoryPageCursor(t *testing.T) {
	// given
	sort := mustParseMemoryOrderBy(t, "update_time desc")
	now := time.Date(2026, 9, 16, 10, 0, 0, 1, time.UTC)

	// when
	cursor := NewMemoryPageCursor(sort, []any{now, "m-1"})

	// then - the context is the canonical spelling of the resolved key
	wantSort := []string{"update_time desc", "memory_id"}
	if len(cursor.Sort) != len(wantSort) {
		t.Fatalf("cursor sort = %v, want %v", cursor.Sort, wantSort)
	}
	for i, want := range wantSort {
		if cursor.Sort[i] != want {
			t.Fatalf("cursor sort[%d] = %q, want %q", i, cursor.Sort[i], want)
		}
	}
	// then - the entries carry their typed values
	if got, ok := cursor.Entries[0].Typed().(time.Time); !ok || !got.Equal(now) {
		t.Fatalf("entry 0 typed = %v (%T), want %v", cursor.Entries[0].Typed(), cursor.Entries[0].Typed(), now)
	}
	if got := cursor.Entries[1].Typed(); got != "m-1" {
		t.Fatalf("entry 1 typed = %v (%T), want %q", got, got, "m-1")
	}
}

// TestNewMemoryPageCursor_valueCountMismatch verifies the fail-fast panic for a
// value count that does not match the key length
// (specs/068-wiki-round-pause/contracts/memory-page-token-context.md §1).
func TestNewMemoryPageCursor_valueCountMismatch(t *testing.T) {
	// given
	sort := mustParseMemoryOrderBy(t, "update_time desc")

	// when
	defer func() {
		// then
		if recover() == nil {
			t.Fatal("NewMemoryPageCursor() did not panic for a value count that does not match the key length")
		}
	}()
	_ = NewMemoryPageCursor(sort, []any{"m-1"})
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
