// Package testplan contains agent lifecycle integration tests covering
// agent CRUD, ownership, WebSocket connectivity, and cascade delete behavior.
// This file is compiled as its own test binary.
package testplan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"

	"dominion/common/gopkg/testtool"
	game "dominion/projects/game"

	"github.com/gorilla/websocket"
)

// ─── Local WS types (JSON-based, for gateway WebSocket surface tests) ───────

// wsFrame mirrors the AgentFrame proto message with oneof payload for
// WebSocket communication using protojson camelCase field names.
type wsFrame struct {
	SessionID string         `json:"sessionId"`
	Status    *wsStatusFrame `json:"status,omitempty"`
	Echo      *wsEchoFrame   `json:"echo,omitempty"`
}

// wsStatusFrame mirrors the AgentStatusFrame proto message.
type wsStatusFrame struct {
	Status string `json:"status"`
}

// wsEchoFrame mirrors the AgentEchoFrame proto message.
type wsEchoFrame struct {
	Data string `json:"data"`
}

// ─── TestMain ────────────────────────────────────────────────────────────────

// TestMain seeds a "default" agent profile so that agent creation (which
// requires a profile) works out of the box for lifecycle tests.
func TestMain(m *testing.M) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileURL := fmt.Sprintf("%s%s%s", sutHostURL, pathPrefix, "prompts/agentProfiles")
	body := []byte(`{"agentProfileName":"default","enabled":true}`)
	req, err := http.NewRequest(http.MethodPost, profileURL, bytes.NewReader(body))
	if err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: create default profile request: %v\n", err)
		os.Exit(1)
	}
	req.Header.Set(headerEnv, sutEnvName)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: create default profile: %v\n", err)
		os.Exit(1)
	}
	resp.Body.Close()

	os.Exit(m.Run())
}

// ─── Tests from system_test.go ───────────────────────────────────────────────

// TestCreateAgent verifies that an agent can be created under a session via
// POST /api/v1/sessions/{id}/agent and returns owner_index and owner fields.
func TestCreateAgent(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	// given
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)

	// when
	respBody := createAgent(t, sutHostURL, sutEnvName, sessionID)

	// then
	got := new(agentResponse)
	if err := json.Unmarshal(respBody, got); err != nil {
		t.Fatalf("json.Unmarshal agent response: %v", err)
	}
	if got.Owner == "" {
		t.Error("agent owner is empty, want non-empty")
	}
	if got.OwnerIndex < 0 {
		t.Errorf("agent owner_index = %d, want >= 0", got.OwnerIndex)
	}
}

// TestMongoRecordsExist verifies that session and agent records are persisted
// in MongoDB by retrieving them via GET requests.
func TestMongoRecordsExist(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	// given
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	createAgent(t, sutHostURL, sutEnvName, sessionID)

	// when: get session
	sessBody := getSession(t, sutHostURL, sutEnvName, sessionID)

	// then: session exists in Mongo
	sess := new(sessionResponse)
	if err := json.Unmarshal(sessBody, sess); err != nil {
		t.Fatalf("json.Unmarshal session response: %v", err)
	}
	wantName := "sessions/" + sessionID
	if sess.Name != wantName {
		t.Errorf("session name = %q, want %q", sess.Name, wantName)
	}

	// when: get agent
	agentBody := getAgent(t, sutHostURL, sutEnvName, sessionID)

	// then: agent (owner) exists in Mongo
	agent := new(agentResponse)
	if err := json.Unmarshal(agentBody, agent); err != nil {
		t.Fatalf("json.Unmarshal agent response: %v", err)
	}
	if agent.Owner == "" {
		t.Error("agent owner is empty, want non-empty (agent must be persisted in Mongo)")
	}
}

