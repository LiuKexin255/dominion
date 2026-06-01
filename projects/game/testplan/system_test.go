// Package testplan contains system-level integration tests for the game agent
// system. Tests are executed as part of a guitar test plan that deploys all
// four services (session, proxy, agent, gateway) plus MongoDB.
package testplan

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"testing"

	"dominion/common/gopkg/testtool"

	"github.com/gorilla/websocket"
)

const (
	headerEnv  = "env"
	pathPrefix = "/api/v1/"
)

// sessionResponse mirrors the Session proto message returned via gRPC-gateway
// with protojson camelCase field names.
type sessionResponse struct {
	Name       string `json:"name"`
	SessionID  string `json:"sessionId"`
	CreateTime string `json:"createTime"`
}

// agentResponse mirrors the Agent proto message returned via gRPC-gateway
// with protojson camelCase field names.
type agentResponse struct {
	Name       string `json:"name"`
	SessionID  string `json:"sessionId"`
	OwnerIndex int32  `json:"ownerIndex"`
	Owner      string `json:"owner"`
	CreateTime string `json:"createTime"`
}

// listSessionsResponse mirrors the ListSessionsResponse proto message.
type listSessionsResponse struct {
	Sessions      []sessionResponse `json:"sessions"`
	NextPageToken string            `json:"nextPageToken"`
}

// wsFrame mirrors the AgentFrame proto message with oneof payload for
// WebSocket communication using protojson camelCase field names.
type wsFrame struct {
	SessionID  string             `json:"sessionId"`
	FrameID    string             `json:"frameId,omitempty"`
	CreateTime string             `json:"createTime,omitempty"`
	Status     *wsStatusFrame     `json:"status,omitempty"`
	Echo       *wsEchoFrame       `json:"echo,omitempty"`
	Screenshot *wsScreenshotFrame `json:"screenshot,omitempty"`
	Ack        *wsAckFrame        `json:"ack,omitempty"`
}

// wsStatusFrame mirrors the AgentStatusFrame proto message.
type wsStatusFrame struct {
	Status string `json:"status"`
}

// wsEchoFrame mirrors the AgentEchoFrame proto message.
type wsEchoFrame struct {
	Data string `json:"data"`
}

// wsAckFrame mirrors the AgentAckFrame proto message.
type wsAckFrame struct {
	AckFrameID string `json:"ackFrameId"`
	Message    string `json:"message,omitempty"`
}

// wsScreenshotFrame mirrors the AgentScreenshotFrame proto message.
type wsScreenshotFrame struct {
	CaptureID string `json:"captureId"`
	Encoding  string `json:"encoding"`
	Data      string `json:"data"`
	WidthPx   int32  `json:"widthPx"`
	HeightPx  int32  `json:"heightPx"`
}

// createSession sends a POST request with an empty CreateSessionRequest
// body ({}) and returns the server-generated session ID together with the
// raw response body.
func createSession(t *testing.T, sutHostURL, sutEnvName string) (string, []byte) {
	t.Helper()

	reqBody := []byte("{}")
	reqURL := fmt.Sprintf("%s%s%s", sutHostURL, pathPrefix, "sessions")
	req, err := http.NewRequest(http.MethodPost, reqURL, bytes.NewReader(reqBody))
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

	sess := new(sessionResponse)
	if err := json.Unmarshal(respBody, sess); err != nil {
		t.Fatalf("json.Unmarshal createSession response: %v", err)
	}
	if sess.SessionID == "" {
		t.Fatal("createSession: server returned empty sessionId")
	}
	return sess.SessionID, respBody
}

// listSessions sends a GET request to list sessions with the given page
// size and returns the raw response body.
func listSessions(t *testing.T, sutHostURL, sutEnvName string, pageSize int) []byte {
	t.Helper()

	reqURL := fmt.Sprintf("%s%ssessions?page_size=%d", sutHostURL, pathPrefix, pageSize)
	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		t.Fatalf("http.NewRequest listSessions: %v", err)
	}
	req.Header.Set(headerEnv, sutEnvName)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET listSessions: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read listSessions response: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET listSessions status=%d, body=%s", resp.StatusCode, respBody)
	}
	return respBody
}

