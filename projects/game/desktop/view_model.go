package main

import (
	"encoding/json"
	"time"

	"dominion/projects/game"

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

// TeamView is the Wails view model for game.Team: the execution subject of a
// session with its per-template agents (spec 031-team-template-mode
// contracts/api-contract.md §3.3).
type TeamView struct {
	Name       string           `json:"name"`
	SessionID  string           `json:"sessionId"`
	Agents     []*TeamAgentView `json:"agents"`
	CreateTime string           `json:"createTime,omitempty"`
}

// TeamAgentView is the Wails view model for game.TeamAgent: one agent inside
// the team, with its user-input acceptance flag (FR-031).
type TeamAgentView struct {
	Name             string `json:"name"`
	AcceptsUserInput bool   `json:"acceptsUserInput"`
}

// MessageViewModel is the Wails view model for game.Message. The message
// content is projected as the same MessageParts shape a live TeamFrame's
// message_parts payload carries, so history and live view render identically:
// Content holds the protojson-serialized MessageParts ({"parts":[...]} with
// camelCase field names, flattened oneofs, and base64 image bytes), matching
// what app.go's chatstream emits for live messageParts frames
// (spec 023 FR-009; contracts/content-model-contract.md §5/§8).
type MessageViewModel struct {
	Name       string         `json:"name"`
	MessageID  string         `json:"messageId"`
	Role       string         `json:"role"`
	Agent      string         `json:"agent"`
	CreateTime string         `json:"createTime,omitempty"`
	Content    map[string]any `json:"content,omitempty"`
}

// CreateTeamProfileView is the Wails input struct for creating a TeamProfile.
// The template-specific spec is the typed oneof variant (saolei:
// SaoleiProfile{player_model, planner_model, player_prompt, planner_prompt}) —
// no generic key-value fields (spec 031-team-template-mode
// contracts/api-contract.md §3.5). The base prompts are optional: empty means
// "unset" and falls back to the template default base (spec
// 031-team-template-mode spec.md FR-034).
type CreateTeamProfileView struct {
	ProfileName   string `json:"profileName"`
	PlayerModel   string `json:"playerModel"`
	PlannerModel  string `json:"plannerModel"`
	PlayerPrompt  string `json:"playerPrompt"`
	PlannerPrompt string `json:"plannerPrompt"`
}

// TeamProfileView is the Wails view model for game.TeamProfile. The spec
// oneof is flattened per-template: for saolei the active variant
// (spec.saolei) projects to PlayerModel/PlannerModel and the optional base
// prompts PlayerPrompt/PlannerPrompt (empty = template default base, spec
// 031-team-template-mode spec.md FR-034).
type TeamProfileView struct {
	Name          string `json:"name"`
	ProfileName   string `json:"profileName"`
	PlayerModel   string `json:"playerModel"`
	PlannerModel  string `json:"plannerModel"`
	PlayerPrompt  string `json:"playerPrompt"`
	PlannerPrompt string `json:"plannerPrompt"`
}

// ListTeamProfilesView is the Wails view model for game.ListTeamProfilesResponse.
type ListTeamProfilesView struct {
	TeamProfiles  []*TeamProfileView `json:"teamProfiles"`
	NextPageToken string             `json:"nextPageToken,omitempty"`
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

// teamViewFromProto converts a proto Team to a view model. The session id is
// derived from the Team resource name
// (templates/{template}/sessions/{session}/team) so the frontend can scope
// per-session operations without a separate lookup.
func teamViewFromProto(t *game.Team) *TeamView {
	if t == nil {
		return nil
	}
	agents := t.GetAgents()
	agentViews := make([]*TeamAgentView, len(agents))
	for i, a := range agents {
		agentViews[i] = teamAgentViewFromProto(a)
	}
	name, err := game.ParseTeamName(t.GetName())
	sessionID := ""
	if err == nil {
		sessionID = name.SessionID
	}
	return &TeamView{
		Name:       t.GetName(),
		SessionID:  sessionID,
		Agents:     agentViews,
		CreateTime: timestampString(t.GetCreateTime()),
	}
}

// teamAgentViewFromProto converts a proto TeamAgent to a view model.
func teamAgentViewFromProto(a *game.TeamAgent) *TeamAgentView {
	if a == nil {
		return nil
	}
	return &TeamAgentView{
		Name:             a.GetName(),
		AcceptsUserInput: a.GetAcceptsUserInput(),
	}
}

// ToMessageViewModels converts a slice of proto Message to view models. Each
// message's Content MessageParts is serialized via protojson (camelCase field
// names, flattened oneofs, base64 image bytes) so it matches the live
// TeamFrame messageParts emitted by app.go's chatstream — history and live
// view render identically. The MessagePart oneof flattens so each part's
// active variant (text/thinking/image/toolCall/toolResult) appears camelCase.
func ToMessageViewModels(messages []*game.Message) []*MessageViewModel {
	if messages == nil {
		return nil
	}
	views := make([]*MessageViewModel, len(messages))
	for i, m := range messages {
		views[i] = &MessageViewModel{
			Name:       m.GetName(),
			MessageID:  m.GetMessageId(),
			Role:       m.GetRole().String(),
			Agent:      m.GetAgent(),
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

// timestampString formats a protobuf Timestamp as an RFC3339 string.
// Returns "" if t is nil.
func timestampString(t *timestamppb.Timestamp) string {
	if t == nil {
		return ""
	}
	return t.AsTime().Format(time.RFC3339)
}

// teamProfileViewFromProto converts a proto TeamProfile to a view model. The
// typed spec oneof is flattened: the active saolei variant projects to
// PlayerModel/PlannerModel and the optional base prompts
// PlayerPrompt/PlannerPrompt (empty = template default base — the only
// saolei-specialized fields, FR-027/FR-034, spec 031-team-template-mode
// spec.md).
func teamProfileViewFromProto(p *game.TeamProfile) *TeamProfileView {
	if p == nil {
		return nil
	}
	profileName := ""
	if name, err := game.ParseTeamProfileName(p.GetName()); err == nil {
		profileName = name.ProfileID
	}
	saolei := p.GetSaolei()
	view := &TeamProfileView{
		Name:        p.GetName(),
		ProfileName: profileName,
	}
	if saolei != nil {
		view.PlayerModel = saolei.GetPlayerModel()
		view.PlannerModel = saolei.GetPlannerModel()
		view.PlayerPrompt = saolei.GetPlayerPrompt()
		view.PlannerPrompt = saolei.GetPlannerPrompt()
	}
	return view
}

func listTeamProfilesViewFromProto(r *game.ListTeamProfilesResponse) *ListTeamProfilesView {
	if r == nil {
		return nil
	}
	profiles := r.GetTeamProfiles()
	views := make([]*TeamProfileView, len(profiles))
	for i, p := range profiles {
		views[i] = teamProfileViewFromProto(p)
	}
	return &ListTeamProfilesView{
		TeamProfiles:  views,
		NextPageToken: r.GetNextPageToken(),
	}
}
