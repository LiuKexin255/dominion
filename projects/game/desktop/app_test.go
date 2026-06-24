package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dominion/projects/game"
	"dominion/projects/game/desktop/internal/api"
	"dominion/projects/game/desktop/internal/applog"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// mockWSServer creates an httptest server that upgrades to WebSocket and calls handler.
func mockWSServer(t *testing.T, handler func(*websocket.Conn)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			OriginPatterns: []string{"*"},
		})
		if err != nil {
			t.Logf("websocket accept failed: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		handler(conn)
	}))
}

// TestConnectAgent_ProbeSuccess verifies that ConnectAgent stores a.ws
// when the probe round-trip succeeds.
func TestConnectAgent_ProbeSuccess(t *testing.T) {
	// given: mock WS server that responds to the probe ping with any frame
	srv := mockWSServer(t, func(conn *websocket.Conn) {
		ctx := context.Background()
		// read the probe frame
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		frame := new(game.AgentFrame)
		if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(data, frame); err != nil {
			return
		}
		// respond with an ack frame (any response proves the round-trip)
		ackFrame := &game.AgentFrame{
			SessionId:  frame.GetSessionId(),
			FrameId:    "test-ack-frame",
			CreateTime: timestamppb.Now(),
			Payload: &game.AgentFrame_Ack{
				Ack: &game.AgentAckFrame{AckFrameId: frame.GetFrameId(), Message: "ok"},
			},
		}
		resp, _ := protojson.Marshal(ackFrame)
		conn.Write(ctx, websocket.MessageText, resp)
	})
	defer srv.Close()

	// when: ConnectAgent with a valid session ID
	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())
	app.cfg = api.Config{GatewayURL: srv.URL}

	err := app.ConnectAgent("test-session")

	// then: probe succeeds, ws and sessionID are stored
	if err != nil {
		t.Fatalf("ConnectAgent() unexpected error: %v", err)
	}
	if app.ws == nil {
		t.Fatal("expected app.ws to be non-nil after successful ConnectAgent")
	}
	if app.sessionID != "test-session" {
		t.Fatalf("expected sessionID %q, got %q", "test-session", app.sessionID)
	}

	// clean up
	app.CloseAgent()
}

// TestConnectAgent_ProbeFailure verifies that ConnectAgent closes WS
// and does NOT store state when the probe times out (no response).
func TestConnectAgent_ProbeFailure(t *testing.T) {
	// given: mock WS server that reads the probe but never responds
	srv := mockWSServer(t, func(conn *websocket.Conn) {
		ctx := context.Background()
		_, _, err := conn.Read(ctx)
		if err != nil {
			return
		}
		// Block forever — simulating an unreachable agent.
		select {}
	})
	defer srv.Close()

	// when: ConnectAgent with a valid session ID
	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())
	app.cfg = api.Config{GatewayURL: srv.URL}

	err := app.ConnectAgent("test-session")

	// then: probe fails, ws is nil, error returned
	if err == nil {
		t.Fatal("ConnectAgent() expected error, got nil")
	}
	if app.ws != nil {
		t.Fatal("expected app.ws to be nil after failed ConnectAgent")
	}
	if app.sessionID != "" {
		t.Fatalf("expected sessionID to be empty, got %q", app.sessionID)
	}

	// the error should mention probe or receive
	errMsg := err.Error()
	if !strings.Contains(errMsg, "probe") && !strings.Contains(errMsg, "receive") {
		t.Errorf("error message should mention probe/receive, got: %s", errMsg)
	}
}

// TestConnectAgent_EmptySessionID verifies empty sessionID returns error immediately.
func TestConnectAgent_EmptySessionID(t *testing.T) {
	// given: App with no connection
	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())

	// when: ConnectAgent with empty session ID
	err := app.ConnectAgent("")

	// then: immediate error, no ws state change
	if err == nil {
		t.Fatal("ConnectAgent() expected error for empty session_id, got nil")
	}
	if app.ws != nil {
		t.Fatal("expected app.ws to be nil")
	}
	if !strings.Contains(err.Error(), "session_id") {
		t.Errorf("error should mention session_id, got: %s", err.Error())
	}
}

