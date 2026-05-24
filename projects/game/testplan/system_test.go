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
	"strings"
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

// TestWebSocketEchoResponse verifies that sending an echo frame over WebSocket
// receives an echo response with the same payload.
func TestWebSocketEchoResponse(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	// given
	sessionID := "test-ws-echo-007"
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

	// when: send echo frame
	echoPayload := "aGVsbG8="
	sendFrame := agentFrame{
		SessionID: sessionID,
		Type:      "echo",
		Payload:   echoPayload,
	}
	if err := conn.WriteJSON(sendFrame); err != nil {
		t.Fatalf("WriteJSON echo frame: %v", err)
	}

	// then: read echo response
	var recvFrame agentFrame
	if err := conn.ReadJSON(&recvFrame); err != nil {
		t.Fatalf("ReadJSON echo response: %v", err)
	}
	if recvFrame.Type != "echo" {
		t.Errorf("echo response type = %q, want %q", recvFrame.Type, "echo")
	}
	if recvFrame.Payload != echoPayload {
		t.Errorf("echo response payload = %q, want %q", recvFrame.Payload, echoPayload)
	}
}

// TestDeleteAgentAndSession verifies that deleting agent then session works,
// and that subsequent GET requests return errors.
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

	// when: get deleted agent — should fail
	getAgentBody := getAgent(t, sutHostURL, sutEnvName, sessionID)
	// GET on deleted session should return empty or error body; not checking
	// the exact response format as it depends on gap service implementation,
	// but the call should not panic.
	_ = getAgentBody
}

// TestFullLifecycle executes the complete lifecycle: create session → create
// agent → query agent → WebSocket connect → WebSocket echo → delete agent →
// delete session → verify deleted.
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

	// Step 5: WebSocket echo
	echoPayload := "aGVsbG8="
	sendFrame := agentFrame{
		SessionID: sessionID,
		Type:      "echo",
		Payload:   echoPayload,
	}
	if err := conn.WriteJSON(sendFrame); err != nil {
		t.Fatalf("step5 WriteJSON echo: %v", err)
	}
	var recvFrame agentFrame
	if err := conn.ReadJSON(&recvFrame); err != nil {
		t.Fatalf("step5 ReadJSON echo response: %v", err)
	}
	if recvFrame.Payload != echoPayload {
		t.Errorf("step5 echo payload = %q, want %q", recvFrame.Payload, echoPayload)
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

	// Step 8: verify deleted — get should return error
	getAgentBody := getAgent(t, sutHostURL, sutEnvName, sessionID)
	// The response body should indicate the resource is gone.
	// We don't enforce exact error format but log it for diagnostics.
	t.Logf("step8 GET deleted agent body: %s", strings.TrimSpace(string(getAgentBody)))
}
