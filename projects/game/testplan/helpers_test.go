// Package testplan contains shared types and helpers used across all
// game agent integration test files.
package testplan

import (
	"bytes"
	"encoding/base64"
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

// Expected fake-llm response content. These MUST be kept in sync with
// projects/game/fake-llm/service/testdata/. The T1 unit test
// TestNewMessageStore_LoadsEmbeddedSamples pins the embedded testdata as the
// single source of truth — if testdata changes, that test fails first and
// reminds you to update the constants below in lockstep. See
// projects/game/testplan/README.md §5 for the full workflow.
const (
	expectedGreetingReasoning = "The user is greeting me, I should respond warmly."
	expectedGreetingText      = "Hello! How can I help you today?"
	expectedFarewellReasoning = "The user is saying goodbye."
	expectedFarewellText      = "Goodbye! Have a great day!"
	expectedChatReasoning     = "Responding with text only, no tools needed."
	expectedChatText          = "Sure, let's chat!"
)

// smallScreenshotData is a minimal 1×1 PNG used as screenshot payload in
// multimodal-turn tests. The fake-LLM ignores image bytes (only text blocks
// drive keyword matching), so the actual pixel content is irrelevant — tests
// only verify the server accepts and processes the multimodal frame.
var smallScreenshotData = mustBase64PNG()

// mustBase64PNG decodes a minimal 1×1 transparent PNG. It panics on failure,
// which would indicate a bug in the test itself rather than the SUT.
func mustBase64PNG() []byte {
	raw, err := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAAC0lEQVQI12NgAAIABQABNjN9GQAAAABJRf5ErkJggg==",
	)
	if err != nil {
		panic(fmt.Sprintf("decode test png: %v", err))
	}
	return raw
}

// ─── JSON-response types (mirroring proto messages) ─────────────────────────