// TestConnectAgent_ProbeTimeout verifies 10-second timeout triggers properly.
func TestConnectAgent_ProbeTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timeout test in short mode")
	}
	// given: mock WS server that never responds
	srv := mockWSServer(t, func(conn *websocket.Conn) {
		ctx := context.Background()
		_, _, err := conn.Read(ctx)
		if err != nil {
			return
		}
		select {} // block forever
	})
	defer srv.Close()

	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())
	app.cfg = api.Config{GatewayURL: srv.URL}

	// when: ConnectAgent should time out after 10 seconds
	start := time.Now()
	err := app.ConnectAgent("test-session")
	elapsed := time.Since(start)

	// then: probe times out
	if err == nil {
		t.Fatal("ConnectAgent() expected error from timeout, got nil")
	}
	if app.ws != nil {
		t.Fatal("expected app.ws to be nil after timeout")
	}
	// Timeout should be around 10 seconds (allow some tolerance)
	if elapsed > 15*time.Second {
		t.Errorf("timeout took too long: %v, expected ~10s", elapsed)
	}
	t.Logf("timeout occurred after: %v", elapsed)
}

// TestCreateAgentProfile_Success verifies CreateAgentProfile delegates to client
// and returns the converted view model.
func TestCreateAgentProfile_Success(t *testing.T) {
	// given: mock server that responds to POST /api/v1/prompts/agentProfiles
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/prompts/agentProfiles" {
			t.Errorf("expected /api/v1/prompts/agentProfiles, got %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		req := new(game.CreateAgentProfileRequest)
		if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(body, req); err != nil {
			t.Fatalf("failed to parse request body: %v", err)
		}
		if req.GetAgentProfileName() != "test-agent" {
			t.Errorf("expected agent_profile_name %q, got %q", "test-agent", req.GetAgentProfileName())
		}
		if req.GetModel() != "gpt-4" {
			t.Errorf("expected model %q, got %q", "gpt-4", req.GetModel())
		}
		if req.GetSystemPrompt() != "You are a test assistant." {
			t.Errorf("expected system_prompt %q, got %q", "You are a test assistant.", req.GetSystemPrompt())
		}
		if req.GetEnabled() != true {
			t.Errorf("expected enabled true, got %v", req.GetEnabled())
		}
		if got := req.GetToolNames(); len(got) != 1 || got[0] != "mouse" {
			t.Errorf("expected tool_names [\"mouse\"], got %v", got)
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"name":"agentProfiles/test-agent","agentProfileName":"test-agent","model":"gpt-4","systemPrompt":"You are a test assistant.","enabled":true,"toolNames":["mouse"]}`)
	}))
	defer srv.Close()

	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())
	app.client = api.NewClient(api.Config{GatewayURL: srv.URL})

	// when
	view, err := app.CreateAgentProfile(CreateAgentProfileView{
		AgentProfileName: "test-agent",
		Model:            "gpt-4",
		SystemPrompt:     "You are a test assistant.",
		Enabled:          true,
		ToolNames:        []string{"mouse"},
	})

	// then
	if err != nil {
		t.Fatalf("CreateAgentProfile() unexpected error: %v", err)
	}
	if view == nil {
		t.Fatal("CreateAgentProfile() returned nil view")
	}
	if view.AgentProfileName != "test-agent" {
		t.Errorf("expected AgentProfileName %q, got %q", "test-agent", view.AgentProfileName)
	}
	if view.Model != "gpt-4" {
		t.Errorf("expected Model %q, got %q", "gpt-4", view.Model)
	}
	if view.SystemPrompt != "You are a test assistant." {
		t.Errorf("expected SystemPrompt %q, got %q", "You are a test assistant.", view.SystemPrompt)
	}
	if view.Enabled != true {
		t.Errorf("expected Enabled true, got %v", view.Enabled)
	}
	if len(view.ToolNames) != 1 || view.ToolNames[0] != "mouse" {
		t.Errorf("expected ToolNames [\"mouse\"], got %v", view.ToolNames)
	}
}

// TestCreateAgentProfile_Error verifies CreateAgentProfile propagates client error.
func TestCreateAgentProfile_Error(t *testing.T) {
	// given: mock server returning 409 Conflict
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		fmt.Fprint(w, `{"error":"already exists"}`)
	}))
	defer srv.Close()

	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())
	app.client = api.NewClient(api.Config{GatewayURL: srv.URL})

	// when
	view, err := app.CreateAgentProfile(CreateAgentProfileView{
		AgentProfileName: "test-agent",
	})

	// then
	if err == nil {
		t.Fatal("CreateAgentProfile() expected error, got nil")
	}
	if view != nil {
		t.Fatal("CreateAgentProfile() expected nil view on error")
	}
	if !strings.Contains(err.Error(), "create agent profile") {
		t.Errorf("error should contain 'create agent profile', got %q", err.Error())
	}
}

// TestGetAgentProfile_Success verifies GetAgentProfile delegates to client and returns view.
func TestGetAgentProfile_Success(t *testing.T) {
	// given: mock server responding to GET /api/v1/prompts/agentProfiles/test-agent
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		wantPath := "/api/v1/prompts/agentProfiles/test-agent"
		if r.URL.Path != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"name":"agentProfiles/test-agent","agentProfileName":"test-agent","model":"gpt-4","systemPrompt":"You are a test assistant.","enabled":true,"toolNames":["mouse"]}`)
	}))
	defer srv.Close()

	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())
	app.client = api.NewClient(api.Config{GatewayURL: srv.URL})

	// when
	view, err := app.GetAgentProfile("test-agent")

	// then
	if err != nil {
		t.Fatalf("GetAgentProfile() unexpected error: %v", err)
	}
	if view == nil {
		t.Fatal("GetAgentProfile() returned nil view")
	}
	if view.AgentProfileName != "test-agent" {
		t.Errorf("expected AgentProfileName %q, got %q", "test-agent", view.AgentProfileName)
	}
	if view.Model != "gpt-4" {
		t.Errorf("expected Model %q, got %q", "gpt-4", view.Model)
	}
	if len(view.ToolNames) != 1 || view.ToolNames[0] != "mouse" {
		t.Errorf("expected ToolNames [\"mouse\"], got %v", view.ToolNames)
	}
}