// TestGetAgentReturnsOwner verifies that GET /api/v1/sessions/{id}/agent
// returns the expected owner_index and owner fields in the response.
func TestGetAgentReturnsOwner(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	// given
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	createAgent(t, sutHostURL, sutEnvName, sessionID)

	// when
	respBody := getAgent(t, sutHostURL, sutEnvName, sessionID)

	// then
	got := new(agentResponse)
	if err := json.Unmarshal(respBody, got); err != nil {
		t.Fatalf("json.Unmarshal agent response: %v", err)
	}
	if got.Owner == "" {
		t.Error("agent owner is empty, want non-empty")
	}
	if got.OwnerIndex < 0 {
		t.Errorf("agent owner_index = %d, want >= 0", got.OwnerIndex)
	}
}

// TestConsistentOwnerRouting verifies that querying the same session's agent
// multiple times returns the same owner_index, confirming consistent routing.
func TestConsistentOwnerRouting(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	// given
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	createAgent(t, sutHostURL, sutEnvName, sessionID)

	// when: query agent twice
	firstBody := getAgent(t, sutHostURL, sutEnvName, sessionID)
	secondBody := getAgent(t, sutHostURL, sutEnvName, sessionID)

	// then: owner_index should be the same
	first := new(agentResponse)
	if err := json.Unmarshal(firstBody, first); err != nil {
		t.Fatalf("json.Unmarshal first agent response: %v", err)
	}
	second := new(agentResponse)
	if err := json.Unmarshal(secondBody, second); err != nil {
		t.Fatalf("json.Unmarshal second agent response: %v", err)
	}
	if first.OwnerIndex != second.OwnerIndex {
		t.Errorf("owner_index not consistent: first=%d, second=%d", first.OwnerIndex, second.OwnerIndex)
	}
	if first.Owner != second.Owner {
		t.Errorf("owner not consistent: first=%q, second=%q", first.Owner, second.Owner)
	}
}

// TestWebSocketConnect verifies that connecting to the WebSocket endpoint
// returns a successful 101 Switching Protocols upgrade.
func TestWebSocketConnect(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	// given
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	createAgent(t, sutHostURL, sutEnvName, sessionID)

	// when
	wsPath := fmt.Sprintf("/api/v1/sessions/%s/agent/connect", sessionID)
	wsURL := buildWSURL(sutHostURL, wsPath)

	header := http.Header{}
	header.Set(headerEnv, sutEnvName)

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("websocket.Dial: %v", err)
	}
	defer conn.Close()

	// then: expect 101 Switching Protocols
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Errorf("WebSocket upgrade status = %d, want %d", resp.StatusCode, http.StatusSwitchingProtocols)
	}
}

// TestWebSocketStatusResponse verifies that sending a status frame over
// WebSocket receives a status response with status "idle", and that
// sending an echo frame receives an echoed response.
func TestWebSocketStatusResponse(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	// given
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	createAgent(t, sutHostURL, sutEnvName, sessionID)

	wsPath := fmt.Sprintf("/api/v1/sessions/%s/agent/connect", sessionID)
	wsURL := buildWSURL(sutHostURL, wsPath)

	header := http.Header{}
	header.Set(headerEnv, sutEnvName)

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("websocket.Dial: %v", err)
	}
	defer conn.Close()

	// when: send status frame
	statusFrame := wsFrame{
		SessionID: sessionID,
		Status:    &wsStatusFrame{},
	}
	if err := conn.WriteJSON(statusFrame); err != nil {
		t.Fatalf("WriteJSON status frame: %v", err)
	}

	// then: read status response
	var statusResp wsFrame
	if err := conn.ReadJSON(&statusResp); err != nil {
		t.Fatalf("ReadJSON status response: %v", err)
	}
	if statusResp.Status == nil {
		t.Fatal("status response has no status oneof")
	}
	if statusResp.Status.Status != "idle" {
		t.Errorf("status response status = %q, want %q", statusResp.Status.Status, "idle")
	}

	// when: send echo frame
	echoPayload := "aGVsbG8="
	echoFrame := wsFrame{
		SessionID: sessionID,
		Echo:      &wsEchoFrame{Data: echoPayload},
	}
	if err := conn.WriteJSON(echoFrame); err != nil {
		t.Fatalf("WriteJSON echo frame: %v", err)
	}

	// then: read echo response
	var echoResp wsFrame
	if err := conn.ReadJSON(&echoResp); err != nil {
		t.Fatalf("ReadJSON echo response: %v", err)
	}
	if echoResp.Echo == nil {
		t.Fatal("echo response has no echo oneof")
	}
	if echoResp.Echo.Data != echoPayload {
		t.Errorf("echo response data = %q, want %q", echoResp.Echo.Data, echoPayload)
	}
}