// sessionResponse mirrors the Session proto message returned via gRPC-gateway
// with protojson camelCase field names.
type sessionResponse struct {
	Name       string `json:"name"`
	SessionID  string `json:"sessionId"`
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

// ─── Agent Helpers (proto-based) ────────────────────────────────────────────

// getAgent sends a GET request to retrieve the agent for a session.
// Calls t.Fatal on non-200 responses.
func getAgent(t *testing.T, sutHostURL, sutEnvName, sessionID string) *game.Agent {
	t.Helper()

	reqURL := fmt.Sprintf("%s%ssessions/%s/agent", sutHostURL, pathPrefix, sessionID)
	resp, respBody := doHTTP(t, http.MethodGet, reqURL, sutEnvName, nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET agent status=%d, body=%s", resp.StatusCode, respBody)
	}

	agent := new(game.Agent)
	opts := protojson.UnmarshalOptions{DiscardUnknown: true}
	if err := opts.Unmarshal(respBody, agent); err != nil {
		t.Fatalf("Unmarshal Agent: %v (raw: %s)", err, string(respBody))
	}
	return agent
}

// ─── Message Helpers (proto-based) ────────────────────────────────────────

// listMessages sends a GET request to list messages for a session and returns
// the parsed ListMessagesResponse. Calls t.Fatal on non-200 responses.
func listMessages(t *testing.T, sutHostURL, sutEnvName, sessionID string) *game.ListMessagesResponse {
	t.Helper()

	reqURL := fmt.Sprintf("%s%ssessions/%s/messages", sutHostURL, pathPrefix, sessionID)
	resp, respBody := doHTTP(t, http.MethodGet, reqURL, sutEnvName, nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET listMessages status=%d, body=%s", resp.StatusCode, respBody)
	}

	lmr := new(game.ListMessagesResponse)
	opts := protojson.UnmarshalOptions{DiscardUnknown: true}
	if err := opts.Unmarshal(respBody, lmr); err != nil {
		t.Fatalf("Unmarshal ListMessagesResponse: %v (raw: %s)", err, string(respBody))
	}
	return lmr
}

// ─── WebSocket Helpers (proto-based) ────────────────────────────────────────

// connectAgentWS connects to the session WebSocket endpoint and returns the
// connection. Calls t.Fatal on any error.
func connectAgentWS(t *testing.T, sutHostURL, sutEnvName, sessionID string) *websocket.Conn {
	t.Helper()

	wsPath := fmt.Sprintf("/api/v1/sessions/%s/connect", sessionID)
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

// buildTextFrame constructs an AgentFrame with a text payload, setting the
// session ID, agent profile name, and sender.
func buildTextFrame(sessionID, agentProfileName, content string, sender game.FrameSender) *game.AgentFrame {
	return &game.AgentFrame{
		SessionId:        sessionID,
		AgentProfileName: agentProfileName,
		Payload: &game.AgentFrame_UserTurn{
			UserTurn: &game.AgentUserTurnFrame{Text: content},
		},
		Sender: sender,
	}
}

// sendTextWithProfile builds a user-text frame with the given profile and
// sends it over the WebSocket connection. Calls t.Fatal on write errors.
func sendTextWithProfile(t *testing.T, conn *websocket.Conn, sessionID, agentProfileName, text string) {
	t.Helper()
	frame := buildTextFrame(sessionID, agentProfileName, text, game.FrameSender_FRAME_SENDER_USER)
	writeWSFrame(t, conn, frame)
}

// buildScreenshotFrame constructs a minimal AgentScreenshotFrame carrying a
// 1×1 PNG (smallScreenshotData) plus the metadata required by the proto.
// sessionID is used to derive stable capture/screenshot IDs for diagnostics.
func buildScreenshotFrame(sessionID string) *game.AgentScreenshotFrame {
	return &game.AgentScreenshotFrame{
		CaptureId:    fmt.Sprintf("cap-%s", sessionID),
		Encoding:     game.ImageEncoding_IMAGE_ENCODING_PNG,
		Data:         smallScreenshotData,
		WidthPx:      1,
		HeightPx:     1,
		ScaleFactor:  1.0,
		WindowTitle:  "Test Window",
		ScreenshotId: fmt.Sprintf("scr-%s", sessionID),
	}
}

// buildUserTurnFrame constructs an AgentFrame whose payload is an
// AgentUserTurnFrame carrying the given text and an optional screenshot.
// Pass a nil screenshot for a text-only user turn.
func buildUserTurnFrame(sessionID, profileName, text string, screenshot *game.AgentScreenshotFrame) *game.AgentFrame {
	ut := &game.AgentUserTurnFrame{Text: text}
	if screenshot != nil {
		ut.Screenshot = screenshot
	}
	return &game.AgentFrame{
		SessionId:        sessionID,
		AgentProfileName: profileName,
		Sender:           game.FrameSender_FRAME_SENDER_USER,
		Payload: &game.AgentFrame_UserTurn{
			UserTurn: ut,
		},
	}
}

// sendUserTurn builds and writes a user_turn frame over the WebSocket.
// Pass a nil screenshot for a text-only turn.
func sendUserTurn(t *testing.T, conn *websocket.Conn, sessionID, profileName, text string, screenshot *game.AgentScreenshotFrame) {
	t.Helper()
	writeWSFrame(t, conn, buildUserTurnFrame(sessionID, profileName, text, screenshot))
}

// buildOperationResultFrame constructs an AgentFrame whose payload is an
// operation_result with the given status and message. Used to simulate a
// desktop-executed mouse operation result delivered back to the agent.
func buildOperationResultFrame(sessionID, operationID string, status game.AgentOperationResultStatus, message string) *game.AgentFrame {
	return &game.AgentFrame{
		SessionId: sessionID,
		Sender:    game.FrameSender_FRAME_SENDER_USER,
		Payload: &game.AgentFrame_OperationResult{
			OperationResult: &game.AgentOperationResultFrame{
				OperationId: operationID,
				Status:      status,
				Message:     message,
			},
		},
	}
}

// updateAgentProfileTools sends an HTTP PATCH to add the given tool names to
// an existing agent profile via UpdateAgentProfile. Returns the HTTP status
// code and response body.
func updateAgentProfileTools(t *testing.T, sutHostURL, sutEnvName, profileName string, toolNames []string) (int, []byte) {
	t.Helper()

	patchProfile := &game.AgentProfile{
		Name:      "agentProfiles/" + profileName,
		ToolNames: toolNames,
	}
	body, err := protojson.Marshal(patchProfile)
	if err != nil {
		t.Fatalf("protojson.Marshal patch profile: %v", err)
	}

	reqURL := fmt.Sprintf("%s%sprompts/agentProfiles/%s?update_mask=tool_names",
		sutHostURL, pathPrefix, profileName)
	resp, respBody := doHTTP(t, http.MethodPatch, reqURL, sutEnvName, body)
	return resp.StatusCode, respBody
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
