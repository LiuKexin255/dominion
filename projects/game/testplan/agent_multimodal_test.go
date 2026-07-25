// Package testplan contains agent multimodal-turn integration tests.
// These tests validate the agent's processing of content PartBlock payloads
// carrying text and/or an ImagePart through the WebSocket surface, using the
// fake-llm test artifact for deterministic responses.
package testplan

import (
	"fmt"
	"strings"
	"testing"

	"dominion/common/gopkg/testtool"
	game "dominion/projects/game"
	"dominion/projects/game/pkg/gameconst"
)

// TestAgentMultimodalTextPlusImageTurn verifies that a content frame whose
// PartBlock holds BOTH a TextPart and an ImagePart is accepted, and the agent
// produces thinking + text response frames. The text carries the "hello"
// keyword so fake-llm deterministically returns the greeting template, proving
// the multimodal content frame was processed end-to-end.
func TestAgentMultimodalTextPlusImageTurn(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("mm-tpi-%s", uniqueSuffix())

	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		Parent:         gameconst.PromptsParent,
		AgentProfileId: profileName,
		AgentProfile: &game.AgentProfile{
			Model:        "gpt-4",
			SystemPrompt: "You are a multimodal test agent.",
			Enabled:      true,
		},
	})
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	frame := buildUserTurnFrame(sessionID, profileName, "hello multimodal", buildImageFrame(sessionID))
	writeWSFrame(t, conn, frame)

	thinkingFrame := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return frameHasThinking(f)
	})
	if thinkingFrame == nil {
		t.Fatal("did not receive thinking frame for text+image turn")
	}
	if !strings.Contains(frameThinking(thinkingFrame), expectedGreetingReasoning) {
		t.Errorf("thinking = %q, want to contain %q",
			frameThinking(thinkingFrame), expectedGreetingReasoning)
	}

	textFrame := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return frameHasText(f)
	})
	if textFrame == nil {
		t.Fatal("did not receive text frame for text+image turn")
	}
	if !strings.Contains(frameText(textFrame), expectedGreetingText) {
		t.Errorf("text = %q, want to contain %q",
			frameText(textFrame), expectedGreetingText)
	}
}

// TestAgentMultimodalImageOnlyTurn verifies that a content frame containing
// ONLY an ImagePart (empty TextPart) is accepted by the server. Because the
// text is empty, fake-llm cannot keyword-match and returns a random template —
// the test only verifies that the server processes the frame and returns a
// response (thinking, text, or warn) without error.
func TestAgentMultimodalImageOnlyTurn(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("mm-img-%s", uniqueSuffix())

	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		Parent:         gameconst.PromptsParent,
		AgentProfileId: profileName,
		AgentProfile: &game.AgentProfile{
			Model:        "gpt-4",
			SystemPrompt: "You are a multimodal test agent.",
			Enabled:      true,
		},
	})
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	frame := buildUserTurnFrame(sessionID, profileName, "", buildImageFrame(sessionID))
	writeWSFrame(t, conn, frame)

	respFrame := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return frameHasThinking(f) || frameHasText(f) || frameWarn(f) != nil
	})
	if respFrame == nil {
		t.Fatal("did not receive any response frame for image-only turn")
	}
	switch {
	case frameWarn(respFrame) != nil:
		t.Logf("warn (acceptable for empty-text turn): %q", frameWarn(respFrame).GetMessage())
	case frameHasText(respFrame):
		t.Logf("text response received: %q", frameText(respFrame))
	case frameHasThinking(respFrame):
		t.Logf("thinking response received: %q", frameThinking(respFrame))
	}
}
