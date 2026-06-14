// Package testplan contains agent dialog integration tests.
// These tests validate the agent's text dialog capability through the
// gateway HTTP + WebSocket surface, using the fake LLM test artifact
// that returns deterministic responses.
package testplan

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"dominion/common/gopkg/testtool"
	game "dominion/projects/game"
)

// ─── Tests ───────────────────────────────────────────────────────────────────

// TestAgentDialogCreateAndConnect verifies the complete setup flow:
// create session → create agent with test profile → connect WebSocket.
func TestAgentDialogCreateAndConnect(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("ad-cc-%s", uniqueSuffix())

	// Create profile, session, agent
	profile := createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		AgentProfileName: profileName,
		Model:            "gpt-4",
		SystemPrompt:     "You are a test agent.",
		Enabled:          true,
	})
	if profile.GetAgentProfileName() != profileName {
		t.Errorf("profile name = %q, want %q", profile.GetAgentProfileName(), profileName)
	}

	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	if sessionID == "" {
		t.Fatal("sessionID is empty")
	}

	agent := createAgentWithProfile(t, sutHostURL, sutEnvName, sessionID, profileName)
	if agent.GetAgentProfileName() != profileName {
		t.Errorf("agent profile name = %q, want %q", agent.GetAgentProfileName(), profileName)
	}

	// Connect WebSocket
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	conn.Close()
}

// TestAgentDialogTextToResponse verifies the core dialog flow:
// send text frame → receive thinking frame → receive text frame
// → verify FrameSender.AGENT on response frames.
func TestAgentDialogTextToResponse(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("ad-ttr-%s", uniqueSuffix())

	// Setup
	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		AgentProfileName: profileName,
		Model:            "gpt-4",
		SystemPrompt:     "You are a test agent.",
		Enabled:          true,
	})
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	_ = createAgentWithProfile(t, sutHostURL, sutEnvName, sessionID, profileName)

	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	// Send text frame with sender=USER
	sendText := "Hello, agent!"
	textFrame := &game.AgentFrame{
		SessionId: sessionID,
		Payload: &game.AgentFrame_Text{
			Text: &game.AgentTextFrame{Content: sendText},
		},
		Sender: game.FrameSender_FRAME_SENDER_USER,
	}
	writeWSFrame(t, conn, textFrame)

	// Receive thinking frame
	thinkingFrame := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return f.GetThinking() != nil
	})
	if thinkingFrame == nil {
		t.Fatal("did not receive thinking frame")
	}
	if thinkingFrame.GetSender() != game.FrameSender_FRAME_SENDER_AGENT {
		t.Errorf("thinking sender = %s, want AGENT", senderString(thinkingFrame.GetSender()))
	}
	if thinkingFrame.GetThinking().GetContent() == "" {
		t.Error("thinking content is empty")
	}
	t.Logf("thinking: %q (sender=%s)", thinkingFrame.GetThinking().GetContent(), senderString(thinkingFrame.GetSender()))

	// Receive text frame
	textRespFrame := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return f.GetText() != nil
	})
	if textRespFrame == nil {
		t.Fatal("did not receive text frame")
	}
	if textRespFrame.GetSender() != game.FrameSender_FRAME_SENDER_AGENT {
		t.Errorf("text sender = %s, want AGENT", senderString(textRespFrame.GetSender()))
	}
	if textRespFrame.GetText().GetContent() == "" {
		t.Error("text content is empty")
	}
	t.Logf("text: %q (sender=%s)", textRespFrame.GetText().GetContent(), senderString(textRespFrame.GetSender()))
}

