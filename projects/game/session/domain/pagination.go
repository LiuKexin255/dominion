// Package domain defines the session domain model and repository contract.
package domain

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

// cursorJSON is the JSON intermediate representation for page token serialization.
type cursorJSON struct {
	CreateTime string `json:"create_time"`
	SessionID  string `json:"session_id"`
}

// EncodePageToken serializes a ListPageCursor into a base64url-encoded JSON token.
func EncodePageToken(cursor *ListPageCursor) (string, error) {
	cj := cursorJSON{
		CreateTime: cursor.CreateTime.UTC().Format(time.RFC3339Nano),
		SessionID:  cursor.SessionID,
	}
	b, err := json.Marshal(cj)
	if err != nil {
		return "", fmt.Errorf("encode page token: %w", err)
	}
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(b), nil
}

// DecodePageToken deserializes a base64url-encoded JSON token into a ListPageCursor.
func DecodePageToken(token string) (*ListPageCursor, error) {
	if token == "" {
		return nil, fmt.Errorf("decode page token: empty token")
	}

	b, err := base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(token)
	if err != nil {
		return nil, fmt.Errorf("decode page token: invalid base64: %w", err)
	}

	var cj cursorJSON
	if err := json.Unmarshal(b, &cj); err != nil {
		return nil, fmt.Errorf("decode page token: invalid json: %w", err)
	}

	if cj.CreateTime == "" {
		return nil, fmt.Errorf("decode page token: missing create_time")
	}
	if cj.SessionID == "" {
		return nil, fmt.Errorf("decode page token: missing session_id")
	}

	createTime, err := time.Parse(time.RFC3339Nano, cj.CreateTime)
	if err != nil {
		return nil, fmt.Errorf("decode page token: invalid create_time: %w", err)
	}

	cursor := &ListPageCursor{
		CreateTime: createTime,
		SessionID:  cj.SessionID,
	}
	return cursor, nil
}
