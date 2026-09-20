package domain

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// WikiSearchPageToken is the decoded SearchWikis page token: the normalized query
// the token was issued under and the number of leading hits already returned
// (the offset of the next page). Search results are ordered deterministically
// (exact > prefix > substring, then normalized title ascending), so the offset
// identifies the next page within that fixed result sequence (spec 068 FR-004;
// specs/068-wiki-round-pause/contracts/wiki-service.md §4.4).
type WikiSearchPageToken struct {
	Query  string `json:"q"`
	Offset int    `json:"offset"`
}

// EncodeWikiSearchPageToken serializes a search cursor into a base64url
// (NoPadding) JSON object {"q":...,"offset":...}. It is a total function: a
// nil cursor, an empty query or a non-positive offset yields an empty token —
// a hand-built half cursor fails closed instead of minting a token that can
// never decode (AIP-158 opacity: https://google.aip.dev/158).
func EncodeWikiSearchPageToken(cursor *WikiSearchPageToken) string {
	if cursor == nil || cursor.Query == "" || cursor.Offset <= 0 {
		return ""
	}
	// json.Marshal cannot fail for this all-scalar structure
	// (https://google.github.io/styleguide/go/decisions#handle-errors).
	b, _ := json.Marshal(cursor)
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(b)
}

// DecodeWikiSearchPageToken decodes a SearchWikis page token and validates it
// against the request's query: the query is normalized with NormalizeTitle and
// must equal the token's carried q, so continuing a search with a different
// query rejects with ErrInvalidPageToken → INVALID_ARGUMENT. Empty tokens and
// empty queries are rejected the same way (spec 068 FR-004;
// specs/068-wiki-round-pause/contracts/wiki-service.md §4.4).
func DecodeWikiSearchPageToken(token, query string) (*WikiSearchPageToken, error) {
	if token == "" {
		return nil, fmt.Errorf("%w: empty token", ErrInvalidPageToken)
	}
	b, err := base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(token)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid base64: %v", ErrInvalidPageToken, err)
	}
	var cursor WikiSearchPageToken
	if err := json.Unmarshal(b, &cursor); err != nil {
		return nil, fmt.Errorf("%w: invalid json: %v", ErrInvalidPageToken, err)
	}
	q := NormalizeTitle(query)
	if q == "" {
		return nil, fmt.Errorf("%w: empty query", ErrInvalidPageToken)
	}
	if cursor.Query != q {
		return nil, fmt.Errorf("%w: query does not match the token", ErrInvalidPageToken)
	}
	if cursor.Offset <= 0 {
		return nil, fmt.Errorf("%w: offset must be positive", ErrInvalidPageToken)
	}
	return &cursor, nil
}

// WikiListPageToken is the decoded ListWikis page token: the wiki id to resume
// after (exclusive) in the fixed wiki_id ascending order. ListWikis has no
// order_by, so the token carries no request context — its shape is independent
// of the search token (spec 068 FR-003;
// specs/068-wiki-round-pause/contracts/memory-page-token-context.md §1).
type WikiListPageToken struct {
	WikiID string `json:"wiki_id"`
}

// EncodeWikiListPageToken serializes a list cursor into a base64url
// (NoPadding) JSON object {"wiki_id":...}. A nil cursor or an empty wiki id
// yields an empty token (fail closed, like EncodeWikiSearchPageToken).
func EncodeWikiListPageToken(cursor *WikiListPageToken) string {
	if cursor == nil || cursor.WikiID == "" {
		return ""
	}
	b, _ := json.Marshal(cursor)
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(b)
}

// DecodeWikiListPageToken decodes a ListWikis page token; every failure wraps
// ErrInvalidPageToken → INVALID_ARGUMENT (AIP-158: https://google.aip.dev/158).
func DecodeWikiListPageToken(token string) (*WikiListPageToken, error) {
	if token == "" {
		return nil, fmt.Errorf("%w: empty token", ErrInvalidPageToken)
	}
	b, err := base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(token)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid base64: %v", ErrInvalidPageToken, err)
	}
	var cursor WikiListPageToken
	if err := json.Unmarshal(b, &cursor); err != nil {
		return nil, fmt.Errorf("%w: invalid json: %v", ErrInvalidPageToken, err)
	}
	if cursor.WikiID == "" {
		return nil, fmt.Errorf("%w: missing wiki_id", ErrInvalidPageToken)
	}
	return &cursor, nil
}
