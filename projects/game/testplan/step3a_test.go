// Package testplan contains system-level integration tests for the step3a
// prompt, agent profile, and screenshot-to-operation workflow.
package testplan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"dominion/common/gopkg/testtool"
	game "dominion/projects/game"

	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/encoding/protojson"
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

// ─── Shared Helpers (duplicated from system_test.go) ────────────────────────

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

// createSession sends a POST request with an empty CreateSessionRequest
// body ({}) and returns the server-generated session ID.
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

// ─── New Helpers ───────────────────────────────────────────────────────────

// uniqueSuffix returns a short timestamp-based suffix to make resource names
// unique across test runs.
func uniqueSuffix() string {
	return fmt.Sprintf("%d", time.Now().UnixNano()%10000000)
}

// doHTTP executes an HTTP request and returns the response body for
// successful responses. Calls t.Fatal on connection errors.
func doHTTP(t *testing.T, method, url, envName string, body []byte) (*http.Response, []byte) {
	t.Helper()

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		t.Fatalf("http.NewRequest %s %s: %v", method, url, err)
	}
	req.Header.Set(headerEnv, envName)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}

	respBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read response %s %s: %v", method, url, err)
	}

	return resp, respBody
}

// ─── Agent Profile Helpers ────────────────────────────────────────────────