// TestWebSocketUnknownFields verifies that sending a valid AgentFrame JSON
// with extra unknown fields still receives a valid response, confirming the
// gateway uses protojson DiscardUnknown.
func TestWebSocketUnknownFields(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	// given
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	createAgent(t, sutHostURL, sutEnvName, sessionID)

	wsPath := fmt.Sprintf("/api/v1/sessions/%s/agent/connect", sessionID)
	wsURL := buildWSURL(sutHostURL, wsPath)

	header := http.Header{}
	header.Set(headerEnv, sutEnvName)

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("websocket.Dial: %v", err)
	}
	defer conn.Close()

	// when: send JSON with unknown extra fields
	unknownJSON := fmt.Sprintf(
		`{"sessionId":%q,"status":{"status":""},"unknownField":"should_be_ignored"}`,
		sessionID,
	)
	if err := conn.WriteMessage(websocket.TextMessage, []byte(unknownJSON)); err != nil {
		t.Fatalf("WriteMessage unknown fields: %v", err)
	}

	// then: expect a valid response (unknown fields discarded by gateway)
	var recvFrame wsFrame
	if err := conn.ReadJSON(&recvFrame); err != nil {
		t.Fatalf("ReadJSON response: %v", err)
	}
	if recvFrame.Status == nil {
		t.Fatal("response has no status oneof (unknown fields should be discarded)")
	}
}

// TestWebSocketInvalidJSON verifies that sending invalid JSON over WebSocket
// causes the connection to be closed with an error (no echo fallback).
func TestWebSocketInvalidJSON(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	// given
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	createAgent(t, sutHostURL, sutEnvName, sessionID)

	wsPath := fmt.Sprintf("/api/v1/sessions/%s/agent/connect", sessionID)
	wsURL := buildWSURL(sutHostURL, wsPath)

	header := http.Header{}
	header.Set(headerEnv, sutEnvName)

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("websocket.Dial: %v", err)
	}
	defer conn.Close()

	// when: send invalid JSON
	if err := conn.WriteMessage(websocket.TextMessage, []byte("{not valid json")); err != nil {
		t.Fatalf("WriteMessage invalid JSON: %v", err)
	}

	// then: expect connection to be closed or read error
	var recvFrame wsFrame
	err = conn.ReadJSON(&recvFrame)
	if err == nil {
		t.Fatal("ReadJSON should return error for invalid JSON, got nil")
	}
}

// TestDeleteAgentAndSession verifies that deleting agent then session works,
// and that subsequent GET requests return NotFound status codes.
func TestDeleteAgentAndSession(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	// given
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	createAgent(t, sutHostURL, sutEnvName, sessionID)

	// when: delete agent
	agentResp := deleteAgent(t, sutHostURL, sutEnvName, sessionID)
	defer agentResp.Body.Close()

	// then: delete agent returns 200
	if agentResp.StatusCode != http.StatusOK {
		t.Errorf("DELETE agent status = %d, want %d", agentResp.StatusCode, http.StatusOK)
	}

	// when: delete session
	sessResp := deleteSession(t, sutHostURL, sutEnvName, sessionID)
	defer sessResp.Body.Close()

	// then: delete session returns 200
	if sessResp.StatusCode != http.StatusOK {
		t.Errorf("DELETE session status = %d, want %d", sessResp.StatusCode, http.StatusOK)
	}

	// when: get deleted agent
	agentStatus, _ := getAgentWithStatus(t, sutHostURL, sutEnvName, sessionID)

	// then: should return NotFound
	if agentStatus != http.StatusNotFound {
		t.Errorf("GET deleted agent status = %d, want %d", agentStatus, http.StatusNotFound)
	}

	// when: get deleted session
	sessStatus, _ := getSessionWithStatus(t, sutHostURL, sutEnvName, sessionID)

	// then: should return NotFound (cascading cleanup)
	if sessStatus != http.StatusNotFound {
		t.Errorf("GET deleted session status = %d, want %d", sessStatus, http.StatusNotFound)
	}
}