// TestGetAgentProfile_NotFound verifies GetAgentProfile propagates not found error.
func TestGetAgentProfile_NotFound(t *testing.T) {
	// given: mock server returning 404
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":"not found"}`)
	}))
	defer srv.Close()

	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())
	app.client = api.NewClient(api.Config{GatewayURL: srv.URL})

	// when
	view, err := app.GetAgentProfile("nonexistent")

	// then
	if err == nil {
		t.Fatal("GetAgentProfile() expected error, got nil")
	}
	if view != nil {
		t.Fatal("GetAgentProfile() expected nil view on error")
	}
	if !strings.Contains(err.Error(), "get agent profile") {
		t.Errorf("error should contain 'get agent profile', got %q", err.Error())
	}
}

// TestCreateAgentProfile_ToolNamesRoundTrip verifies that ToolNames supplied at
// create time survive a create-then-get round trip through the view layer.
func TestCreateAgentProfile_ToolNamesRoundTrip(t *testing.T) {
	// given: mock server handling both POST (create) and GET (get)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			req := new(game.CreateAgentProfileRequest)
			if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(body, req); err != nil {
				t.Fatalf("failed to parse create body: %v", err)
			}
			if got := req.GetToolNames(); len(got) != 1 || got[0] != "mouse" {
				t.Errorf("POST expected tool_names [\"mouse\"], got %v", got)
			}
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"name":"agentProfiles/tool-test","agentProfileName":"tool-test","enabled":true,"toolNames":["mouse"]}`)
		case http.MethodGet:
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"name":"agentProfiles/tool-test","agentProfileName":"tool-test","enabled":true,"toolNames":["mouse"]}`)
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	}))
	defer srv.Close()

	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())
	app.client = api.NewClient(api.Config{GatewayURL: srv.URL})

	// when: create then get the profile
	created, err := app.CreateAgentProfile(CreateAgentProfileView{
		AgentProfileName: "tool-test",
		Enabled:          true,
		ToolNames:        []string{"mouse"},
	})
	if err != nil {
		t.Fatalf("CreateAgentProfile() unexpected error: %v", err)
	}
	got, err := app.GetAgentProfile("tool-test")
	if err != nil {
		t.Fatalf("GetAgentProfile() unexpected error: %v", err)
	}

	// then: both views carry ToolNames ["mouse"]
	if len(created.ToolNames) != 1 || created.ToolNames[0] != "mouse" {
		t.Errorf("created: expected ToolNames [\"mouse\"], got %v", created.ToolNames)
	}
	if len(got.ToolNames) != 1 || got.ToolNames[0] != "mouse" {
		t.Errorf("got: expected ToolNames [\"mouse\"], got %v", got.ToolNames)
	}
}

