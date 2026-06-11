// Package testplan contains shared types and helpers used across all
// game agent integration test files.
package testplan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"

	game "dominion/projects/game"

	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/encoding/protojson"
)

// ─── Constants ──────────────────────────────────────────────────────────────

const (
	headerEnv     = "env"
	pathPrefix    = "/api/v1/"
	wsReadTimeout = 30 * time.Second
)

// ─── JSON-response types (mirroring proto messages) ─────────────────────────

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

// ─── General Helpers ────────────────────────────────────────────────────────

// uniqueSuffix returns a short timestamp-based suffix to make resource names
// unique across test runs.
func uniqueSuffix() string {
	return fmt.Sprintf("%d", time.Now().UnixNano()%10000000)
}

// doHTTP executes an HTTP request and returns the response and body.
// Calls t.Fatal on connection or read errors.
func doHTTP(t *testing.T, method, rawurl, envName string, body []byte) (*http.Response, []byte) {
	t.Helper()

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequest(method, rawurl, bodyReader)
	if err != nil {
		t.Fatalf("http.NewRequest %s %s: %v", method, rawurl, err)
	}
	req.Header.Set(headerEnv, envName)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, rawurl, err)
	}

	respBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read response %s %s: %v", method, rawurl, err)
	}

	return resp, respBody
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

// ─── Session Helpers (JSON-based) ───────────────────────────────────────────

// createSession sends a POST request with an empty CreateSessionRequest
// body ({}) and returns the server-generated session ID together with the
// raw response body. Calls t.Fatal on non-200 responses.
func createSession(t *testing.T, sutHostURL, sutEnvName string) (string, []byte) {
	t.Helper()

	reqBody := []byte("{}")
	reqURL := fmt.Sprintf("%s%s%s", sutHostURL, pathPrefix, "sessions")

	resp, respBody := doHTTP(t, http.MethodPost, reqURL, sutEnvName, reqBody)

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
// size and returns the raw response body. Calls t.Fatal on non-200 responses.
func listSessions(t *testing.T, sutHostURL, sutEnvName string, pageSize int) []byte {
	t.Helper()

	reqURL := fmt.Sprintf("%s%ssessions?page_size=%d", sutHostURL, pathPrefix, pageSize)
	resp, respBody := doHTTP(t, http.MethodGet, reqURL, sutEnvName, nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET listSessions status=%d, body=%s", resp.StatusCode, respBody)
	}
	return respBody
}

// getSession sends a GET request for a session and returns the response body.
// Calls t.Fatal on non-200 responses.
func getSession(t *testing.T, sutHostURL, sutEnvName, sessionID string) []byte {
	t.Helper()

	reqURL := fmt.Sprintf("%s%ssessions/%s", sutHostURL, pathPrefix, sessionID)
	resp, respBody := doHTTP(t, http.MethodGet, reqURL, sutEnvName, nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET session status=%d, body=%s", resp.StatusCode, respBody)
	}
	return respBody
}

// getSessionWithStatus sends a GET request for a session and returns the HTTP
// status code and response body. Does NOT fatal on non-200 responses.
func getSessionWithStatus(t *testing.T, sutHostURL, sutEnvName, sessionID string) (int, []byte) {
	t.Helper()

	reqURL := fmt.Sprintf("%s%ssessions/%s", sutHostURL, pathPrefix, sessionID)
	resp, respBody := doHTTP(t, http.MethodGet, reqURL, sutEnvName, nil)

	return resp.StatusCode, respBody
}

// deleteSession sends a DELETE request for a session. Does NOT fatal on
// non-200 responses.
func deleteSession(t *testing.T, sutHostURL, sutEnvName, sessionID string) *http.Response {
	t.Helper()

	reqURL := fmt.Sprintf("%s%ssessions/%s", sutHostURL, pathPrefix, sessionID)
	resp, _ := doHTTP(t, http.MethodDelete, reqURL, sutEnvName, nil)

	return resp
}

// ─── Agent Helpers (JSON-based) ─────────────────────────────────────────────

// createAgent sends a POST request to create an agent under the given session
// with agentProfileName set to "default", and returns the raw response body.
// Calls t.Fatal on non-200 responses.
// The "default" profile must be seeded by TestMain.
func createAgent(t *testing.T, sutHostURL, sutEnvName, sessionID string) []byte {
	t.Helper()

	reqBody := []byte(`{"agentProfileName":"default"}`)
	reqURL := fmt.Sprintf("%s%ssessions/%s/agent", sutHostURL, pathPrefix, sessionID)
	resp, respBody := doHTTP(t, http.MethodPost, reqURL, sutEnvName, reqBody)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST createAgent status=%d, body=%s", resp.StatusCode, respBody)
	}
	return respBody
}

