// Package testplan contains agent checkpoint integration tests covering
// checkpoint resume, concurrent message serialization, and tool-result
// status preservation across session re-entry (spec 023 FR-012..FR-015),
// adapted to the saolei team model (spec 031-team-template-mode): each test
// sets up the team stack via setupTeamSession (session → saolei TeamProfile
// → CreateTeam) before connecting — CreateTeam MUST precede Connect (no lazy
// creation, FR-033). The former per-profile-model case moved to the
// saolei_team suite; the former cross-profile-history case was removed (a
// session's team is bound to one TeamProfile at CreateTeam — profile
// switching no longer exists).
package testplan

import (
	"fmt"
	"strings"
	"testing"

	"dominion/common/gopkg/testtool"
	game "dominion/projects/game"
)

// TestAgentCheckpointResume verifies the full checkpoint-resume flow:
// create agent → send 3 messages → leave play → re-enter play →
// list messages → verify all prior messages present → send follow-up
// referencing turn 1 → verify agent produces a response (still functional).
func TestAgentCheckpointResume(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("ckpt-resume-%s", uniqueSuffix())

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")

	// Enter play — connect WebSocket
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)

	// Each turn carries the greeting keyword so responses are deterministic.
	messages := []string{
		"Hello, my name is Alice and I work as a software engineer.",
		"Hello, how are you today?",
		"Hello, what is 2+2?",
	}
	var responseTexts []string
	for _, msg := range messages {
		sendText(t, conn, sessionID, msg)

		thinkingResp := drainWSFrame(t, conn, func(f *game.TeamFrame) bool {
			return frameHasThinking(f)
		})
		if thinkingResp == nil {
			t.Fatalf("message %q: did not receive thinking response", msg)
		}
		if !strings.Contains(frameThinking(thinkingResp), expectedGreetingReasoning) {
			t.Errorf("message %q: thinking = %q, want to contain %q", msg, frameThinking(thinkingResp), expectedGreetingReasoning)
		}
		textResp := drainWSFrame(t, conn, func(f *game.TeamFrame) bool {
			return frameHasText(f)
		})
		if textResp == nil {
			t.Fatalf("message %q: did not receive text response", msg)
		}
		if !strings.Contains(frameText(textResp), expectedGreetingText) {
			t.Errorf("message %q: text = %q, want to contain %q", msg, frameText(textResp), expectedGreetingText)
		}
		responseTexts = append(responseTexts, frameText(textResp))
		t.Logf("turn %d: user=%q → agent=%q", len(responseTexts), msg, frameText(textResp))
	}

	if len(responseTexts) != 3 {
		t.Fatalf("got %d responses, want 3", len(responseTexts))
	}

	// Leave play
	conn.Close()

	// List messages — verify at least 6 messages (3 user + 3 agent responses)
	lmr := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "player")
	gotCount := len(lmr.GetMessages())
	if gotCount < 6 {
		t.Errorf("ListMessages after 3 turns returned %d messages, want at least 6", gotCount)
	}
	for i, msg := range lmr.GetMessages() {
		t.Logf("message[%d]: type=%s role=%s content=%q", i, messageKind(msg), roleString(msg.GetRole()), messageText(msg))
	}

	// Verify user messages are present and in order
	foundFirst := false
	for _, msg := range lmr.GetMessages() {
		if msg.GetRole() == game.MessageRole_MESSAGE_ROLE_USER && messageText(msg) == messages[0] {
			foundFirst = true
			break
		}
	}
	if !foundFirst {
		t.Errorf("first user message %q not found in ListMessages response", messages[0])
	}

	// Re-enter play
	conn2 := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn2.Close()

	// Send follow-up referencing turn 1, carrying the greeting keyword.
	followUp := "Hello, what is my name and what do I do for work?"
	textFrame := buildTextFrame(sessionID, "player", followUp)
	writeWSFrame(t, conn2, textFrame)

	followThinking := drainWSFrame(t, conn2, func(f *game.TeamFrame) bool {
		return frameHasThinking(f)
	})
	if followThinking == nil {
		t.Fatal("did not receive thinking response for follow-up after re-enter")
	}
	textResp := drainWSFrame(t, conn2, func(f *game.TeamFrame) bool {
		return frameHasText(f)
	})
	if textResp == nil {
		t.Fatal("did not receive text response for follow-up after re-enter")
	}
	if textResp.GetRole() != game.MessageRole_MESSAGE_ROLE_AGENT {
		t.Errorf("follow-up role = %s, want AGENT", roleString(textResp.GetRole()))
	}
	if !strings.Contains(frameText(textResp), expectedGreetingText) {
		t.Errorf("follow-up text = %q, want to contain %q", frameText(textResp), expectedGreetingText)
	}
	t.Logf("follow-up response: %s", frameText(textResp))

	// Verify message count increased
	lmr2 := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "player")
	if len(lmr2.GetMessages()) <= gotCount {
		t.Errorf("ListMessages after follow-up returned %d messages, want > %d", len(lmr2.GetMessages()), gotCount)
	}
}