// createAgentProfile creates an agent profile via HTTP POST and returns the
// parsed response. Calls t.Fatal on any error.
func createAgentProfile(t *testing.T, baseURL, envName string, req *game.CreateAgentProfileRequest) *game.AgentProfile {
	t.Helper()

	body, err := protojson.Marshal(req)
	if err != nil {
		t.Fatalf("protojson.Marshal CreateAgentProfileRequest: %v", err)
	}

	url := fmt.Sprintf("%s%s%s", baseURL, pathPrefix, "prompts/agentProfiles")
	resp, respBody := doHTTP(t, http.MethodPost, url, envName, body)

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
func getAgentProfile(t *testing.T, baseURL, envName, profileName string) *game.AgentProfile {
	t.Helper()

	url := fmt.Sprintf("%s%s%s/%s", baseURL, pathPrefix, "prompts/agentProfiles", profileName)
	resp, respBody := doHTTP(t, http.MethodGet, url, envName, nil)

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

// ─── Skill Helpers ────────────────────────────────────────────────────────

// createSkill creates a skill via HTTP POST and returns the parsed response.
func createSkill(t *testing.T, baseURL, envName string, req *game.CreateSkillRequest) *game.Skill {
	t.Helper()

	body, err := protojson.Marshal(req)
	if err != nil {
		t.Fatalf("protojson.Marshal CreateSkillRequest: %v", err)
	}

	url := fmt.Sprintf("%s%s%s", baseURL, pathPrefix, "prompts/skills")
	resp, respBody := doHTTP(t, http.MethodPost, url, envName, body)

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

// getSkill retrieves a skill by name via HTTP GET.
func getSkill(t *testing.T, baseURL, envName, skillName string) *game.Skill {
	t.Helper()

	url := fmt.Sprintf("%s%s%s/%s", baseURL, pathPrefix, "prompts/skills", skillName)
	resp, respBody := doHTTP(t, http.MethodGet, url, envName, nil)

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

// ─── Agent Helpers ────────────────────────────────────────────────────────

// createAgentWithProfileBody creates an agent under the given session,
// sending the provided JSON body (which may include agentProfileName).
// Returns the parsed Agent proto.
func createAgentWithProfileBody(t *testing.T, baseURL, envName, sessionID string, body []byte) *game.Agent {
	t.Helper()

	url := fmt.Sprintf("%s%ssessions/%s/agent", baseURL, pathPrefix, sessionID)
	resp, respBody := doHTTP(t, http.MethodPost, url, envName, body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST createAgent status=%d, body=%s", resp.StatusCode, respBody)
	}

	agent := new(game.Agent)
	opts := protojson.UnmarshalOptions{DiscardUnknown: true}
	if err := opts.Unmarshal(respBody, agent); err != nil {
		t.Fatalf("Unmarshal Agent: %v (raw: %s)", err, string(respBody))
	}
	return agent
}

// ─── WebSocket Helpers ────────────────────────────────────────────────────

const wsReadTimeout = 30 * time.Second

// connectAgentWS connects to the agent WebSocket endpoint and returns the
// connection. Calls t.Fatal on any error.
func connectAgentWS(t *testing.T, baseURL, envName, sessionID string) *websocket.Conn {
	t.Helper()

	wsPath := fmt.Sprintf("/api/v1/sessions/%s/agent/connect", sessionID)
	wsURL := buildWSURL(baseURL, wsPath)

	header := http.Header{}
	header.Set(headerEnv, envName)

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
// WebSocket connection.
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

// readRawPNG reads the test screenshot fixture and returns the raw bytes.
func readRawPNG(t *testing.T) []byte {
	t.Helper()

	data, err := os.ReadFile("testdata/test_screenshot.png")
	if err != nil {
		t.Fatalf("read test_screenshot.png: %v", err)
	}
	return data
}

// ─── Test 1: Prompt Profile Create → Get ─────────────────────────────────

// TestPromptProfileCreateGet verifies that an agent profile can be created
// via POST /api/v1/prompts/agentProfiles and retrieved via GET with matching
// fields.
func TestPromptProfileCreateGet(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("test-profile-%s", uniqueSuffix())

	// given: create the profile
	createReq := &game.CreateAgentProfileRequest{
		AgentProfileName: profileName,
		Model:            "gpt-4",
		SystemPrompt:     "You are a test agent.",
		SkillNames:       []string{"navigation"},
		McpNames:         []string{"screenshot-tool"},
		Enabled:          true,
	}

	created := createAgentProfile(t, sutHostURL, sutEnvName, createReq)

	// then: verify created profile fields
	if created.GetAgentProfileName() != profileName {
		t.Errorf("created AgentProfileName = %q, want %q", created.GetAgentProfileName(), profileName)
	}
	if created.GetModel() != "gpt-4" {
		t.Errorf("created Model = %q, want %q", created.GetModel(), "gpt-4")
	}
	if created.GetName() == "" {
		t.Error("created Name is empty, want non-empty")
	}

	// when: get the profile
	fetched := getAgentProfile(t, sutHostURL, sutEnvName, profileName)

	// then: verify fetched fields match
	if fetched.GetAgentProfileName() != profileName {
		t.Errorf("fetched AgentProfileName = %q, want %q", fetched.GetAgentProfileName(), profileName)
	}
	if fetched.GetModel() != "gpt-4" {
		t.Errorf("fetched Model = %q, want %q", fetched.GetModel(), "gpt-4")
	}
	if fetched.GetSystemPrompt() != "You are a test agent." {
		t.Errorf("fetched SystemPrompt = %q, want %q", fetched.GetSystemPrompt(), "You are a test agent.")
	}
	if fetched.GetEnabled() != true {
		t.Errorf("fetched Enabled = %v, want true", fetched.GetEnabled())
	}
}

// ─── Test 2: Prompt Skill Create → Get ───────────────────────────────────

// TestPromptSkillCreateGet verifies that a skill can be created via POST
// /api/v1/prompts/skills and retrieved via GET with matching fields.
func TestPromptSkillCreateGet(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	skillName := fmt.Sprintf("test-skill-%s", uniqueSuffix())
	skillContent := "Navigate efficiently through the game world."

	// given: create the skill
	createReq := &game.CreateSkillRequest{
		SkillName: skillName,
		Content:   skillContent,
		Enabled:   true,
	}

	created := createSkill(t, sutHostURL, sutEnvName, createReq)

	// then: verify created skill fields
	if created.GetSkillName() != skillName {
		t.Errorf("created SkillName = %q, want %q", created.GetSkillName(), skillName)
	}
	if created.GetContent() != skillContent {
		t.Errorf("created Content = %q, want %q", created.GetContent(), skillContent)
	}
	if created.GetName() == "" {
		t.Error("created Name is empty, want non-empty")
	}

	// when: get the skill
	fetched := getSkill(t, sutHostURL, sutEnvName, skillName)

	// then: verify fetched fields match
	if fetched.GetSkillName() != skillName {
		t.Errorf("fetched SkillName = %q, want %q", fetched.GetSkillName(), skillName)
	}
	if fetched.GetContent() != skillContent {
		t.Errorf("fetched Content = %q, want %q", fetched.GetContent(), skillContent)
	}
	if fetched.GetEnabled() != true {
		t.Errorf("fetched Enabled = %v, want true", fetched.GetEnabled())
	}
}

// ─── Test 3: Create Agent with Named Profile ─────────────────────────────

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
	agentBody := fmt.Sprintf(`{"agentProfileName":"%s"}`, profileName)
	agent := createAgentWithProfileBody(t, sutHostURL, sutEnvName, sessionID, []byte(agentBody))

	// then: verify agent uses the correct profile
	if agent.GetOwner() == "" {
		t.Error("agent owner is empty, want non-empty")
	}
	if agent.GetAgentProfileName() != profileName {
		t.Errorf("agent AgentProfileName = %q, want %q", agent.GetAgentProfileName(), profileName)
	}
}

// ─── Test 4: Create Agent with Missing Profile ───────────────────────────

// TestCreateAgentMissingProfile verifies that creating an agent with a
// non-existent or empty profile name returns an error response.
func TestCreateAgentMissingProfile(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	// given: create a session (no profile exists with a random name)
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)

	// when: try to create agent with non-existent profile
	missingName := fmt.Sprintf("non-existent-profile-%s", uniqueSuffix())
	agentBody := fmt.Sprintf(`{"agentProfileName":"%s"}`, missingName)

	url := fmt.Sprintf("%s%ssessions/%s/agent", sutHostURL, pathPrefix, sessionID)
	resp, respBody := doHTTP(t, http.MethodPost, url, sutEnvName, []byte(agentBody))

	// then: expect error response (NOT 200 OK)
	if resp.StatusCode == http.StatusOK {
		t.Errorf("POST agent with non-existent profile returned 200, want error. body=%s", respBody)
	}

	// when: also try to create agent with empty profile name
	resp2, respBody2 := doHTTP(t, http.MethodPost, url, sutEnvName, []byte(`{"agentProfileName":""}`))

	// then: expect error response for empty profile too
	if resp2.StatusCode == http.StatusOK {
		t.Errorf("POST agent with empty profile returned 200, want error. body=%s", respBody2)
	}
}

// ─── Test 5: Screenshot to Operation ─────────────────────────────────────

// TestScreenshotToOperation verifies that sending a screenshot over
// WebSocket results in receiving text and operation frames from the agent.
func TestScreenshotToOperation(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("s2o-profile-%s", uniqueSuffix())

	// given: create profile, session, agent, connect WS
	_ = createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		AgentProfileName: profileName,
		Model:            "gpt-4",
		SystemPrompt:     "You are a test agent that processes screenshots.",
		Enabled:          true,
	})

	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	agentBody := fmt.Sprintf(`{"agentProfileName":"%s"}`, profileName)
	_ = createAgentWithProfileBody(t, sutHostURL, sutEnvName, sessionID, []byte(agentBody))

	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	// when: send screenshot frame
	rawPNG := readRawPNG(t)
	invokeID := fmt.Sprintf("invoke-%s", uniqueSuffix())
	screenshotFrame := &game.AgentFrame{
		SessionId: sessionID,
		InvokeId:  invokeID,
		Sequence:  0,
		Payload: &game.AgentFrame_Screenshot{
			Screenshot: &game.AgentScreenshotFrame{
				CaptureId: "s2o-cap-001",
				Encoding:  game.ImageEncoding_IMAGE_ENCODING_PNG,
				Data:      rawPNG,
				WidthPx:   10,
				HeightPx:  10,
			},
		},
	}
	writeWSFrame(t, conn, screenshotFrame)

	// then: read response frames — expect at least one operation frame
	var hasText, hasOp bool
	for i := 0; i < 10; i++ {
		frame := readWSFrame(t, conn)

		if frame.GetText() != nil {
			hasText = true
		}
		if frame.GetOperation() != nil {
			hasOp = true
			// Sequence is on the outer AgentFrame envelope, not on the inner
			// AgentOperationFrame (which no longer has a Sequence field).
			if frame.GetSequence() < 0 {
				t.Error("operation frame envelope has invalid sequence")
			}
			break
		}
		// Also check for other non-error frame types
		if frame.GetAck() != nil || frame.GetStatus() != nil {
			continue
		}
	}

	if !hasOp {
		t.Error("did not receive an operation frame after sending screenshot")
	}
	if !hasText {
		t.Log("did not receive a text frame (may be optional)")
	}
}

// ─── Test 6: Reject Stale Sequence ───────────────────────────────────────

// TestRejectStaleSequence verifies that sending a screenshot with a stale
// (already used) sequence number produces a warn frame in the new
// screenshot→operation loop.
func TestRejectStaleSequence(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("stale-profile-%s", uniqueSuffix())

	// given: setup
	_ = createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		AgentProfileName: profileName,
		Model:            "gpt-4",
		SystemPrompt:     "You are a test agent.",
		Enabled:          true,
	})

	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	agentBody := fmt.Sprintf(`{"agentProfileName":"%s"}`, profileName)
	_ = createAgentWithProfileBody(t, sutHostURL, sutEnvName, sessionID, []byte(agentBody))

	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	// given: send first screenshot with sequence 0, receive operation
	rawPNG := readRawPNG(t)
	invokeID := fmt.Sprintf("invoke-%s", uniqueSuffix())
	screenshotFrame := &game.AgentFrame{
		SessionId: sessionID,
		InvokeId:  invokeID,
		Sequence:  0,
		Payload: &game.AgentFrame_Screenshot{
			Screenshot: &game.AgentScreenshotFrame{
				CaptureId: "stale-cap-001",
				Encoding:  game.ImageEncoding_IMAGE_ENCODING_PNG,
				Data:      rawPNG,
				WidthPx:   10,
				HeightPx:  10,
			},
		},
	}
	writeWSFrame(t, conn, screenshotFrame)

	var opFrame *game.AgentFrame
	for i := 0; i < 10; i++ {
		frame := readWSFrame(t, conn)
		if frame.GetOperation() != nil {
			opFrame = frame
			break
		}
	}
	if opFrame == nil {
		t.Fatal("did not receive operation frame")
	}

	// when: send another screenshot reusing the same sequence 0 (stale)
	// In the new flow, desktop sends next screenshot instead of operation_result.
	// A stale sequence means the desktop reused a sequence number already consumed.
	staleScreenshotFrame := &game.AgentFrame{
		SessionId: sessionID,
		InvokeId:  invokeID,
		Sequence:  0, // stale — same sequence as first screenshot
		Payload: &game.AgentFrame_Screenshot{
			Screenshot: &game.AgentScreenshotFrame{
				CaptureId: "stale-cap-002",
				Encoding:  game.ImageEncoding_IMAGE_ENCODING_PNG,
				Data:      rawPNG,
				WidthPx:   10,
				HeightPx:  10,
			},
		},
	}
	writeWSFrame(t, conn, staleScreenshotFrame)

	// then: expect a warn frame
	var gotWarn bool
	for i := 0; i < 5; i++ {
		frame := readWSFrame(t, conn)
		if frame.GetWarn() != nil {
			gotWarn = true
			break
		}
	}
	if !gotWarn {
		t.Error("expected warn frame for stale sequence, but none received")
	}
}

