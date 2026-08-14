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
		template   string
		statusCode int
		respBody   string
		wantErr    bool
	}{
		{
			name:       "success",
			template:   "saolei",
			statusCode: http.StatusOK,
			respBody:   `{"name":"templates/saolei/sessions/test-session","createTime":"2024-01-01T00:00:00Z"}`,
			wantErr:    false,
		},
		{
			name:       "server error",
			template:   "saolei",
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
				wantPath := "/api/v1/templates/" + tt.template + "/sessions"
				if r.URL.Path != wantPath {
					t.Errorf("expected %s, got %s", wantPath, r.URL.Path)
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
			session, err := client.CreateSession(context.Background(), tt.template)

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
			if session.GetName() != "templates/saolei/sessions/test-session" {
				t.Errorf("expected name %q, got %q", "templates/saolei/sessions/test-session", session.GetName())
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
		if r.URL.Path != "/api/v1/templates/saolei/sessions" {
			t.Errorf("expected /api/v1/templates/saolei/sessions, got %s", r.URL.Path)
		}

		// return a session with server-generated ID
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"name":"templates/saolei/sessions/server-gen-abc","createTime":"2024-06-01T12:00:00Z"}`))
	}))
	defer srv.Close()

	client := NewClient(Config{GatewayURL: srv.URL})

	// when: call CreateSession (no session_id argument)
	session, err := client.CreateSession(context.Background(), "saolei")

	// then: verify SessionId is extracted from response
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.GetName() != "templates/saolei/sessions/server-gen-abc" {
		t.Errorf("expected name %q, got %q", "templates/saolei/sessions/server-gen-abc", session.GetName())
	}
}

// ---------------------------------------------------------------------------
// TestClient_ListSessions
// ---------------------------------------------------------------------------

func TestClient_ListSessions(t *testing.T) {
	tests := []struct {
		name       string
		template   string
		pageSize   int32
		pageToken  string
		statusCode int
		respBody   string
		wantErr    bool
		wantErrMsg string
	}{
		{
			name:       "success with page_size and page_token",
			template:   "saolei",
			pageSize:   10,
			pageToken:  "token1",
			statusCode: http.StatusOK,
			respBody:   `{"sessions":[{"name":"templates/saolei/sessions/s1","createTime":"2024-01-01T00:00:00Z"},{"name":"templates/saolei/sessions/s2","createTime":"2024-01-02T00:00:00Z"}],"nextPageToken":"next"}`,
			wantErr:    false,
		},
		{
			name:       "success with page_size only",
			template:   "saolei",
			pageSize:   5,
			statusCode: http.StatusOK,
			respBody:   `{"sessions":[],"nextPageToken":""}`,
			wantErr:    false,
		},
		{
			name:       "success with no parameters",
			template:   "saolei",
			statusCode: http.StatusOK,
			respBody:   `{"sessions":[{"name":"templates/saolei/sessions/s1","createTime":"2024-01-01T00:00:00Z"}]}`,
			wantErr:    false,
		},
		{
			name:       "server error",
			template:   "saolei",
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
				wantPath := "/api/v1/templates/" + tt.template + "/sessions"
				if r.URL.Path != wantPath {
					t.Errorf("expected %s, got %s", wantPath, r.URL.Path)
				}

				if tt.pageSize > 0 {
					if got := r.URL.Query().Get("page_size"); got != fmtInt32(tt.pageSize) {
						t.Errorf("expected page_size %d, got %s", tt.pageSize, got)
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
			resp, err := client.ListSessions(context.Background(), tt.template, tt.pageSize, tt.pageToken)

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
				if resp.GetSessions()[0].GetName() != "templates/saolei/sessions/s1" {
					t.Errorf("expected first name %q, got %q", "templates/saolei/sessions/s1", resp.GetSessions()[0].GetName())
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
		template   string
		sessionID  string
		statusCode int
		respBody   string
		wantErr    bool
	}{
		{
			name:       "success",
			template:   "saolei",
			sessionID:  "test123",
			statusCode: http.StatusOK,
			respBody:   `{"name":"templates/saolei/sessions/test123","createTime":"2024-01-01T00:00:00Z"}`,
			wantErr:    false,
		},
		{
			name:       "not found",
			template:   "saolei",
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
				wantPath := "/api/v1/templates/" + tt.template + "/sessions/" + tt.sessionID
				if r.URL.Path != wantPath {
					t.Errorf("expected %s, got %s", wantPath, r.URL.Path)
				}
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.respBody))
			}))
			defer srv.Close()

			client := NewClient(Config{GatewayURL: srv.URL})

			// when
			session, err := client.GetSession(context.Background(), tt.template, tt.sessionID)

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
			if session.GetName() != "templates/saolei/sessions/"+tt.sessionID {
				t.Errorf("expected name %q, got %q", "templates/saolei/sessions/"+tt.sessionID, session.GetName())
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
				wantPath := "/api/v1/templates/saolei/sessions/" + tt.sessionID
				if r.URL.Path != wantPath {
					t.Errorf("expected %s, got %s", wantPath, r.URL.Path)
				}
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.respBody))
			}))
			defer srv.Close()

			client := NewClient(Config{GatewayURL: srv.URL})

			// when
			err := client.DeleteSession(context.Background(), "saolei", tt.sessionID)

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
// TestClient_GetTeam
// ---------------------------------------------------------------------------

func TestClient_GetTeam(t *testing.T) {
	tests := []struct {
		name       string
		template   string
		sessionID  string
		statusCode int
		respBody   string
		wantErr    bool
	}{
		{
			name:       "success",
			template:   "saolei",
			sessionID:  "sess-1",
			statusCode: http.StatusOK,
			respBody:   `{"name":"templates/saolei/sessions/sess-1/team","agents":[{"name":"player","acceptsUserInput":true},{"name":"planner","acceptsUserInput":false}],"createTime":"2024-01-01T00:00:00Z"}`,
			wantErr:    false,
		},
		{
			name:       "not found (team not created)",
			template:   "saolei",
			sessionID:  "no-team",
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
				wantPath := "/api/v1/templates/" + tt.template + "/sessions/" + tt.sessionID + "/team"
				if r.URL.Path != wantPath {
					t.Errorf("expected %s, got %s", wantPath, r.URL.Path)
				}
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.respBody))
			}))
			defer srv.Close()

			client := NewClient(Config{GatewayURL: srv.URL})

			// when
			team, err := client.GetTeam(context.Background(), tt.template, tt.sessionID)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), "get team") {
					t.Errorf("error should contain 'get team', got %q", err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if team == nil {
				t.Fatal("expected team, got nil")
			}
			if team.GetName() != "templates/saolei/sessions/sess-1/team" {
				t.Errorf("expected name %q, got %q", "templates/saolei/sessions/sess-1/team", team.GetName())
			}
			if len(team.GetAgents()) != 2 {
				t.Fatalf("expected 2 agents, got %d", len(team.GetAgents()))
			}
			if team.GetAgents()[0].GetName() != "player" || !team.GetAgents()[0].GetAcceptsUserInput() {
				t.Errorf("expected first agent player/acceptsUserInput=true, got %+v", team.GetAgents()[0])
			}
			if team.GetAgents()[1].GetName() != "planner" || team.GetAgents()[1].GetAcceptsUserInput() {
				t.Errorf("expected second agent planner/acceptsUserInput=false, got %+v", team.GetAgents()[1])
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestClient_UpdateTeam
// ---------------------------------------------------------------------------

func TestClient_UpdateTeam(t *testing.T) {
	tests := []struct {
		name            string
		template        string
		sessionID       string
		profile         string
		updateMaskPaths []string
		allowMissing    bool
		statusCode      int
		respBody        string
		wantErr         bool
	}{
		{
			name:            "success materialize with allow_missing",
			template:        "saolei",
			sessionID:       "sess-1",
			profile:         "templates/saolei/profiles/p1",
			updateMaskPaths: []string{"profile"},
			allowMissing:    true,
			statusCode:      http.StatusOK,
			respBody:        `{"name":"templates/saolei/sessions/sess-1/team","profile":"templates/saolei/profiles/p1","agents":[{"name":"player","acceptsUserInput":true}],"createTime":"2024-01-01T00:00:00Z"}`,
			wantErr:         false,
		},
		{
			name:         "not found without allow_missing",
			template:     "saolei",
			sessionID:    "sess-2",
			profile:      "templates/saolei/profiles/p2",
			allowMissing: false,
			statusCode:   http.StatusNotFound,
			respBody:     `{"error":"not found"}`,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPatch {
					t.Errorf("expected PATCH, got %s", r.Method)
				}
				wantPath := "/api/v1/templates/" + tt.template + "/sessions/" + tt.sessionID + "/team"
				if r.URL.Path != wantPath {
					t.Errorf("expected %s, got %s", wantPath, r.URL.Path)
				}
				if tt.allowMissing {
					if got := r.URL.Query().Get("allow_missing"); got != "true" {
						t.Errorf("expected allow_missing true, got %q", got)
					}
				} else if got := r.URL.Query().Get("allow_missing"); got != "" {
					t.Errorf("expected no allow_missing query when false, got %q", got)
				}
				if got := r.URL.Query().Get("update_mask"); got != strings.Join(tt.updateMaskPaths, ",") {
					t.Errorf("expected update_mask %q, got %q", strings.Join(tt.updateMaskPaths, ","), got)
				}
				body, _ := io.ReadAll(r.Body)
				req := new(game.Team)
				if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(body, req); err != nil {
					t.Fatalf("failed to parse request body: %v", err)
				}
				wantName := "templates/" + tt.template + "/sessions/" + tt.sessionID + "/team"
				if req.GetName() != wantName {
					t.Errorf("expected name %q, got %q", wantName, req.GetName())
				}
				if req.GetProfile() != tt.profile {
					t.Errorf("expected profile %q, got %q", tt.profile, req.GetProfile())
				}
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.respBody))
			}))
			defer srv.Close()

			client := NewClient(Config{GatewayURL: srv.URL})

			// when
			team, err := client.UpdateTeam(context.Background(), tt.template, tt.sessionID, tt.profile, tt.updateMaskPaths, tt.allowMissing)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), "update team") {
					t.Errorf("error should contain 'update team', got %q", err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if team == nil {
				t.Fatal("expected team, got nil")
			}
			if team.GetName() != "templates/saolei/sessions/sess-1/team" {
				t.Errorf("expected name %q, got %q", "templates/saolei/sessions/sess-1/team", team.GetName())
			}
			if team.GetProfile() != tt.profile {
				t.Errorf("expected profile %q, got %q", tt.profile, team.GetProfile())
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
		agent      string
		statusCode int
		respBody   string
		wantErr    bool
		wantCount  int
	}{
		{
			name:       "success with messages",
			sessionID:  "test-session",
			agent:      "player",
			statusCode: http.StatusOK,
			respBody:   `{"messages":[{"name":"templates/saolei/sessions/test-session/team/agents/player/messages/msg-1","messageId":"msg-1","role":"MESSAGE_ROLE_USER","agent":"player","content":{"parts":[{"text":{"content":"hello"}}]},"createTime":"2024-01-01T00:00:00Z"},{"name":"templates/saolei/sessions/test-session/team/agents/player/messages/msg-2","messageId":"msg-2","role":"MESSAGE_ROLE_AGENT","agent":"player","content":{"parts":[{"text":{"content":"hi there"}}]},"createTime":"2024-01-01T00:00:01Z"}]}`,
			wantErr:    false,
			wantCount:  2,
		},
		{
			name:       "success with empty list",
			sessionID:  "empty-session",
			agent:      "planner",
			statusCode: http.StatusOK,
			respBody:   `{"messages":[]}`,
			wantErr:    false,
			wantCount:  0,
		},
		{
			name:       "server error",
			sessionID:  "bad-session",
			agent:      "player",
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
				wantPath := "/api/v1/templates/saolei/sessions/" + tt.sessionID + "/team/agents/" + tt.agent + "/messages"
				if r.URL.Path != wantPath {
					t.Errorf("expected %s, got %s", wantPath, r.URL.Path)
				}
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.respBody))
			}))
			defer srv.Close()

			client := NewClient(Config{GatewayURL: srv.URL})

			// when
			resp, err := client.ListMessages(context.Background(), "saolei", tt.sessionID, tt.agent)

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
				if first.GetAgent() != tt.agent {
					t.Errorf("expected agent %q, got %q", tt.agent, first.GetAgent())
				}
				if got := firstTextPartContent(first); got != "hello" {
					t.Errorf("expected text part content %q, got %q", "hello", got)
				}
				if first.GetRole() != game.MessageRole_MESSAGE_ROLE_USER {
					t.Errorf("expected role MESSAGE_ROLE_USER, got %v", first.GetRole())
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestClient_RefreshTeam
// ---------------------------------------------------------------------------

func TestClient_RefreshTeam(t *testing.T) {
	tests := []struct {
		name       string
		sessionID  string
		statusCode int
		respBody   string
		wantErr    bool
	}{
		{
			name:       "success",
			sessionID:  "ref-me",
			statusCode: http.StatusOK,
			wantErr:    false,
		},
		{
			name:       "not found",
			sessionID:  "no-team",
			statusCode: http.StatusNotFound,
			respBody:   `{"error":"not found"}`,
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
				wantPath := "/api/v1/templates/saolei/sessions/" + tt.sessionID + "/team:refresh"
				if r.URL.Path != wantPath {
					t.Errorf("expected %s, got %s", wantPath, r.URL.Path)
				}
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.respBody))
			}))
			defer srv.Close()

			client := NewClient(Config{GatewayURL: srv.URL})

			// when
			err := client.RefreshTeam(context.Background(), "saolei", tt.sessionID)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), "refresh team") {
					t.Errorf("error should contain 'refresh team', got %q", err.Error())
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
// TestClient_URLTrailingSlash
// ---------------------------------------------------------------------------

func TestClient_URLTrailingSlash(t *testing.T) {
	// given: config with trailing slash in GatewayURL
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "//") {
			t.Errorf("URL path contains double slash: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"name":"templates/saolei/sessions/ts","createTime":"2024-01-01T00:00:00Z"}`))
	}))
	defer srv.Close()

	client := NewClient(Config{GatewayURL: srv.URL + "/"})

	// when: make a request
	session, err := client.GetSession(context.Background(), "saolei", "ts")

	// then: no double slash, request succeeds
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.GetName() != "templates/saolei/sessions/ts" {
		t.Errorf("expected name %q, got %q", "templates/saolei/sessions/ts", session.GetName())
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
				w.Write([]byte(`{"name":"templates/saolei/sessions/e","createTime":"2024-01-01T00:00:00Z"}`))
			}))
			defer srv.Close()

			client := NewClient(Config{
				GatewayURL: srv.URL,
				Env:        tt.env,
			})

			// when
			_, err := client.GetSession(context.Background(), "saolei", "e")

			// then
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestClient_CreateTeamProfile
// ---------------------------------------------------------------------------

