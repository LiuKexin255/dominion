// Package testplan contains agent dialog integration tests.
// These tests validate the agent's text dialog capability through the
// gateway HTTP + WebSocket surface, using the fake LLM test artifact
// that returns deterministic responses.
package testplan

import (
	"fmt"
	"net/http"
	"strings"
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
	// "Hello, agent!" carries the greeting keyword "hello" so fake-llm
	// deterministically returns the greeting template (see README §4).
	if thinkingFrame.GetSender() != game.FrameSender_FRAME_SENDER_AGENT {
		t.Errorf("thinking sender = %s, want AGENT", senderString(thinkingFrame.GetSender()))
	}
	if !strings.Contains(thinkingFrame.GetThinking().GetContent(), expectedGreetingReasoning) {
		t.Errorf("thinking = %q, want to contain %q", thinkingFrame.GetThinking().GetContent(), expectedGreetingReasoning)
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
	if !strings.Contains(textRespFrame.GetText().GetContent(), expectedGreetingText) {
		t.Errorf("text = %q, want to contain %q", textRespFrame.GetText().GetContent(), expectedGreetingText)
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

	// Send text carrying the greeting keyword so the response is deterministic.
	textFrame := &game.AgentFrame{
		SessionId:        sessionID,
		AgentProfileName: profileName,
		Payload: &game.AgentFrame_Text{
			Text: &game.AgentTextFrame{Content: "Hello ordering test"},
		},
		Sender: game.FrameSender_FRAME_SENDER_USER,
	}
	writeWSFrame(t, conn, textFrame)

	// Read frames in order — first must be thinking, second must be text
	frame1 := readWSFrame(t, conn)
	if frame1.GetThinking() == nil {
		t.Fatal("frame 1: expected thinking, got something else")
	}
	if !strings.Contains(frame1.GetThinking().GetContent(), expectedGreetingReasoning) {
		t.Errorf("frame 1 thinking = %q, want to contain %q", frame1.GetThinking().GetContent(), expectedGreetingReasoning)
	}
	frame2 := readWSFrame(t, conn)
	if frame2.GetText() == nil {
		t.Fatal("frame 2: expected text, got something else")
	}
	if !strings.Contains(frame2.GetText().GetContent(), expectedGreetingText) {
		t.Errorf("frame 2 text = %q, want to contain %q", frame2.GetText().GetContent(), expectedGreetingText)
	}

	t.Logf("frame 1 thinking: %q", frame1.GetThinking().GetContent())
	t.Logf("frame 2 text: %q", frame2.GetText().GetContent())
}

// TestAgentDialogDeterministicContent verifies that fake-llm returns the
// template-matched content deterministically: a prompt carrying the greeting
// keyword yields the greeting reasoning + text from the embedded testdata.
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

	// "Hello world" carries the greeting keyword "hello".
	textFrame := &game.AgentFrame{
		SessionId:        sessionID,
		AgentProfileName: profileName,
		Payload: &game.AgentFrame_Text{
			Text: &game.AgentTextFrame{Content: "Hello world"},
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
	if !strings.Contains(thinkingFrame.GetThinking().GetContent(), expectedGreetingReasoning) {
		t.Errorf("thinking = %q, want to contain %q", thinkingFrame.GetThinking().GetContent(), expectedGreetingReasoning)
	}

	// Read and verify text content
	textRespFrame := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return f.GetText() != nil
	})
	if textRespFrame == nil {
		t.Fatal("did not receive text frame")
	}
	if !strings.Contains(textRespFrame.GetText().GetContent(), expectedGreetingText) {
		t.Errorf("text = %q, want to contain %q", textRespFrame.GetText().GetContent(), expectedGreetingText)
	}
}

// TestAgentDialogFIFOQueue verifies that sending 3 messages in rapid
// succession yields responses in FIFO order. Because fake-llm matches by
// keyword, each turn is made to carry a DISTINCT keyword backed by a DISTINCT
// template so the response identity proves the processing order: greeting,
// farewell, greeting.
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

	// Each message triggers a different template via a distinct keyword so the
	// response text proves which input was processed.
	messages := []string{
		"hello world",   // greeting
		"goodbye world", // farewell
		"hi friend",     // greeting again (hi is a greeting keyword)
	}
	for _, msg := range messages {
		sendTextWithProfile(t, conn, sessionID, profileName, msg)
	}

	wantTexts := []string{expectedGreetingText, expectedFarewellText, expectedGreetingText}

	for i, want := range wantTexts {
		_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
			return f.GetThinking() != nil
		})
		textFrame := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
			return f.GetText() != nil
		})
		if textFrame == nil {
			t.Fatalf("turn %d: did not receive text response frame", i)
		}
		if !strings.Contains(textFrame.GetText().GetContent(), want) {
			t.Errorf("response %d = %q, want to contain %q (FIFO order violated)", i, textFrame.GetText().GetContent(), want)
		}
	}
}

// TestAgentDialogDeleteProfileStillResponds verifies the loose coupling
// design: after the adapter is bound, deleting the agent profile does not
// prevent subsequent messages from being processed, because profile data
// was copied at adapter creation time.
func TestAgentDialogDeleteProfileStillResponds(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("ad-delp-%s", uniqueSuffix())

	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		AgentProfileName: profileName,
		Model:            "gpt-4",
		SystemPrompt:     "You are a test agent.",
		Enabled:          true,
	})
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)

	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	// Turn before deletion carries the greeting keyword.
	sendTextWithProfile(t, conn, sessionID, profileName, "Hello before delete")
	_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return f.GetThinking() != nil })
	firstResp := drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return f.GetText() != nil })
	if firstResp == nil {
		t.Fatal("no response before profile deletion")
	}

	delStatus := deleteAgentProfile(t, sutHostURL, sutEnvName, profileName)
	if delStatus != http.StatusOK && delStatus != http.StatusNoContent {
		t.Fatalf("DELETE profile status = %d, want 200 or 204", delStatus)
	}

	// Turn after deletion carries the farewell keyword so the content assertion
	// is deterministic (no random fallback).
	sendTextWithProfile(t, conn, sessionID, profileName, "Goodbye after delete")

	textRespFrame := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return f.GetText() != nil
	})
	if textRespFrame == nil {
		t.Fatal("did not receive text response after profile deletion")
	}
	if textRespFrame.GetSender() != game.FrameSender_FRAME_SENDER_AGENT {
		t.Errorf("response sender = %s, want AGENT", senderString(textRespFrame.GetSender()))
	}
	if !strings.Contains(textRespFrame.GetText().GetContent(), expectedFarewellText) {
		t.Errorf("response text = %q, want to contain %q", textRespFrame.GetText().GetContent(), expectedFarewellText)
	}
}