// ─── Test 7: Reject Wrong Invoke ID ──────────────────────────────────────

// TestRejectWrongInvokeID verifies that sending a screenshot with an
// invoke_id that does not match the active invoke produces a warn frame
// in the screenshot→operation loop.
func TestRejectWrongInvokeID(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("wronginv-profile-%s", uniqueSuffix())

	// given: setup
	_ = createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		AgentProfileName: profileName,
		Model:            "gpt-4",
		SystemPrompt:     "You are a test agent.",
		Enabled:          true,
	})

	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	agentBody := fmt.Sprintf(`{"agentProfileName":"%s"}`, profileName)
	_ = createAgentWithProfileBody(t, sutHostURL, sutEnvName, sessionID, []byte(agentBody))

	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	// given: send screenshot with invoke_id A, receive operation
	rawPNG := readRawPNG(t)
	invokeIDA := fmt.Sprintf("invoke-%s", uniqueSuffix())
	screenshotFrame := &game.AgentFrame{
		SessionId: sessionID,
		InvokeId:  invokeIDA,
		Sequence:  0,
		Payload: &game.AgentFrame_Screenshot{
			Screenshot: &game.AgentScreenshotFrame{
				CaptureId: "wronginv-cap-001",
				Encoding:  game.ImageEncoding_IMAGE_ENCODING_PNG,
				Data:      rawPNG,
				WidthPx:   10,
				HeightPx:  10,
			},
		},
	}
	writeWSFrame(t, conn, screenshotFrame)

	var opFrame *game.AgentFrame
	for i := 0; i < 10; i++ {
		frame := readWSFrame(t, conn)
		if frame.GetOperation() != nil {
			opFrame = frame
			break
		}
	}
	if opFrame == nil {
		t.Fatal("did not receive operation frame")
	}

	// when: send another screenshot with wrong invoke_id B
	// In the screenshot→operation loop, the invoke_id must match the active
	// invoke. A mismatched invoke_id indicates a protocol error.
	invokeIDB := fmt.Sprintf("wrong-invoke-%s", uniqueSuffix())
	wrongScreenshotFrame := &game.AgentFrame{
		SessionId: sessionID,
		InvokeId:  invokeIDB, // different from invokeIDA
		Sequence:  1,
		Payload: &game.AgentFrame_Screenshot{
			Screenshot: &game.AgentScreenshotFrame{
				CaptureId: "wronginv-cap-002",
				Encoding:  game.ImageEncoding_IMAGE_ENCODING_PNG,
				Data:      rawPNG,
				WidthPx:   10,
				HeightPx:  10,
			},
		},
	}
	writeWSFrame(t, conn, wrongScreenshotFrame)

	// then: expect a warn frame
	var gotWarn bool
	for i := 0; i < 5; i++ {
		frame := readWSFrame(t, conn)
		if frame.GetWarn() != nil {
			gotWarn = true
			break
		}
	}
	if !gotWarn {
		t.Error("expected warn frame for wrong invoke_id, but none received")
	}
}