// TestDeleteSessionCascade verifies that deleting a session directly (without
// deleting the agent first) cascades cleanup to the agent/owner records.
func TestDeleteSessionCascade(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	// given
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	createAgent(t, sutHostURL, sutEnvName, sessionID)

	// when: delete session directly (without deleting agent first)
	sessResp := deleteSession(t, sutHostURL, sutEnvName, sessionID)
	defer sessResp.Body.Close()

	// then: delete session returns 200
	if sessResp.StatusCode != http.StatusOK {
		t.Errorf("DELETE session status = %d, want %d", sessResp.StatusCode, http.StatusOK)
	}

	// when: get deleted session
	sessStatus, _ := getSessionWithStatus(t, sutHostURL, sutEnvName, sessionID)

	// then: session is deleted
	if sessStatus != http.StatusNotFound {
		t.Errorf("GET deleted session status = %d, want %d", sessStatus, http.StatusNotFound)
	}

	// when: get agent after session deletion
	agentStatus, _ := getAgentWithStatus(t, sutHostURL, sutEnvName, sessionID)

	// then: agent/owner records are cleaned up (cascade)
	if agentStatus != http.StatusNotFound {
		t.Errorf("GET agent after session delete status = %d, want %d (cascade cleanup)", agentStatus, http.StatusNotFound)
	}
}

// TestDeleteAgentCascade verifies that deleting an agent cleans up the owner
// record. The session should still exist after agent deletion.
func TestDeleteAgentCascade(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	// given
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	createAgent(t, sutHostURL, sutEnvName, sessionID)

	// when: delete agent
	agentResp := deleteAgent(t, sutHostURL, sutEnvName, sessionID)
	defer agentResp.Body.Close()

	// then: delete agent returns 200
	if agentResp.StatusCode != http.StatusOK {
		t.Errorf("DELETE agent status = %d, want %d", agentResp.StatusCode, http.StatusOK)
	}

	// when: get deleted agent
	agentStatus, _ := getAgentWithStatus(t, sutHostURL, sutEnvName, sessionID)

	// then: agent is deleted
	if agentStatus != http.StatusNotFound {
		t.Errorf("GET deleted agent status = %d, want %d", agentStatus, http.StatusNotFound)
	}

	// when: get session — should still exist
	sessStatus, sessBody := getSessionWithStatus(t, sutHostURL, sutEnvName, sessionID)

	// then: session still exists
	if sessStatus != http.StatusOK {
		t.Errorf("GET session after agent delete status = %d, want %d (session should still exist)", sessStatus, http.StatusOK)
		t.Logf("session body: %s", string(sessBody))
	}
}

