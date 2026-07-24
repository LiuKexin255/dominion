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
// parsed response. Per AIP-133 + grpc-gateway body binding ("body:
// agent_profile"), the HTTP body is the AgentProfile JSON (extracted from the
// request's agent_profile field) while parent comes from the URI path and
// agent_profile_id comes from the query string. Calls t.Fatal on any error.
func createAgentProfile(t *testing.T, sutHostURL, sutEnvName string, req *game.CreateAgentProfileRequest) *game.AgentProfile {
	t.Helper()

	body, err := protojson.Marshal(req.GetAgentProfile())
	if err != nil {
		t.Fatalf("protojson.Marshal AgentProfile: %v", err)
	}

	reqURL := fmt.Sprintf("%s%s%s?agent_profile_id=%s", sutHostURL, pathPrefix, "prompts/agentProfiles", req.GetAgentProfileId())
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
// Per AIP-133 + grpc-gateway body binding ("body: skill"), the HTTP body is
// the Skill JSON while parent comes from the URI path and skill_id comes from
// the query string. Calls t.Fatal on any error.
func createSkill(t *testing.T, sutHostURL, sutEnvName string, req *game.CreateSkillRequest) *game.Skill {
	t.Helper()

	body, err := protojson.Marshal(req.GetSkill())
	if err != nil {
		t.Fatalf("protojson.Marshal Skill: %v", err)
	}

	reqURL := fmt.Sprintf("%s%s%s?skill_id=%s", sutHostURL, pathPrefix, "prompts/skills", req.GetSkillId())
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

// refreshAgent triggers the agent's RefreshAgent RPC for a session via HTTP
// POST to /api/v1/sessions/{sessionID}/agent:refresh (game.proto
// RefreshAgent http rule). The body is "{}" — the `name` field is captured
// from the URI path by grpc-gateway (mirroring
// projects/game/desktop/internal/api/client.go RefreshAgent). Calls t.Fatal
// on non-2xx responses. After refresh the session agent's adapter is
// invalidated so the next turn rebuilds it for the supplied profile
// (specs/021-agent-session-resync/contracts/agent-session-lifecycle-contract.md §2).
func refreshAgent(t *testing.T, sutHostURL, sutEnvName, sessionID string) {
	t.Helper()

	reqURL := fmt.Sprintf("%s%ssessions/%s/agent:refresh", sutHostURL, pathPrefix, sessionID)
	resp, respBody := doHTTP(t, http.MethodPost, reqURL, sutEnvName, []byte("{}"))

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		t.Fatalf("POST refreshAgent status=%d, body=%s", resp.StatusCode, respBody)
	}
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

// buildTextFrame constructs an AgentFrame whose content payload is a PartBlock
// holding a single TextPart. Sets the session ID, agent profile name, sender.
func buildTextFrame(sessionID, agentProfileName, content string, sender game.FrameSender) *game.AgentFrame {
	return &game.AgentFrame{
		SessionId:        sessionID,
		AgentProfileName: agentProfileName,
		Sender:           sender,
		Payload: &game.AgentFrame_Content{
			Content: &game.PartBlock{Parts: []*game.Part{
				{Kind: &game.Part_Text{Text: &game.TextPart{Content: content}}},
			}},
		},
	}
}

// sendTextWithProfile builds a user-text frame with the given profile and
// sends it over the WebSocket connection. Calls t.Fatal on write errors.
func sendTextWithProfile(t *testing.T, conn *websocket.Conn, sessionID, agentProfileName, text string) {
	t.Helper()
	frame := buildTextFrame(sessionID, agentProfileName, text, game.FrameSender_FRAME_SENDER_USER)
	writeWSFrame(t, conn, frame)
}

// sendStatusFrame writes a status-ping AgentFrame over the WebSocket with the
// given StatusSignalStatus. The desktop sends this on session (re-)entry to
// probe the agent's working state; the agent responds with a derived
// StatusSignal (ACTIVE/IDLE/UNSPECIFIED)
// (specs/021-agent-session-resync/contracts/agent-desktop-channel-contract.md §1).
func sendStatusFrame(t *testing.T, conn *websocket.Conn, sessionID string, status game.StatusSignalStatus) {
	t.Helper()
	frame := &game.AgentFrame{
		SessionId: sessionID,
		Sender:    game.FrameSender_FRAME_SENDER_USER,
		Payload: &game.AgentFrame_Status{
			Status: &game.StatusSignal{Status: status},
		},
	}
	writeWSFrame(t, conn, frame)
}

// buildImageFrame constructs a minimal ImagePart carrying a 1×1 PNG
// (smallScreenshotData) plus the metadata required by the proto. The returned
// part is embedded in a user-turn PartBlock by buildUserTurnFrame.
func buildImageFrame(sessionID string) *game.ImagePart {
	return &game.ImagePart{
		Encoding:    game.ImageEncoding_IMAGE_ENCODING_PNG,
		Data:        smallScreenshotData,
		WidthPx:     1,
		HeightPx:    1,
		ScaleFactor: 1.0,
		WindowTitle: "Test Window",
	}
}

// buildUserTurnFrame constructs an AgentFrame whose content payload is a
// PartBlock of [TextPart, (optional) ImagePart]. Pass a nil image for a
// text-only user turn.
func buildUserTurnFrame(sessionID, profileName, text string, image *game.ImagePart) *game.AgentFrame {
	parts := []*game.Part{
		{Kind: &game.Part_Text{Text: &game.TextPart{Content: text}}},
	}
	if image != nil {
		parts = append(parts, &game.Part{Kind: &game.Part_Image{Image: image}})
	}
	return &game.AgentFrame{
		SessionId:        sessionID,
		AgentProfileName: profileName,
		Sender:           game.FrameSender_FRAME_SENDER_USER,
		Payload: &game.AgentFrame_Content{
			Content: &game.PartBlock{Parts: parts},
		},
	}
}

// sendUserTurn builds and writes a content user-turn frame over the WebSocket.
// Pass a nil image for a text-only turn.
func sendUserTurn(t *testing.T, conn *websocket.Conn, sessionID, profileName, text string, image *game.ImagePart) {
	t.Helper()
	writeWSFrame(t, conn, buildUserTurnFrame(sessionID, profileName, text, image))
}

// buildOperationResultFrame constructs an AgentFrame whose content payload is a
// PartBlock holding a ToolResultPart. Used to simulate a desktop-executed tool
// operation result delivered back to the agent.
func buildOperationResultFrame(sessionID, toolID string, status game.ToolResultStatus, message string) *game.AgentFrame {
	return &game.AgentFrame{
		SessionId: sessionID,
		Sender:    game.FrameSender_FRAME_SENDER_USER,
		Payload: &game.AgentFrame_Content{
			Content: &game.PartBlock{Parts: []*game.Part{
				{Kind: &game.Part_ToolResult{ToolResult: &game.ToolResultPart{
					ToolId:  toolID,
					Status:  status,
					Message: message,
				}}},
			}},
		},
	}
}

// ─── Content-projection helpers ─────────────────────────────────────────────
//
// The Part model folds every content variant into a PartBlock of Parts, so a
// thinking or text response is now a content frame whose PartBlock carries a
// ThinkingPart / TextPart — not a dedicated frame payload. These helpers
// project a part-kind out of a content frame (or Message) the way the old
// frame.GetThinking() / frame.GetText() / message.GetText() accessors did.

// frameHasThinking reports whether a content frame's PartBlock holds a
// ThinkingPart.
func frameHasThinking(f *game.AgentFrame) bool {
	if f.GetContent() == nil {
		return false
	}
	for _, p := range f.GetContent().GetParts() {
		if p.GetThinking() != nil {
			return true
		}
	}
	return false
}

// frameHasText reports whether a content frame's PartBlock holds a TextPart.
func frameHasText(f *game.AgentFrame) bool {
	if f.GetContent() == nil {
		return false
	}
	for _, p := range f.GetContent().GetParts() {
		if p.GetText() != nil {
			return true
		}
	}
	return false
}

// frameThinking returns the content of the first ThinkingPart in a content
// frame's PartBlock, or "" if the frame has no thinking part.
func frameThinking(f *game.AgentFrame) string {
	if f.GetContent() == nil {
		return ""
	}
	for _, p := range f.GetContent().GetParts() {
		if t := p.GetThinking(); t != nil {
			return t.GetContent()
		}
	}
	return ""
}

// frameText returns the content of the first TextPart in a content frame's
// PartBlock, or "" if the frame has no text part.
func frameText(f *game.AgentFrame) string {
	if f.GetContent() == nil {
		return ""
	}
	for _, p := range f.GetContent().GetParts() {
		if t := p.GetText(); t != nil {
			return t.GetContent()
		}
	}
	return ""
}

// messageKind returns the part-kind string of the first Part in a Message's
// content PartBlock ("text", "thinking", "image", "mouseMove", "mouseClick",
// "toolResult"), or "" if the message has no content. Replaces the removed
// Message.type field — Part.kind self-describes, so a separate discriminator
// is redundant on the wire, but tests still need a kind label for logging.
func messageKind(m *game.Message) string {
	if m.GetContent() == nil || len(m.GetContent().GetParts()) == 0 {
		return ""
	}
	p := m.GetContent().GetParts()[0]
	switch {
	case p.GetText() != nil:
		return "text"
	case p.GetThinking() != nil:
		return "thinking"
	case p.GetImage() != nil:
		return "image"
	case p.GetMouseMove() != nil:
		return "mouseMove"
	case p.GetMouseClick() != nil:
		return "mouseClick"
	case p.GetToolResult() != nil:
		return "toolResult"
	}
	return ""
}

// messageText returns the content of the first TextPart in a Message's content
// PartBlock, or "" if none. Replaces the removed Message.text field.
func messageText(m *game.Message) string {
	if m.GetContent() == nil {
		return ""
	}
	for _, p := range m.GetContent().GetParts() {
		if t := p.GetText(); t != nil {
			return t.GetContent()
		}
	}
	return ""
}

// updateAgentProfileTools sends an HTTP PATCH to add the given tool names to
// an existing agent profile via UpdateAgentProfile. Returns the HTTP status
// code and response body.
func updateAgentProfileTools(t *testing.T, sutHostURL, sutEnvName, profileName string, toolNames []string) (int, []byte) {
	t.Helper()

	patchProfile := &game.AgentProfile{
		Name:      "prompts/agentProfiles/" + profileName,
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

// ─── Operation-dispatch helpers ─────────────────────────────────────────────
//
// When the model emits a tool_call, the agent executes the tool, which calls
// OperationBridge.dispatch. The bridge wraps the operation Part (MouseMovePart,
// MouseClickPart, KeyboardPressPart, or MouseMoveAndClickPart) in a content
// AgentFrame and writes it to the session WebSocket sink. A large test that
// "plays the desktop" reads that operation frame, echoes the stamped tool_id
// back in a ToolResultPart, and the bridge resolves the pending dispatch. The
// helpers below project the operation Part out of a content frame and reply
// with a matching ToolResultPart — they are shared by the agent_operation and
// agent_saolei suites so neither copies the logic (style/large_test.md §反模式3).

// frameOperationToolID returns the tool_id stamped on the first tool-operation
// Part in a content frame (keyboard_press / mouse_move_and_click / mouse_move /
// mouse_click), or "" when the frame carries no operation Part. tool_id is the
// correlation key the bridge matches against the ToolResultPart (see
// projects/game/agent/src/operation-bridge.ts dispatch/handleResult).
func frameOperationToolID(f *game.AgentFrame) string {
	if f.GetContent() == nil {
		return ""
	}
	for _, p := range f.GetContent().GetParts() {
		if kp := p.GetKeyboardPress(); kp != nil {
			return kp.GetToolId()
		}
		if mmc := p.GetMouseMoveAndClick(); mmc != nil {
			return mmc.GetToolId()
		}
		if mm := p.GetMouseMove(); mm != nil {
			return mm.GetToolId()
		}
		if mc := p.GetMouseClick(); mc != nil {
			return mc.GetToolId()
		}
	}
	return ""
}

// frameKeyboardPress returns the first KeyboardPressPart in a content frame,
// or nil. Used by the saolei suite to assert saolei_init dispatched an F2 key.
func frameKeyboardPress(f *game.AgentFrame) *game.KeyboardPressPart {
	if f.GetContent() == nil {
		return nil
	}
	for _, p := range f.GetContent().GetParts() {
		if kp := p.GetKeyboardPress(); kp != nil {
			return kp
		}
	}
	return nil
}

// frameMouseMoveAndClick returns the first MouseMoveAndClickPart in a content
// frame, or nil. Used by the saolei suite to assert a cell operation dispatched
// the correct window-message mouse Part.
func frameMouseMoveAndClick(f *game.AgentFrame) *game.MouseMoveAndClickPart {
	if f.GetContent() == nil {
		return nil
	}
	for _, p := range f.GetContent().GetParts() {
		if mmc := p.GetMouseMoveAndClick(); mmc != nil {
			return mmc
		}
	}
	return nil
}

// frameMouseMove returns the first MouseMovePart in a content frame, or nil.
// Used by the agent_operation suite to assert a mouse_move tool_call dispatched.
func frameMouseMove(f *game.AgentFrame) *game.MouseMovePart {
	if f.GetContent() == nil {
		return nil
	}
	for _, p := range f.GetContent().GetParts() {
		if mm := p.GetMouseMove(); mm != nil {
			return mm
		}
	}
	return nil
}

// readOperationFrame drains frames until it finds a content frame carrying a
// tool-operation Part, and returns it. Fails the test if no operation frame
// arrives within the drain limit — the model→tool_call→dispatch chain is the
// behaviour under test, so its absence is a real failure, not a timeout.
func readOperationFrame(t *testing.T, conn *websocket.Conn) *game.AgentFrame {
	t.Helper()
	f := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return frameOperationToolID(f) != ""
	})
	if f == nil {
		t.Fatal("did not receive an operation Part frame from the agent " +
			"(model→tool_call→dispatch chain did not fire)")
	}
	return f
}

// respondToOperation writes a ToolResultPart back over the WebSocket whose
// tool_id matches the operation frame's stamped id, simulating a desktop that
// executed the operation. The bridge's handleResult resolves the pending
// dispatch so the model's tool-call loop continues.
func respondToOperation(t *testing.T, conn *websocket.Conn, sessionID string, opFrame *game.AgentFrame, status game.ToolResultStatus, message string) {
	t.Helper()
	toolID := frameOperationToolID(opFrame)
	if toolID == "" {
		t.Fatalf("respondToOperation: operation frame has no tool_id")
	}
	writeWSFrame(t, conn, buildOperationResultFrame(sessionID, toolID, status, message))
}
