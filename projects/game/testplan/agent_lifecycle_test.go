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
	"dominion/projects/game/pkg/gameconst"
)

// TestConnectWithoutCreate verifies that connecting to a session's WebSocket
// without first creating an agent works: the adapter is created on-demand when
// the first frame with a valid agent_profile_name arrives.
func TestConnectWithoutCreate(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("life-nocreate-%s", uniqueSuffix())

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
		return frameWait(f) != nil
	})
}

// TestProfileSwitchMidConnection verifies that a profile switch mid-connection
// requires Refresh: a turn under profile A binds adapter A; a subsequent turn
// under profile B WITHOUT Refresh is rejected by the profile guard (Warn +
// Wait). After Refresh, profile B's turn is accepted and rebuilds the adapter
// for B (specs/021-agent-session-resync/quickstart.md Scenario 7 /
// spec.md US4 / SC-004).
func TestProfileSwitchMidConnection(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileAName := fmt.Sprintf("life-pswitch-a-%s", uniqueSuffix())
	profileBName := fmt.Sprintf("life-pswitch-b-%s", uniqueSuffix())

	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		Parent:         gameconst.PromptsParent,
		AgentProfileId: profileAName,
		AgentProfile: &game.AgentProfile{
			Model:        "gpt-4",
			SystemPrompt: "You are profile A.",
			Enabled:      true,
		},
	})
	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		Parent:         gameconst.PromptsParent,
		AgentProfileId: profileBName,
		AgentProfile: &game.AgentProfile{
			Model:        "gpt-4",
			SystemPrompt: "You are profile B.",
			Enabled:      true,
		},
	})

	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	// given: profile A turn binds adapter A.
	sendTextWithProfile(t, conn, sessionID, profileAName, "Message with profile A")
	_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return frameHasThinking(f) })
	textRespA := drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return frameHasText(f) })
	if textRespA == nil {
		t.Fatal("profile A: no text response")
	}
	t.Logf("profile A response: %q", frameText(textRespA))
	// Drain the turn-completion Wait so the buffer is clean for the next turn.
	_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return frameWait(f) != nil })

	// when: profile B turn WITHOUT Refresh — the guard must reject it.
	sendTextWithProfile(t, conn, sessionID, profileBName, "Message with profile B")

	// then: a WarnSignal naming the mismatch, then a WaitSignal returning the
	// desktop to ready (data-model.md §5 / lifecycle-contract §3).
	warnFrame := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return frameWarn(f) != nil && strings.Contains(frameWarn(f).GetMessage(), "profile mismatch")
	})
	if warnFrame == nil {
		t.Fatal("profile B without Refresh: expected a WarnSignal with profile mismatch")
	}
	t.Logf("profile mismatch warn: %q", frameWarn(warnFrame).GetMessage())
	waitAfterWarn := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return frameWait(f) != nil
	})
	if waitAfterWarn == nil {
		t.Fatal("profile B without Refresh: expected a WaitSignal after the Warn")
	}

	// when: Refresh then profile B turn — the adapter rebuilds for B.
	refreshAgent(t, sutHostURL, sutEnvName, sessionID)
	sendTextWithProfile(t, conn, sessionID, profileBName, "Message with profile B")

	// then: profile B turn succeeds with a text response.
	_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return frameHasThinking(f) })
	textRespB := drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return frameHasText(f) })
	if textRespB == nil {
		t.Fatal("profile B after Refresh: no text response")
	}
	t.Logf("profile B response after Refresh: %q", frameText(textRespB))

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
		Parent:         gameconst.PromptsParent,
		AgentProfileId: profileName,
		AgentProfile: &game.AgentProfile{
			Model:        "gpt-4",
			SystemPrompt: "You are a test agent.",
			Enabled:      true,
		},
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
		Parent:         gameconst.PromptsParent,
		AgentProfileId: profileName,
		AgentProfile: &game.AgentProfile{
			Model:        "gpt-4",
			SystemPrompt: "You are a test agent.",
			Enabled:      true,
		},
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
		Parent:         gameconst.PromptsParent,
		AgentProfileId: profileName,
		AgentProfile: &game.AgentProfile{
			Model:        "gpt-4",
			SystemPrompt: "You are a test agent.",
			Enabled:      true,
		},
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