// TestAgentDialogThinkingBeforeText verifies that the thinking frame arrives
// before the text frame — the ordering guarantee from the handler.
func TestAgentDialogThinkingBeforeText(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("ad-tbt-%s", uniqueSuffix())

	// Setup
	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		AgentProfileName: profileName,
		Model:            "gpt-4",
		SystemPrompt:     "You are a test agent.",
		Enabled:          true,
	})
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	_ = createAgentWithProfile(t, sutHostURL, sutEnvName, sessionID, profileName)

	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	// Send text
	textFrame := &game.AgentFrame{
		SessionId: sessionID,
		Payload: &game.AgentFrame_Text{
			Text: &game.AgentTextFrame{Content: "Ordering test"},
		},
		Sender: game.FrameSender_FRAME_SENDER_USER,
	}
	writeWSFrame(t, conn, textFrame)

	// Read frames in order — first must be thinking, second must be text
	frame1 := readWSFrame(t, conn)
	if frame1.GetThinking() == nil {
		t.Fatal("frame 1: expected thinking, got something else")
	}
	frame2 := readWSFrame(t, conn)
	if frame2.GetText() == nil {
		t.Fatal("frame 2: expected text, got something else")
	}

	t.Logf("frame 1 thinking: %q", frame1.GetThinking().GetContent())
	t.Logf("frame 2 text: %q", frame2.GetText().GetContent())
}

// TestAgentDialogDeterministicContent verifies the fake LLM response content
// is deterministic: thinking is always "Processing your message..." and text
// follows the pattern "Hello! This is a simulated response. You said: ...".
func TestAgentDialogDeterministicContent(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("ad-det-%s", uniqueSuffix())

	// Setup
	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		AgentProfileName: profileName,
		Model:            "gpt-4",
		SystemPrompt:     "You are a test agent.",
		Enabled:          true,
	})
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	_ = createAgentWithProfile(t, sutHostURL, sutEnvName, sessionID, profileName)

	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	userMessage := "Hello world"

	// Send text
	textFrame := &game.AgentFrame{
		SessionId: sessionID,
		Payload: &game.AgentFrame_Text{
			Text: &game.AgentTextFrame{Content: userMessage},
		},
		Sender: game.FrameSender_FRAME_SENDER_USER,
	}
	writeWSFrame(t, conn, textFrame)

	// Read and verify thinking content
	thinkingFrame := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return f.GetThinking() != nil
	})
	if thinkingFrame == nil {
		t.Fatal("did not receive thinking frame")
	}
	expectedThinking := "Processing your message..."
	if thinkingFrame.GetThinking().GetContent() != expectedThinking {
		t.Errorf("thinking = %q, want %q", thinkingFrame.GetThinking().GetContent(), expectedThinking)
	}

	// Read and verify text content
	textRespFrame := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return f.GetText() != nil
	})
	if textRespFrame == nil {
		t.Fatal("did not receive text frame")
	}
	expectedText := fmt.Sprintf("Hello! This is a simulated response. You said: %s", userMessage)
	if textRespFrame.GetText().GetContent() != expectedText {
		t.Errorf("text = %q, want %q", textRespFrame.GetText().GetContent(), expectedText)
	}
}

// TestAgentDialogFIFOQueue verifies that sending 3 messages in rapid
// succession yields responses in FIFO order — each message gets its own
// thinking+text pair, in the order they were sent.
func TestAgentDialogFIFOQueue(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("ad-fifo-%s", uniqueSuffix())

	// Setup
	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		AgentProfileName: profileName,
		Model:            "gpt-4",
		SystemPrompt:     "You are a test agent.",
		Enabled:          true,
	})
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	_ = createAgentWithProfile(t, sutHostURL, sutEnvName, sessionID, profileName)

	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	// Send 3 messages rapidly
	messages := []string{
		"First message",
		"Second message",
		"Third message",
	}
	for _, msg := range messages {
		textFrame := &game.AgentFrame{
			SessionId: sessionID,
			Payload: &game.AgentFrame_Text{
				Text: &game.AgentTextFrame{Content: msg},
			},
			Sender: game.FrameSender_FRAME_SENDER_USER,
		}
		writeWSFrame(t, conn, textFrame)
	}

	// Collect all text response frames — they should match the messages in FIFO order
	var responseTexts []string
	for i := 0; i < len(messages); i++ {
		textFrame := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
			return f.GetText() != nil
		})
		if textFrame == nil {
			t.Fatalf("message %d: did not receive text response frame", i)
		}
		responseTexts = append(responseTexts, textFrame.GetText().GetContent())
	}

	if len(responseTexts) != len(messages) {
		t.Fatalf("got %d text responses, want %d", len(responseTexts), len(messages))
	}

	for i, msg := range messages {
		expectedText := fmt.Sprintf("Hello! This is a simulated response. You said: %s", msg)
		if responseTexts[i] != expectedText {
			t.Errorf("response %d = %q, want %q", i, responseTexts[i], expectedText)
		}
	}
}