// TestAgentCheckpointResumeVerifyContext verifies that after leaving and
// re-entering play, ListMessages returns the complete conversation history
// in chronological order with correct message counts.
func TestAgentCheckpointResumeVerifyContext(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("ckpt-verify-%s", uniqueSuffix())

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")

	// Send 2 messages, each carrying the greeting keyword.
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	userMessages := []string{"Hello, turn one", "Hello, turn two"}
	for _, msg := range userMessages {
		sendText(t, conn, sessionID, msg)

		thinkingResp := drainWSFrame(t, conn, func(f *game.TeamFrame) bool {
			return frameHasThinking(f)
		})
		if thinkingResp == nil {
			t.Fatalf("message %q: no thinking response", msg)
		}
		if !strings.Contains(frameThinking(thinkingResp), expectedGreetingReasoning) {
			t.Errorf("message %q: thinking = %q, want to contain %q", msg, frameThinking(thinkingResp), expectedGreetingReasoning)
		}
		textResp := drainWSFrame(t, conn, func(f *game.TeamFrame) bool {
			return frameHasText(f)
		})
		if textResp == nil {
			t.Fatalf("message %q: no text response", msg)
		}
		if !strings.Contains(frameText(textResp), expectedGreetingText) {
			t.Errorf("message %q: text = %q, want to contain %q", msg, frameText(textResp), expectedGreetingText)
		}
	}
	conn.Close()

	// Leave and re-enter — messages should still be there
	lmr := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "player")
	if len(lmr.GetMessages()) < 4 {
		t.Errorf("ListMessages after 2 turns returned %d messages, want at least 4", len(lmr.GetMessages()))
	}

	// Verify content-bearing messages are present
	for i, msg := range lmr.GetMessages() {
		if messageKind(msg) == "text" && messageText(msg) == "" {
			t.Errorf("message[%d]: text part has empty content", i)
		}
	}

	// Re-connect and send a third message carrying the greeting keyword.
	conn2 := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn2.Close()

	thirdMsg := "Hello, turn three continuing"
	textFrame := buildTextFrame(sessionID, "player", thirdMsg)
	writeWSFrame(t, conn2, textFrame)

	thirdThinking := drainWSFrame(t, conn2, func(f *game.TeamFrame) bool {
		return frameHasThinking(f)
	})
	if thirdThinking == nil {
		t.Fatal("third message: no thinking response after re-enter")
	}
	textR := drainWSFrame(t, conn2, func(f *game.TeamFrame) bool {
		return frameHasText(f)
	})
	if textR == nil {
		t.Fatal("third message: no text response after re-enter")
	}
	if !strings.Contains(frameText(textR), expectedGreetingText) {
		t.Errorf("third message text = %q, want to contain %q", frameText(textR), expectedGreetingText)
	}

	// Verify message count increased by 2 (1 user + 1 agent)
	lmr2 := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "player")
	if len(lmr2.GetMessages()) < 6 {
		t.Errorf("ListMessages after 3rd turn returned %d messages, want at least 6", len(lmr2.GetMessages()))
	}
	t.Logf("total messages after 3 turns: %d", len(lmr2.GetMessages()))
}

