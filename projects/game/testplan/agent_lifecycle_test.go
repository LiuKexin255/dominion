// Package testplan contains agent lifecycle integration tests covering
// connect-without-create, profile switching, connection exclusivity,
// GetAgent for never-connected sessions, and disconnect/reconnect history
// persistence.
package testplan

import (
	"fmt"
	"testing"
	"time"

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
		return f.GetThinking() != nil
	})
	if thinkingFrame == nil {
		t.Fatal("did not receive thinking frame")
	}
	if thinkingFrame.GetSender() != game.FrameSender_FRAME_SENDER_AGENT {
		t.Errorf("thinking sender = %s, want AGENT", senderString(thinkingFrame.GetSender()))
	}
	t.Logf("thinking: %q", thinkingFrame.GetThinking().GetContent())

	// Receive text frame.
	textFrame := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return f.GetText() != nil
	})
	if textFrame == nil {
		t.Fatal("did not receive text frame")
	}
	if textFrame.GetSender() != game.FrameSender_FRAME_SENDER_AGENT {
		t.Errorf("text sender = %s, want AGENT", senderString(textFrame.GetSender()))
	}
	t.Logf("text: %q", textFrame.GetText().GetContent())

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
	_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return f.GetThinking() != nil })
	textRespA := drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return f.GetText() != nil })
	if textRespA == nil {
		t.Fatal("profile A: no text response")
	}
	t.Logf("profile A response: %q", textRespA.GetText().GetContent())

	// Turn 2: profile B — should switch adapter without creating a new one.
	sendTextWithProfile(t, conn, sessionID, profileBName, "Message with profile B")
	_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return f.GetThinking() != nil })
	textRespB := drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return f.GetText() != nil })
	if textRespB == nil {
		t.Fatal("profile B: no text response")
	}
	t.Logf("profile B response: %q", textRespB.GetText().GetContent())

	// Verify history is shared across profiles.
	lmr := listMessages(t, sutHostURL, sutEnvName, sessionID)
	gotCount := len(lmr.GetMessages())
	if gotCount < 4 {
		t.Errorf("ListMessages returned %d messages, want at least 4 (2 user + 2 agent)", gotCount)
	}
	for i, msg := range lmr.GetMessages() {
		t.Logf("message[%d]: type=%s sender=%s content=%q",
			i, msg.GetType(), senderString(msg.GetSender()), msg.GetContent())
	}
}

// TestConnectionExclusivity verifies that when a second WebSocket connects to
// the same session, the first connection is kicked (closed). Only one
// connection per session is permitted.
func TestConnectionExclusivity(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("life-excl-%s", uniqueSuffix())

	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		AgentProfileName: profileName,
		Model:            "gpt-4",
		SystemPrompt:     "You are a test agent.",
		Enabled:          true,
	})

	sessionID, _ := createSession(t, sutHostURL, sutEnvName)

	// Connect first WS and start a message (triggers streaming).
	conn1 := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	sendTextWithProfile(t, conn1, sessionID, profileName, "First connection message")

	// Connect second WS — this should kick conn1.
	conn2 := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn2.Close()

	// conn1 should be closed (kicked) — read should return error.
	conn1.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, _, err := conn1.ReadMessage()
	if err == nil {
		conn1.Close()
		t.Fatal("conn1 was not kicked after conn2 connected")
	}
	conn1.Close()
	t.Logf("conn1 kicked as expected: %v", err)

	// conn2 should still be functional.
	sendTextWithProfile(t, conn2, sessionID, profileName, "Second connection message")
	_ = drainWSFrame(t, conn2, func(f *game.AgentFrame) bool { return f.GetThinking() != nil })
	textResp := drainWSFrame(t, conn2, func(f *game.AgentFrame) bool { return f.GetText() != nil })
	if textResp == nil {
		t.Fatal("conn2: no text response after kicking conn1")
	}
	t.Logf("conn2 response: %q", textResp.GetText().GetContent())
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

	// Call GetAgent without ever connecting — expect 200 with empty profile.
	agent := getAgent(t, sutHostURL, sutEnvName, sessionID)
	if agent.GetSessionId() != sessionID {
		t.Errorf("session_id = %q, want %q", agent.GetSessionId(), sessionID)
	}
	if agent.GetAgentProfileName() != "" {
		t.Errorf("agent_profile_name = %q, want empty (never connected)", agent.GetAgentProfileName())
	}
	t.Logf("GetAgent for never-connected session: session_id=%q, agent_profile_name=%q",
		agent.GetSessionId(), agent.GetAgentProfileName())
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
		_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return f.GetThinking() != nil })
		textResp := drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return f.GetText() != nil })
		if textResp == nil {
			t.Fatalf("message %q: no text response", msg)
		}
		t.Logf("exchange: %q → %q", msg, textResp.GetText().GetContent())
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
			i, msg.GetType(), senderString(msg.GetSender()), msg.GetContent())
	}

	// Verify both user messages are present.
	foundFirst := false
	foundSecond := false
	for _, msg := range lmr.GetMessages() {
		if msg.GetSender() == game.FrameSender_FRAME_SENDER_USER {
			if msg.GetContent() == messages[0] {
				foundFirst = true
			}
			if msg.GetContent() == messages[1] {
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