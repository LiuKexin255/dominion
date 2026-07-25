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
	"dominion/projects/game/pkg/gameconst"
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
		Parent:         gameconst.PromptsParent,
		AgentProfileId: profileName,
		AgentProfile: &game.AgentProfile{
			Model:        "gpt-4",
			SystemPrompt: "You are a test agent.",
			Enabled:      true,
		},
	})
	if profile.GetName() != "prompts/agentProfiles/"+profileName {
		t.Errorf("profile name = %q, want %q", profile.GetName(), "prompts/agentProfiles/"+profileName)
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
// send a content text frame → receive thinking frame → receive text frame
// → verify FrameSender.AGENT on response frames.
func TestAgentDialogTextToResponse(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("ad-ttr-%s", uniqueSuffix())

	// Setup
	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		Parent:         gameconst.PromptsParent,
		AgentProfileId: profileName,
		AgentProfile: &game.AgentProfile{
			Model:        "gpt-4",
			SystemPrompt: "You are a test agent.",
			Enabled:      true,
		},
	})
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	// Send a content frame carrying a TextPart with sender=USER.
	sendText := "Hello, agent!"
	textFrame := buildTextFrame(sessionID, profileName, sendText, game.FrameSender_FRAME_SENDER_USER)
	writeWSFrame(t, conn, textFrame)

	// Receive thinking frame
	thinkingFrame := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return frameHasThinking(f)
	})
	if thinkingFrame == nil {
		t.Fatal("did not receive thinking frame")
	}
	// "Hello, agent!" carries the greeting keyword "hello" so fake-llm
	// deterministically returns the greeting template (see README §4).
	if thinkingFrame.GetSender() != game.FrameSender_FRAME_SENDER_AGENT {
		t.Errorf("thinking sender = %s, want AGENT", senderString(thinkingFrame.GetSender()))
	}
	if !strings.Contains(frameThinking(thinkingFrame), expectedGreetingReasoning) {
		t.Errorf("thinking = %q, want to contain %q", frameThinking(thinkingFrame), expectedGreetingReasoning)
	}
	t.Logf("thinking: %q (sender=%s)", frameThinking(thinkingFrame), senderString(thinkingFrame.GetSender()))

	// Receive text frame
	textRespFrame := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return frameHasText(f)
	})
	if textRespFrame == nil {
		t.Fatal("did not receive text frame")
	}
	if textRespFrame.GetSender() != game.FrameSender_FRAME_SENDER_AGENT {
		t.Errorf("text sender = %s, want AGENT", senderString(textRespFrame.GetSender()))
	}
	if !strings.Contains(frameText(textRespFrame), expectedGreetingText) {
		t.Errorf("text = %q, want to contain %q", frameText(textRespFrame), expectedGreetingText)
	}
	t.Logf("text: %q (sender=%s)", frameText(textRespFrame), senderString(textRespFrame.GetSender()))
}

// TestAgentDialogThinkingBeforeText verifies that the thinking frame arrives
// before the text frame — the ordering guarantee from the handler.
func TestAgentDialogThinkingBeforeText(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("ad-tbt-%s", uniqueSuffix())

	// Setup
	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		Parent:         gameconst.PromptsParent,
		AgentProfileId: profileName,
		AgentProfile: &game.AgentProfile{
			Model:        "gpt-4",
			SystemPrompt: "You are a test agent.",
			Enabled:      true,
		},
	})
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	// Send text carrying the greeting keyword so the response is deterministic.
	textFrame := buildTextFrame(sessionID, profileName, "Hello ordering test", game.FrameSender_FRAME_SENDER_USER)
	writeWSFrame(t, conn, textFrame)

	// Read frames in order — first must be thinking, second must be text
	frame1 := readWSFrame(t, conn)
	if !frameHasThinking(frame1) {
		t.Fatal("frame 1: expected thinking, got something else")
	}
	if !strings.Contains(frameThinking(frame1), expectedGreetingReasoning) {
		t.Errorf("frame 1 thinking = %q, want to contain %q", frameThinking(frame1), expectedGreetingReasoning)
	}
	frame2 := readWSFrame(t, conn)
	if !frameHasText(frame2) {
		t.Fatal("frame 2: expected text, got something else")
	}
	if !strings.Contains(frameText(frame2), expectedGreetingText) {
		t.Errorf("frame 2 text = %q, want to contain %q", frameText(frame2), expectedGreetingText)
	}

	t.Logf("frame 1 thinking: %q", frameThinking(frame1))
	t.Logf("frame 2 text: %q", frameText(frame2))
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
		Parent:         gameconst.PromptsParent,
		AgentProfileId: profileName,
		AgentProfile: &game.AgentProfile{
			Model:        "gpt-4",
			SystemPrompt: "You are a test agent.",
			Enabled:      true,
		},
	})
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	// "Hello world" carries the greeting keyword "hello".
	textFrame := buildTextFrame(sessionID, profileName, "Hello world", game.FrameSender_FRAME_SENDER_USER)
	writeWSFrame(t, conn, textFrame)

	// Read and verify thinking content
	thinkingFrame := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return frameHasThinking(f)
	})
	if thinkingFrame == nil {
		t.Fatal("did not receive thinking frame")
	}
	if !strings.Contains(frameThinking(thinkingFrame), expectedGreetingReasoning) {
		t.Errorf("thinking = %q, want to contain %q", frameThinking(thinkingFrame), expectedGreetingReasoning)
	}

	// Read and verify text content
	textRespFrame := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return frameHasText(f)
	})
	if textRespFrame == nil {
		t.Fatal("did not receive text frame")
	}
	if !strings.Contains(frameText(textRespFrame), expectedGreetingText) {
		t.Errorf("text = %q, want to contain %q", frameText(textRespFrame), expectedGreetingText)
	}
}

