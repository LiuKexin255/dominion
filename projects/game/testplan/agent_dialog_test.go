// Package testplan contains agent dialog integration tests.
// These tests validate the agent's text dialog capability through the
// gateway HTTP + WebSocket surface, using the fake LLM test artifact
// that returns deterministic responses.
package testplan

import (
	"fmt"
	"net/http"
	"testing"

	"dominion/common/gopkg/testtool"
	game "dominion/projects/game"
)

// ─── Tests ───────────────────────────────────────────────────────────────────

// TestAgentDialogCreateAndConnect verifies the setup flow:
// create profile → create session → connect WebSocket.
func TestAgentDialogCreateAndConnect(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("ad-cc-%s", uniqueSuffix())

	// Create profile, session
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
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	// Send text frame with sender=USER
	sendText := "Hello, agent!"
	textFrame := &game.AgentFrame{
		SessionId:        sessionID,
		AgentProfileName: profileName,
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
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	// Send text
	textFrame := &game.AgentFrame{
		SessionId:        sessionID,
		AgentProfileName: profileName,
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
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	userMessage := "Hello world"

	// Send text
	textFrame := &game.AgentFrame{
		SessionId:        sessionID,
		AgentProfileName: profileName,
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
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	// Send 3 messages rapidly
	messages := []string{
		"First message",
		"Second message",
		"Third message",
	}
	for _, msg := range messages {
		sendTextWithProfile(t, conn, sessionID, profileName, msg)
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

	// Setup: create profile, session
	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		AgentProfileName: profileName,
		Model:            "gpt-4",
		SystemPrompt:     "You are a test agent.",
		Enabled:          true,
	})
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)

	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	// Delete the profile
	delStatus := deleteAgentProfile(t, sutHostURL, sutEnvName, profileName)
	if delStatus != http.StatusOK && delStatus != http.StatusNoContent {
		t.Fatalf("DELETE profile status = %d, want 200 or 204", delStatus)
	}

	// Send text — agent should still respond using bound adapter data
	userMessage := "Still works after profile deleted?"
	textFrame := &game.AgentFrame{
		SessionId:        sessionID,
		AgentProfileName: profileName,
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
