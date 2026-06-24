// Package testplan contains agent multimodal-turn integration tests.
// These tests validate the agent's processing of AgentUserTurnFrame
// payloads carrying text and/or screenshot data through the WebSocket
// surface, using the fake-llm test artifact for deterministic responses.
package testplan

import (
	"fmt"
	"strings"
	"testing"

	"dominion/common/gopkg/testtool"
	game "dominion/projects/game"
)

// TestAgentMultimodalTextPlusImageTurn verifies that a user_turn frame
// containing BOTH text and a screenshot is accepted, and the agent
// produces thinking + text response frames. The text carries the
// "hello" keyword so fake-llm deterministically returns the greeting
// template, proving the multimodal user_turn was processed end-to-end.
func TestAgentMultimodalTextPlusImageTurn(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("mm-tpi-%s", uniqueSuffix())

	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		AgentProfileName: profileName,
		Model:            "gpt-4",
		SystemPrompt:     "You are a multimodal test agent.",
		Enabled:          true,
	})
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	frame := buildUserTurnFrame(sessionID, profileName, "hello multimodal", buildImageFrame(sessionID))
	writeWSFrame(t, conn, frame)

	thinkingFrame := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return f.GetThinking() != nil
	})
	if thinkingFrame == nil {
		t.Fatal("did not receive thinking frame for text+image turn")
	}
	if !strings.Contains(thinkingFrame.GetThinking().GetContent(), expectedGreetingReasoning) {
		t.Errorf("thinking = %q, want to contain %q",
			thinkingFrame.GetThinking().GetContent(), expectedGreetingReasoning)
	}

	textFrame := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return f.GetText() != nil
	})
	if textFrame == nil {
		t.Fatal("did not receive text frame for text+image turn")
	}
	if !strings.Contains(textFrame.GetText().GetContent(), expectedGreetingText) {
		t.Errorf("text = %q, want to contain %q",
			textFrame.GetText().GetContent(), expectedGreetingText)
	}
}

// TestAgentMultimodalImageOnlyTurn verifies that a user_turn containing
// ONLY a screenshot (no text) is accepted by the server. Because the
// text is empty, fake-llm cannot keyword-match and returns a random
// template — the test only verifies that the server processes the
// frame and returns a response (thinking, text, or warn) without error.
func TestAgentMultimodalImageOnlyTurn(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("mm-img-%s", uniqueSuffix())

	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		AgentProfileName: profileName,
		Model:            "gpt-4",
		SystemPrompt:     "You are a multimodal test agent.",
		Enabled:          true,
	})
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	frame := buildUserTurnFrame(sessionID, profileName, "", buildImageFrame(sessionID))
	writeWSFrame(t, conn, frame)

	respFrame := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return f.GetThinking() != nil || f.GetText() != nil || f.GetWarn() != nil
	})
	if respFrame == nil {
		t.Fatal("did not receive any response frame for image-only turn")
	}
	switch {
	case respFrame.GetWarn() != nil:
		t.Logf("warn (acceptable for empty-text turn): %q", respFrame.GetWarn().GetMessage())
	case respFrame.GetText() != nil:
		t.Logf("text response received: %q", respFrame.GetText().GetContent())
	case respFrame.GetThinking() != nil:
		t.Logf("thinking response received: %q", respFrame.GetThinking().GetContent())
	}
}
