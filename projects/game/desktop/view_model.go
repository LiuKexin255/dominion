package main

import (
	"time"

	game "dominion/projects/game"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// SessionView is the Wails view model for game.Session.
type SessionView struct {
	SessionID  string `json:"sessionId"`
	CreateTime string `json:"createTime,omitempty"`
}

// ListSessionsView is the Wails view model for game.ListSessionsResponse.
type ListSessionsView struct {
	Sessions      []SessionView `json:"sessions"`
	NextPageToken string        `json:"nextPageToken,omitempty"`
}

// AgentView is the Wails view model for game.Agent.
type AgentView struct {
	SessionID  string `json:"sessionId"`
	OwnerIndex int32  `json:"ownerIndex"`
	Owner      string `json:"owner,omitempty"`
	CreateTime string `json:"createTime,omitempty"`
}

// sessionViewFromProto converts a proto Session to a view model.
func sessionViewFromProto(s *game.Session) SessionView {
	if s == nil {
		return SessionView{}
	}
	return SessionView{
		SessionID:  s.GetSessionId(),
		CreateTime: timestampString(s.GetCreateTime()),
	}
}

// listSessionsViewFromProto converts a proto ListSessionsResponse to a view model.
func listSessionsViewFromProto(r *game.ListSessionsResponse) ListSessionsView {
	if r == nil {
		return ListSessionsView{Sessions: []SessionView{}}
	}
	sessions := r.GetSessions()
	views := make([]SessionView, len(sessions))
	for i, s := range sessions {
		views[i] = sessionViewFromProto(s)
	}
	return ListSessionsView{
		Sessions:      views,
		NextPageToken: r.GetNextPageToken(),
	}
}

// agentViewFromProto converts a proto Agent to a view model.
func agentViewFromProto(a *game.Agent) AgentView {
	if a == nil {
		return AgentView{}
	}
	return AgentView{
		SessionID:  a.GetSessionId(),
		OwnerIndex: a.GetOwnerIndex(),
		Owner:      a.GetOwner(),
		CreateTime: timestampString(a.GetCreateTime()),
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