// getAgent sends a GET request for an agent and returns the raw response body.
// Calls t.Fatal on non-200 responses.
func getAgent(t *testing.T, sutHostURL, sutEnvName, sessionID string) []byte {
	t.Helper()

	reqURL := fmt.Sprintf("%s%ssessions/%s/agent", sutHostURL, pathPrefix, sessionID)
	resp, respBody := doHTTP(t, http.MethodGet, reqURL, sutEnvName, nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET agent status=%d, body=%s", resp.StatusCode, respBody)
	}
	return respBody
}

// getAgentWithStatus sends a GET request for an agent and returns the HTTP
// status code and response body. Does NOT fatal on non-200 responses.
func getAgentWithStatus(t *testing.T, sutHostURL, sutEnvName, sessionID string) (int, []byte) {
	t.Helper()

	reqURL := fmt.Sprintf("%s%ssessions/%s/agent", sutHostURL, pathPrefix, sessionID)
	resp, respBody := doHTTP(t, http.MethodGet, reqURL, sutEnvName, nil)

	return resp.StatusCode, respBody
}

// deleteAgent sends a DELETE request for an agent. Does NOT fatal on non-200
// responses.
func deleteAgent(t *testing.T, sutHostURL, sutEnvName, sessionID string) *http.Response {
	t.Helper()

	reqURL := fmt.Sprintf("%s%ssessions/%s/agent", sutHostURL, pathPrefix, sessionID)
	resp, _ := doHTTP(t, http.MethodDelete, reqURL, sutEnvName, nil)

	return resp
}

// ─── Agent Helpers (proto-based) ────────────────────────────────────────────

// createAgentWithProfile creates an agent under the given session with the
// specified profile name. Returns the parsed Agent proto. Calls t.Fatal on
// non-200 responses.
func createAgentWithProfile(t *testing.T, sutHostURL, sutEnvName, sessionID, profileName string) *game.Agent {
	t.Helper()

	body := []byte(fmt.Sprintf(`{"agentProfileName":"%s"}`, profileName))
	reqURL := fmt.Sprintf("%s%ssessions/%s/agent", sutHostURL, pathPrefix, sessionID)
	resp, respBody := doHTTP(t, http.MethodPost, reqURL, sutEnvName, body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST createAgentWithProfile status=%d, body=%s", resp.StatusCode, respBody)
	}

	agent := new(game.Agent)
	opts := protojson.UnmarshalOptions{DiscardUnknown: true}
	if err := opts.Unmarshal(respBody, agent); err != nil {
		t.Fatalf("Unmarshal Agent: %v (raw: %s)", err, string(respBody))
	}
	return agent
}

