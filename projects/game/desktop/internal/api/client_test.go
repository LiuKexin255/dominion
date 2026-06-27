package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	game "dominion/projects/game"

	"google.golang.org/protobuf/encoding/protojson"
)

// ---------------------------------------------------------------------------
// TestClient_CreateSession
// ---------------------------------------------------------------------------

func TestClient_CreateSession(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		respBody   string
		wantErr    bool
	}{
		{
			name:       "success",
			statusCode: http.StatusOK,
			respBody:   `{"name":"sessions/test-session","sessionId":"test-session","createTime":"2024-01-01T00:00:00Z"}`,
			wantErr:    false,
		},
		{
			name:       "server error",
			statusCode: http.StatusInternalServerError,
			respBody:   "internal error",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given: mock server that validates request and returns canned response
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("expected POST, got %s", r.Method)
				}
				if r.URL.Path != "/api/v1/sessions" {
					t.Errorf("expected /api/v1/sessions, got %s", r.URL.Path)
				}
				if r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
				}

				body, _ := io.ReadAll(r.Body)
				if strings.TrimSpace(string(body)) != `{}` {
					t.Errorf("expected request body {}, got %s", string(body))
				}

				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.respBody))
			}))
			defer srv.Close()

			client := NewClient(Config{GatewayURL: srv.URL})

			// when: call CreateSession
			session, err := client.CreateSession(context.Background())

			// then: verify result
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), "create session") {
					t.Errorf("error should contain 'create session', got %q", err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if session == nil {
				t.Fatal("expected session, got nil")
			}
			if session.GetSessionId() != "test-session" {
				t.Errorf("expected session_id %q, got %q", "test-session", session.GetSessionId())
			}
			if session.GetName() != "sessions/test-session" {
				t.Errorf("expected name %q, got %q", "sessions/test-session", session.GetName())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestCreateSession_ServerGeneratedID
// ---------------------------------------------------------------------------

func TestCreateSession_ServerGeneratedID(t *testing.T) {
	// given: mock server that verifies empty request body
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// verify empty request body sent as {}
		body, _ := io.ReadAll(r.Body)
		if strings.TrimSpace(string(body)) != `{}` {
			t.Errorf("expected empty request body {}, got %s", string(body))
		}

		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		// return a session with server-generated ID
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"name":"sessions/server-gen-abc","sessionId":"server-gen-abc","createTime":"2024-06-01T12:00:00Z"}`))
	}))
	defer srv.Close()

	client := NewClient(Config{GatewayURL: srv.URL})

	// when: call CreateSession (no session_id argument)
	session, err := client.CreateSession(context.Background())

	// then: verify SessionId is extracted from response
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.GetSessionId() != "server-gen-abc" {
		t.Errorf("expected session_id %q, got %q", "server-gen-abc", session.GetSessionId())
	}
	if session.GetName() != "sessions/server-gen-abc" {
		t.Errorf("expected name %q, got %q", "sessions/server-gen-abc", session.GetName())
	}
}

// ---------------------------------------------------------------------------
// TestClient_ListSessions
// ---------------------------------------------------------------------------

func TestClient_ListSessions(t *testing.T) {
	tests := []struct {
		name       string
		pageSize   int32
		pageToken  string
		statusCode int
		respBody   string
		wantErr    bool
		wantErrMsg string
	}{
		{
			name:       "success with page_size and page_token",
			pageSize:   10,
			pageToken:  "token1",
			statusCode: http.StatusOK,
			respBody:   `{"sessions":[{"name":"sessions/s1","sessionId":"s1","createTime":"2024-01-01T00:00:00Z"},{"name":"sessions/s2","sessionId":"s2","createTime":"2024-01-02T00:00:00Z"}],"nextPageToken":"next"}`,
			wantErr:    false,
		},
		{
			name:       "success with page_size only",
			pageSize:   5,
			statusCode: http.StatusOK,
			respBody:   `{"sessions":[],"nextPageToken":""}`,
			wantErr:    false,
		},
		{
			name:       "success with no parameters",
			statusCode: http.StatusOK,
			respBody:   `{"sessions":[{"name":"sessions/s1","sessionId":"s1","createTime":"2024-01-01T00:00:00Z"}]}`,
			wantErr:    false,
		},
		{
			name:       "server error",
			pageSize:   10,
			statusCode: http.StatusInternalServerError,
			respBody:   "internal error",
			wantErr:    true,
			wantErrMsg: "list sessions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("expected GET, got %s", r.Method)
				}
				if r.URL.Path != "/api/v1/sessions" {
					t.Errorf("expected /api/v1/sessions, got %s", r.URL.Path)
				}

				if tt.pageSize > 0 {
					if got := r.URL.Query().Get("page_size"); got != "" {
						if got != fmtInt32(tt.pageSize) {
							t.Errorf("expected page_size %d, got %s", tt.pageSize, got)
						}
					}
				}
				if tt.pageToken != "" {
					if got := r.URL.Query().Get("page_token"); got != tt.pageToken {
						t.Errorf("expected page_token %q, got %q", tt.pageToken, got)
					}
				}

				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.respBody))
			}))
			defer srv.Close()

			client := NewClient(Config{GatewayURL: srv.URL})

			// when
			resp, err := client.ListSessions(context.Background(), tt.pageSize, tt.pageToken)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.wantErrMsg != "" && !strings.Contains(err.Error(), tt.wantErrMsg) {
					t.Errorf("error should contain %q, got %q", tt.wantErrMsg, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp == nil {
				t.Fatal("expected response, got nil")
			}
			if tt.pageToken == "token1" {
				if resp.GetNextPageToken() != "next" {
					t.Errorf("expected next_page_token %q, got %q", "next", resp.GetNextPageToken())
				}
				if len(resp.GetSessions()) != 2 {
					t.Errorf("expected 2 sessions, got %d", len(resp.GetSessions()))
				}
				if resp.GetSessions()[0].GetSessionId() != "s1" {
					t.Errorf("expected first session_id %q, got %q", "s1", resp.GetSessions()[0].GetSessionId())
				}
			}
			if tt.pageSize == 5 {
				if len(resp.GetSessions()) != 0 {
					t.Errorf("expected 0 sessions, got %d", len(resp.GetSessions()))
				}
			}
		})
	}
}

// fmtInt32 formats an int32 as a string without importing fmt.
func fmtInt32(v int32) string {
	return fmt.Sprintf("%d", v)
}

// ---------------------------------------------------------------------------
// TestClient_GetSession
// ---------------------------------------------------------------------------

func TestClient_GetSession(t *testing.T) {
	tests := []struct {
		name       string
		sessionID  string
		statusCode int
		respBody   string
		wantErr    bool
	}{
		{
			name:       "success",
			sessionID:  "test123",
			statusCode: http.StatusOK,
			respBody:   `{"name":"sessions/test123","sessionId":"test123","createTime":"2024-01-01T00:00:00Z"}`,
			wantErr:    false,
		},
		{
			name:       "not found",
			sessionID:  "missing",
			statusCode: http.StatusNotFound,
			respBody:   `{"error":"not found"}`,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("expected GET, got %s", r.Method)
				}
				wantPath := "/api/v1/sessions/" + tt.sessionID
				if r.URL.Path != wantPath {
					t.Errorf("expected %s, got %s", wantPath, r.URL.Path)
				}
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.respBody))
			}))
			defer srv.Close()

			client := NewClient(Config{GatewayURL: srv.URL})

			// when
			session, err := client.GetSession(context.Background(), tt.sessionID)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if session.GetSessionId() != tt.sessionID {
				t.Errorf("expected session_id %q, got %q", tt.sessionID, session.GetSessionId())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestClient_DeleteSession
// ---------------------------------------------------------------------------

func TestClient_DeleteSession(t *testing.T) {
	tests := []struct {
		name       string
		sessionID  string
		statusCode int
		respBody   string
		wantErr    bool
	}{
		{
			name:       "success",
			sessionID:  "del-me",
			statusCode: http.StatusOK,
			wantErr:    false,
		},
		{
			name:       "server error",
			sessionID:  "del-fail",
			statusCode: http.StatusInternalServerError,
			respBody:   "internal error",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodDelete {
					t.Errorf("expected DELETE, got %s", r.Method)
				}
				wantPath := "/api/v1/sessions/" + tt.sessionID
				if r.URL.Path != wantPath {
					t.Errorf("expected %s, got %s", wantPath, r.URL.Path)
				}
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.respBody))
			}))
			defer srv.Close()

			client := NewClient(Config{GatewayURL: srv.URL})

			// when
			err := client.DeleteSession(context.Background(), tt.sessionID)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestClient_GetAgent
// ---------------------------------------------------------------------------

func TestClient_GetAgent(t *testing.T) {
	tests := []struct {
		name       string
		sessionID  string
		statusCode int
		respBody   string
		wantErr    bool
	}{
		{
			name:       "success",
			sessionID:  "sess-1",
			statusCode: http.StatusOK,
			respBody:   `{"name":"sessions/sess-1/agent","sessionId":"sess-1","createTime":"2024-01-01T00:00:00Z"}`,
			wantErr:    false,
		},
		{
			name:       "not found",
			sessionID:  "no-agent",
			statusCode: http.StatusNotFound,
			respBody:   "not found",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("expected GET, got %s", r.Method)
				}
				wantPath := "/api/v1/sessions/" + tt.sessionID + "/agent"
				if r.URL.Path != wantPath {
					t.Errorf("expected %s, got %s", wantPath, r.URL.Path)
				}
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.respBody))
			}))
			defer srv.Close()

			client := NewClient(Config{GatewayURL: srv.URL})

			// when
			agent, err := client.GetAgent(context.Background(), tt.sessionID)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if agent == nil {
				t.Fatal("expected agent, got nil")
			}
			if agent.GetSessionId() != tt.sessionID {
				t.Errorf("expected session_id %q, got %q", tt.sessionID, agent.GetSessionId())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestClient_ListMessages
// ---------------------------------------------------------------------------

func TestClient_ListMessages(t *testing.T) {
	tests := []struct {
		name       string
		sessionID  string
		statusCode int
		respBody   string
		wantErr    bool
		wantCount  int
	}{
		{
			name:       "success with messages",
			sessionID:  "test-session",
			statusCode: http.StatusOK,
			respBody:   `{"messages":[{"name":"sessions/test-session/messages/msg-1","messageId":"msg-1","sender":"FRAME_SENDER_USER","content":{"parts":[{"text":{"content":"hello"}}]},"createTime":"2024-01-01T00:00:00Z"},{"name":"sessions/test-session/messages/msg-2","messageId":"msg-2","sender":"FRAME_SENDER_AGENT","content":{"parts":[{"text":{"content":"hi there"}}]},"createTime":"2024-01-01T00:00:01Z"}]}`,
			wantErr:    false,
			wantCount:  2,
		},
		{
			name:       "success with empty list",
			sessionID:  "empty-session",
			statusCode: http.StatusOK,
			respBody:   `{"messages":[]}`,
			wantErr:    false,
			wantCount:  0,
		},
		{
			name:       "server error",
			sessionID:  "bad-session",
			statusCode: http.StatusInternalServerError,
			respBody:   "internal error",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("expected GET, got %s", r.Method)
				}
				wantPath := "/api/v1/sessions/" + tt.sessionID + "/messages"
				if r.URL.Path != wantPath {
					t.Errorf("expected %s, got %s", wantPath, r.URL.Path)
				}
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.respBody))
			}))
			defer srv.Close()

			client := NewClient(Config{GatewayURL: srv.URL})

			// when
			resp, err := client.ListMessages(context.Background(), tt.sessionID)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), "list messages") {
					t.Errorf("error should contain 'list messages', got %q", err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp == nil {
				t.Fatal("expected response, got nil")
			}
			if len(resp.GetMessages()) != tt.wantCount {
				t.Errorf("expected %d messages, got %d", tt.wantCount, len(resp.GetMessages()))
			}
			if tt.wantCount > 0 {
				first := resp.GetMessages()[0]
				if first.GetMessageId() != "msg-1" {
					t.Errorf("expected message_id %q, got %q", "msg-1", first.GetMessageId())
				}
				if got := firstTextPartContent(first); got != "hello" {
					t.Errorf("expected text part content %q, got %q", "hello", got)
				}
				if first.GetSender() != game.FrameSender_FRAME_SENDER_USER {
					t.Errorf("expected sender FRAME_SENDER_USER, got %v", first.GetSender())
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestClient_URLTrailingSlash
// ---------------------------------------------------------------------------

func TestClient_URLTrailingSlash(t *testing.T) {
	// given: config with trailing slash in GatewayURL
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "//") {
			t.Errorf("URL path contains double slash: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"name":"sessions/ts","sessionId":"ts","createTime":"2024-01-01T00:00:00Z"}`))
	}))
	defer srv.Close()

	client := NewClient(Config{GatewayURL: srv.URL + "/"})

	// when: make a request
	session, err := client.GetSession(context.Background(), "ts")

	// then: no double slash, request succeeds
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.GetSessionId() != "ts" {
		t.Errorf("expected session_id %q, got %q", "ts", session.GetSessionId())
	}
}

