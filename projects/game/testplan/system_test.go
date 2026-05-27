// Package testplan contains system-level integration tests for the game agent
// system. Tests are executed as part of a guitar test plan that deploys all
// four services (session, proxy, agent, gateway) plus MongoDB.
package testplan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"testing"

	"dominion/common/gopkg/testtool"

	"github.com/gorilla/websocket"
)

const (
	headerEnv  = "env"
	pathPrefix = "/api/v1/"
)

// sessionResponse mirrors the Session proto message returned via gRPC-gateway.
type sessionResponse struct {
	Name       string `json:"name"`
	SessionID  string `json:"session_id"`
	CreateTime string `json:"create_time"`
}

// agentResponse mirrors the Agent proto message returned via gRPC-gateway.
type agentResponse struct {
	Name       string `json:"name"`
	SessionID  string `json:"session_id"`
	OwnerIndex int32  `json:"owner_index"`
	Owner      string `json:"owner"`
	CreateTime string `json:"create_time"`
}

// agentFrame mirrors the AgentFrame proto message for WebSocket communication.
type agentFrame struct {
	SessionID string `json:"session_id"`
	Type      string `json:"type"`
	Payload   string `json:"payload"`
}

// createSession sends a POST request to create a new session and returns the
// response body as bytes.
func createSession(t *testing.T, sutHostURL, sutEnvName, sessionID string) []byte {
	t.Helper()

	reqBody := map[string]string{"session_id": sessionID}
	body, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("json.Marshal createSession request: %v", err)
	}

	reqURL := fmt.Sprintf("%s%s%s", sutHostURL, pathPrefix, "sessions")
	req, err := http.NewRequest(http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("http.NewRequest createSession: %v", err)
	}
	req.Header.Set(headerEnv, sutEnvName)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST createSession: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read createSession response: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST createSession status=%d, body=%s", resp.StatusCode, respBody)
	}
	return respBody
}

// createAgent sends a POST request to create an agent under the given session
// and returns the response body as bytes.
func createAgent(t *testing.T, sutHostURL, sutEnvName, sessionID string) []byte {
	t.Helper()

	reqBody := map[string]interface{}{"agent": struct{}{}}
	body, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("json.Marshal createAgent request: %v", err)
	}

	reqURL := fmt.Sprintf("%s%ssessions/%s/agent", sutHostURL, pathPrefix, sessionID)
	req, err := http.NewRequest(http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("http.NewRequest createAgent: %v", err)
	}
	req.Header.Set(headerEnv, sutEnvName)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST createAgent: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read createAgent response: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST createAgent status=%d, body=%s", resp.StatusCode, respBody)
	}
	return respBody
}

// getSession sends a GET request for a session and returns the response body.
func getSession(t *testing.T, sutHostURL, sutEnvName, sessionID string) []byte {
	t.Helper()

	reqURL := fmt.Sprintf("%s%ssessions/%s", sutHostURL, pathPrefix, sessionID)
	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		t.Fatalf("http.NewRequest getSession: %v", err)
	}
	req.Header.Set(headerEnv, sutEnvName)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET session: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read getSession response: %v", err)
	}
	return respBody
}

// getAgent sends a GET request for an agent and returns the response body.
func getAgent(t *testing.T, sutHostURL, sutEnvName, sessionID string) []byte {
	t.Helper()

	reqURL := fmt.Sprintf("%s%ssessions/%s/agent", sutHostURL, pathPrefix, sessionID)
	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		t.Fatalf("http.NewRequest getAgent: %v", err)
	}
	req.Header.Set(headerEnv, sutEnvName)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET agent: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read getAgent response: %v", err)
	}
	return respBody
}

// deleteAgent sends a DELETE request for an agent.
func deleteAgent(t *testing.T, sutHostURL, sutEnvName, sessionID string) *http.Response {
	t.Helper()

	reqURL := fmt.Sprintf("%s%ssessions/%s/agent", sutHostURL, pathPrefix, sessionID)
	req, err := http.NewRequest(http.MethodDelete, reqURL, nil)
	if err != nil {
		t.Fatalf("http.NewRequest deleteAgent: %v", err)
	}
	req.Header.Set(headerEnv, sutEnvName)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE agent: %v", err)
	}
	return resp
}

