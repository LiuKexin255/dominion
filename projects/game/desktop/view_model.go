package main

import (
	"encoding/base64"
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

// MessageViewModel is the Wails view model for game.Message.
type MessageViewModel struct {
	Name            string                      `json:"name"`
	MessageID       string                      `json:"messageId"`
	Sender          string                      `json:"sender"`
	Type            string                      `json:"type"`
	Content         string                      `json:"content"`
	ImageData       string                      `json:"imageData,omitempty"`
	CreateTime      string                      `json:"createTime,omitempty"`
	Operation       *MessageOperationJSON       `json:"operation,omitempty"`
	OperationResult *MessageOperationResultJSON `json:"operationResult,omitempty"`
}

// MessageOperationJSON is the JSON carrier for an operation frame attached to a
// history Message. It mirrors the live AgentFrame.operation shape (produced by
// app.go's frameToMap via protojson) so history renders identically to live view.
type MessageOperationJSON map[string]any

// MessageOperationResultJSON is the JSON carrier for an operation-result frame
// attached to a history Message. It mirrors the live AgentFrame.operationResult
// shape (protojson) so history renders identically to live view.
type MessageOperationResultJSON map[string]any

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

// ToMessageViewModels converts a slice of proto Message to view models.
func ToMessageViewModels(messages []*game.Message) []*MessageViewModel {
	if messages == nil {
		return nil
	}
	views := make([]*MessageViewModel, len(messages))
	for i, m := range messages {
		vm := &MessageViewModel{
			Name:            m.GetName(),
			MessageID:       m.GetMessageId(),
			Sender:          m.GetSender().String(),
			Type:            m.GetType(),
			Content:         m.GetText(),
			CreateTime:      timestampString(m.GetCreateTime()),
			Operation:       operationToJSON(m.GetOperation()),
			OperationResult: operationResultToJSON(m.GetOperationResult()),
		}
		if m.GetType() == "image" {
			if data := m.GetImageData(); len(data) > 0 {
				vm.ImageData = base64.StdEncoding.EncodeToString(data)
			}
		}
		views[i] = vm
	}
	return views
}

// protoToJSONMap marshals a proto message via protojson and decodes it into a
// generic map, mirroring app.go's frameToMap: camelCase field names, flattened
// oneofs, enums serialized as their string names, and bytes as base64.
func protoToJSONMap(m proto.Message) map[string]any {
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

// operationToJSON converts a proto AgentOperationFrame to its history JSON
// carrier, or nil when the operation is absent.
func operationToJSON(op *game.AgentOperationFrame) *MessageOperationJSON {
	if op == nil {
		return nil
	}
	m := protoToJSONMap(op)
	return (*MessageOperationJSON)(&m)
}

// operationResultToJSON converts a proto AgentOperationResultFrame to its
// history JSON carrier, or nil when the result is absent.
func operationResultToJSON(r *game.AgentOperationResultFrame) *MessageOperationResultJSON {
	if r == nil {
		return nil
	}
	m := protoToJSONMap(r)
	return (*MessageOperationResultJSON)(&m)
}

// CreateAgentProfileView is the Wails input struct for creating an AgentProfile.
// Per UI contract FR-004, SkillNames and McpNames are omitted in this version.
type CreateAgentProfileView struct {
	AgentProfileName string   `json:"agentProfileName"`
	Model            string   `json:"model"`
	SystemPrompt     string   `json:"systemPrompt"`
	Enabled          bool     `json:"enabled"`
	ToolNames        []string `json:"toolNames"`
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