// createAgentWithBody creates an agent under the given session with an
// arbitrary JSON body. Returns the parsed Agent (nil on non-200), the HTTP
// status code, and the raw response body. Does NOT fatal on non-200 responses.
func createAgentWithBody(t *testing.T, sutHostURL, sutEnvName, sessionID string, body []byte) (*game.Agent, int, []byte) {
	t.Helper()

	reqURL := fmt.Sprintf("%s%ssessions/%s/agent", sutHostURL, pathPrefix, sessionID)
	resp, respBody := doHTTP(t, http.MethodPost, reqURL, sutEnvName, body)

	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, respBody
	}

	agent := new(game.Agent)
	opts := protojson.UnmarshalOptions{DiscardUnknown: true}
	if err := opts.Unmarshal(respBody, agent); err != nil {
		t.Fatalf("Unmarshal Agent: %v (raw: %s)", err, string(respBody))
	}
	return agent, resp.StatusCode, respBody
}

// ─── Profile/Skill Helpers (proto-based) ────────────────────────────────────

// createAgentProfile creates an agent profile via HTTP POST and returns the
// parsed response. Calls t.Fatal on any error.
func createAgentProfile(t *testing.T, sutHostURL, sutEnvName string, req *game.CreateAgentProfileRequest) *game.AgentProfile {
	t.Helper()

	body, err := protojson.Marshal(req)
	if err != nil {
		t.Fatalf("protojson.Marshal CreateAgentProfileRequest: %v", err)
	}

	reqURL := fmt.Sprintf("%s%s%s", sutHostURL, pathPrefix, "prompts/agentProfiles")
	resp, respBody := doHTTP(t, http.MethodPost, reqURL, sutEnvName, body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST createAgentProfile status=%d, body=%s", resp.StatusCode, respBody)
	}

	profile := new(game.AgentProfile)
	opts := protojson.UnmarshalOptions{DiscardUnknown: true}
	if err := opts.Unmarshal(respBody, profile); err != nil {
		t.Fatalf("Unmarshal AgentProfile: %v (raw: %s)", err, string(respBody))
	}
	return profile
}

// getAgentProfile retrieves an agent profile by name via HTTP GET.
// Calls t.Fatal on non-200 responses.
func getAgentProfile(t *testing.T, sutHostURL, sutEnvName, profileName string) *game.AgentProfile {
	t.Helper()

	reqURL := fmt.Sprintf("%s%s%s/%s", sutHostURL, pathPrefix, "prompts/agentProfiles", profileName)
	resp, respBody := doHTTP(t, http.MethodGet, reqURL, sutEnvName, nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET agentProfile status=%d, body=%s", resp.StatusCode, respBody)
	}

	profile := new(game.AgentProfile)
	opts := protojson.UnmarshalOptions{DiscardUnknown: true}
	if err := opts.Unmarshal(respBody, profile); err != nil {
		t.Fatalf("Unmarshal AgentProfile: %v (raw: %s)", err, string(respBody))
	}
	return profile
}

// deleteAgentProfile deletes an agent profile by name via HTTP DELETE.
// Returns the HTTP status code.
func deleteAgentProfile(t *testing.T, sutHostURL, sutEnvName, profileName string) int {
	t.Helper()

	reqURL := fmt.Sprintf("%s%s%s/%s", sutHostURL, pathPrefix, "prompts/agentProfiles", profileName)
	resp, _ := doHTTP(t, http.MethodDelete, reqURL, sutEnvName, nil)

	return resp.StatusCode
}

// createSkill creates a skill via HTTP POST and returns the parsed response.
// Calls t.Fatal on any error.
func createSkill(t *testing.T, sutHostURL, sutEnvName string, req *game.CreateSkillRequest) *game.Skill {
	t.Helper()

	body, err := protojson.Marshal(req)
	if err != nil {
		t.Fatalf("protojson.Marshal CreateSkillRequest: %v", err)
	}

	reqURL := fmt.Sprintf("%s%s%s", sutHostURL, pathPrefix, "prompts/skills")
	resp, respBody := doHTTP(t, http.MethodPost, reqURL, sutEnvName, body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST createSkill status=%d, body=%s", resp.StatusCode, respBody)
	}

	skill := new(game.Skill)
	opts := protojson.UnmarshalOptions{DiscardUnknown: true}
	if err := opts.Unmarshal(respBody, skill); err != nil {
		t.Fatalf("Unmarshal Skill: %v (raw: %s)", err, string(respBody))
	}
	return skill
}

