package domain

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
)

// wikiTokenPayload returns the base64url (NoPadding) encoding of raw, the wire
// form the decoders accept.
func wikiTokenPayload(raw string) string {
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte(raw))
}

func TestEncodeWikiSearchPageToken(t *testing.T) {
	tests := []struct {
		name   string
		cursor *WikiSearchCursor
		want   string
	}{
		{name: "nil cursor fails closed", cursor: nil, want: ""},
		{name: "empty query fails closed", cursor: &WikiSearchCursor{Query: "", Offset: 5}, want: ""},
		{name: "zero offset fails closed", cursor: &WikiSearchCursor{Query: "abc", Offset: 0}, want: ""},
		{name: "negative offset fails closed", cursor: &WikiSearchCursor{Query: "abc", Offset: -1}, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got := EncodeWikiSearchPageToken(tt.cursor)

			// then
			if got != tt.want {
				t.Fatalf("EncodeWikiSearchPageToken(%+v) = %q, want %q", tt.cursor, got, tt.want)
			}
		})
	}
}

func TestWikiSearchPageTokenWireShape(t *testing.T) {
	// given
	cursor := &WikiSearchCursor{Query: "策略", Offset: 20}

	// when
	token := EncodeWikiSearchPageToken(cursor)
	raw, err := base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(token)
	if err != nil {
		t.Fatalf("DecodeString() setup error: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("Unmarshal() setup error: %v", err)
	}

	// then - exactly the two documented keys
	if len(payload) != 2 {
		t.Fatalf("wire payload = %v, want exactly the q and offset keys", payload)
	}
	if payload["q"] != "策略" {
		t.Fatalf("wire payload q = %v, want %q", payload["q"], "策略")
	}
	if payload["offset"] != float64(20) {
		t.Fatalf("wire payload offset = %v, want 20", payload["offset"])
	}
}

func TestDecodeWikiSearchPageToken(t *testing.T) {
	tests := []struct {
		name        string
		tokenQuery  string
		decodeQuery string
		offset      int
		wantErr     bool
	}{
		{
			name:        "round trip",
			tokenQuery:  "策略",
			decodeQuery: "策略",
			offset:      15,
		},
		{
			name:        "full-width continuation query normalizes to the token query",
			tokenQuery:  "abc",
			decodeQuery: "ＡＢＣ",
			offset:      3,
		},
		{
			name:        "different query is rejected",
			tokenQuery:  "策略",
			decodeQuery: "开局",
			offset:      1,
			wantErr:     true,
		},
		{
			name:        "empty query is rejected",
			tokenQuery:  "策略",
			decodeQuery: "",
			offset:      1,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			token := EncodeWikiSearchPageToken(&WikiSearchCursor{Query: tt.tokenQuery, Offset: tt.offset})

			// when
			cursor, err := DecodeWikiSearchPageToken(token, tt.decodeQuery)

			// then
			if tt.wantErr {
				assertInvalidPageToken(t, err)
				return
			}
			if err != nil {
				t.Fatalf("DecodeWikiSearchPageToken() unexpected error: %v", err)
			}
			if cursor.Offset != tt.offset {
				t.Fatalf("DecodeWikiSearchPageToken() offset = %d, want %d", cursor.Offset, tt.offset)
			}
		})
	}
}

func TestDecodeWikiSearchPageTokenMalformed(t *testing.T) {
	tests := []struct {
		name  string
		token string
	}{
		{name: "empty token", token: ""},
		{name: "invalid base64", token: "not base64!!"},
		{name: "invalid json", token: wikiTokenPayload("not-json")},
		{name: "non-object top level", token: wikiTokenPayload(`[1,2]`)},
		{name: "missing q", token: wikiTokenPayload(`{"offset":5}`)},
		{name: "missing offset", token: wikiTokenPayload(`{"q":"abc"}`)},
		{name: "non-positive offset", token: wikiTokenPayload(`{"q":"abc","offset":0}`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			_, err := DecodeWikiSearchPageToken(tt.token, "abc")

			// then
			assertInvalidPageToken(t, err)
		})
	}
}

func TestEncodeWikiListPageToken(t *testing.T) {
	tests := []struct {
		name   string
		cursor *WikiListCursor
		want   string
	}{
		{name: "nil cursor fails closed", cursor: nil, want: ""},
		{name: "empty wiki id fails closed", cursor: &WikiListCursor{WikiID: ""}, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got := EncodeWikiListPageToken(tt.cursor)

			// then
			if got != tt.want {
				t.Fatalf("EncodeWikiListPageToken(%+v) = %q, want %q", tt.cursor, got, tt.want)
			}
		})
	}
}

func TestWikiListPageTokenRoundTrip(t *testing.T) {
	// given
	token := EncodeWikiListPageToken(&WikiListCursor{WikiID: "wiki-42"})

	// when
	cursor, err := DecodeWikiListPageToken(token)

	// then
	if err != nil {
		t.Fatalf("DecodeWikiListPageToken() unexpected error: %v", err)
	}
	if cursor.WikiID != "wiki-42" {
		t.Fatalf("DecodeWikiListPageToken() wiki_id = %q, want %q", cursor.WikiID, "wiki-42")
	}
}

func TestDecodeWikiListPageTokenMalformed(t *testing.T) {
	tests := []struct {
		name  string
		token string
	}{
		{name: "empty token", token: ""},
		{name: "invalid base64", token: "not base64!!"},
		{name: "invalid json", token: wikiTokenPayload("not-json")},
		{name: "non-object top level", token: wikiTokenPayload(`[1,2]`)},
		{name: "missing wiki_id", token: wikiTokenPayload(`{}`)},
		{name: "empty wiki_id", token: wikiTokenPayload(`{"wiki_id":""}`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			_, err := DecodeWikiListPageToken(tt.token)

			// then
			assertInvalidPageToken(t, err)
		})
	}
}

// assertInvalidPageToken asserts err wraps ErrInvalidPageToken.
func assertInvalidPageToken(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected ErrInvalidPageToken, got nil")
	}
	if !errors.Is(err, ErrInvalidPageToken) {
		t.Fatalf("error = %v, want wrapping ErrInvalidPageToken", err)
	}
}