// TestFullLifecycle executes the complete lifecycle: create session → create
// agent → query agent → WebSocket connect → WebSocket status response →
// delete agent → delete session → verify deleted.
func TestFullLifecycle(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	// Step 1: create session
	sessionID, sessBody := createSession(t, sutHostURL, sutEnvName)
	sess := new(sessionResponse)
	if err := json.Unmarshal(sessBody, sess); err != nil {
		t.Fatalf("step1 json.Unmarshal session: %v", err)
	}
	wantName := "sessions/" + sessionID
	if sess.Name != wantName {
		t.Errorf("step1 session name = %q, want %q", sess.Name, wantName)
	}

	// Step 2: create agent
	agentBody := createAgent(t, sutHostURL, sutEnvName, sessionID)
	agent := new(agentResponse)
	if err := json.Unmarshal(agentBody, agent); err != nil {
		t.Fatalf("step2 json.Unmarshal agent: %v", err)
	}
	if agent.Owner == "" || agent.OwnerIndex < 0 {
		t.Fatalf("step2 agent owner=%q owner_index=%d, want non-empty and >=0", agent.Owner, agent.OwnerIndex)
	}

	// Step 3: query agent — verify owner fields
	qBody := getAgent(t, sutHostURL, sutEnvName, sessionID)
	qAgent := new(agentResponse)
	if err := json.Unmarshal(qBody, qAgent); err != nil {
		t.Fatalf("step3 json.Unmarshal agent: %v", err)
	}
	if qAgent.Owner == "" || qAgent.OwnerIndex < 0 {
		t.Errorf("step3 agent owner=%q owner_index=%d, want non-empty and >=0", qAgent.Owner, qAgent.OwnerIndex)
	}

	// Step 4: WebSocket connect
	wsPath := fmt.Sprintf("/api/v1/sessions/%s/agent/connect", sessionID)
	wsURL := buildWSURL(sutHostURL, wsPath)
	header := http.Header{}
	header.Set(headerEnv, sutEnvName)

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("step4 websocket.Dial: %v", err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Errorf("step4 WebSocket upgrade status = %d, want %d", resp.StatusCode, http.StatusSwitchingProtocols)
	}

	// Step 5: WebSocket status response
	statusFrame := wsFrame{
		SessionID: sessionID,
		Status:    &wsStatusFrame{},
	}
	if err := conn.WriteJSON(statusFrame); err != nil {
		t.Fatalf("step5 WriteJSON status: %v", err)
	}
	var recvFrame wsFrame
	if err := conn.ReadJSON(&recvFrame); err != nil {
		t.Fatalf("step5 ReadJSON status response: %v", err)
	}
	if recvFrame.Status == nil {
		t.Fatal("step5 response has no status oneof")
	}
	if recvFrame.Status.Status != "idle" {
		t.Errorf("step5 status = %q, want %q", recvFrame.Status.Status, "idle")
	}
	conn.Close()

	// Step 6: delete agent
	delAgentResp := deleteAgent(t, sutHostURL, sutEnvName, sessionID)
	defer delAgentResp.Body.Close()
	if delAgentResp.StatusCode != http.StatusOK {
		t.Errorf("step6 DELETE agent status = %d, want %d", delAgentResp.StatusCode, http.StatusOK)
	}

	// Step 7: delete session
	delSessResp := deleteSession(t, sutHostURL, sutEnvName, sessionID)
	defer delSessResp.Body.Close()
	if delSessResp.StatusCode != http.StatusOK {
		t.Errorf("step7 DELETE session status = %d, want %d", delSessResp.StatusCode, http.StatusOK)
	}

	// Step 8: verify deleted — GET should return NotFound
	agentStatus, agentGetBody := getAgentWithStatus(t, sutHostURL, sutEnvName, sessionID)
	if agentStatus != http.StatusNotFound {
		t.Errorf("step8 GET deleted agent status = %d, want %d, body: %s", agentStatus, http.StatusNotFound, agentGetBody)
	}

	sessStatus, sessGetBody := getSessionWithStatus(t, sutHostURL, sutEnvName, sessionID)
	if sessStatus != http.StatusNotFound {
		t.Errorf("step8 GET deleted session status = %d, want %d, body: %s", sessStatus, http.StatusNotFound, sessGetBody)
	}
}

// TestListSessions verifies that listing sessions returns all created sessions
// and that nextPageToken is empty when all sessions fit in one page.
func TestListSessions(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	// given: create two sessions
	sess1ID, _ := createSession(t, sutHostURL, sutEnvName)
	sess2ID, _ := createSession(t, sutHostURL, sutEnvName)

	// when: list sessions with large page size to avoid pagination from prior tests
	respBody := listSessions(t, sutHostURL, sutEnvName, 100)

	// then: both sessions are in the response
	got := new(listSessionsResponse)
	if err := json.Unmarshal(respBody, got); err != nil {
		t.Fatalf("json.Unmarshal listSessions response: %v", err)
	}

	found1 := false
	found2 := false
	for _, s := range got.Sessions {
		if s.SessionID == sess1ID {
			found1 = true
		}
		if s.SessionID == sess2ID {
			found2 = true
		}
	}
	if !found1 {
		t.Errorf("session %q not found in list result", sess1ID)
	}
	if !found2 {
		t.Errorf("session %q not found in list result", sess2ID)
	}
	if got.NextPageToken != "" && len(got.Sessions) < 100 {
		t.Errorf("next_page_token = %q, want empty (got %d sessions)", got.NextPageToken, len(got.Sessions))
	}
}