// deleteSession sends a DELETE request for a session.
func deleteSession(t *testing.T, sutHostURL, sutEnvName, sessionID string) *http.Response {
	t.Helper()

	reqURL := fmt.Sprintf("%s%ssessions/%s", sutHostURL, pathPrefix, sessionID)
	req, err := http.NewRequest(http.MethodDelete, reqURL, nil)
	if err != nil {
		t.Fatalf("http.NewRequest deleteSession: %v", err)
	}
	req.Header.Set(headerEnv, sutEnvName)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE session: %v", err)
	}
	return resp
}

// buildWSURL constructs a WebSocket URL from the HTTP endpoint by replacing
// the scheme with ws or wss.
func buildWSURL(sutHostURL, path string) string {
	u, err := url.Parse(sutHostURL)
	if err != nil {
		panic(fmt.Sprintf("parse sutHostURL %q: %v", sutHostURL, err))
	}
	host := u.Host
	scheme := "ws"
	if u.Scheme == "https" {
		scheme = "wss"
	}
	return fmt.Sprintf("%s://%s%s", scheme, host, path)
}

// getAgentWithStatus sends a GET request for an agent and returns the HTTP
// status code and response body without fatalling on non-200 responses.
func getAgentWithStatus(t *testing.T, sutHostURL, sutEnvName, sessionID string) (int, []byte) {
	t.Helper()

	reqURL := fmt.Sprintf("%s%ssessions/%s/agent", sutHostURL, pathPrefix, sessionID)
	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		t.Fatalf("http.NewRequest getAgentWithStatus: %v", err)
	}
	req.Header.Set(headerEnv, sutEnvName)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET agent: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read getAgentWithStatus response: %v", err)
	}
	return resp.StatusCode, respBody
}

// getSessionWithStatus sends a GET request for a session and returns the HTTP
// status code and response body without fatalling on non-200 responses.
func getSessionWithStatus(t *testing.T, sutHostURL, sutEnvName, sessionID string) (int, []byte) {
	t.Helper()

	reqURL := fmt.Sprintf("%s%ssessions/%s", sutHostURL, pathPrefix, sessionID)
	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		t.Fatalf("http.NewRequest getSessionWithStatus: %v", err)
	}
	req.Header.Set(headerEnv, sutEnvName)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET session: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read getSessionWithStatus response: %v", err)
	}
	return resp.StatusCode, respBody
}

// TestCreateSession verifies that a session can be created successfully via
// POST /api/v1/sessions and returns the expected resource name.
func TestCreateSession(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	// given
	sessionID := "test-create-001"

	// when
	respBody := createSession(t, sutHostURL, sutEnvName, sessionID)

	// then
	got := new(sessionResponse)
	if err := json.Unmarshal(respBody, got); err != nil {
		t.Fatalf("json.Unmarshal session response: %v", err)
	}
	wantName := "sessions/" + sessionID
	if got.Name != wantName {
		t.Errorf("session name = %q, want %q", got.Name, wantName)
	}
}

// TestCreateAgent verifies that an agent can be created under a session via
// POST /api/v1/sessions/{id}/agent and returns owner_index and owner fields.
func TestCreateAgent(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	// given
	sessionID := "test-create-002"
	createSession(t, sutHostURL, sutEnvName, sessionID)

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
	sessionID := "test-mongo-003"
	createSession(t, sutHostURL, sutEnvName, sessionID)
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
	sessionID := "test-owner-004"
	createSession(t, sutHostURL, sutEnvName, sessionID)
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
	// owner_index and owner should be consistent — both present
}