// TestAgentDialogMessageContentDisplayOnly verifies spec 023 FR-005 in the
// dialog module: after a plain text turn, ListMessages returns Messages
// whose content.parts carry ONLY display-only MessagePart kinds
// (text/thinking/image/toolCall/toolResult) — no FlowPart (mouse/keyboard
// operation or wait/warn/status signal) ever appears in Message.content.
// The content-model split is structural (Message.content is typed
// MessageParts), so this test guards a future regression that reintroduces
// an operation-shaped entry. It reuses the shared helpers in helpers_test.go
// (style/large_test.md §反模式3 — do not copy helpers).
func TestAgentDialogMessageContentDisplayOnly(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("ad-DispOnly-%s", uniqueSuffix())

	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		Parent:         gameconst.PromptsParent,
		AgentProfileId: profileName,
		AgentProfile: &game.AgentProfile{
			Model:        "gpt-4",
			SystemPrompt: "You are a test agent.",
			Enabled:      true,
		},
	})
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	// Run a text turn carrying the greeting keyword so the response is
	// deterministic. The wait FlowPart the agent emits at turn end is a
	// flow_parts frame on the live socket (control channel); it MUST NOT
	// be reconstructed into any Message.content.
	sendTextWithProfile(t, conn, sessionID, profileName, "Hello display-only test")
	_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return frameHasThinking(f) })
	_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return frameHasText(f) })
	// Drain the terminal wait FlowPart so the turn settles before listing.
	_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return frameWait(f) != nil })

	// then: ListMessages returns Messages whose content is display-only.
	lmr := listMessages(t, sutHostURL, sutEnvName, sessionID)
	assertMessageContentDisplayOnly(t, lmr.GetMessages())

	// Sanity: the user text survived in history (a regression that
	// dropped text would silently pass the display-only guard).
	if !messagesContainText(lmr.GetMessages(), "Hello display-only test") {
		t.Errorf("ListMessages did not surface the user text 'Hello display-only test' — history reconstruction regressed")
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
		Parent:         gameconst.PromptsParent,
		AgentProfileId: profileName,
		AgentProfile: &game.AgentProfile{
			Model:        "gpt-4",
			SystemPrompt: "You are a test agent.",
			Enabled:      true,
		},
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
			return frameHasThinking(f)
		})
		textFrame := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
			return frameHasText(f)
		})
		if textFrame == nil {
			t.Fatalf("turn %d: did not receive text response frame", i)
		}
		if !strings.Contains(frameText(textFrame), want) {
			t.Errorf("response %d = %q, want to contain %q (FIFO order violated)", i, frameText(textFrame), want)
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
		Parent:         gameconst.PromptsParent,
		AgentProfileId: profileName,
		AgentProfile: &game.AgentProfile{
			Model:        "gpt-4",
			SystemPrompt: "You are a test agent.",
			Enabled:      true,
		},
	})
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)

	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	// Turn before deletion carries the greeting keyword.
	sendTextWithProfile(t, conn, sessionID, profileName, "Hello before delete")
	_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return frameHasThinking(f) })
	firstResp := drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return frameHasText(f) })
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
		return frameHasText(f)
	})
	if textRespFrame == nil {
		t.Fatal("did not receive text response after profile deletion")
	}
	if textRespFrame.GetSender() != game.FrameSender_FRAME_SENDER_AGENT {
		t.Errorf("response sender = %s, want AGENT", senderString(textRespFrame.GetSender()))
	}
	if !strings.Contains(frameText(textRespFrame), expectedFarewellText) {
		t.Errorf("response text = %q, want to contain %q", frameText(textRespFrame), expectedFarewellText)
	}
}