// TestUpdateAgentProfile_ToolNames verifies that an update with the tool_names
// mask round-trips ToolNames through the view layer.
func TestUpdateAgentProfile_ToolNames(t *testing.T) {
	// given: mock server handling both PATCH (update) and GET (get)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPatch:
			if got := r.URL.Query().Get("update_mask"); got != "tool_names" {
				t.Errorf("PATCH expected update_mask=tool_names, got %q", got)
			}
			body, _ := io.ReadAll(r.Body)
			profile := new(game.AgentProfile)
			if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(body, profile); err != nil {
				t.Fatalf("failed to parse patch body: %v", err)
			}
			if got := profile.GetToolNames(); len(got) != 1 || got[0] != "keyboard" {
				t.Errorf("PATCH expected tool_names [\"keyboard\"], got %v", got)
			}
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"name":"agentProfiles/tool-test","agentProfileName":"tool-test","enabled":true,"toolNames":["keyboard"]}`)
		case http.MethodGet:
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"name":"agentProfiles/tool-test","agentProfileName":"tool-test","enabled":true,"toolNames":["keyboard"]}`)
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	}))
	defer srv.Close()

	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())
	app.client = api.NewClient(api.Config{GatewayURL: srv.URL})

	// when: update with the tool_names mask then get
	updated, err := app.UpdateAgentProfile("tool-test", AgentProfileView{
		AgentProfileName: "tool-test",
		Enabled:          true,
		ToolNames:        []string{"keyboard"},
	}, []string{"tool_names"})
	if err != nil {
		t.Fatalf("UpdateAgentProfile() unexpected error: %v", err)
	}
	got, err := app.GetAgentProfile("tool-test")
	if err != nil {
		t.Fatalf("GetAgentProfile() unexpected error: %v", err)
	}

	// then: both views carry ToolNames ["keyboard"]
	if len(updated.ToolNames) != 1 || updated.ToolNames[0] != "keyboard" {
		t.Errorf("updated: expected ToolNames [\"keyboard\"], got %v", updated.ToolNames)
	}
	if len(got.ToolNames) != 1 || got.ToolNames[0] != "keyboard" {
		t.Errorf("got: expected ToolNames [\"keyboard\"], got %v", got.ToolNames)
	}
}

// TestDeleteAgentProfile_Success verifies DeleteAgentProfile returns nil on success.
func TestDeleteAgentProfile_Success(t *testing.T) {
	// given: mock server responding to DELETE /api/v1/prompts/agentProfiles/del-me
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		wantPath := "/api/v1/prompts/agentProfiles/del-me"
		if r.URL.Path != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())
	app.client = api.NewClient(api.Config{GatewayURL: srv.URL})

	// when
	err := app.DeleteAgentProfile("del-me")

	// then
	if err != nil {
		t.Fatalf("DeleteAgentProfile() unexpected error: %v", err)
	}
}

// TestDeleteAgentProfile_NotFound verifies DeleteAgentProfile propagates 404 error.
func TestDeleteAgentProfile_NotFound(t *testing.T) {
	// given: mock server returning 404
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":"not found"}`)
	}))
	defer srv.Close()

	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())
	app.client = api.NewClient(api.Config{GatewayURL: srv.URL})

	// when
	err := app.DeleteAgentProfile("nonexistent")

	// then
	if err == nil {
		t.Fatal("DeleteAgentProfile() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "delete agent profile") {
		t.Errorf("error should contain 'delete agent profile', got %q", err.Error())
	}
}