// ─── Test 8: Delete Agent Idempotent ────────────────────────────────────

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

// ─── Test 9: Full Step3a Lifecycle ──────────────────────────────────────

// TestFullStep3aLifecycle executes the complete step3a lifecyle: create
// profile and skill, create session and agent with profile, connect
// WebSocket, send screenshot to receive operation, then send the next
// screenshot to continue the screenshot→operation loop.
func TestFullStep3aLifecycle(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("full-lifecycle-%s", uniqueSuffix())
	skillName := fmt.Sprintf("full-skill-%s", uniqueSuffix())

	// Step 1: create skill
	createdSkill := createSkill(t, sutHostURL, sutEnvName, &game.CreateSkillRequest{
		SkillName: skillName,
		Content:   "Full lifecycle test skill.",
		Enabled:   true,
	})
	if createdSkill.GetName() == "" {
		t.Error("step1: created skill Name is empty")
	}

	// Step 2: create agent profile referencing the skill
	createdProfile := createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		AgentProfileName: profileName,
		Model:            "gpt-4",
		SystemPrompt:     "Full lifecycle test agent.",
		SkillNames:       []string{skillName},
		Enabled:          true,
	})
	if createdProfile.GetName() == "" {
		t.Fatal("step2: created profile Name is empty")
	}
	if len(createdProfile.GetSkillNames()) != 1 || createdProfile.GetSkillNames()[0] != skillName {
		t.Errorf("step2: profile SkillNames = %v, want [%s]", createdProfile.GetSkillNames(), skillName)
	}

	// Step 3: create session
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	if sessionID == "" {
		t.Fatal("step3: sessionId is empty")
	}

	// Step 4: create agent with the profile
	agentBody := fmt.Sprintf(`{"agentProfileName":"%s"}`, profileName)
	agent := createAgentWithProfileBody(t, sutHostURL, sutEnvName, sessionID, []byte(agentBody))
	if agent.GetOwner() == "" {
		t.Fatal("step4: agent owner is empty")
	}
	if agent.GetAgentProfileName() != profileName {
		t.Errorf("step4: agent AgentProfileName = %q, want %q", agent.GetAgentProfileName(), profileName)
	}

	// Step 5: connect WebSocket
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	// Step 6: send screenshot, receive operation
	rawPNG := readRawPNG(t)
	invokeID := fmt.Sprintf("invoke-%s", uniqueSuffix())
	screenshotFrame := &game.AgentFrame{
		SessionId: sessionID,
		InvokeId:  invokeID,
		Sequence:  0,
		Payload: &game.AgentFrame_Screenshot{
			Screenshot: &game.AgentScreenshotFrame{
				CaptureId: "lifecycle-cap-001",
				Encoding:  game.ImageEncoding_IMAGE_ENCODING_PNG,
				Data:      rawPNG,
				WidthPx:   10,
				HeightPx:  10,
			},
		},
	}
	writeWSFrame(t, conn, screenshotFrame)

	var opFrame *game.AgentFrame
	for i := 0; i < 10; i++ {
		frame := readWSFrame(t, conn)
		if frame.GetOperation() != nil {
			opFrame = frame
			break
		}
	}
	if opFrame == nil {
		t.Fatal("step6: did not receive operation frame")
	}

	// Step 7: send next screenshot to continue the screenshot→operation loop.
	// After receiving the operation, the desktop executes it and captures
	// the next screenshot, sending it to the agent for the next operation.
	// This replaces the old operation_result flow.
	nextScreenshotFrame := &game.AgentFrame{
		SessionId: sessionID,
		InvokeId:  invokeID,
		Sequence:  1,
		Payload: &game.AgentFrame_Screenshot{
			Screenshot: &game.AgentScreenshotFrame{
				CaptureId: "lifecycle-cap-002",
				Encoding:  game.ImageEncoding_IMAGE_ENCODING_PNG,
				Data:      rawPNG,
				WidthPx:   10,
				HeightPx:  10,
			},
		},
	}
	writeWSFrame(t, conn, nextScreenshotFrame)

	// then: verify the agent responds with another operation (multi-cycle)
	var opFrame2 *game.AgentFrame
	for i := 0; i < 10; i++ {
		frame := readWSFrame(t, conn)
		if frame.GetOperation() != nil {
			opFrame2 = frame
			break
		}
	}
	if opFrame2 == nil {
		t.Fatal("step7: did not receive second operation frame after next screenshot")
	}
}

