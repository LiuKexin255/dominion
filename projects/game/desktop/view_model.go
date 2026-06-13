package main

import (
	"time"

	game "dominion/projects/game"
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
	Sessions      []SessionView `json:"sessions"`
	NextPageToken string        `json:"nextPageToken,omitempty"`
}

// AgentView is the Wails view model for game.Agent.
type AgentView struct {
	Name       string `json:"name"`
	SessionID  string `json:"sessionId"`
	CreateTime string `json:"createTime,omitempty"`
}

// sessionViewFromProto converts a proto Session to a view model.
func sessionViewFromProto(s *game.Session) *SessionView {
	if s == nil {
		return nil
	}
	return &SessionView{
		Name:       s.GetName(),
		SessionID:  s.GetSessionId(),
		CreateTime: timestampString(s.GetCreateTime()),
	}
}

// listSessionsViewFromProto converts a proto ListSessionsResponse to a view model.
func listSessionsViewFromProto(r *game.ListSessionsResponse) *ListSessionsView {
	if r == nil {
		return nil
	}
	sessions := r.GetSessions()
	views := make([]SessionView, len(sessions))
	for i, s := range sessions {
		views[i] = *sessionViewFromProto(s)
	}
	return &ListSessionsView{
		Sessions:      views,
		NextPageToken: r.GetNextPageToken(),
	}
}

// agentViewFromProto converts a proto Agent to a view model.
func agentViewFromProto(a *game.Agent) *AgentView {
	if a == nil {
		return nil
	}
	return &AgentView{
		Name:       a.GetName(),
		SessionID:  a.GetSessionId(),
		CreateTime: timestampString(a.GetCreateTime()),
	}
}

// CreateAgentProfileView is the Wails input struct for creating an AgentProfile.
// Per UI contract FR-004, SkillNames and McpNames are omitted in this version.
type CreateAgentProfileView struct {
	AgentProfileName string `json:"agentProfileName"`
	Model            string `json:"model"`
	SystemPrompt     string `json:"systemPrompt"`
	Enabled          bool   `json:"enabled"`
}

// AgentProfileView is the Wails view model for game.AgentProfile.
type AgentProfileView struct {
	Name              string   `json:"name"`
	AgentProfileName  string   `json:"agentProfileName"`
	Model             string   `json:"model"`
	SystemPrompt      string   `json:"systemPrompt"`
	SkillNames        []string `json:"skillNames"`
	McpNames          []string `json:"mcpNames"`
	Enabled           bool     `json:"enabled"`
	CreateTime        string   `json:"createTime,omitempty"`
	UpdateTime        string   `json:"updateTime,omitempty"`
}

// ListAgentProfilesView is the Wails view model for game.ListAgentProfilesResponse.
type ListAgentProfilesView struct {
	AgentProfiles []AgentProfileView `json:"agentProfiles"`
	NextPageToken string             `json:"nextPageToken,omitempty"`
}

// OperationResultView is the Wails view model for an operation execution result.
type OperationResultView struct {
	OperationID string `json:"operationId"`
	Sequence    int64  `json:"sequence"`
	Status      int32  `json:"status"`
	Message     string `json:"message,omitempty"`
}

// timestampString formats a protobuf Timestamp as an RFC3339 string.
// Returns "" if t is nil.
func timestampString(t *timestamppb.Timestamp) string {
	if t == nil {
		return ""
	}
	return t.AsTime().Format(time.RFC3339)
}

func agentProfileViewFromProto(p *game.AgentProfile) *AgentProfileView {
	if p == nil {
		return nil
	}
	return &AgentProfileView{
		Name:             p.GetName(),
		AgentProfileName: p.GetAgentProfileName(),
		Model:            p.GetModel(),
		SystemPrompt:     p.GetSystemPrompt(),
		SkillNames:       p.GetSkillNames(),
		McpNames:         p.GetMcpNames(),
		Enabled:          p.GetEnabled(),
		CreateTime:       timestampString(p.GetCreateTime()),
		UpdateTime:       timestampString(p.GetUpdateTime()),
	}
}

func listAgentProfilesViewFromProto(r *game.ListAgentProfilesResponse) *ListAgentProfilesView {
	if r == nil {
		return nil
	}
	profiles := r.GetAgentProfiles()
	views := make([]AgentProfileView, len(profiles))
	for i, p := range profiles {
		views[i] = *agentProfileViewFromProto(p)
	}
	return &ListAgentProfilesView{
		AgentProfiles: views,
		NextPageToken: r.GetNextPageToken(),
	}
}
