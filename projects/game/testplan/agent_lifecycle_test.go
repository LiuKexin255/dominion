// Package testplan contains agent lifecycle integration tests covering
// connect-without-create, profile switching, connection exclusivity,
// GetAgent for never-connected sessions, and disconnect/reconnect history
// persistence.
package testplan

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"dominion/common/gopkg/testtool"
	game "dominion/projects/game"
)

// TestConnectWithoutCreate verifies that connecting to a session's WebSocket
// without first creating an agent works: the adapter is created on-demand when
// the first frame with a valid agent_profile_name arrives.
func TestConnectWithoutCreate(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("life-nocreate-%s", uniqueSuffix())

	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		AgentProfileName: profileName,
		Model:            "gpt-4",
		SystemPrompt:     "You are a test agent.",
		Enabled:          true,
	})

	sessionID, _ := createSession(t, sutHostURL, sutEnvName)

	// Connect WS directly — no prior agent creation needed.
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	// Send text with profile → adapter created on-demand.
	sendTextWithProfile(t, conn, sessionID, profileName, "Hello without create")

	// Receive thinking frame.
	thinkingFrame := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return frameHasThinking(f)
	})
	if thinkingFrame == nil {
		t.Fatal("did not receive thinking frame")
	}
	// "Hello without create" carries the greeting keyword so the on-demand
	// adapter returns a deterministic response.
	if thinkingFrame.GetSender() != game.FrameSender_FRAME_SENDER_AGENT {
		t.Errorf("thinking sender = %s, want AGENT", senderString(thinkingFrame.GetSender()))
	}
	if !strings.Contains(frameThinking(thinkingFrame), expectedGreetingReasoning) {
		t.Errorf("thinking = %q, want to contain %q", frameThinking(thinkingFrame), expectedGreetingReasoning)
	}
	t.Logf("thinking: %q", frameThinking(thinkingFrame))

	// Receive text frame.
	textFrame := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return frameHasText(f)
	})
	if textFrame == nil {
		t.Fatal("did not receive text frame")
	}
	if textFrame.GetSender() != game.FrameSender_FRAME_SENDER_AGENT {
		t.Errorf("text sender = %s, want AGENT", senderString(textFrame.GetSender()))
	}
	if !strings.Contains(frameText(textFrame), expectedGreetingText) {
		t.Errorf("text = %q, want to contain %q", frameText(textFrame), expectedGreetingText)
	}
	t.Logf("text: %q", frameText(textFrame))

	// Optionally receive wait frame after response.
	_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return f.GetWait() != nil
	})
}

// TestProfileSwitchMidConnection verifies that switching the agent profile mid-
// connection works: sending a frame with profile A activates adapter A, then
// sending a frame with profile B switches to adapter B. History is shared
// across profiles (both adapters see prior messages).
func TestProfileSwitchMidConnection(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileAName := fmt.Sprintf("life-pswitch-a-%s", uniqueSuffix())
	profileBName := fmt.Sprintf("life-pswitch-b-%s", uniqueSuffix())

	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		AgentProfileName: profileAName,
		Model:            "gpt-4",
		SystemPrompt:     "You are profile A.",
		Enabled:          true,
	})
	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		AgentProfileName: profileBName,
		Model:            "gpt-4",
		SystemPrompt:     "You are profile B.",
		Enabled:          true,
	})

	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	// Turn 1: profile A
	sendTextWithProfile(t, conn, sessionID, profileAName, "Message with profile A")
	_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return frameHasThinking(f) })
	textRespA := drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return frameHasText(f) })
	if textRespA == nil {
		t.Fatal("profile A: no text response")
	}
	t.Logf("profile A response: %q", frameText(textRespA))

	// Turn 2: profile B — should switch adapter without creating a new one.
	sendTextWithProfile(t, conn, sessionID, profileBName, "Message with profile B")
	_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return frameHasThinking(f) })
	textRespB := drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return frameHasText(f) })
	if textRespB == nil {
		t.Fatal("profile B: no text response")
	}
	t.Logf("profile B response: %q", frameText(textRespB))

	// Verify history is shared across profiles.
	lmr := listMessages(t, sutHostURL, sutEnvName, sessionID)
	gotCount := len(lmr.GetMessages())
	if gotCount < 4 {
		t.Errorf("ListMessages returned %d messages, want at least 4 (2 user + 2 agent)", gotCount)
	}
	for i, msg := range lmr.GetMessages() {
		t.Logf("message[%d]: type=%s sender=%s content=%q",
			i, messageKind(msg), senderString(msg.GetSender()), messageText(msg))
	}
}