// TestListMessages_Success verifies ListMessages delegates to client and
// converts proto messages to MessageViewModels.
func TestListMessages_Success(t *testing.T) {
	// given: mock server responding to GET /api/v1/sessions/test-session/messages
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		wantPath := "/api/v1/sessions/test-session/messages"
		if r.URL.Path != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"messages":[{"name":"sessions/test-session/messages/msg-1","messageId":"msg-1","sender":"FRAME_SENDER_USER","type":"text","content":"text","text":"hello","createTime":"2024-01-01T00:00:00Z"},{"name":"sessions/test-session/messages/msg-2","messageId":"msg-2","sender":"FRAME_SENDER_AGENT","type":"thinking","content":"text","text":"pondering","createTime":"2024-01-01T00:00:01Z"}]}`)
	}))
	defer srv.Close()

	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())
	app.client = api.NewClient(api.Config{GatewayURL: srv.URL})

	// when
	views, err := app.ListMessages("test-session")

	// then
	if err != nil {
		t.Fatalf("ListMessages() unexpected error: %v", err)
	}
	if views == nil {
		t.Fatal("ListMessages() returned nil views")
	}
	if len(views) != 2 {
		t.Fatalf("expected 2 view models, got %d", len(views))
	}
	if views[0].MessageID != "msg-1" {
		t.Errorf("expected first MessageID %q, got %q", "msg-1", views[0].MessageID)
	}
	if views[0].Content != "hello" {
		t.Errorf("expected first Content %q, got %q", "hello", views[0].Content)
	}
	if views[0].Sender != "FRAME_SENDER_USER" {
		t.Errorf("expected first Sender %q, got %q", "FRAME_SENDER_USER", views[0].Sender)
	}
	if views[0].Type != "text" {
		t.Errorf("expected first Type %q, got %q", "text", views[0].Type)
	}
	if views[1].MessageID != "msg-2" {
		t.Errorf("expected second MessageID %q, got %q", "msg-2", views[1].MessageID)
	}
	if views[1].Type != "thinking" {
		t.Errorf("expected second Type %q, got %q", "thinking", views[1].Type)
	}
}

// TestListMessages_Empty verifies ListMessages returns no view models for empty list.
func TestListMessages_Empty(t *testing.T) {
	// given: mock server returning empty messages list
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"messages":[]}`)
	}))
	defer srv.Close()

	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())
	app.client = api.NewClient(api.Config{GatewayURL: srv.URL})

	// when
	views, err := app.ListMessages("empty-session")

	// then
	if err != nil {
		t.Fatalf("ListMessages() unexpected error: %v", err)
	}
	if len(views) != 0 {
		t.Errorf("expected 0 view models, got %d", len(views))
	}
}

// TestListMessages_EmptySessionID verifies empty sessionID returns error immediately.
func TestListMessages_EmptySessionID(t *testing.T) {
	// given: App with no client needed
	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())

	// when
	views, err := app.ListMessages("")

	// then
	if err == nil {
		t.Fatal("ListMessages() expected error for empty session_id, got nil")
	}
	if views != nil {
		t.Fatal("ListMessages() expected nil views on error")
	}
	if !strings.Contains(err.Error(), "session_id") {
		t.Errorf("error should mention session_id, got: %s", err.Error())
	}
}

// TestListMessages_Error verifies ListMessages propagates client error.
func TestListMessages_Error(t *testing.T) {
	// given: mock server returning 500
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "internal error")
	}))
	defer srv.Close()

	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())
	app.client = api.NewClient(api.Config{GatewayURL: srv.URL})

	// when
	views, err := app.ListMessages("bad-session")

	// then
	if err == nil {
		t.Fatal("ListMessages() expected error, got nil")
	}
	if views != nil {
		t.Fatal("ListMessages() expected nil views on error")
	}
	if !strings.Contains(err.Error(), "list messages") {
		t.Errorf("error should contain 'list messages', got %q", err.Error())
	}
}