// ---------------------------------------------------------------------------
// TestClient_EnvHeader
// ---------------------------------------------------------------------------

func TestClient_EnvHeader(t *testing.T) {
	tests := []struct {
		name    string
		env     string
		wantEnv string
	}{
		{
			name:    "env header set",
			env:     "production",
			wantEnv: "production",
		},
		{
			name:    "env header empty",
			env:     "",
			wantEnv: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got := r.Header.Get("env")
				if got != tt.wantEnv {
					t.Errorf("expected env header %q, got %q", tt.wantEnv, got)
				}
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"name":"sessions/e","sessionId":"e","createTime":"2024-01-01T00:00:00Z"}`))
			}))
			defer srv.Close()

			client := NewClient(Config{
				GatewayURL: srv.URL,
				Env:        tt.env,
			})

			// when
			_, err := client.GetSession(context.Background(), "e")

			// then
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestClient_CreateAgentProfile
// ---------------------------------------------------------------------------

func TestClient_CreateAgentProfile(t *testing.T) {
	tests := []struct {
		name       string
		req        *game.CreateAgentProfileRequest
		statusCode int
		respBody   string
		wantErr    bool
		wantName   string
	}{
		{
			name: "success",
			req: &game.CreateAgentProfileRequest{
				AgentProfileName: "my-agent",
				Model:            "gpt-4",
				SystemPrompt:     "You are a helpful assistant.",
			},
			statusCode: http.StatusOK,
			respBody:   `{"name":"agentProfiles/my-agent","model":"gpt-4","systemPrompt":"You are a helpful assistant.","skillNames":["skill1"],"mcpNames":["mcp1"],"enabled":true}`,
			wantErr:    false,
			wantName:   "agentProfiles/my-agent",
		},
		{
			name: "conflict",
			req: &game.CreateAgentProfileRequest{
				AgentProfileName: "existing",
			},
			statusCode: http.StatusConflict,
			respBody:   `{"error":"already exists"}`,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("expected POST, got %s", r.Method)
				}
				if r.URL.Path != "/api/v1/prompts/agentProfiles" {
					t.Errorf("expected /api/v1/prompts/agentProfiles, got %s", r.URL.Path)
				}
				if r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
				}

				body, _ := io.ReadAll(r.Body)
				req := new(game.CreateAgentProfileRequest)
				if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(body, req); err != nil {
					t.Fatalf("failed to parse request body: %v", err)
				}
				if req.GetAgentProfileName() != tt.req.GetAgentProfileName() {
					t.Errorf("expected agent_profile_name %q, got %q", tt.req.GetAgentProfileName(), req.GetAgentProfileName())
				}
				if req.GetModel() != tt.req.GetModel() {
					t.Errorf("expected model %q, got %q", tt.req.GetModel(), req.GetModel())
				}
				if req.GetSystemPrompt() != tt.req.GetSystemPrompt() {
					t.Errorf("expected system_prompt %q, got %q", tt.req.GetSystemPrompt(), req.GetSystemPrompt())
				}

				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.respBody))
			}))
			defer srv.Close()

			client := NewClient(Config{GatewayURL: srv.URL})

			// when
			profile, err := client.CreateAgentProfile(context.Background(), tt.req)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), "create agent profile") {
					t.Errorf("error should contain 'create agent profile', got %q", err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if profile == nil {
				t.Fatal("expected profile, got nil")
			}
			if profile.GetName() != tt.wantName {
				t.Errorf("expected name %q, got %q", tt.wantName, profile.GetName())
			}
			if profile.GetModel() != "gpt-4" {
				t.Errorf("expected model %q, got %q", "gpt-4", profile.GetModel())
			}
			if profile.GetSystemPrompt() != "You are a helpful assistant." {
				t.Errorf("expected system_prompt %q, got %q", "You are a helpful assistant.", profile.GetSystemPrompt())
			}
			if len(profile.GetSkillNames()) != 1 || profile.GetSkillNames()[0] != "skill1" {
				t.Errorf("expected skill_names [skill1], got %v", profile.GetSkillNames())
			}
			if len(profile.GetMcpNames()) != 1 || profile.GetMcpNames()[0] != "mcp1" {
				t.Errorf("expected mcp_names [mcp1], got %v", profile.GetMcpNames())
			}
			if !profile.GetEnabled() {
				t.Errorf("expected enabled true, got false")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestClient_GetAgentProfile
// ---------------------------------------------------------------------------

func TestClient_GetAgentProfile(t *testing.T) {
	tests := []struct {
		name             string
		agentProfileName string
		statusCode       int
		respBody         string
		wantErr          bool
	}{
		{
			name:             "success",
			agentProfileName: "my-agent",
			statusCode:       http.StatusOK,
			respBody:         `{"name":"agentProfiles/my-agent","model":"gpt-4","systemPrompt":"You are a helpful assistant.","enabled":true}`,
			wantErr:          false,
		},
		{
			name:             "not found",
			agentProfileName: "nonexistent",
			statusCode:       http.StatusNotFound,
			respBody:         `{"error":"not found"}`,
			wantErr:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("expected GET, got %s", r.Method)
				}
				wantPath := "/api/v1/prompts/agentProfiles/" + tt.agentProfileName
				if r.URL.Path != wantPath {
					t.Errorf("expected %s, got %s", wantPath, r.URL.Path)
				}
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.respBody))
			}))
			defer srv.Close()

			client := NewClient(Config{GatewayURL: srv.URL})

			// when
			profile, err := client.GetAgentProfile(context.Background(), tt.agentProfileName)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), "get agent profile") {
					t.Errorf("error should contain 'get agent profile', got %q", err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if profile == nil {
				t.Fatal("expected profile, got nil")
			}
			if profile.GetName() != "agentProfiles/"+tt.agentProfileName {
				t.Errorf("expected name %q, got %q", "agentProfiles/"+tt.agentProfileName, profile.GetName())
			}
			if profile.GetModel() != "gpt-4" {
				t.Errorf("expected model %q, got %q", "gpt-4", profile.GetModel())
			}
			if profile.GetSystemPrompt() != "You are a helpful assistant." {
				t.Errorf("expected system_prompt %q, got %q", "You are a helpful assistant.", profile.GetSystemPrompt())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestClient_DeleteAgentProfile
// ---------------------------------------------------------------------------

func TestClient_DeleteAgentProfile(t *testing.T) {
	tests := []struct {
		name             string
		agentProfileName string
		statusCode       int
		respBody         string
		wantErr          bool
	}{
		{
			name:             "success",
			agentProfileName: "del-me",
			statusCode:       http.StatusOK,
			wantErr:          false,
		},
		{
			name:             "not found",
			agentProfileName: "nonexistent",
			statusCode:       http.StatusNotFound,
			respBody:         `{"error":"not found"}`,
			wantErr:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodDelete {
					t.Errorf("expected DELETE, got %s", r.Method)
				}
				wantPath := "/api/v1/prompts/agentProfiles/" + tt.agentProfileName
				if r.URL.Path != wantPath {
					t.Errorf("expected %s, got %s", wantPath, r.URL.Path)
				}
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.respBody))
			}))
			defer srv.Close()

			client := NewClient(Config{GatewayURL: srv.URL})

			// when
			err := client.DeleteAgentProfile(context.Background(), tt.agentProfileName)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), "delete agent profile") {
					t.Errorf("error should contain 'delete agent profile', got %q", err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// firstTextPartContent returns the content string of the first TextPart in a
// Message's PartBlock, or "" when absent. Used by ListMessages tests to
// assert history content projected through the Part model.
func firstTextPartContent(m *game.Message) string {
	if m == nil || m.GetContent() == nil {
		return ""
	}
	for _, part := range m.GetContent().GetParts() {
		if tp := part.GetText(); tp != nil {
			return tp.GetContent()
		}
	}
	return ""
}
