package main

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	game "dominion/projects/game"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ---------------------------------------------------------------------------
// TestSessionViewFromProto
// ---------------------------------------------------------------------------

func TestSessionViewFromProto(t *testing.T) {
	// given: a proto Session with all fields
	createTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	proto := &game.Session{
		Name:       "sessions/test-session-1",
		SessionId:  "test-session-1",
		CreateTime: timestamppb.New(createTime),
	}

	// when: convert to view model
	view := sessionViewFromProto(proto)

	// then: verify fields match
	if view.Name != "sessions/test-session-1" {
		t.Fatalf("expected Name %q, got %q", "sessions/test-session-1", view.Name)
	}
	if view.SessionID != "test-session-1" {
		t.Fatalf("expected SessionID %q, got %q", "test-session-1", view.SessionID)
	}
	if view.CreateTime != "2024-01-01T00:00:00Z" {
		t.Fatalf("expected CreateTime %q, got %q", "2024-01-01T00:00:00Z", view.CreateTime)
	}

	// and: JSON marshalling uses camelCase
	data, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	jsonStr := string(data)
	if !strings.Contains(jsonStr, `"sessionId"`) {
		t.Fatalf("expected JSON to contain 'sessionId', got: %s", jsonStr)
	}
	if strings.Contains(jsonStr, `"session_id"`) {
		t.Fatalf("expected JSON to NOT contain 'session_id', got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"createTime"`) {
		t.Fatalf("expected JSON to contain 'createTime', got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"name"`) {
		t.Fatalf("expected JSON to contain 'name', got: %s", jsonStr)
	}
}

// ---------------------------------------------------------------------------
// TestSessionViewFromProto_Nil
// ---------------------------------------------------------------------------

func TestSessionViewFromProto_Nil(t *testing.T) {
	// given: nil proto Session
	// when: convert to view model
	view := sessionViewFromProto(nil)

	// then: returns nil
	if view != nil {
		t.Fatal("expected nil, got non-nil")
	}
}

// ---------------------------------------------------------------------------
// TestListSessionsViewFromProto
// ---------------------------------------------------------------------------

func TestListSessionsViewFromProto(t *testing.T) {
	// given: a proto ListSessionsResponse with multiple sessions and nextPageToken
	createTime1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	createTime2 := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	proto := &game.ListSessionsResponse{
		Sessions: []*game.Session{
			{
				SessionId:  "s1",
				CreateTime: timestamppb.New(createTime1),
			},
			{
				SessionId:  "s2",
				CreateTime: timestamppb.New(createTime2),
			},
		},
		NextPageToken: "next-token-42",
	}

	// when: convert to view model
	view := listSessionsViewFromProto(proto)

	// then: verify sessions count
	if len(view.Sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(view.Sessions))
	}
	if view.Sessions[0].SessionID != "s1" {
		t.Fatalf("expected first session SessionID %q, got %q", "s1", view.Sessions[0].SessionID)
	}
	if view.Sessions[0].CreateTime != "2024-01-01T00:00:00Z" {
		t.Fatalf("expected first session CreateTime %q, got %q", "2024-01-01T00:00:00Z", view.Sessions[0].CreateTime)
	}
	if view.Sessions[1].SessionID != "s2" {
		t.Fatalf("expected second session SessionID %q, got %q", "s2", view.Sessions[1].SessionID)
	}
	if view.NextPageToken != "next-token-42" {
		t.Fatalf("expected NextPageToken %q, got %q", "next-token-42", view.NextPageToken)
	}
}

// ---------------------------------------------------------------------------
// TestListSessionsViewFromProto_NilSessions
// ---------------------------------------------------------------------------

func TestListSessionsViewFromProto_NilSessions(t *testing.T) {
	tests := []struct {
		name  string
		input *game.ListSessionsResponse
	}{
		{
			name:  "nil response",
			input: nil,
		},
		{
			name:  "nil sessions slice",
			input: &game.ListSessionsResponse{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when: convert to view model
			view := listSessionsViewFromProto(tt.input)

			if tt.input == nil {
				// then: returns nil
				if view != nil {
					t.Fatal("expected nil, got non-nil")
				}
				return
			}

			// then: non-nil view with empty Sessions slice
			if view == nil {
				t.Fatal("expected non-nil view, got nil")
			}
			if view.Sessions == nil {
				t.Fatal("expected non-nil Sessions slice, got nil")
			}
			if len(view.Sessions) != 0 {
				t.Fatalf("expected empty Sessions, got %d", len(view.Sessions))
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestAgentViewFromProto
// ---------------------------------------------------------------------------

func TestAgentViewFromProto(t *testing.T) {
	// given: a proto Agent with all fields
	createTime := time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)
	proto := &game.Agent{
		Name:             "sessions/sess-agent-1/agent",
		SessionId:        "sess-agent-1",
		CreateTime:       timestamppb.New(createTime),
		AgentProfileName: "test-profile",
	}

	// when: convert to view model
	view := agentViewFromProto(proto)

	// then: verify fields match
	if view.SessionID != "sess-agent-1" {
		t.Fatalf("expected SessionID %q, got %q", "sess-agent-1", view.SessionID)
	}
	if view.AgentProfileName != "test-profile" {
		t.Fatalf("expected AgentProfileName %q, got %q", "test-profile", view.AgentProfileName)
	}

	// and: JSON marshalling uses camelCase
	data, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	jsonStr := string(data)
	if !strings.Contains(jsonStr, `"sessionId"`) {
		t.Fatalf("expected JSON to contain 'sessionId', got: %s", jsonStr)
	}
	if strings.Contains(jsonStr, `"session_id"`) {
		t.Fatalf("expected JSON to NOT contain 'session_id', got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"agentProfileName"`) {
		t.Fatalf("expected JSON to contain 'agentProfileName', got: %s", jsonStr)
	}
}

// ---------------------------------------------------------------------------
// TestAgentViewFromProto_Nil
// ---------------------------------------------------------------------------

func TestAgentViewFromProto_Nil(t *testing.T) {
	// given: nil proto Agent
	// when: convert to view model
	view := agentViewFromProto(nil)

	// then: returns nil
	if view != nil {
		t.Fatal("expected nil, got non-nil")
	}
}

// ---------------------------------------------------------------------------
// TestToMessageViewModels
// ---------------------------------------------------------------------------

func TestToMessageViewModels(t *testing.T) {
	// given: a slice of proto Messages whose content is a PartBlock carrying
	// a text part (user) and a thinking part (agent)
	createTime := time.Date(2024, 7, 1, 12, 0, 0, 0, time.UTC)
	messages := []*game.Message{
		{
			Name:       "sessions/sess-1/agent/messages/msg-1",
			MessageId:  "msg-1",
			Sender:     game.FrameSender_FRAME_SENDER_USER,
			CreateTime: timestamppb.New(createTime),
			Content: &game.PartBlock{Parts: []*game.Part{
				{Kind: &game.Part_Text{Text: &game.TextPart{Content: "Hello from user"}}},
			}},
		},
		{
			Name:       "sessions/sess-1/agent/messages/msg-2",
			MessageId:  "msg-2",
			Sender:     game.FrameSender_FRAME_SENDER_AGENT,
			CreateTime: timestamppb.New(createTime),
			Content: &game.PartBlock{Parts: []*game.Part{
				{Kind: &game.Part_Thinking{Thinking: &game.ThinkingPart{Content: "Agent is thinking"}}},
			}},
		},
	}

	// when: convert to view models
	views := ToMessageViewModels(messages)

	// then: verify result length
	if len(views) != 2 {
		t.Fatalf("expected 2 view models, got %d", len(views))
	}

	// and: verify first message fields
	if views[0].Name != "sessions/sess-1/agent/messages/msg-1" {
		t.Fatalf("expected Name %q, got %q", "sessions/sess-1/agent/messages/msg-1", views[0].Name)
	}
	if views[0].MessageID != "msg-1" {
		t.Fatalf("expected MessageID %q, got %q", "msg-1", views[0].MessageID)
	}
	if views[0].Sender != "FRAME_SENDER_USER" {
		t.Fatalf("expected Sender %q, got %q", "FRAME_SENDER_USER", views[0].Sender)
	}
	if views[0].CreateTime != "2024-07-01T12:00:00Z" {
		t.Fatalf("expected CreateTime %q, got %q", "2024-07-01T12:00:00Z", views[0].CreateTime)
	}
	if got := messagePartText(views[0].Content); got != "Hello from user" {
		t.Fatalf("expected first text part content %q, got %q", "Hello from user", got)
	}

	// and: verify second message fields
	if views[1].Name != "sessions/sess-1/agent/messages/msg-2" {
		t.Fatalf("expected Name %q, got %q", "sessions/sess-1/agent/messages/msg-2", views[1].Name)
	}
	if views[1].MessageID != "msg-2" {
		t.Fatalf("expected MessageID %q, got %q", "msg-2", views[1].MessageID)
	}
	if views[1].Sender != "FRAME_SENDER_AGENT" {
		t.Fatalf("expected Sender %q, got %q", "FRAME_SENDER_AGENT", views[1].Sender)
	}
	if got := messagePartThinking(views[1].Content); got != "Agent is thinking" {
		t.Fatalf("expected second thinking part content %q, got %q", "Agent is thinking", got)
	}

	// and: JSON marshalling uses camelCase
	data, err := json.Marshal(views)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	jsonStr := string(data)
	if !strings.Contains(jsonStr, `"messageId"`) {
		t.Fatalf("expected JSON to contain 'messageId', got: %s", jsonStr)
	}
	if strings.Contains(jsonStr, `"message_id"`) {
		t.Fatalf("expected JSON to NOT contain 'message_id', got: %s", jsonStr)
	}
}

// ---------------------------------------------------------------------------
// TestToMessageViewModels_Image
// ---------------------------------------------------------------------------

func TestToMessageViewModels_Image(t *testing.T) {
	createTime := time.Date(2024, 7, 1, 12, 0, 0, 0, time.UTC)
	rawImage := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A}
	messages := []*game.Message{
		{
			Name:       "sessions/sess-1/agent/messages/img-1",
			MessageId:  "img-1",
			Sender:     game.FrameSender_FRAME_SENDER_USER,
			CreateTime: timestamppb.New(createTime),
			Content: &game.PartBlock{Parts: []*game.Part{
				{Kind: &game.Part_Image{Image: &game.ImagePart{
					Encoding: game.ImageEncoding_IMAGE_ENCODING_PNG,
					Data:     rawImage,
				}}},
			}},
		},
	}

	views := ToMessageViewModels(messages)

	if len(views) != 1 {
		t.Fatalf("expected 1 view model, got %d", len(views))
	}
	if views[0].MessageID != "img-1" {
		t.Fatalf("expected MessageID %q, got %q", "img-1", views[0].MessageID)
	}
	// Content carries the PartBlock; the image part's base64 data round-trips.
	imgB64 := firstPartImageBase64(views[0].Content)
	if imgB64 == "" {
		t.Fatalf("expected non-empty image data in Content, got %v", views[0].Content)
	}
	expectedB64 := base64.StdEncoding.EncodeToString(rawImage)
	if imgB64 != expectedB64 {
		t.Fatalf("expected image data %q, got %q", expectedB64, imgB64)
	}

	data, err := json.Marshal(views[0])
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	jsonStr := string(data)
	if !strings.Contains(jsonStr, `"content"`) {
		t.Fatalf("expected JSON to contain 'content', got: %s", jsonStr)
	}
}

// firstPartImageBase64 extracts the base64 data string of the first image
// part in a serialized PartBlock view-model Content map, or "" when absent.
func firstPartImageBase64(content map[string]any) string {
	parts, ok := content["parts"].([]any)
	if !ok {
		return ""
	}
	for _, p := range parts {
		part, ok := p.(map[string]any)
		if !ok {
			continue
		}
		img, ok := part["image"].(map[string]any)
		if !ok {
			continue
		}
		if s, ok := img["data"].(string); ok {
			return s
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// TestToMessageViewModels_Nil
// ---------------------------------------------------------------------------

func TestToMessageViewModels_Nil(t *testing.T) {
	// given: nil Messages slice
	// when: convert to view models
	views := ToMessageViewModels(nil)

	// then: returns nil
	if views != nil {
		t.Fatal("expected nil, got non-nil")
	}
}

// ---------------------------------------------------------------------------
// TestTimestampString_Nil
// ---------------------------------------------------------------------------

func TestTimestampString_Nil(t *testing.T) {
	// given: nil Timestamp
	// when: format with timestampString
	result := timestampString(nil)

	// then: returns empty string
	if result != "" {
		t.Fatalf("expected empty string, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// TestAgentProfileViewFromProto_ToolNames
// ---------------------------------------------------------------------------

func TestAgentProfileViewFromProto_ToolNames(t *testing.T) {
	// given: a proto AgentProfile with ToolNames set
	proto := &game.AgentProfile{
		Name:      "agentProfiles/tool-test",
		Model:     "gpt-4",
		ToolNames: []string{"mouse", "keyboard"},
	}

	// when: convert to view model
	view := agentProfileViewFromProto(proto)

	// then: ToolNames round-trips with matching length and values
	if view == nil {
		t.Fatal("expected non-nil view, got nil")
	}
	if len(view.ToolNames) != 2 {
		t.Fatalf("expected ToolNames length 2, got %d", len(view.ToolNames))
	}
	if view.ToolNames[0] != "mouse" {
		t.Errorf("expected ToolNames[0] %q, got %q", "mouse", view.ToolNames[0])
	}
	if view.ToolNames[1] != "keyboard" {
		t.Errorf("expected ToolNames[1] %q, got %q", "keyboard", view.ToolNames[1])
	}

	// and: JSON marshalling uses camelCase
	data, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	jsonStr := string(data)
	if !strings.Contains(jsonStr, `"toolNames"`) {
		t.Fatalf("expected JSON to contain 'toolNames', got: %s", jsonStr)
	}
	if strings.Contains(jsonStr, `"tool_names"`) {
		t.Fatalf("expected JSON to NOT contain 'tool_names', got: %s", jsonStr)
	}
}