// TestConsistentOwnerRouting verifies that querying the same session's agent
// multiple times returns the same owner_index, confirming consistent routing.
func TestConsistentOwnerRouting(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	// given
	sessionID := "test-consist-005"
	createSession(t, sutHostURL, sutEnvName, sessionID)
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
	sessionID := "test-ws-conn-006"
	createSession(t, sutHostURL, sutEnvName, sessionID)
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
// WebSocket receives a status response with payload "initialized", and that
// sending an echo frame receives an echoed response.
func TestWebSocketStatusResponse(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	// given
	sessionID := "test-ws-status-007"
	createSession(t, sutHostURL, sutEnvName, sessionID)
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
	statusFrame := agentFrame{
		SessionID: sessionID,
		Type:      "status",
		Payload:   "",
	}
	if err := conn.WriteJSON(statusFrame); err != nil {
		t.Fatalf("WriteJSON status frame: %v", err)
	}

	// then: read status response
	var statusResp agentFrame
	if err := conn.ReadJSON(&statusResp); err != nil {
		t.Fatalf("ReadJSON status response: %v", err)
	}
	if statusResp.Type != "status" {
		t.Errorf("status response type = %q, want %q", statusResp.Type, "status")
	}
	if statusResp.Payload != "initialized" {
		t.Errorf("status response payload = %q, want %q", statusResp.Payload, "initialized")
	}

	// when: send echo frame
	echoPayload := "aGVsbG8="
	echoFrame := agentFrame{
		SessionID: sessionID,
		Type:      "echo",
		Payload:   echoPayload,
	}
	if err := conn.WriteJSON(echoFrame); err != nil {
		t.Fatalf("WriteJSON echo frame: %v", err)
	}

	// then: read echo response
	var echoResp agentFrame
	if err := conn.ReadJSON(&echoResp); err != nil {
		t.Fatalf("ReadJSON echo response: %v", err)
	}
	if echoResp.Type != "echo" {
		t.Errorf("echo response type = %q, want %q", echoResp.Type, "echo")
	}
	if echoResp.Payload != echoPayload {
		t.Errorf("echo response payload = %q, want %q", echoResp.Payload, echoPayload)
	}
}

// TestWebSocketUnknownFields verifies that sending a valid AgentFrame JSON
// with extra unknown fields still receives a valid response, confirming the
// gateway uses protojson DiscardUnknown.
func TestWebSocketUnknownFields(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	// given
	sessionID := "test-ws-unknown-010"
	createSession(t, sutHostURL, sutEnvName, sessionID)
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
		`{"session_id":%q,"type":"status","payload":"","unknown_field":"should_be_ignored"}`,
		sessionID,
	)
	if err := conn.WriteMessage(websocket.TextMessage, []byte(unknownJSON)); err != nil {
		t.Fatalf("WriteMessage unknown fields: %v", err)
	}

	// then: expect a valid response (unknown fields discarded by gateway)
	var recvFrame agentFrame
	if err := conn.ReadJSON(&recvFrame); err != nil {
		t.Fatalf("ReadJSON response: %v", err)
	}
	if recvFrame.Type != "status" {
		t.Errorf("response type = %q, want %q", recvFrame.Type, "status")
	}
}

// TestWebSocketInvalidJSON verifies that sending invalid JSON over WebSocket
// causes the connection to be closed with an error (no echo fallback).
func TestWebSocketInvalidJSON(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	// given
	sessionID := "test-ws-invalid-011"
	createSession(t, sutHostURL, sutEnvName, sessionID)
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
	var recvFrame agentFrame
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
	sessionID := "test-delete-008"
	createSession(t, sutHostURL, sutEnvName, sessionID)
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
	sessionID := "test-delete-session-cascade-012"
	createSession(t, sutHostURL, sutEnvName, sessionID)
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
	sessionID := "test-delete-agent-cascade-013"
	createSession(t, sutHostURL, sutEnvName, sessionID)
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

	sessionID := "test-lifecycle-009"

	// Step 1: create session
	sessBody := createSession(t, sutHostURL, sutEnvName, sessionID)
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
	statusFrame := agentFrame{
		SessionID: sessionID,
		Type:      "status",
		Payload:   "",
	}
	if err := conn.WriteJSON(statusFrame); err != nil {
		t.Fatalf("step5 WriteJSON status: %v", err)
	}
	var recvFrame agentFrame
	if err := conn.ReadJSON(&recvFrame); err != nil {
		t.Fatalf("step5 ReadJSON status response: %v", err)
	}
	if recvFrame.Type != "status" {
		t.Errorf("step5 response type = %q, want %q", recvFrame.Type, "status")
	}
	if recvFrame.Payload != "initialized" {
		t.Errorf("step5 status payload = %q, want %q", recvFrame.Payload, "initialized")
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
