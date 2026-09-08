package main

import (
	"time"

	"dominion/projects/game"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// SessionView is the Wails view model for game.Session.
type SessionView struct {
	Name       string `json:"name"`
	SessionID  string `json:"sessionId"`
	CreateTime string `json:"createTime,omitempty"`
}

// ListSessionsView is the Wails view model for game.ListSessionsResponse.
type ListSessionsView struct {
	Sessions      []*SessionView `json:"sessions"`
	NextPageToken string         `json:"nextPageToken,omitempty"`
}

// sessionViewFromProto converts a proto Session to a view model. The session
// id is derived from the Session resource name
// (templates/{template}/sessions/{session}) so the frontend keeps the Wails
// `sessionId` JSON shape without a separate proto field
// (specs/035-proto-contract-refine/contracts/resource-fields.md §3.1).
func sessionViewFromProto(s *game.Session) *SessionView {
	if s == nil {
		return nil
	}
	sessionID := ""
	if name, err := game.ParseSessionName(s.GetName()); err == nil {
		sessionID = name.SessionID
	}
	return &SessionView{
		Name:       s.GetName(),
		SessionID:  sessionID,
		CreateTime: timestampString(s.GetCreateTime()),
	}
}

// listSessionsViewFromProto converts a proto ListSessionsResponse to a view model.
func listSessionsViewFromProto(r *game.ListSessionsResponse) *ListSessionsView {
	if r == nil {
		return nil
	}
	sessions := r.GetSessions()
	views := make([]*SessionView, len(sessions))
	for i, s := range sessions {
		views[i] = sessionViewFromProto(s)
	}
	return &ListSessionsView{
		Sessions:      views,
		NextPageToken: r.GetNextPageToken(),
	}
}

// timestampString formats a protobuf Timestamp as an RFC3339 string.
// Returns "" if t is nil.
func timestampString(t *timestamppb.Timestamp) string {
	if t == nil {
		return ""
	}
	return t.AsTime().Format(time.RFC3339)
}
