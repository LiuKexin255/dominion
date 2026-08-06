// Package testplan contains agent dialog integration tests.
// These tests validate the team's player-agent text dialog capability
// through the gateway HTTP + WebSocket surface, using the fake LLM test
// artifact that returns deterministic responses. Each test sets up the team
// stack via setupTeamSession (session → saolei TeamProfile → CreateTeam)
// before connecting — CreateTeam MUST precede Connect (no lazy creation,
// spec 031-team-template-mode FR-033).
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
// create saolei TeamProfile → create session → CreateTeam → connect WebSocket.
func TestAgentDialogCreateAndConnect(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("ad-cc-%s", uniqueSuffix())

	// Create the TeamProfile, session, and Team (FR-033 — CreateTeam is the
	// only Team creation point and MUST precede Connect).
	profile := createTeamProfile(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")
	if profile.GetName() != "templates/"+saoleiTemplateID+"/profiles/"+profileName {
		t.Errorf("profile name = %q, want %q", profile.GetName(), "templates/"+saoleiTemplateID+"/profiles/"+profileName)
	}

	sessionID, _ := createSession(t, sutHostURL, sutEnvName, saoleiTemplateID)
	if sessionID == "" {
		t.Fatal("sessionID is empty")
	}
	createTeam(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, profileName)

	// Connect WebSocket
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	conn.Close()
}

// TestAgentDialogTextToResponse verifies the core dialog flow:
// send a content text frame → receive thinking frame → receive text frame
// → verify MessageRole.AGENT on response frames.
func TestAgentDialogTextToResponse(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("ad-ttr-%s", uniqueSuffix())

	// Setup
	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// Send a content frame carrying a TextPart (UserFrame — inbound is
	// inherently the user; specs/035-proto-contract-refine/contracts/
	// frame-split.md §2).
	sendText := "Hello, agent!"
	textFrame := buildTextFrame(sessionID, "player", sendText)
	writeWSFrame(t, conn, textFrame)

	// Receive thinking frame
	thinkingFrame := drainWSFrame(t, conn, func(f *game.TeamFrame) bool {
		return frameHasThinking(f)
	})
	if thinkingFrame == nil {
		t.Fatal("did not receive thinking frame")
	}
	// "Hello, agent!" carries the greeting keyword "hello" so fake-llm
	// deterministically returns the greeting template (see README §4).
	if thinkingFrame.GetRole() != game.MessageRole_MESSAGE_ROLE_AGENT {
		t.Errorf("thinking role = %s, want AGENT", roleString(thinkingFrame.GetRole()))
	}
	if !strings.Contains(frameThinking(thinkingFrame), expectedGreetingReasoning) {
		t.Errorf("thinking = %q, want to contain %q", frameThinking(thinkingFrame), expectedGreetingReasoning)
	}
	t.Logf("thinking: %q (role=%s)", frameThinking(thinkingFrame), roleString(thinkingFrame.GetRole()))

	// Receive text frame
	textRespFrame := drainWSFrame(t, conn, func(f *game.TeamFrame) bool {
		return frameHasText(f)
	})
	if textRespFrame == nil {
		t.Fatal("did not receive text frame")
	}
	if textRespFrame.GetRole() != game.MessageRole_MESSAGE_ROLE_AGENT {
		t.Errorf("text role = %s, want AGENT", roleString(textRespFrame.GetRole()))
	}
	if !strings.Contains(frameText(textRespFrame), expectedGreetingText) {
		t.Errorf("text = %q, want to contain %q", frameText(textRespFrame), expectedGreetingText)
	}
	t.Logf("text: %q (role=%s)", frameText(textRespFrame), roleString(textRespFrame.GetRole()))
}

// TestAgentDialogThinkingBeforeText verifies that the thinking frame arrives
// before the text frame — the ordering guarantee from the handler.
func TestAgentDialogThinkingBeforeText(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("ad-tbt-%s", uniqueSuffix())

	// Setup
	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// Send text carrying the greeting keyword so the response is deterministic.
	textFrame := buildTextFrame(sessionID, "player", "Hello ordering test")
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
	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// "Hello world" carries the greeting keyword "hello".
	textFrame := buildTextFrame(sessionID, "player", "Hello world")
	writeWSFrame(t, conn, textFrame)

	// Read and verify thinking content
	thinkingFrame := drainWSFrame(t, conn, func(f *game.TeamFrame) bool {
		return frameHasThinking(f)
	})
	if thinkingFrame == nil {
		t.Fatal("did not receive thinking frame")
	}
	if !strings.Contains(frameThinking(thinkingFrame), expectedGreetingReasoning) {
		t.Errorf("thinking = %q, want to contain %q", frameThinking(thinkingFrame), expectedGreetingReasoning)
	}

	// Read and verify text content
	textRespFrame := drainWSFrame(t, conn, func(f *game.TeamFrame) bool {
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

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// Run a text turn carrying the greeting keyword so the response is
	// deterministic. The wait FlowPart the agent emits at turn end is a
	// flow_parts frame on the live socket (control channel); it MUST NOT
	// be reconstructed into any Message.content.
	sendText(t, conn, sessionID, "Hello display-only test")
	_ = drainWSFrame(t, conn, func(f *game.TeamFrame) bool { return frameHasThinking(f) })
	_ = drainWSFrame(t, conn, func(f *game.TeamFrame) bool { return frameHasText(f) })
	// Drain the terminal wait FlowPart so the turn settles before listing.
	_ = drainWSFrame(t, conn, func(f *game.TeamFrame) bool { return frameWait(f) != nil })

	// then: ListMessages returns Messages whose content is display-only.
	lmr := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "player")
	assertMessageContentDisplayOnly(t, lmr.GetMessages())

	// Sanity: the user text survived in history (a regression that
	// dropped text would silently pass the display-only guard).
	if !messagesContainText(lmr.GetMessages(), "Hello display-only test") {
		t.Errorf("ListMessages did not surface the user text 'Hello display-only test' — history reconstruction regressed")
	}
}

// TestAgentDialogFIFOQueue verifies that messages sent one at a time — each
// followed by a full turn drain (thinking + text + wait) before the next is
// submitted — are processed independently and in FIFO order. Because fake-llm
// matches by keyword, each turn carries a DISTINCT keyword backed by a DISTINCT
// template so the response identity proves the processing order: greeting,
// farewell, greeting.
//
// This is the sequential-submit form of FIFO ordering: each message starts
// only after the previous turn's wait returned the loop to idle. The
// rapid-submit form — messages arriving mid-run, merged by the TurnLoop into
// one aggregated turn — is covered by the gate-controlled unit tests in
// projects/game/agent/src/turn-loop.test.ts ("combines ALL pending queued
// messages into one aggregated turn (FIFO)", turn-loop.test.ts:398)
// (specs/030-queued-chat-input/spec.md FR-005).
func TestAgentDialogFIFOQueue(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("ad-fifo-%s", uniqueSuffix())

	// Setup
	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// Each message triggers a different template via a distinct keyword so the
	// response text proves which input was processed.
	messages := []string{
		"hello world",   // greeting
		"goodbye world", // farewell
		"hi friend",     // greeting again (hi is a greeting keyword)
	}
	wantTexts := []string{expectedGreetingText, expectedFarewellText, expectedGreetingText}

	// Submit each message sequentially — drain the full turn (thinking + text +
	// wait) before sending the next — so each message is processed as an
	// independent turn and the responses prove FIFO order. A rapid burst would
	// instead be merged by the per-session TurnLoop into one aggregated turn
	// (specs/030-queued-chat-input/spec.md FR-005).
	for i, msg := range messages {
		sendText(t, conn, sessionID, msg)
		_ = drainWSFrame(t, conn, func(f *game.TeamFrame) bool {
			return frameHasThinking(f)
		})
		textFrame := drainWSFrame(t, conn, func(f *game.TeamFrame) bool {
			return frameHasText(f)
		})
		if textFrame == nil {
			t.Fatalf("turn %d: did not receive text response frame", i)
		}
		if !strings.Contains(frameText(textFrame), wantTexts[i]) {
			t.Errorf("response %d = %q, want to contain %q (FIFO order violated)", i, frameText(textFrame), wantTexts[i])
		}
		// Drain the terminal wait so the loop returns to idle before the next
		// submit; otherwise the next message would land mid-run and be combined.
		_ = drainWSFrame(t, conn, func(f *game.TeamFrame) bool {
			return frameWait(f) != nil
		})
	}
}

// TestAgentDialogDeleteTeamProfileStillResponds verifies the loose coupling
// design: after the team is created, deleting the saolei TeamProfile does
// not prevent subsequent messages from being processed, because the team's
// player/planner models were resolved at CreateTeam time (server.ts
// SessionTeamStore factory reads the profile once).
func TestAgentDialogDeleteTeamProfileStillResponds(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("ad-delp-%s", uniqueSuffix())

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")

	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// Turn before deletion carries the greeting keyword.
	sendText(t, conn, sessionID, "Hello before delete")
	_ = drainWSFrame(t, conn, func(f *game.TeamFrame) bool { return frameHasThinking(f) })
	firstResp := drainWSFrame(t, conn, func(f *game.TeamFrame) bool { return frameHasText(f) })
	if firstResp == nil {
		t.Fatal("no response before TeamProfile deletion")
	}

	delStatus := deleteTeamProfile(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName)
	if delStatus != http.StatusOK && delStatus != http.StatusNoContent {
		t.Fatalf("DELETE team profile status = %d, want 200 or 204", delStatus)
	}

	// Turn after deletion carries the farewell keyword so the content assertion
	// is deterministic (no random fallback).
	sendText(t, conn, sessionID, "Goodbye after delete")

	textRespFrame := drainWSFrame(t, conn, func(f *game.TeamFrame) bool {
		return frameHasText(f)
	})
	if textRespFrame == nil {
		t.Fatal("did not receive text response after TeamProfile deletion")
	}
	if textRespFrame.GetRole() != game.MessageRole_MESSAGE_ROLE_AGENT {
		t.Errorf("response role = %s, want AGENT", roleString(textRespFrame.GetRole()))
	}
	if !strings.Contains(frameText(textRespFrame), expectedFarewellText) {
		t.Errorf("response text = %q, want to contain %q", frameText(textRespFrame), expectedFarewellText)
	}
}

// ─── Queue-while-running tests (specs/030-queued-chat-input) ────────────────
//
// The ONLY queue-while-running large test below is
// TestAgentDialogQueueMidTurnInjection: it covers the feature 038 mid-turn
// injection of a queued message at the tool-result boundary — a TOOL turn,
// where the saolei tool dispatch provides a reliably controllable in-flight
// window (the tool stays blocked until the desktop replies)
// (specs/038-queue-input-mid-turn/spec.md FR-001).
//
// The no-tool fallback (specs/038-queue-input-mid-turn/spec.md FR-004) and the
// spec 030 next-turn handoff / multi-message combine / does-not-disturb /
// QueueSignal-visibility behaviors are NOT covered at the large-test layer:
// fake-llm has no delay mechanism and a no-tool turn responds in milliseconds,
// so the fallback window between the first reasoning step and the turn-end
// drain cannot be synchronized end-to-end — the earlier large tests relying on
// it were unreliable and have been removed (designer evaluation; no code
// change, per style/large_test.md no dead code). These behaviors are
// deterministically covered by unit tests in projects/game/agent/src/
// turn-loop.test.ts, whose gate-controlled fake runner holds the turn
// in-flight and drains the buffer through the SAME turn-end `runLoop` code
// path the FR-004 fallback uses: does-not-disturb + next-turn handoff
// ("submit while running buffers and does not disturb the in-flight turn",
// turn-loop.test.ts:242), multi-message combine ("combines ALL pending queued
// messages into one aggregated turn (FIFO)", turn-loop.test.ts:398),
// QueueSignal depth visibility ("emits QueueSignal(new depth) on each submit
// while running", turn-loop.test.ts:526), and the turn-end drain ("after a
// drainQueue call the turn-end drain sees an empty buffer (no double-drain)",
// turn-loop.test.ts:665).

// TestAgentDialogQueueMidTurnInjection verifies specs/038-queue-input-mid-turn
// US1 (FR-001): a message submitted while a saolei TOOL is executing is
// drained MID-TURN — at the next beforeModel boundary, NOT at turn end — and
// reaches the agent (it is injected into the conversation state and persisted
// in history).
//
// The flow drives a real saolei_init dispatch (same trigger as
// TestAgentOperationDispatchLoopSuccess): the user turn makes fake-LLM return
// a saolei_init tool_call, the agent dispatches the F2 KeyboardPressPart
// through OperationBridge, and the tool stays in-flight until the desktop
// replies. Inside that window the test submits "watch out for mines" — the
// TurnLoop buffers it and emits QueueSignal(1)
// (specs/030-queued-chat-input/contracts/queue-channel-contract.md §2). The
// desktop reply resolves the dispatch; the player's `queueDrain` beforeModel
// middleware (projects/game/agent/src/team/player.ts) then drains the buffer
// before the next model call, emitting QueueSignal(0) mid-turn
// (specs/038-queue-input-mid-turn/contracts/turn-loop-drain-contract.md
// emission table).
//
// The depth-0 QueueSignal appears BEFORE the terminal wait frame — the shape
// of the turn-loop-drain-contract sequence (submit⇒1, drain⇒0, wait): the
// drain happened while the turn was still running, not at turn idle. NOTE:
// this ordering alone does NOT distinguish spec 038 mid-turn injection from a
// spec 030 turn-end drain — under the 030 regression the depth-0 signal also
// precedes a wait (turn 2's wait). The distinguishing assertion is the
// saolei_click check: NO saolei_click dispatch after the init — the injected
// HumanMessage became the last message of the next model call, so the fake-LLM
// tools branch never saw the init tool result alone, and the init→click→click
// chain was interrupted (under 030 turn-end semantics the queued message would
// be deferred to the turn-end hand-off, so the chain would complete first).
// Exactly ONE terminal wait is also asserted, but the count only guards turn
// completion — drainUntilWait stops at the first wait, so countWaitFrames
// cannot detect a second turn; the no-second-turn guarantee follows from the
// saolei_click check above (the interrupted chain means the injection happened
// mid-turn).
func TestAgentDialogQueueMidTurnInjection(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("ad-mid-%s", uniqueSuffix())

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// when: a user turn matching the fake-LLM "saolei-start" keyword makes
	// fake-LLM return a saolei_init tool_call (FR-002).
	sendText(t, conn, sessionID, "please start saolei game")

	// The player emits the tool_call MessagePart frame AND dispatches the F2
	// KeyboardPressPart FlowPart through OperationBridge (they race on the WS —
	// readToolCallAndOperation collects both in a single pass). The tool is
	// now in-flight: it blocks on the desktop's FlowResultPart reply.
	_, initOpFrame := readToolCallAndOperation(t, conn)

	// While the tool is executing (BEFORE the reply), submit a message. The
	// TurnLoop buffers it and emits QueueSignal(1). The submit is ordered on
	// the same WS before the reply below, so the depth-1 signal is written to
	// the sink before any post-reply frame — drainUntilWait below sees it.
	sendText(t, conn, sessionID, "watch out for mines")

	// The desktop replies with a recognizable in-progress board; the bridge
	// resolves the pending dispatch. The next beforeModel fires queueDrain,
	// which injects the buffered message mid-turn (QueueSignal(0)).
	screenshot := buildSaoleiFlowResultScreenshot(saoleiBoardInitPNG)
	respondToOperationWithScreenshot(t, conn, sessionID, initOpFrame,
		game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED, "F2 pressed", screenshot)

	// Collect ALL remaining frames until the terminal wait. readToolCallAndOperation
	// already consumed the tool_call + operation frames; neither carries a
	// QueueSignal (the queued message is submitted AFTER it returns), so the
	// frames below carry the COMPLETE QueueSignal sequence [1, 0].
	frames := drainUntilWait(t, conn)

	// QueueSignal depth sequence: [1, 0] — submit-while-running grew the
	// buffer to 1, then the MID-TURN drainQueue cleared it to 0
	// (specs/038-queue-input-mid-turn/contracts/turn-loop-drain-contract.md
	// emission table).
	depths := queueSignalDepths(frames)
	if len(depths) < 2 {
		t.Fatalf("expected at least 2 QueueSignal frames (submit+drain), got %d: %v", len(depths), depths)
	}
	if depths[0] != 1 {
		t.Errorf("first QueueSignal depth = %d, want 1 (message buffered while the saolei tool was in-flight)", depths[0])
	}

	// Shape guard: the depth-0 signal exists and appears BEFORE the terminal
	// wait, matching the turn-loop-drain-contract sequence (submit⇒1, drain
	// ⇒0, wait)
	// (specs/038-queue-input-mid-turn/contracts/turn-loop-drain-contract.md
	// "Mid-turn drainQueue clears the buffer" row). NOTE: this ordering alone
	// does NOT distinguish spec 038 mid-turn injection from a spec 030
	// turn-end drain — under the 030 regression the depth-0 signal also
	// precedes a wait (turn 2's wait). The distinguishing assertion is the
	// saolei_click check below.
	zeroIdx, waitIdx := -1, -1
	for i, f := range frames {
		if zeroIdx == -1 {
			if q := frameQueueSignal(f); q != nil && q.GetQueuedCount() == 0 {
				zeroIdx = i
			}
		}
		if waitIdx == -1 && frameWait(f) != nil {
			waitIdx = i
		}
	}
	if zeroIdx == -1 {
		t.Errorf("no QueueSignal depth 0 found in sequence %v (mid-turn drain signal missing)", depths)
	}
	if waitIdx == -1 {
		t.Fatalf("no wait frame found in %d drained frames", len(frames))
	}
	if zeroIdx != -1 && zeroIdx > waitIdx {
		t.Errorf("QueueSignal depth 0 (mid-turn drain) at frame %d appears AFTER the wait at frame %d — "+
			"the buffer was drained at turn end, not mid-turn (specs/038-queue-input-mid-turn/spec.md FR-001)",
			zeroIdx, waitIdx)
	}

	// Confirm the turn reached completion (a terminal wait frame was
	// observed). drainUntilWait returns at the first wait, so this guards
	// against a missing/timeout turn rather than a second turn — the
	// no-second-turn guarantee is asserted by the saolei_click check below.
	waitCount := countWaitFrames(frames)
	if waitCount != 1 {
		t.Errorf("wait frame count = %d, want 1 (the turn must reach completion; count 0 means the turn missed or timed out before a wait — drainUntilWait stops at the first wait, so a second turn cannot be detected here)", waitCount)
	}

	// KEY (distinguishing) assertion: the injected message interrupted the
	// saolei tool chain — no saolei_click dispatch may follow the init reply.
	// The injected HumanMessage became the last message of the next model
	// call, so the fake-LLM matched the user text instead of chaining the init
	// tool result into saolei_click{3,4}. This is the check that separates
	// spec 038 mid-turn injection from the spec 030 turn-end drain: under
	// 030 semantics the queued message would be deferred to the turn-end
	// hand-off, so the init→click chain would complete first.
	for _, f := range frames {
		if mmc := frameMouseMoveAndClick(f); mmc != nil {
			t.Errorf("saolei_click dispatch found after the mid-turn drain — the queued message was NOT injected before the next model call: %v", mmc)
		}
	}

	// then: the queued message reached the agent — it was injected into the
	// conversation state and persisted, so ListMessages surfaces it as a USER
	// message (injection-seam-contract.md §3 — the HumanMessage is appended to
	// the player channel via messagesStateReducer). NOTE: "watch out for
	// mines" matches no fake-LLM keyword (verified against the MessageStore
	// fixtures in projects/game/fake-llm/service/testdata), so the post-drain
	// model response is the matcher's random fallback — its CONTENT is
	// intentionally not asserted; the deterministic proof of delivery is the
	// persisted history.
	lmr := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "player")
	if !messagesContainText(lmr.GetMessages(), "watch out for mines") {
		t.Errorf("ListMessages did not surface the queued message 'watch out for mines' — " +
			"the mid-turn injection did not reach the agent (specs/038-queue-input-mid-turn/spec.md FR-001)")
	}
}