func TestClient_CreateTeamProfile(t *testing.T) {
	tests := []struct {
		name       string
		template   string
		profileID  string
		profile    *game.TeamProfile
		statusCode int
		respBody   string
		wantErr    bool
	}{
		{
			name:      "success",
			template:  "saolei",
			profileID: "my-profile",
			profile: &game.TeamProfile{
				Spec: &game.TeamProfile_Saolei{
					Saolei: &game.SaoleiProfile{
						PlayerModel:   "openai/gpt-4o",
						PlannerModel:  "anthropic/claude-3-5-sonnet",
						PlayerPrompt:  "player base prompt",
						PlannerPrompt: "planner base prompt",
					},
				},
			},
			statusCode: http.StatusOK,
			respBody:   `{"name":"templates/saolei/profiles/my-profile","saolei":{"playerModel":"openai/gpt-4o","plannerModel":"anthropic/claude-3-5-sonnet","playerPrompt":"player base prompt","plannerPrompt":"planner base prompt"}}`,
			wantErr:    false,
		},
		{
			name:       "conflict",
			template:   "saolei",
			profileID:  "existing",
			profile:    &game.TeamProfile{},
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
				wantPath := "/api/v1/templates/" + tt.template + "/profiles"
				if r.URL.Path != wantPath {
					t.Errorf("expected %s, got %s", wantPath, r.URL.Path)
				}
				if r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
				}

				body, _ := io.ReadAll(r.Body)
				got := new(game.TeamProfile)
				if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(body, got); err != nil {
					t.Fatalf("failed to parse request body: %v", err)
				}
				gotID := r.URL.Query().Get("team_profile_id")
				if gotID != tt.profileID {
					t.Errorf("expected team_profile_id %q, got %q", tt.profileID, gotID)
				}
				if tt.profile.GetSaolei() != nil && got.GetSaolei() == nil {
					t.Errorf("expected saolei spec variant, got nil")
				}
				if tt.profile.GetSaolei() != nil && got.GetSaolei().GetPlayerModel() != tt.profile.GetSaolei().GetPlayerModel() {
					t.Errorf("expected player_model %q, got %+v", tt.profile.GetSaolei().GetPlayerModel(), got.GetSaolei())
				}
				if tt.profile.GetSaolei() != nil && got.GetSaolei().GetPlayerPrompt() != tt.profile.GetSaolei().GetPlayerPrompt() {
					t.Errorf("expected player_prompt %q, got %q", tt.profile.GetSaolei().GetPlayerPrompt(), got.GetSaolei().GetPlayerPrompt())
				}

				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.respBody))
			}))
			defer srv.Close()

			client := NewClient(Config{GatewayURL: srv.URL})

			// when
			profile, err := client.CreateTeamProfile(context.Background(), tt.template, tt.profileID, tt.profile)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), "create team profile") {
					t.Errorf("error should contain 'create team profile', got %q", err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if profile == nil {
				t.Fatal("expected profile, got nil")
			}
			if profile.GetName() != "templates/saolei/profiles/my-profile" {
				t.Errorf("expected name %q, got %q", "templates/saolei/profiles/my-profile", profile.GetName())
			}
			if profile.GetSaolei().GetPlayerModel() != "openai/gpt-4o" {
				t.Errorf("expected player_model %q, got %q", "openai/gpt-4o", profile.GetSaolei().GetPlayerModel())
			}
			if profile.GetSaolei().GetPlannerModel() != "anthropic/claude-3-5-sonnet" {
				t.Errorf("expected planner_model %q, got %q", "anthropic/claude-3-5-sonnet", profile.GetSaolei().GetPlannerModel())
			}
			if profile.GetSaolei().GetPlayerPrompt() != "player base prompt" {
				t.Errorf("expected player_prompt %q, got %q", "player base prompt", profile.GetSaolei().GetPlayerPrompt())
			}
			if profile.GetSaolei().GetPlannerPrompt() != "planner base prompt" {
				t.Errorf("expected planner_prompt %q, got %q", "planner base prompt", profile.GetSaolei().GetPlannerPrompt())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestClient_GetTeamProfile
// ---------------------------------------------------------------------------

func TestClient_GetTeamProfile(t *testing.T) {
	tests := []struct {
		name        string
		profileName string
		statusCode  int
		respBody    string
		wantErr     bool
	}{
		{
			name:        "success",
			profileName: "my-profile",
			statusCode:  http.StatusOK,
			respBody:    `{"name":"templates/saolei/profiles/my-profile","saolei":{"playerModel":"openai/gpt-4o","plannerModel":"anthropic/claude-3-5-sonnet"}}`,
			wantErr:     false,
		},
		{
			name:        "not found",
			profileName: "nonexistent",
			statusCode:  http.StatusNotFound,
			respBody:    `{"error":"not found"}`,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("expected GET, got %s", r.Method)
				}
				wantPath := "/api/v1/templates/saolei/profiles/" + tt.profileName
				if r.URL.Path != wantPath {
					t.Errorf("expected %s, got %s", wantPath, r.URL.Path)
				}
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.respBody))
			}))
			defer srv.Close()

			client := NewClient(Config{GatewayURL: srv.URL})

			// when
			profile, err := client.GetTeamProfile(context.Background(), "saolei", tt.profileName)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), "get team profile") {
					t.Errorf("error should contain 'get team profile', got %q", err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if profile == nil {
				t.Fatal("expected profile, got nil")
			}
			if profile.GetName() != "templates/saolei/profiles/my-profile" {
				t.Errorf("expected name %q, got %q", "templates/saolei/profiles/my-profile", profile.GetName())
			}
			if profile.GetSaolei().GetPlayerModel() != "openai/gpt-4o" {
				t.Errorf("expected player_model %q, got %q", "openai/gpt-4o", profile.GetSaolei().GetPlayerModel())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestClient_ListTeamProfiles
// ---------------------------------------------------------------------------

func TestClient_ListTeamProfiles(t *testing.T) {
	tests := []struct {
		name       string
		pageSize   int32
		pageToken  string
		statusCode int
		respBody   string
		wantErr    bool
		wantCount  int
	}{
		{
			name:       "success with pagination",
			pageSize:   10,
			pageToken:  "tok",
			statusCode: http.StatusOK,
			respBody:   `{"teamProfiles":[{"name":"templates/saolei/profiles/p1"},{"name":"templates/saolei/profiles/p2"}],"nextPageToken":"next"}`,
			wantErr:    false,
			wantCount:  2,
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
			// given
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("expected GET, got %s", r.Method)
				}
				if r.URL.Path != "/api/v1/templates/saolei/profiles" {
					t.Errorf("expected /api/v1/templates/saolei/profiles, got %s", r.URL.Path)
				}
				if tt.pageSize > 0 {
					if got := r.URL.Query().Get("page_size"); got != fmtInt32(tt.pageSize) {
						t.Errorf("expected page_size %d, got %s", tt.pageSize, got)
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
			resp, err := client.ListTeamProfiles(context.Background(), "saolei", tt.pageSize, tt.pageToken)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), "list team profiles") {
					t.Errorf("error should contain 'list team profiles', got %q", err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp == nil {
				t.Fatal("expected response, got nil")
			}
			if len(resp.GetTeamProfiles()) != tt.wantCount {
				t.Errorf("expected %d team profiles, got %d", tt.wantCount, len(resp.GetTeamProfiles()))
			}
			if tt.wantCount > 0 {
				if resp.GetTeamProfiles()[0].GetName() != "templates/saolei/profiles/p1" {
					t.Errorf("expected first name %q, got %q", "templates/saolei/profiles/p1", resp.GetTeamProfiles()[0].GetName())
				}
				if resp.GetNextPageToken() != "next" {
					t.Errorf("expected next_page_token %q, got %q", "next", resp.GetNextPageToken())
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestClient_UpdateTeamProfile
// ---------------------------------------------------------------------------

func TestClient_UpdateTeamProfile(t *testing.T) {
	tests := []struct {
		name            string
		profileName     string
		profile         *game.TeamProfile
		updateMaskPaths []string
		statusCode      int
		respBody        string
		wantErr         bool
	}{
		{
			name:        "success with oneof member mask",
			profileName: "my-profile",
			profile: &game.TeamProfile{
				Name: "templates/saolei/profiles/my-profile",
				Spec: &game.TeamProfile_Saolei{
					Saolei: &game.SaoleiProfile{
						PlayerModel:   "openai/gpt-5",
						PlayerPrompt:  "custom player base",
						PlannerPrompt: "custom planner base",
					},
				},
			},
			updateMaskPaths: []string{
				"saolei.player_model",
				"saolei.planner_model",
				"saolei.player_prompt",
				"saolei.planner_prompt",
			},
			statusCode: http.StatusOK,
			respBody:   `{"name":"templates/saolei/profiles/my-profile","saolei":{"playerModel":"openai/gpt-5","playerPrompt":"custom player base","plannerPrompt":"custom planner base"}}`,
			wantErr:    false,
		},
		{
			name:        "not found",
			profileName: "nonexistent",
			profile:     &game.TeamProfile{Name: "templates/saolei/profiles/nonexistent"},
			statusCode:  http.StatusNotFound,
			respBody:    `{"error":"not found"}`,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPatch {
					t.Errorf("expected PATCH, got %s", r.Method)
				}
				wantPath := "/api/v1/templates/saolei/profiles/" + tt.profileName
				if r.URL.Path != wantPath {
					t.Errorf("expected %s, got %s", wantPath, r.URL.Path)
				}
				if got := r.URL.Query().Get("update_mask"); got != strings.Join(tt.updateMaskPaths, ",") {
					t.Errorf("expected update_mask %q, got %q", strings.Join(tt.updateMaskPaths, ","), got)
				}
				body, _ := io.ReadAll(r.Body)
				got := new(game.TeamProfile)
				if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(body, got); err != nil {
					t.Fatalf("failed to parse patch body: %v", err)
				}
				if got.GetName() != tt.profile.GetName() {
					t.Errorf("expected body name %q, got %q", tt.profile.GetName(), got.GetName())
				}
				if tt.profile.GetSaolei() != nil && got.GetSaolei().GetPlayerModel() != tt.profile.GetSaolei().GetPlayerModel() {
					t.Errorf("expected player_model %q, got %q", tt.profile.GetSaolei().GetPlayerModel(), got.GetSaolei().GetPlayerModel())
				}
				if tt.profile.GetSaolei() != nil && got.GetSaolei().GetPlayerPrompt() != tt.profile.GetSaolei().GetPlayerPrompt() {
					t.Errorf("expected player_prompt %q, got %q", tt.profile.GetSaolei().GetPlayerPrompt(), got.GetSaolei().GetPlayerPrompt())
				}
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.respBody))
			}))
			defer srv.Close()

			client := NewClient(Config{GatewayURL: srv.URL})

			// when
			updated, err := client.UpdateTeamProfile(context.Background(), "saolei", tt.profileName, tt.profile, tt.updateMaskPaths)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), "update team profile") {
					t.Errorf("error should contain 'update team profile', got %q", err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if updated == nil {
				t.Fatal("expected updated profile, got nil")
			}
			if updated.GetSaolei().GetPlayerModel() != "openai/gpt-5" {
				t.Errorf("expected player_model %q, got %q", "openai/gpt-5", updated.GetSaolei().GetPlayerModel())
			}
			if updated.GetSaolei().GetPlayerPrompt() != "custom player base" {
				t.Errorf("expected player_prompt %q, got %q", "custom player base", updated.GetSaolei().GetPlayerPrompt())
			}
			if updated.GetSaolei().GetPlannerPrompt() != "custom planner base" {
				t.Errorf("expected planner_prompt %q, got %q", "custom planner base", updated.GetSaolei().GetPlannerPrompt())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestClient_DeleteTeamProfile
// ---------------------------------------------------------------------------

func TestClient_DeleteTeamProfile(t *testing.T) {
	tests := []struct {
		name        string
		profileName string
		statusCode  int
		respBody    string
		wantErr     bool
	}{
		{
			name:        "success",
			profileName: "del-me",
			statusCode:  http.StatusOK,
			wantErr:     false,
		},
		{
			name:        "not found",
			profileName: "nonexistent",
			statusCode:  http.StatusNotFound,
			respBody:    `{"error":"not found"}`,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodDelete {
					t.Errorf("expected DELETE, got %s", r.Method)
				}
				wantPath := "/api/v1/templates/saolei/profiles/" + tt.profileName
				if r.URL.Path != wantPath {
					t.Errorf("expected %s, got %s", wantPath, r.URL.Path)
				}
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.respBody))
			}))
			defer srv.Close()

			client := NewClient(Config{GatewayURL: srv.URL})

			// when
			err := client.DeleteTeamProfile(context.Background(), "saolei", tt.profileName)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), "delete team profile") {
					t.Errorf("error should contain 'delete team profile', got %q", err.Error())
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