// TestAgentConcurrentSerialization verifies that sending two messages
// rapidly to the same agent yields responses in FIFO send order without
// interleaving. Each turn carries a DISTINCT keyword backed by a DISTINCT
// template (greeting then farewell) so the response identity proves order.
func TestAgentConcurrentSerialization(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("conc-fifo-%s", uniqueSuffix())

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// Distinct keywords → distinct templates, so response text proves FIFO order.
	messages := []string{"hello first", "goodbye second"}
	for _, msg := range messages {
		sendText(t, conn, sessionID, msg)
	}

	wantTexts := []string{expectedGreetingText, expectedFarewellText}

	for i, want := range wantTexts {
		_ = drainWSFrame(t, conn, func(f *game.TeamFrame) bool {
			return frameHasThinking(f)
		})
		textFrame := drainWSFrame(t, conn, func(f *game.TeamFrame) bool {
			return frameHasText(f)
		})
		if textFrame == nil {
			t.Fatalf("message %d: did not receive text response", i)
		}
		if !strings.Contains(frameText(textFrame), want) {
			t.Errorf("response %d = %q, want to contain %q (FIFO order violated)", i, frameText(textFrame), want)
		}
	}
}

// TestAgentCheckpointToolResultStatusPersists verifies spec 023 FR-012/FR-013
// (the history-status fix, quickstart.md Scenario 6) for the saolei TEAM
// model: the saolei MCP tool results read neutral
// (TOOL_RESULT_STATUS_UNSPECIFIED, NEVER FAILED — spec 023 D12) in the LIVE
// frames, and that neutrality MUST survive leaving and re-entering the
// session — `ListMessages` is a stateless reconstruction of the checkpoint,
// so the statuses must not flip across WS reconnects. Before the fix the
// status was guessed by `inferToolResultStatus` (FAILED unless the text
// contained "ok"/"succeeded") — a regression here would surface spurious
// FAILED entries for the saolei text-board results.
func TestAgentCheckpointToolResultStatusPersists(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("ckpt-status-%s", uniqueSuffix())

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")

	// given: one full saolei init→click→click turn against a recognizable
	// in-progress board (saolei_1.png). Every saolei tool_result is a TEXT
	// board whose status is neutral (UNSPECIFIED, never FAILED — D12).
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	sendText(t, conn, sessionID, "please start saolei game")

	screenshot := buildSaoleiFlowResultScreenshot(saoleiBoardInitPNG)
	// The agent emits the tool_call MessagePart frame and the operation
	// FlowPart frame concurrently (see readToolCallAndOperation doc); collect
	// both in one pass so neither is dropped by an early drain. The tool_call
	// frame's content is asserted in agent_operation_test.go; this suite only
	// needs the operation frame to reply and the resulting tool_result status.
	_, initOp := readToolCallAndOperation(t, conn)
	if frameKeyboardPress(initOp) == nil {
		t.Fatalf("saolei_init did not dispatch a KeyboardPressPart FlowPart; frame parts: %v",
			initOp.GetFlowParts().GetParts())
	}
	respondToOperationWithScreenshot(t, conn, sessionID, initOp,
		game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED, "F2 pressed, new game started", screenshot)
	if fr := drainWSFrame(t, conn, frameHasToolResult); fr == nil {
		t.Fatal("turn 1: did not receive a tool_result MessagePart frame after the init reply")
	}

	// Play the desktop through the chained saolei_click{3,4} → {5,6}
	// dispatches (sample_saolei_tools.yaml) so the turn completes.
	for _, step := range []struct{ cellX, cellY int32 }{
		{saoleiClick1X, saoleiClick1Y},
		{saoleiClick2X, saoleiClick2Y},
	} {
		clickFrame := readOperationFrame(t, conn)
		if frameMouseMoveAndClick(clickFrame) == nil {
			t.Fatalf("saolei_click(%d,%d) did not dispatch a MouseMoveAndClickPart FlowPart", step.cellX, step.cellY)
		}
		respondToOperationWithScreenshot(t, conn, sessionID, clickFrame,
			game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED,
			fmt.Sprintf("cell at (%d,%d) revealed", step.cellX, step.cellY), screenshot)
	}
	// Drain the terminal text frame and the wait FlowPart so the turn is
	// fully settled (the in-progress board does not trigger the planner).
	_ = drainWSFrame(t, conn, func(f *game.TeamFrame) bool { return frameHasText(f) })
	_ = drainWSFrame(t, conn, func(f *game.TeamFrame) bool { return frameWait(f) != nil })

	// when: the session is left (WS closed). ListMessages reads from the
	// checkpoint, not the live socket, so it must reflect the neutral
	// statuses without reconnect-dependent state.
	conn.Close()
	lmrAfterLeave := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "player")

	// then: every saolei tool_result stays neutral — no spurious FAILED from
	// text-heuristic inference (spec 023 FR-012/FR-013; data-model.md §6).
	assertNeutralToolResultStatuses(t, lmrAfterLeave.GetMessages())

	// when: the session is re-entered (fresh WS). The checkpoint persists;
	// ListMessages must return the same neutral statuses.
	conn2 := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn2.Close()
	lmrAfterReenter := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "player")

	// then: identical statuses after re-entry (live≡history for status).
	assertNeutralToolResultStatuses(t, lmrAfterReenter.GetMessages())

	// then: no operation FlowPart in Message.content (FR-005).
	assertMessageContentDisplayOnly(t, lmrAfterReenter.GetMessages())
}