// ─── Test 10: Screenshot Operation Loop ──────────────────────────────────

// TestScreenshotOperationLoop explicitly verifies the new screenshot→operation
// loop by executing 2+ cycles: send screenshot → receive operation → send next
// screenshot → receive another operation. It verifies outer frame Sequence
// increments correctly across cycles.
func TestScreenshotOperationLoop(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("loop-profile-%s", uniqueSuffix())

	// given: setup
	_ = createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		AgentProfileName: profileName,
		Model:            "gpt-4",
		SystemPrompt:     "You are a test agent that processes screenshots.",
		Enabled:          true,
	})

	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	agentBody := fmt.Sprintf(`{"agentProfileName":"%s"}`, profileName)
	_ = createAgentWithProfileBody(t, sutHostURL, sutEnvName, sessionID, []byte(agentBody))

	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	rawPNG := readRawPNG(t)
	invokeID := fmt.Sprintf("invoke-%s", uniqueSuffix())

	// Cycle 1: send screenshot with sequence 0, receive operation
	screenshot1 := &game.AgentFrame{
		SessionId: sessionID,
		InvokeId:  invokeID,
		Sequence:  0,
		Payload: &game.AgentFrame_Screenshot{
			Screenshot: &game.AgentScreenshotFrame{
				CaptureId: "loop-cap-001",
				Encoding:  game.ImageEncoding_IMAGE_ENCODING_PNG,
				Data:      rawPNG,
				WidthPx:   10,
				HeightPx:  10,
			},
		},
	}
	writeWSFrame(t, conn, screenshot1)

	var opFrame1 *game.AgentFrame
	for i := 0; i < 10; i++ {
		frame := readWSFrame(t, conn)
		if frame.GetOperation() != nil {
			opFrame1 = frame
			break
		}
	}
	if opFrame1 == nil {
		t.Fatal("cycle 1: did not receive operation frame")
	}

	// Cycle 2: send next screenshot with sequence 1, receive next operation
	screenshot2 := &game.AgentFrame{
		SessionId: sessionID,
		InvokeId:  invokeID,
		Sequence:  1,
		Payload: &game.AgentFrame_Screenshot{
			Screenshot: &game.AgentScreenshotFrame{
				CaptureId: "loop-cap-002",
				Encoding:  game.ImageEncoding_IMAGE_ENCODING_PNG,
				Data:      rawPNG,
				WidthPx:   10,
				HeightPx:  10,
			},
		},
	}
	writeWSFrame(t, conn, screenshot2)

	var opFrame2 *game.AgentFrame
	for i := 0; i < 10; i++ {
		frame := readWSFrame(t, conn)
		if frame.GetOperation() != nil {
			opFrame2 = frame
			break
		}
	}
	if opFrame2 == nil {
		t.Fatal("cycle 2: did not receive operation frame")
	}

	// then: verify both cycles produced valid operation frames
	// with sequence increment across cycles
	if opFrame1.GetSequence() < 0 {
		t.Errorf("cycle 1: operation frame envelope has invalid sequence=%d", opFrame1.GetSequence())
	}
	if opFrame2.GetSequence() < 0 {
		t.Errorf("cycle 2: operation frame envelope has invalid sequence=%d", opFrame2.GetSequence())
	}
	if opFrame2.GetSequence() <= opFrame1.GetSequence() {
		t.Errorf("cycle 2 sequence=%d did not increment beyond cycle 1 sequence=%d",
			opFrame2.GetSequence(), opFrame1.GetSequence())
	}
	t.Logf("screenshot→operation loop completed: cycle1 seq=%d, cycle2 seq=%d",
		opFrame1.GetSequence(), opFrame2.GetSequence())
}

// ─── Test 11: Create Agent with Empty Profile Error ────────────────────────

// TestCreateAgentEmptyProfileError verifies that creating an agent with an
// empty agentProfileName returns an HTTP error response.
func TestCreateAgentEmptyProfileError(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	// given: create a session (no profile needed — empty profile is rejected
	// regardless of existence)
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)

	// when: try to create agent with empty profile name
	url := fmt.Sprintf("%s%ssessions/%s/agent", sutHostURL, pathPrefix, sessionID)
	resp, respBody := doHTTP(t, http.MethodPost, url, sutEnvName, []byte(`{"agentProfileName":""}`))

	// then: expect error response (NOT any 2xx success)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		t.Errorf("POST agent with empty profile returned %d, want error (non-2xx). body=%s", resp.StatusCode, respBody)
	}
}
