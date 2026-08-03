// Package testplan contains agent multimodal-turn integration tests.
// These tests validate the team's player-agent processing of message_parts
// payloads carrying text and/or an ImagePart through the WebSocket surface,
// using the fake-llm test artifact for deterministic responses. Each test
// sets up the team stack via setupTeamSession (session → saolei TeamProfile
// → CreateTeam) before connecting — CreateTeam MUST precede Connect (no lazy
// creation, spec 031-team-template-mode FR-033).
//
// spec 025 coverage: TestAgentMultimodalLargeImageRoundTrip proves a frame
// whose encoded size exceeds the pre-025 `coder/websocket` default ReadLimit
// (32 KiB) round-trips intact over the binary-protobuf WS leg (FR-007/FR-008/
// FR-010) — under the old protojson-text + 32 KiB regime it would have torn
// the session down with `ErrMessageTooBig`.
package testplan

import (
	"fmt"
	"strings"
	"testing"

	"dominion/common/gopkg/testtool"
	game "dominion/projects/game"
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

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	frame := buildUserTurnFrame(sessionID, "hello multimodal", buildImageFrame(sessionID))
	writeWSFrame(t, conn, frame)

	thinkingFrame := drainWSFrame(t, conn, func(f *game.TeamFrame) bool {
		return frameHasThinking(f)
	})
	if thinkingFrame == nil {
		t.Fatal("did not receive thinking frame for text+image turn")
	}
	if !strings.Contains(frameThinking(thinkingFrame), expectedGreetingReasoning) {
		t.Errorf("thinking = %q, want to contain %q",
			frameThinking(thinkingFrame), expectedGreetingReasoning)
	}

	textFrame := drainWSFrame(t, conn, func(f *game.TeamFrame) bool {
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

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	frame := buildUserTurnFrame(sessionID, "", buildImageFrame(sessionID))
	writeWSFrame(t, conn, frame)

	respFrame := drainWSFrame(t, conn, func(f *game.TeamFrame) bool {
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

// TestAgentMultimodalLargeImageRoundTrip verifies spec 025 FR-007/FR-008/
// FR-010: a user-turn frame whose ImagePart.data exceeds the pre-025
// `coder/websocket` default ReadLimit of 32 KiB is delivered intact over the
// binary-protobuf WS leg (desktop↔gateway), processed by the agent, and
// answered — with no `ErrMessageTooBig` / WS teardown. Under the old
// protojson-text + 32 KiB read-limit regime this frame would have closed the
// connection (image-transport-contract.md §3).
//
// The image is a high-entropy 128×128 PNG (largeScreenshotData, encoded size
// > 32 KiB) — the pixel content is irrelevant (fake-LLM ignores image bytes,
// only text drives keyword matching); the assertion is structural: the frame
// is accepted and a normal thinking/text response follows.
func TestAgentMultimodalLargeImageRoundTrip(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	// given: the large PNG exceeds the pre-025 32 KiB WS default read limit.
	if got := len(largeScreenshotData); got <= 32*1024 {
		t.Fatalf("test bug: largeScreenshotData is only %d bytes (≤ 32 KiB); the large-image round-trip requires an encoded size above the pre-025 coder/websocket default ReadLimit", got)
	}

	profileName := fmt.Sprintf("mm-large-%s", uniqueSuffix())

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// when: a user turn carries text + the large image. The text carries the
	// "hello" keyword so fake-LLM deterministically returns the greeting
	// template, proving the large frame was processed end-to-end (not dropped
	// or rejected by the WS layer).
	frame := buildUserTurnFrame(sessionID, "hello large image", buildLargeImageFrame(sessionID))
	writeWSFrame(t, conn, frame)

	// then: the agent processes the large frame and returns the greeting
	// thinking + text — the connection survived a > 32 KiB binary-proto frame
	// (spec 025 FR-007/FR-010). Under the old regime readWSFrame would have
	// failed with a WS close error before any response arrived.
	thinkingFrame := drainWSFrame(t, conn, func(f *game.TeamFrame) bool {
		return frameHasThinking(f)
	})
	if thinkingFrame == nil {
		t.Fatal("did not receive a thinking frame for the large-image turn — the > 32 KiB frame may have torn the WS session (spec 025 FR-007 regression)")
	}
	if !strings.Contains(frameThinking(thinkingFrame), expectedGreetingReasoning) {
		t.Errorf("thinking = %q, want to contain %q",
			frameThinking(thinkingFrame), expectedGreetingReasoning)
	}

	textFrame := drainWSFrame(t, conn, func(f *game.TeamFrame) bool {
		return frameHasText(f)
	})
	if textFrame == nil {
		t.Fatal("did not receive a text frame for the large-image turn — the agent did not complete the turn after the large frame")
	}
	if !strings.Contains(frameText(textFrame), expectedGreetingText) {
		t.Errorf("text = %q, want to contain %q",
			frameText(textFrame), expectedGreetingText)
	}
}