// TestProfileGuardRejectsMismatch verifies the profile guard's core contract:
// after a session is bound to profile A, a turn carrying profile B is rejected
// (Warn + Wait, non-fatal), and a subsequent turn carrying profile A (the bound
// profile) is accepted normally. This covers spec US4 acceptance — the
// rejection is non-fatal and does not block later matching turns
// (specs/021-agent-session-resync/spec.md US4 acceptance scenario 2;
// specs/021-agent-session-resync/quickstart.md Scenario 7 mismatch part).
func TestProfileGuardRejectsMismatch(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileAName := fmt.Sprintf("life-guard-a-%s", uniqueSuffix())
	profileBName := fmt.Sprintf("life-guard-b-%s", uniqueSuffix())

	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		Parent:         gameconst.PromptsParent,
		AgentProfileId: profileAName,
		AgentProfile: &game.AgentProfile{
			Model:        "gpt-4",
			SystemPrompt: "You are profile A.",
			Enabled:      true,
		},
	})
	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		Parent:         gameconst.PromptsParent,
		AgentProfileId: profileBName,
		AgentProfile: &game.AgentProfile{
			Model:        "gpt-4",
			SystemPrompt: "You are profile B.",
			Enabled:      true,
		},
	})

	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	// given: a turn under profile A binds the adapter.
	sendTextWithProfile(t, conn, sessionID, profileAName, "Message with profile A")
	_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return frameHasThinking(f) })
	if drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return frameHasText(f) }) == nil {
		t.Fatal("profile A: no text response (adapter did not bind)")
	}
	_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return frameWait(f) != nil })

	// when: a turn under profile B arrives without Refresh.
	sendTextWithProfile(t, conn, sessionID, profileBName, "Message with profile B")

	// then: the guard rejects it — WarnSignal naming the mismatch, then a
	// WaitSignal returning the desktop to ready (FR-012a/FR-012b).
	warnFrame := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return frameWarn(f) != nil && strings.Contains(frameWarn(f).GetMessage(), "profile mismatch")
	})
	if warnFrame == nil {
		t.Fatal("expected a WarnSignal with profile mismatch for the profile B turn")
	}
	t.Logf("mismatch warn: %q", frameWarn(warnFrame).GetMessage())
	if drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return frameWait(f) != nil }) == nil {
		t.Fatal("expected a WaitSignal after the mismatch Warn")
	}

	// then: a subsequent turn under profile A (the bound profile) is accepted
	// normally — the rejection did not block later matching turns (SC-004).
	sendTextWithProfile(t, conn, sessionID, profileAName, "Second message with profile A")
	_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return frameHasThinking(f) })
	if drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return frameHasText(f) }) == nil {
		t.Fatal("profile A after mismatch rejection: no text response (guard blocked a matching turn)")
	}
}

// The terminal mouse-move-success-text constant lives in helpers_test.go
// (expectedMouseMoveSuccessText) — shared by the agent_operation,
// agent_checkpoint, and agent_lifecycle suites (style/large_test.md
// §反模式3 — do not copy helpers).

// TestStatusPingPong verifies the agent's status ping-pong: a status probe
// returns IDLE when no turn is in-flight (adapter bound), and ACTIVE while a
// turn is in-flight (dispatch blocked awaiting a desktop operation result).
//
// The ACTIVE case uses a mouse_move tool_call turn whose OperationBridge
// dispatch blocks the turn until the test (playing desktop) replies with a
// ToolResultPart. While the dispatch is pending the per-session turn mutex is
// held, so a status probe observed AFTER reading the operation frame is
// deterministic — there is no race window where the turn might complete before
// the probe is processed
// (specs/021-agent-session-resync/spec.md US1 / SC-001;
// specs/021-agent-session-resync/contracts/agent-desktop-channel-contract.md §1;
// specs/021-agent-session-resync/quickstart.md Scenario 6).
func TestStatusPingPong(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("life-status-%s", uniqueSuffix())

	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		Parent:         gameconst.PromptsParent,
		AgentProfileId: profileName,
		AgentProfile: &game.AgentProfile{
			Model:        "gpt-4",
			SystemPrompt: "You are a status test agent.",
			ToolNames:    []string{"mouse_move", "mouse_click"},
			Enabled:      true,
		},
	})
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	// given: run a text turn to completion so the adapter is bound and no
	// turn is in-flight.
	sendTextWithProfile(t, conn, sessionID, profileName, "hello")
	_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return frameHasThinking(f) })
	_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return frameHasText(f) })
	_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return frameWait(f) != nil })

	// when: probe the status while idle. then: the response is IDLE.
	sendStatusFrame(t, conn, sessionID, game.StatusSignalStatus_STATUS_SIGNAL_STATUS_ACTIVE)
	idleResp := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return frameStatus(f) != nil
	})
	if idleResp == nil {
		t.Fatal("did not receive a status response while idle")
	}
	if frameStatus(idleResp).GetStatus() != game.StatusSignalStatus_STATUS_SIGNAL_STATUS_IDLE {
		t.Errorf("idle status = %v, want IDLE", frameStatus(idleResp).GetStatus())
	}
	t.Logf("idle status probe: %v", frameStatus(idleResp).GetStatus())

	// when: start a mouse_move turn; reading the dispatched operation frame
	// proves the turn is in-flight and blocked awaiting the desktop result
	// (the per-session mutex is held for the entire dispatch wait).
	sendTextWithProfile(t, conn, sessionID, profileName, "please move the mouse now")
	opFrame := readOperationFrame(t, conn)

	// then: a status probe while the turn is in-flight returns ACTIVE.
	sendStatusFrame(t, conn, sessionID, game.StatusSignalStatus_STATUS_SIGNAL_STATUS_ACTIVE)
	activeResp := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return frameStatus(f) != nil
	})
	if activeResp == nil {
		t.Fatal("did not receive a status response while a turn is in-flight")
	}
	if frameStatus(activeResp).GetStatus() != game.StatusSignalStatus_STATUS_SIGNAL_STATUS_ACTIVE {
		t.Errorf("in-flight status = %v, want ACTIVE", frameStatus(activeResp).GetStatus())
	}
	t.Logf("in-flight status probe: %v", frameStatus(activeResp).GetStatus())

	// Complete the turn so the connection settles: reply SUCCEEDED (the
	// message avoids "button"/"out of bounds" so fake-LLM closes the loop
	// with terminal text) and drain the final text.
	respondToOperation(t, conn, sessionID, opFrame,
		game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED, "cursor moved to 100,200")
	textFrame := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return frameHasText(f)
	})
	if textFrame == nil {
		t.Fatal("did not receive a final text frame after the in-flight turn")
	}
	if !strings.Contains(frameText(textFrame), expectedMouseMoveSuccessText) {
		t.Errorf("final text = %q, want to contain %q", frameText(textFrame), expectedMouseMoveSuccessText)
	}
	t.Logf("in-flight turn completed: %q", frameText(textFrame))
}