// TestAgentDialogDeleteProfileStillResponds verifies the loose coupling
// design: after deleting the agent profile, an already-created agent still
// responds because it copied profile data at creation time.
func TestAgentDialogDeleteProfileStillResponds(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("ad-delp-%s", uniqueSuffix())

	// Setup: create profile, session, agent
	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		AgentProfileName: profileName,
		Model:            "gpt-4",
		SystemPrompt:     "You are a test agent.",
		Enabled:          true,
	})
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	agent := createAgentWithProfile(t, sutHostURL, sutEnvName, sessionID, profileName)
	if agent.GetAgentProfileName() != profileName {
		t.Fatalf("setup: agent profile = %q, want %q", agent.GetAgentProfileName(), profileName)
	}

	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	// Delete the profile
	delStatus := deleteAgentProfile(t, sutHostURL, sutEnvName, profileName)
	if delStatus != http.StatusOK && delStatus != http.StatusNoContent {
		t.Fatalf("DELETE profile status = %d, want 200 or 204", delStatus)
	}

	// Send text — agent should still respond using copied profile data
	userMessage := "Still works after profile deleted?"
	textFrame := &game.AgentFrame{
		SessionId: sessionID,
		Payload: &game.AgentFrame_Text{
			Text: &game.AgentTextFrame{Content: userMessage},
		},
		Sender: game.FrameSender_FRAME_SENDER_USER,
	}
	writeWSFrame(t, conn, textFrame)

	// Read text response
	textRespFrame := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return f.GetText() != nil
	})
	if textRespFrame == nil {
		t.Fatal("did not receive text response after profile deletion")
	}
	if textRespFrame.GetSender() != game.FrameSender_FRAME_SENDER_AGENT {
		t.Errorf("response sender = %s, want AGENT", senderString(textRespFrame.GetSender()))
	}
	expectedText := fmt.Sprintf("Hello! This is a simulated response. You said: %s", userMessage)
	if textRespFrame.GetText().GetContent() != expectedText {
		t.Errorf("response text = %q, want %q", textRespFrame.GetText().GetContent(), expectedText)
	}
}

// TestAgentDialogCleanup verifies agent cleanup behavior:
// 1. After creation, the agent exists (GET returns 200).
// 2. After a brief idle period (< 15 min), the agent still exists (no premature cleanup).
// 3. After explicit DELETE, the agent is removed (GET returns 404).
func TestAgentDialogCleanup(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("ad-clean-%s", uniqueSuffix())

	// Setup
	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		AgentProfileName: profileName,
		Model:            "gpt-4",
		SystemPrompt:     "You are a test agent.",
		Enabled:          true,
	})
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	_ = createAgentWithProfile(t, sutHostURL, sutEnvName, sessionID, profileName)

	// Step 1: verify agent exists immediately after creation
	status, _ := getAgentWithStatus(t, sutHostURL, sutEnvName, sessionID)
	if status != http.StatusOK {
		t.Fatalf("GET agent after creation: status=%d, want %d", status, http.StatusOK)
	}

	// Step 2: brief idle period — verify agent is NOT prematurely cleaned up
	time.Sleep(2 * time.Second)
	status2, _ := getAgentWithStatus(t, sutHostURL, sutEnvName, sessionID)
	if status2 != http.StatusOK {
		t.Errorf("GET agent after brief idle: status=%d, want %d (should not be cleaned up yet)", status2, http.StatusOK)
	}
	t.Logf("agent still exists after 2s idle (cleanup threshold is 15 min)")

	// Step 3: delete agent via API — verify removal
	delResp := deleteAgent(t, sutHostURL, sutEnvName, sessionID)
	defer delResp.Body.Close()
	if delResp.StatusCode != http.StatusOK && delResp.StatusCode != http.StatusNoContent {
		t.Errorf("DELETE agent status = %d, want 200 or 204", delResp.StatusCode)
	}

	delGetStatus, _ := getAgentWithStatus(t, sutHostURL, sutEnvName, sessionID)
	if delGetStatus != http.StatusNotFound {
		t.Errorf("GET agent after delete: status=%d, want %d", delGetStatus, http.StatusNotFound)
	}
}
