package main

import (
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
	if view.Name != "sessions/sess-agent-1/agent" {
		t.Fatalf("expected Name %q, got %q", "sessions/sess-agent-1/agent", view.Name)
	}
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
	if !strings.Contains(jsonStr, `"name"`) {
		t.Fatalf("expected JSON to contain 'name', got: %s", jsonStr)
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
	// given: a slice of proto Messages with all fields
	createTime := time.Date(2024, 7, 1, 12, 0, 0, 0, time.UTC)
	messages := []*game.Message{
		{
			Name:       "sessions/sess-1/agent/messages/msg-1",
			MessageId:  "msg-1",
			Sender:     game.FrameSender_FRAME_SENDER_USER,
			Type:       "text",
			Content:    "Hello from user",
			CreateTime: timestamppb.New(createTime),
		},
		{
			Name:       "sessions/sess-1/agent/messages/msg-2",
			MessageId:  "msg-2",
			Sender:     game.FrameSender_FRAME_SENDER_AGENT,
			Type:       "thinking",
			Content:    "Agent is thinking",
			CreateTime: timestamppb.New(createTime),
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
	if views[0].Type != "text" {
		t.Fatalf("expected Type %q, got %q", "text", views[0].Type)
	}
	if views[0].Content != "Hello from user" {
		t.Fatalf("expected Content %q, got %q", "Hello from user", views[0].Content)
	}
	if views[0].CreateTime != "2024-07-01T12:00:00Z" {
		t.Fatalf("expected CreateTime %q, got %q", "2024-07-01T12:00:00Z", views[0].CreateTime)
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
	if views[1].Type != "thinking" {
		t.Fatalf("expected Type %q, got %q", "thinking", views[1].Type)
	}
	if views[1].Content != "Agent is thinking" {
		t.Fatalf("expected Content %q, got %q", "Agent is thinking", views[1].Content)
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