// assertNeutralToolResultStatuses fails the test if any Message's content
// carries a tool_result whose status is not the neutral UNSPECIFIED — saolei
// (MCP) tool results MUST read neutral (spec 023 D12), and the neutrality
// MUST survive checkpoint reconstruction (spec 023 FR-012/FR-013).
func assertNeutralToolResultStatuses(t *testing.T, messages []*game.Message) {
	t.Helper()
	checked := 0
	for i, m := range messages {
		for _, s := range messageToolResultStatuses(m) {
			checked++
			if s != game.ToolResultStatus_TOOL_RESULT_STATUS_UNSPECIFIED {
				t.Errorf("message[%d]: saolei tool_result status = %v, want UNSPECIFIED (neutral — spec 023 D12/FR-013)", i, s)
			}
		}
	}
	if checked == 0 {
		t.Error("no tool_result MessageParts found in history — the saolei dispatch loop produced no tool results")
	}
}

// TestServiceSurvivesDisconnectDuringTurn verifies
// specs/026-agent-abort-crash-fix/spec.md SC-001/SC-002/SC-003 and
// specs/026-agent-abort-crash-fix/quickstart.md Scenario 4: when a desktop
// disconnects MID-TURN (before the closing wait frame), the agent service
// process MUST survive and the session MUST remain reusable on reconnect.
//
// Coverage gap closed by this case (T008a finding): the other cases in this
// binary (TestAgentCheckpointResume, TestAgentCheckpointResumeVerifyContext,
// TestAgentCheckpointToolResultStatusPersists, TestAgentConcurrentSerialization)
// all disconnect AFTER draining the closing wait frame — the turn is already
// complete when the bidi stream closes. TestTeamReconnectDispatchReliability
// (saolei_team_test.go) also disconnects after the in-flight turn completes
// (the text frame is already drained). None of them exercise the
// abort-during-turn path where the catch block's stream.write can race a
// closed peer — the exact crash vector fixed by safeWrite + the global
// unhandledRejection handler
// (specs/026-agent-abort-crash-fix/research.md §D;
// specs/026-agent-abort-crash-fix/contracts/stream-abort-contract.md §1/§3).
//
// Indirect SC-002 verification: testplan large tests are black-box and cannot
// read the agent's container logs (style/large_test.md — tests reach the SUT
// only via HTTP/WS public endpoints). A fatal `unhandledRejection` would, by
// the Node.js ≥15 default (--unhandled-rejections=throw → process.exit(1)),
// restart the agent container. The reconnect-and-resume step below would then
// fail (connection refused / state lost). A passing reconnect therefore
// serves as an indirect assertion of SC-002 (zero fatal unhandled rejections)
// — direct log inspection is not possible from this layer.
func TestServiceSurvivesDisconnectDuringTurn(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("abort-survive-%s", uniqueSuffix())

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")

	// given: a turn is in-flight. The greeting keyword yields a deterministic
	// thinking frame from fake-llm, proving the agent loop has started
	// consuming the user message (the LLM produced at least one streamed
	// block). We deliberately do NOT drain the closing wait frame — the
	// turn MUST still be running when we disconnect below, so the abort path
	// (catch block + finally → releaseMutex) is the code under test.
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	sendText(t, conn, sessionID, "Hello, mid-turn abort")
	firstThinking := drainWSFrame(t, conn, func(f *game.TeamFrame) bool {
		return frameHasThinking(f)
	})
	if firstThinking == nil {
		t.Fatal("did not receive the first thinking frame — turn never started")
	}
	t.Logf("turn started: thinking received, disconnecting before wait frame")

	// when: the desktop bidi stream is torn down MID-TURN. On the agent side
	// this triggers stream.on("end") → abortAllTurns() → controller.abort(),
	// which races against the in-flight LLM stream. Before spec 026, the
	// catch-block stream.write on the now-closed gRPC stream escaped as an
	// unhandled rejection and crashed the process
	// (specs/026-agent-abort-crash-fix/research.md §D).
	conn.Close()

	// then (SC-001 + SC-003): the service process is still alive and the
	// session is reusable. We prove this by reconnecting with the SAME
	// sessionID and starting a NEW turn. If the service had crashed, the WS
	// dial would fail with connection refused; if the per-session turn
	// mutex had leaked (017 FR-005 regression), the new turn would be
	// rejected or hang. A successful thinking + text response after
	// reconnect proves both process liveness (SC-001) and mutex release
	// (SC-003 / spec 026 FR-004 restoring 017 FR-005). drainWSFrame's read
	// deadline (helpers_test.go wsReadTimeout) absorbs the abort-propagation
	// tail latency through the concurrent consumers
	// (specs/026-agent-abort-crash-fix/research.md §C).
	conn2 := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn2.Close()
	sendText(t, conn2, sessionID, "Hello, after reconnect")

	reconnectThinking := drainWSFrame(t, conn2, func(f *game.TeamFrame) bool {
		return frameHasThinking(f)
	})
	if reconnectThinking == nil {
		t.Fatal("post-disconnect reconnect: no thinking frame — service did not survive or mutex was not released (SC-001/SC-003)")
	}
	if !strings.Contains(frameThinking(reconnectThinking), expectedGreetingReasoning) {
		t.Errorf("post-disconnect thinking = %q, want to contain %q",
			frameThinking(reconnectThinking), expectedGreetingReasoning)
	}
	reconnectText := drainWSFrame(t, conn2, func(f *game.TeamFrame) bool {
		return frameHasText(f)
	})
	if reconnectText == nil {
		t.Fatal("post-disconnect reconnect: no text frame — turn did not complete after reconnect (SC-003)")
	}
	if !strings.Contains(frameText(reconnectText), expectedGreetingText) {
		t.Errorf("post-disconnect text = %q, want to contain %q",
			frameText(reconnectText), expectedGreetingText)
	}
	t.Logf("post-disconnect turn completed: %q — service survived mid-turn abort (SC-001/SC-002/SC-003)",
		frameText(reconnectText))

	// then (SC-002, indirect): the reconnect turn above would have failed if
	// the agent service had emitted a fatal unhandled promise rejection
	// during the mid-turn abort (Node.js default --unhandled-rejections=throw
	// → process.exit(1) → service restart). The successful reconnect is the
	// black-box-equivalent assertion of "zero fatal unhandled rejections".
}