// ─── Tests from step3a_test.go ───────────────────────────────────────────────

// TestCreateAgentWithNamedProfile verifies that an agent can be created
// with an explicitly specified agent profile name.
func TestCreateAgentWithNamedProfile(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("explicit-profile-%s", uniqueSuffix())

	// given: create a named profile
	createReq := &game.CreateAgentProfileRequest{
		AgentProfileName: profileName,
		Model:            "gpt-4",
		SystemPrompt:     "Explicit test agent.",
		Enabled:          true,
	}
	_ = createAgentProfile(t, sutHostURL, sutEnvName, createReq)

	// given: create a session
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)

	// when: create agent with the named profile
	agent := createAgentWithProfile(t, sutHostURL, sutEnvName, sessionID, profileName)

	// then: verify agent uses the correct profile
	if agent.GetOwner() == "" {
		t.Error("agent owner is empty, want non-empty")
	}
	if agent.GetAgentProfileName() != profileName {
		t.Errorf("agent AgentProfileName = %q, want %q", agent.GetAgentProfileName(), profileName)
	}
}

// TestCreateAgentMissingProfile verifies that creating an agent with a
// non-existent profile name returns an error response.
func TestCreateAgentMissingProfile(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	// given: create a session (no profile exists with a random name)
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)

	// when: try to create agent with non-existent profile
	missingName := fmt.Sprintf("non-existent-profile-%s", uniqueSuffix())
	agentBody := []byte(fmt.Sprintf(`{"agentProfileName":"%s"}`, missingName))

	got, status, respBody := createAgentWithBody(t, sutHostURL, sutEnvName, sessionID, agentBody)

	// then: expect error response (NOT 200 OK)
	if status == http.StatusOK {
		t.Errorf("POST agent with non-existent profile returned 200, want error. body=%s", respBody)
	}
	_ = got

	// when: also try to create agent with empty profile name
	got2, status2, respBody2 := createAgentWithBody(t, sutHostURL, sutEnvName, sessionID, []byte(`{"agentProfileName":""}`))

	// then: expect error response for empty profile too
	if status2 == http.StatusOK {
		t.Errorf("POST agent with empty profile returned 200, want error. body=%s", respBody2)
	}
	_ = got2
}

// TestCreateAgentEmptyProfileError verifies that creating an agent with an
// empty agentProfileName returns an HTTP error response.
func TestCreateAgentEmptyProfileError(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	// given: create a session (no profile needed — empty profile is rejected
	// regardless of existence)
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)

	// when: try to create agent with empty profile name
	_, status, respBody := createAgentWithBody(t, sutHostURL, sutEnvName, sessionID, []byte(`{"agentProfileName":""}`))

	// then: expect error response (NOT any 2xx success)
	if status >= 200 && status < 300 {
		t.Errorf("POST agent with empty profile returned %d, want error (non-2xx). body=%s", status, respBody)
	}
}

// TestDeleteAgentIdempotent verifies that deleting an agent on a session
// with no existing agent succeeds without error (idempotent behavior).
func TestDeleteAgentIdempotent(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	// given: create a session (no agent created)
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)

	// when: delete agent (not yet created)
	resp := deleteAgent(t, sutHostURL, sutEnvName, sessionID)
	defer resp.Body.Close()

	// then: expect successful deletion (200 or 204)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		t.Errorf("DELETE non-existent agent status = %d, want 200 or 204", resp.StatusCode)
	}
}
