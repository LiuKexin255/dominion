package domain

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

func TestEncodeMemoryPageToken(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 34, 56, 789000000, time.UTC)

	tests := []struct {
		name   string
		cursor *MemoryPageCursor
	}{
		{
			name:   "encode valid cursor returns non-empty token",
			cursor: &MemoryPageCursor{UpdateTime: now, MemoryID: "abc123"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			token, err := EncodeMemoryPageToken(tt.cursor)

			// then
			if err != nil {
				t.Fatalf("EncodeMemoryPageToken() unexpected error: %v", err)
			}
			if token == "" {
				t.Fatal("EncodeMemoryPageToken() returned empty token")
			}
		})
	}
}

func TestDecodeMemoryPageToken(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 34, 56, 789000000, time.UTC)
	rfcNow := now.Format(time.RFC3339Nano)
	later := time.Date(2026, 5, 29, 12, 35, 0, 0, time.UTC)

	mustB64 := func(v string) string {
		return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte(v))
	}

	tests := []struct {
		name    string
		token   string
		want    *MemoryPageCursor
		wantErr bool
	}{
		{
			name:  "round-trip: encode then decode, fields match",
			token: mustEncodeMemoryPageToken(t, &MemoryPageCursor{UpdateTime: now, MemoryID: "abc123"}),
			want:  &MemoryPageCursor{UpdateTime: now, MemoryID: "abc123"},
		},
		{
			name:  "nanosecond precision: update_time with nanoseconds preserved",
			token: mustEncodeMemoryPageToken(t, &MemoryPageCursor{UpdateTime: later, MemoryID: "xyz789"}),
			want:  &MemoryPageCursor{UpdateTime: later, MemoryID: "xyz789"},
		},
		{
			name:    "empty string token returns error",
			token:   "",
			wantErr: true,
		},
		{
			name:    "invalid base64 token returns error",
			token:   "!!!not-base64!!!",
			wantErr: true,
		},
		{
			name:    "valid base64 but invalid JSON returns error",
			token:   mustB64("not-json"),
			wantErr: true,
		},
		{
			name:    "JSON missing update_time returns error",
			token:   mustB64(`{"memory_id":"abc123"}`),
			wantErr: true,
		},
		{
			name:    "JSON missing memory_id returns error",
			token:   mustB64(`{"update_time":"` + rfcNow + `"}`),
			wantErr: true,
		},
		{
			name:    "JSON with unparseable update_time returns error",
			token:   mustB64(`{"update_time":"not-a-time","memory_id":"abc123"}`),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got, err := DecodeMemoryPageToken(tt.token)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("DecodeMemoryPageToken() expected error but got nil, result: %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("DecodeMemoryPageToken() unexpected error: %v", err)
			}
			if !got.UpdateTime.Equal(tt.want.UpdateTime) {
				t.Fatalf("DecodeMemoryPageToken() UpdateTime = %v, want %v", got.UpdateTime, tt.want.UpdateTime)
			}
			if got.MemoryID != tt.want.MemoryID {
				t.Fatalf("DecodeMemoryPageToken() MemoryID = %s, want %s", got.MemoryID, tt.want.MemoryID)
			}
		})
	}
}

// mustEncodeMemoryPageToken encodes the cursor and returns the token, failing
// the test on error.
func mustEncodeMemoryPageToken(t *testing.T, cursor *MemoryPageCursor) string {
	t.Helper()
	token, err := EncodeMemoryPageToken(cursor)
	if err != nil {
		t.Fatalf("EncodeMemoryPageToken() setup error: %v", err)
	}
	return token
}

// TestEncodeMemoryPageToken_jsonFieldNames verifies the JSON encoding uses the
// snake_case field names of the contract (specs/065-agent-v2-team-refine/
// contracts/memory-snapshot-recency.md §1 item 3).
func TestEncodeMemoryPageToken_jsonFieldNames(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 34, 56, 789000000, time.UTC)
	cursor := &MemoryPageCursor{UpdateTime: now, MemoryID: "abc123"}

	token, err := EncodeMemoryPageToken(cursor)
	if err != nil {
		t.Fatalf("EncodeMemoryPageToken() unexpected error: %v", err)
	}

	b, err := base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(token)
	if err != nil {
		t.Fatalf("base64 decode error: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("json unmarshal error: %v", err)
	}

	if _, ok := raw["update_time"]; !ok {
		t.Fatal("expected update_time field in JSON")
	}
	if _, ok := raw["memory_id"]; !ok {
		t.Fatal("expected memory_id field in JSON")
	}
	if _, ok := raw["UpdateTime"]; ok {
		t.Fatal("unexpected UpdateTime field in JSON (should be update_time)")
	}
	if _, ok := raw["MemoryID"]; ok {
		t.Fatal("unexpected MemoryID field in JSON (should be memory_id)")
	}
}