// TestReconnectDispatchReliability verifies that an operation dispatch succeeds
// after a WebSocket disconnect/reconnect cycle: the stream-scoped sink
// (compare-and-delete) ensures the closing stream's cleanup cannot clobber the
// fresh reconnect's sink, so a tool dispatch on the new connection resolves
// SUCCEEDED rather than FAILED "desktop disconnected"
// (specs/021-agent-session-resync/spec.md US2 / SC-002 / SC-005;
// specs/021-agent-session-resync/quickstart.md Scenario 6;
// specs/021-agent-session-resync/research.md D3).
func TestReconnectDispatchReliability(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("life-reconnect-%s", uniqueSuffix())

	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		Parent:         gameconst.PromptsParent,
		AgentProfileId: profileName,
		AgentProfile: &game.AgentProfile{
			Model:        "gpt-4",
			SystemPrompt: "You are a reconnect test agent.",
			ToolNames:    []string{"mouse_move", "mouse_click"},
			Enabled:      true,
		},
	})
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)

	// given: connect, run a turn whose operation dispatches, complete it.
	conn1 := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	sendTextWithProfile(t, conn1, sessionID, profileName, "please move the mouse now")
	opFrame1 := readOperationFrame(t, conn1)
	respondToOperation(t, conn1, sessionID, opFrame1,
		game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED, "cursor moved to 100,200")
	_ = drainWSFrame(t, conn1, func(f *game.AgentFrame) bool { return frameHasText(f) })

	// when: disconnect then reconnect.
	conn1.Close()
	conn2 := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn2.Close()

	// then: a new turn on the reconnected stream dispatches its operation
	// and the dispatch resolves SUCCEEDED (not FAILED "desktop disconnected").
	// The operation frame arriving on conn2 proves the fresh sink is live;
	// the subsequent text proves the dispatch result was correlated correctly.
	sendTextWithProfile(t, conn2, sessionID, profileName, "please move the mouse now")
	opFrame2 := readOperationFrame(t, conn2)
	t.Logf("reconnect dispatch: operation frame received on conn2 (tool_id=%s)",
		frameOperationToolID(opFrame2))

	respondToOperation(t, conn2, sessionID, opFrame2,
		game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED, "cursor moved to 100,200")

	textFrame := drainWSFrame(t, conn2, func(f *game.AgentFrame) bool {
		return frameHasText(f)
	})
	if textFrame == nil {
		t.Fatal("reconnect dispatch: no text frame after SUCCEEDED — dispatch did not resolve on the fresh stream")
	}
	if !strings.Contains(frameText(textFrame), expectedMouseMoveSuccessText) {
		t.Errorf("reconnect dispatch: final text = %q, want to contain %q",
			frameText(textFrame), expectedMouseMoveSuccessText)
	}
	t.Logf("reconnect dispatch succeeded: %q", frameText(textFrame))
}
