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
		Name:       "sessions/sess-agent-1/agent",
		SessionId:  "sess-agent-1",
		OwnerIndex: 0,
		Owner:      "user",
		CreateTime: timestamppb.New(createTime),
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
	if view.OwnerIndex != 0 {
		t.Fatalf("expected OwnerIndex 0, got %d", view.OwnerIndex)
	}
	if view.Owner != "user" {
		t.Fatalf("expected Owner %q, got %q", "user", view.Owner)
	}
	if view.CreateTime != "2024-06-15T10:30:00Z" {
		t.Fatalf("expected CreateTime %q, got %q", "2024-06-15T10:30:00Z", view.CreateTime)
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
	if !strings.Contains(jsonStr, `"ownerIndex"`) {
		t.Fatalf("expected JSON to contain 'ownerIndex', got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"name"`) {
		t.Fatalf("expected JSON to contain 'name', got: %s", jsonStr)
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