// getSkill retrieves a skill by name via HTTP GET. Calls t.Fatal on non-200
// responses.
func getSkill(t *testing.T, sutHostURL, sutEnvName, skillName string) *game.Skill {
	t.Helper()

	reqURL := fmt.Sprintf("%s%s%s/%s", sutHostURL, pathPrefix, "prompts/skills", skillName)
	resp, respBody := doHTTP(t, http.MethodGet, reqURL, sutEnvName, nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET skill status=%d, body=%s", resp.StatusCode, respBody)
	}

	skill := new(game.Skill)
	opts := protojson.UnmarshalOptions{DiscardUnknown: true}
	if err := opts.Unmarshal(respBody, skill); err != nil {
		t.Fatalf("Unmarshal Skill: %v (raw: %s)", err, string(respBody))
	}
	return skill
}

// ─── WebSocket Helpers (proto-based) ────────────────────────────────────────

// connectAgentWS connects to the agent WebSocket endpoint and returns the
// connection. Calls t.Fatal on any error.
func connectAgentWS(t *testing.T, sutHostURL, sutEnvName, sessionID string) *websocket.Conn {
	t.Helper()

	wsPath := fmt.Sprintf("/api/v1/sessions/%s/agent/connect", sessionID)
	wsURL := buildWSURL(sutHostURL, wsPath)

	header := http.Header{}
	header.Set(headerEnv, sutEnvName)

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("websocket.Dial: %v", err)
	}

	if resp.StatusCode != http.StatusSwitchingProtocols {
		conn.Close()
		t.Fatalf("WS upgrade status = %d, want %d", resp.StatusCode, http.StatusSwitchingProtocols)
	}
	return conn
}

// writeWSFrame marshals a proto frame to protojson and writes it over the
// WebSocket connection. Calls t.Fatal on marshal or write errors.
func writeWSFrame(t *testing.T, conn *websocket.Conn, frame *game.AgentFrame) {
	t.Helper()

	data, err := protojson.Marshal(frame)
	if err != nil {
		t.Fatalf("protojson.Marshal frame: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
}

// readWSFrame reads a single WebSocket message and unmarshals it into an
// AgentFrame. Calls t.Fatal on timeout or parse error.
func readWSFrame(t *testing.T, conn *websocket.Conn) *game.AgentFrame {
	t.Helper()

	conn.SetReadDeadline(time.Now().Add(wsReadTimeout))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	frame := new(game.AgentFrame)
	opts := protojson.UnmarshalOptions{DiscardUnknown: true}
	if err := opts.Unmarshal(data, frame); err != nil {
		t.Fatalf("Unmarshal AgentFrame: %v (raw: %s)", err, string(data))
	}
	return frame
}

// drainWSFrame reads and discards frames until a frame matches the predicate,
// or all frames from the timeout are exhausted. Returns the first matching
// frame, or nil if none found. Reads up to 20 frames.
func drainWSFrame(t *testing.T, conn *websocket.Conn, match func(*game.AgentFrame) bool) *game.AgentFrame {
	t.Helper()

	for i := 0; i < 20; i++ {
		frame := readWSFrame(t, conn)
		if match(frame) {
			return frame
		}
	}
	return nil
}

// senderString returns a human-readable name for a FrameSender value (for test
// diagnostics).
func senderString(sender game.FrameSender) string {
	switch sender {
	case game.FrameSender_FRAME_SENDER_USER:
		return "USER"
	case game.FrameSender_FRAME_SENDER_AGENT:
		return "AGENT"
	case game.FrameSender_FRAME_SENDER_SYSTEM:
		return "SYSTEM"
	default:
		return "UNSPECIFIED"
	}
}
