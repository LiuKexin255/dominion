package main

import (
	"encoding/json"
	"time"

	game "dominion/projects/game"
	gameconst "dominion/projects/game/pkg/gameconst"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
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

// AgentView is the Wails view model for the active session agent.
// AgentProfileName represents the active profile (may be empty initially,
// set after the first message).
type AgentView struct {
	SessionID        string `json:"sessionId"`
	AgentProfileName string `json:"agentProfileName"`
}

// MessageViewModel is the Wails view model for game.Message. The message
// content is projected as the same PartBlock shape a live AgentFrame's content
// payload carries, so history and live view render identically: Content holds
// the protojson-serialized PartBlock ({"parts":[...]} with camelCase field
// names and base64 image bytes), matching what app.go's frameToMap emits for
// live content frames.
type MessageViewModel struct {
	Name       string         `json:"name"`
	MessageID  string         `json:"messageId"`
	Sender     string         `json:"sender"`
	CreateTime string         `json:"createTime,omitempty"`
	Content    map[string]any `json:"content,omitempty"`
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
	views := make([]*SessionView, len(sessions))
	for i, s := range sessions {
		views[i] = sessionViewFromProto(s)
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
		SessionID:        a.GetSessionId(),
		AgentProfileName: a.GetAgentProfileName(),
	}
}

// ToMessageViewModels converts a slice of proto Message to view models. Each
// message's Content PartBlock is serialized via protojson (camelCase field
// names, flattened oneofs, base64 image bytes) so it matches the live
// AgentFrame content emitted by app.go's frameToMap — history and live view
// render identically.
func ToMessageViewModels(messages []*game.Message) []*MessageViewModel {
	if messages == nil {
		return nil
	}
	views := make([]*MessageViewModel, len(messages))
	for i, m := range messages {
		views[i] = &MessageViewModel{
			Name:       m.GetName(),
			MessageID:  m.GetMessageId(),
			Sender:     m.GetSender().String(),
			CreateTime: timestampString(m.GetCreateTime()),
			Content:    protoToJSONMap(m.GetContent()),
		}
	}
	return views
}

// protoToJSONMap marshals a proto message via protojson and decodes it into a
// generic map, mirroring app.go's frameToMap: camelCase field names, flattened
// oneofs, enums serialized as their string names, and bytes as base64. Returns
// nil for a nil message so the Content field omits cleanly.
func protoToJSONMap(m proto.Message) map[string]any {
	if m == nil {
		return nil
	}
	jsonBytes, err := protojson.Marshal(m)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(jsonBytes, &out); err != nil {
		return nil
	}
	return out
}

// CreateAgentProfileView is the Wails input struct for creating an AgentProfile.
// McpNames carries selected MCP integrations (e.g. "saolei") per spec
// 018-saolei-mcp FR-021.
type CreateAgentProfileView struct {
	AgentProfileName string   `json:"agentProfileName"`
	Model            string   `json:"model"`
	SystemPrompt     string   `json:"systemPrompt"`
	Enabled          bool     `json:"enabled"`
	ToolNames        []string `json:"toolNames"`
	McpNames         []string `json:"mcpNames"`
}

// AgentProfileView is the Wails view model for game.AgentProfile.
type AgentProfileView struct {
	Name             string   `json:"name"`
	AgentProfileName string   `json:"agentProfileName"`
	Model            string   `json:"model"`
	SystemPrompt     string   `json:"systemPrompt"`
	SkillNames       []string `json:"skillNames"`
	McpNames         []string `json:"mcpNames"`
	ToolNames        []string `json:"toolNames"`
	Enabled          bool     `json:"enabled"`
	CreateTime       string   `json:"createTime,omitempty"`
	UpdateTime       string   `json:"updateTime,omitempty"`
}

// ListAgentProfilesView is the Wails view model for game.ListAgentProfilesResponse.
type ListAgentProfilesView struct {
	AgentProfiles []*AgentProfileView `json:"agentProfiles"`
	NextPageToken string              `json:"nextPageToken,omitempty"`
}

// ChatStreamHandoff is returned by OpenChatStream and carries the
// connection parameters the frontend uses to open an EventSource:
//
//   - Endpoint: the full SSE endpoint URL
//     (http://127.0.0.1:<port>/api/v1/chat/stream), stable for the
//     process lifetime.
//   - Token: a fresh crypto/rand auth token, rotated on every
//     OpenChatStream call so a re-entry invalidates old subscribers.
//   - LastEventID: the highest event ID currently in the log (the
//     highest ID the frontend has already received). Debug/observability
//     only; EventSource cannot set Last-Event-ID on the initial connect
//     so the frontend does NOT consume it as a query param.
//
// OpenChatStream seeds history synchronously from ListMessages.
// Assumption (current single-session desktop scope): history fits in
// memory. A very large history blocking session entry is acceptable
// in this version; pagination / async-seed is future work (F11).
type ChatStreamHandoff struct {
	Endpoint    string `json:"endpoint"`
	Token       string `json:"token"`
	LastEventID int64  `json:"lastEventId"`
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
	profileID, _ := gameconst.AgentProfileID(p.GetName())
	return &AgentProfileView{
		Name:             p.GetName(),
		AgentProfileName: profileID,
		Model:            p.GetModel(),
		SystemPrompt:     p.GetSystemPrompt(),
		SkillNames:       p.GetSkillNames(),
		McpNames:         p.GetMcpNames(),
		ToolNames:        p.GetToolNames(),
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
	views := make([]*AgentProfileView, len(profiles))
	for i, p := range profiles {
		views[i] = agentProfileViewFromProto(p)
	}
	return &ListAgentProfilesView{
		AgentProfiles: views,
		NextPageToken: r.GetNextPageToken(),
	}
}