// createAgent sends a POST request to create an agent under the given session
// and returns the response body as bytes.
func createAgent(t *testing.T, sutHostURL, sutEnvName, sessionID string) []byte {
	t.Helper()

	reqBody := []byte("{}")
	reqURL := fmt.Sprintf("%s%ssessions/%s/agent", sutHostURL, pathPrefix, sessionID)
	req, err := http.NewRequest(http.MethodPost, reqURL, bytes.NewReader(reqBody))
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

// readTestPNG reads the test screenshot fixture from testdata and returns its
// base64-encoded representation.
func readTestPNG(t *testing.T) string {
	t.Helper()

	data, err := os.ReadFile("testdata/test_screenshot.png")
	if err != nil {
		t.Fatalf("read test_screenshot.png: %v", err)
	}
	return base64.StdEncoding.EncodeToString(data)
}

// TestCreateSession verifies that a session can be created successfully via
// POST /api/v1/sessions with an empty body and that the server returns a
// non-empty, server-generated sessionId.
func TestCreateSession(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	// given: empty CreateSessionRequest

	// when
	sessionID, body := createSession(t, sutHostURL, sutEnvName)

	// then
	if sessionID == "" {
		t.Error("createSession returned empty sessionId")
	}

	// verify the response body contains the expected name format
	sess := new(sessionResponse)
	if err := json.Unmarshal(body, sess); err != nil {
		t.Fatalf("json.Unmarshal session response: %v", err)
	}
	wantName := "sessions/" + sessionID
	if sess.Name != wantName {
		t.Errorf("session name = %q, want %q", sess.Name, wantName)
	}
}

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
	// owner_index and owner should be consistent — both present
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
// WebSocket receives a status response with status "initialized", and that
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
	if statusResp.Status.Status != "initialized" {
		t.Errorf("status response status = %q, want %q", statusResp.Status.Status, "initialized")
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
	if recvFrame.Status.Status != "initialized" {
		t.Errorf("step5 status = %q, want %q", recvFrame.Status.Status, "initialized")
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

	// when: list sessions
	respBody := listSessions(t, sutHostURL, sutEnvName, 10)

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
	if got.NextPageToken != "" {
		t.Errorf("next_page_token = %q, want empty", got.NextPageToken)
	}
}

// TestScreenshotFrame verifies that sending a screenshot frame over WebSocket
// receives an ack response with matching ackFrameId and confirmation message.
func TestScreenshotFrame(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	// given: create session, agent, and connect WebSocket
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

	// when: send screenshot frame with PNG data
	pngBase64 := readTestPNG(t)
	captureID := "test-screenshot-cap-001"

	screenshotFrame := wsFrame{
		SessionID: sessionID,
		Screenshot: &wsScreenshotFrame{
			CaptureID: captureID,
			Encoding:  "IMAGE_ENCODING_PNG",
			Data:      pngBase64,
			WidthPx:   10,
			HeightPx:  10,
		},
	}
	if err := conn.WriteJSON(screenshotFrame); err != nil {
		t.Fatalf("WriteJSON screenshot frame: %v", err)
	}

	// then: read ack response
	var ackResp wsFrame
	if err := conn.ReadJSON(&ackResp); err != nil {
		t.Fatalf("ReadJSON ack response: %v", err)
	}
	if ackResp.Ack == nil {
		t.Fatal("ack response has no ack oneof")
	}
	if ackResp.Ack.AckFrameID != captureID {
		t.Errorf("ack ackFrameId = %q, want %q", ackResp.Ack.AckFrameID, captureID)
	}
	if ackResp.Ack.Message != "screenshot received" {
		t.Errorf("ack message = %q, want %q", ackResp.Ack.Message, "screenshot received")
	}
}
