package domain

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

func TestEncodePageToken(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 34, 56, 789000000, time.UTC)

	tests := []struct {
		name   string
		cursor *ListPageCursor
	}{
		{
			name:   "encode valid cursor returns non-empty token",
			cursor: &ListPageCursor{CreateTime: now, SessionID: "abc123"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			token, err := EncodePageToken(tt.cursor)

			// then
			if err != nil {
				t.Fatalf("EncodePageToken() unexpected error: %v", err)
			}
			if token == "" {
				t.Fatal("EncodePageToken() returned empty token")
			}
		})
	}
}

func TestDecodePageToken(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 34, 56, 789000000, time.UTC)
	rfcNow := now.Format(time.RFC3339Nano)

	mustB64 := func(v string) string {
		return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte(v))
	}

	tests := []struct {
		name    string
		token   string
		want    *ListPageCursor
		wantErr bool
	}{
		{
			name:  "normal round-trip: encode then decode, fields match",
			token: mustRoundTripToken(t, &ListPageCursor{CreateTime: now, SessionID: "abc123"}),
			want:  &ListPageCursor{CreateTime: now, SessionID: "abc123"},
		},
		{
			name:  "nanosecond precision: create_time with nanoseconds preserved",
			token: mustRoundTripToken(t, &ListPageCursor{CreateTime: now, SessionID: "xyz789"}),
			want:  &ListPageCursor{CreateTime: now, SessionID: "xyz789"},
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
			name:    "JSON missing create_time returns error",
			token:   mustB64(`{"session_id":"abc123"}`),
			wantErr: true,
		},
		{
			name:    "JSON missing session_id returns error",
			token:   mustB64(`{"create_time":"` + rfcNow + `"}`),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got, err := DecodePageToken(tt.token)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("DecodePageToken() expected error but got nil, result: %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("DecodePageToken() unexpected error: %v", err)
			}
			if !got.CreateTime.Equal(tt.want.CreateTime) {
				t.Fatalf("DecodePageToken() CreateTime = %v, want %v", got.CreateTime, tt.want.CreateTime)
			}
			if got.SessionID != tt.want.SessionID {
				t.Fatalf("DecodePageToken() SessionID = %s, want %s", got.SessionID, tt.want.SessionID)
			}
		})
	}
}

// mustRoundTripToken encodes the cursor and returns the token, failing the test on error.
func mustRoundTripToken(t *testing.T, cursor *ListPageCursor) string {
	t.Helper()
	token, err := EncodePageToken(cursor)
	if err != nil {
		t.Fatalf("EncodePageToken() setup error: %v", err)
	}
	return token
}

// TestDecodePageToken_nilFields verifies that nil cursor encodes successfully.
func TestEncodePageToken_nil(t *testing.T) {
	// given
	cursor := &ListPageCursor{
		CreateTime: time.Date(2026, 5, 29, 12, 34, 56, 0, time.UTC),
		SessionID:  "nil-test",
	}

	// when
	token, err := EncodePageToken(cursor)

	// then
	if err != nil {
		t.Fatalf("EncodePageToken() unexpected error: %v", err)
	}
	if token == "" {
		t.Fatal("EncodePageToken() returned empty token")
	}

	// decode and verify
	got, err := DecodePageToken(token)
	if err != nil {
		t.Fatalf("DecodePageToken() after encode: %v", err)
	}
	if !got.CreateTime.Equal(cursor.CreateTime) {
		t.Fatalf("DecodePageToken() CreateTime = %v, want %v", got.CreateTime, cursor.CreateTime)
	}
	if got.SessionID != cursor.SessionID {
		t.Fatalf("DecodePageToken() SessionID = %s, want %s", got.SessionID, cursor.SessionID)
	}
}

// verify that the JSON encoding uses the expected field names.
func TestEncodePageToken_jsonFieldNames(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 34, 56, 789000000, time.UTC)
	cursor := &ListPageCursor{CreateTime: now, SessionID: "abc123"}

	token, err := EncodePageToken(cursor)
	if err != nil {
		t.Fatalf("EncodePageToken() unexpected error: %v", err)
	}

	b, err := base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(token)
	if err != nil {
		t.Fatalf("base64 decode error: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("json unmarshal error: %v", err)
	}

	if _, ok := raw["create_time"]; !ok {
		t.Fatal("expected create_time field in JSON")
	}
	if _, ok := raw["session_id"]; !ok {
		t.Fatal("expected session_id field in JSON")
	}
	if _, ok := raw["CreateTime"]; ok {
		t.Fatal("unexpected CreateTime field in JSON (should be create_time)")
	}
	if _, ok := raw["SessionID"]; ok {
		t.Fatal("unexpected SessionID field in JSON (should be session_id)")
	}
}