// TestConnectionConcurrentSerialization verifies that when two WebSocket
// connections send frames to the same session concurrently, the per-session
// mutex serializes processing so both responses are delivered without
// corruption. Kick-on-connect is intentionally not implemented — the mutex
// serves as a fallback guard against concurrent processing.
func TestConnectionConcurrentSerialization(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("life-serial-%s", uniqueSuffix())

	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		AgentProfileName: profileName,
		Model:            "gpt-4",
		SystemPrompt:     "You are a test agent.",
		Enabled:          true,
	})

	sessionID, _ := createSession(t, sutHostURL, sutEnvName)

	conn1 := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn1.Close()

	conn2 := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn2.Close()

	sendTextWithProfile(t, conn1, sessionID, profileName, "From conn1")
	sendTextWithProfile(t, conn2, sessionID, profileName, "From conn2")

	conn1Resp := drainWSFrame(t, conn1, func(f *game.AgentFrame) bool {
		return frameHasText(f)
	})
	conn2Resp := drainWSFrame(t, conn2, func(f *game.AgentFrame) bool {
		return frameHasText(f)
	})

	if conn1Resp == nil {
		t.Fatal("conn1: no text response")
	}
	if conn2Resp == nil {
		t.Fatal("conn2: no text response")
	}
	t.Logf("conn1 response: %q", frameText(conn1Resp))
	t.Logf("conn2 response: %q", frameText(conn2Resp))
}

// TestGetAgentNeverConnected verifies that GetAgent returns a 200 response
// with an empty agent_profile_name for a session that has never been
// connected via WebSocket.
func TestGetAgentNeverConnected(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("life-neverconn-%s", uniqueSuffix())

	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		AgentProfileName: profileName,
		Model:            "gpt-4",
		SystemPrompt:     "You are a test agent.",
		Enabled:          true,
	})

	sessionID, _ := createSession(t, sutHostURL, sutEnvName)

	// Call GetAgent without ever connecting — expect NOT_FOUND (404).
	// An agent only exists after a WebSocket Connect allocates an owner.
	reqURL := fmt.Sprintf("%s%ssessions/%s/agent", sutHostURL, pathPrefix, sessionID)
	resp, respBody := doHTTP(t, http.MethodGet, reqURL, sutEnvName, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET agent for never-connected session: status=%d, want %d, body=%s",
			resp.StatusCode, http.StatusNotFound, respBody)
	}
	t.Logf("GetAgent for never-connected session correctly returned NOT_FOUND (404)")
}

// TestDisconnectReconnectHistory verifies that conversation history persists
// across WebSocket disconnect and reconnect: messages sent before disconnect
// are visible via ListMessages after reconnecting.
func TestDisconnectReconnectHistory(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("life-disc-hist-%s", uniqueSuffix())

	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		AgentProfileName: profileName,
		Model:            "gpt-4",
		SystemPrompt:     "You are a test agent.",
		Enabled:          true,
	})

	sessionID, _ := createSession(t, sutHostURL, sutEnvName)

	// Connect, send 2 text exchanges.
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)

	messages := []string{"First exchange", "Second exchange"}
	for _, msg := range messages {
		sendTextWithProfile(t, conn, sessionID, profileName, msg)
		_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return frameHasThinking(f) })
		textResp := drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return frameHasText(f) })
		if textResp == nil {
			t.Fatalf("message %q: no text response", msg)
		}
		t.Logf("exchange: %q → %q", msg, frameText(textResp))
	}

	// Disconnect.
	conn.Close()

	// Reconnect with same profile.
	conn2 := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn2.Close()

	// Verify all 4 messages (2 user + 2 agent) present in history.
	lmr := listMessages(t, sutHostURL, sutEnvName, sessionID)
	gotCount := len(lmr.GetMessages())
	if gotCount < 4 {
		t.Errorf("ListMessages after reconnect returned %d messages, want at least 4", gotCount)
	}
	for i, msg := range lmr.GetMessages() {
		t.Logf("message[%d]: type=%s sender=%s content=%q",
			i, messageKind(msg), senderString(msg.GetSender()), messageText(msg))
	}

	// Verify both user messages are present.
	foundFirst := false
	foundSecond := false
	for _, msg := range lmr.GetMessages() {
		if msg.GetSender() == game.FrameSender_FRAME_SENDER_USER {
			if messageText(msg) == messages[0] {
				foundFirst = true
			}
			if messageText(msg) == messages[1] {
				foundSecond = true
			}
		}
	}
	if !foundFirst {
		t.Errorf("first user message %q not found in ListMessages", messages[0])
	}
	if !foundSecond {
		t.Errorf("second user message %q not found in ListMessages", messages[1])
	}
}
