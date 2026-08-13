// Package testplan contains the agent stream-stall recovery large-test
// suite.
//
// agent_stall_test.go validates the end-to-end stall recovery of
// specs/043-llm-stream-stall-recovery (T011) and the spec 044
// recalibration (specs/044-llm-stall-recovery-fix T011 cases b/c) against
// the deployed agent:
//
//   - TestAgentStallRecoveryWithQueuedMessage (043 US1 + US2, quickstart
//     Scenario 5): a user turn whose text matches the fake-LLM stall
//     template (sample_stall.yaml) receives the opening reasoning delta
//     and then silence while the connection stays alive — the exact
//     "TCP alive but no SSE data" failure mode. Within the configured
//     idle window the agent detects the stall, emits a `warn` notice and
//     a `wait` (FR-004/FR-005), RETAINS the queued-message buffer (no
//     QueueSignal(0) — FR-006, the key distinction from a user abort),
//     and delivers the queued message as the next turn's input (FR-007).
//   - TestAgentStallToolExecutionNotFalselyDetected (043 US3, quickstart
//     Scenario 6): a saolei_operate dispatch delayed by the desktop for
//     longer than the idle timeout must NOT raise a false
//     NodeTimeoutError — the client-side heartbeat wrapper
//     (`withIdleHeartbeat` calling `config.heartbeat()` every
//     TOOL_HEARTBEAT_INTERVAL_MS,
//     specs/043-llm-stream-stall-recovery/research.md R7.2) keeps the
//     idle timer alive on the production saolei MCP path
//     (buildSaoleiMcpTools → MCP HTTP → bridge.dispatch). No warn/wait
//     frames may appear during the tool wait, and the turn completes
//     normally after the reply.
//   - TestAgentStallDetectedWithinConfiguredWindow (044 US1 regression,
//     FR-001/SC-004): a genuine silent-stream dropout is still detected
//     within the stall deploy's configured 60s window (spec 044 raised
//     FR-001's minimum from 15s to 60s) — the elapsed detection time is
//     bracketed to catch a regression to either the old 15s/30s windows
//     or the 120s default.
//   - TestAgentStallPersistsPartialOutput (044 US3, FR-004/FR-005/
//     SC-002/SC-003): the already-streamed partial output of a stalled
//     turn survives reconnection — ListMessages returns it with the
//     interrupted marker (PartCompletion_INTERRUPTED) on the tail part.
//
// All tests run against the stall deploy (deploy_agent_stall.yaml), which
// sets GAME_STREAM_IDLE_TIMEOUT_MS=60000 (044 FR-001's 60s minimum; the
// pre-044 15s value would be clamped to 120s by the agent's 60s-minimum
// clamp, breaking the suite's timing — see
// specs/044-llm-stall-recovery-fix/tasks.md T011). US3 (043) exercises
// the saolei tool module but shares this file/binary because it depends
// on the SAME shortened-idle-timeout deployment topology — the suite is
// organised around the stall-recovery module's deployment, per
// style/large_test.md §按 suite 编排 (a suite = one deployment topology +
// one focused case set). The saolei tooling (readOperationFrame /
// respondToOperationWithScreenshot / saolei fixtures) is reused from the
// shared helpers, not copied.
package testplan

import (
	"fmt"
	"testing"
	"time"

	"dominion/common/gopkg/testtool"
	game "dominion/projects/game"
)

// stallToolReplyDelay exceeds the stall deploy's idle timeout
// (GAME_STREAM_IDLE_TIMEOUT_MS=60000 — spec 044 FR-001's 60s minimum): a
// desktop reply delayed by this long proves the idle timer survived the
// whole tool wait only via the heartbeat wrapper (a bare idleTimeout
// would fire at 60s).
const stallToolReplyDelay = 65 * time.Second

// stallWindow is the stall deploy's configured idle timeout
// (deploy_agent_stall.yaml GAME_STREAM_IDLE_TIMEOUT_MS=60000 — spec 044
// FR-001's 60s minimum; values below are clamped to 120s by the agent).
// stallDetectMin / stallDetectMax bracket the window with margins generous
// enough for CI variance while still catching a regression in either
// direction: the old 15s/30s windows detect at ~15s/~30s (below the
// lower bound), the 120s default detects at ~120s (above the upper
// bound).
const (
	stallWindow    = 60 * time.Second
	stallDetectMin = 45 * time.Second
	stallDetectMax = 115 * time.Second
)

// TestAgentStallRecoveryWithQueuedMessage drives the end-to-end stall
// recovery with buffer retention (specs/043-llm-stream-stall-recovery US1
// + US2; quickstart Scenario 5; FR-001/FR-004/FR-005/FR-006/FR-007;
// SC-001/SC-002):
//
//  1. A user turn matching the "stall now" keyword makes fake-LLM emit
//     its reasoning delta and then stop with the connection alive
//     (sample_stall.yaml) — the desktop sees the thinking bubble start.
//  2. While the turn is stalled, a second message is queued: the TurnLoop
//     buffers it and emits QueueSignal(1).
//  3. Within the configured idle window (~60s in the stall deploy) the
//     stall is detected: a `warn` frame (visible notice, FR-005) is
//     followed by a `wait` frame (idle, FR-004).
//  4. The buffer is RETAINED: no QueueSignal(0) precedes the recovery
//     wait (FR-006 — the stall takes the non-abort error terminal, unlike
//     a user abort which clears the buffer).
//  5. The session is fully recovered: a new message starts a normal turn
//     that completes normally (terminal wait — SC-004), and the queued
//     message is surfaced as a USER message in ListMessages — delivered
//     as the next turn's combined input (FR-007), not lost.
func TestAgentStallRecoveryWithQueuedMessage(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	profileName := fmt.Sprintf("stall-%s", uniqueSuffix())

	// given: a saolei-enabled profile and a connected session.
	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")
	conn := connectAgentWSTrace(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// when (1): a user turn whose text matches the "stall now" keyword
	// makes fake-LLM emit the opening reasoning delta and then stop
	// (connection alive, no further data — sample_stall.yaml).
	sendText(t, conn, sessionID, "please stall now")

	// then (1): the partial reasoning arrives (the desktop sees the
	// thinking bubble start) — the stall must happen MID-stream, after
	// real output, per the feature's failure-mode definition.
	thinkingFrame := drainWSFrame(t, conn, func(f *game.TeamFrame) bool {
		return frameHasThinking(f) && f.GetAgent() == "player"
	})
	if thinkingFrame == nil {
		t.Fatal("did not receive the player's thinking frame — the stall must start AFTER partial reasoning output")
	}
	if got := frameThinking(thinkingFrame); got != expectedStallReasoning {
		t.Errorf("thinking content = %q, want %q (the stall template's reasoning)", got, expectedStallReasoning)
	}

	// when (2): while the turn is stalled (BEFORE the idle timeout
	// fires), queue a message: the TurnLoop buffers it and emits
	// QueueSignal(1).
	sendText(t, conn, sessionID, "watch out for mines")
	queuedFrame := drainWSFrame(t, conn, frameHasQueueSignal)
	if queuedFrame == nil {
		t.Fatal("did not receive a QueueSignal frame after queuing during the stall")
	}
	if q := frameQueueSignal(queuedFrame); q.GetQueuedCount() != 1 {
		t.Errorf("queued_count = %d, want 1 (the message must be buffered during the stall)", q.GetQueuedCount())
	}

	// then (2): within the configured idle window (~60s in the stall
	// deploy) the stall is detected: a warn frame (visible notice,
	// FR-005) is followed by a wait frame (idle, FR-004/SC-001).
	// drainUntilWait collects the whole stream up to the terminal wait.
	stallFrames := drainUntilWait(t, conn)
	warnSeen := false
	for _, f := range stallFrames {
		if frameWarn(f) != nil {
			warnSeen = true
		}
	}
	if !warnSeen {
		t.Errorf("no warn frame among the %d stall-recovery frames — the stall was not surfaced to the desktop (FR-005)", len(stallFrames))
	}
	if got := countWaitFrames(stallFrames); got != 1 {
		t.Errorf("wait frame count = %d, want 1 (the stall must terminate the turn and return to idle, FR-004)", got)
	}

	// then (3): the buffer is RETAINED — the stall took the non-abort
	// error terminal, so no QueueSignal(0) may precede the recovery wait
	// (FR-006; a user abort would have cleared the buffer with
	// QueueSignal(0), specs/030-queued-chat-input FR-011). Retention
	// emits NO depth-change signal: the depth is unchanged (still 1), and
	// a QueueSignal is pushed only when the depth CHANGES
	// (specs/030-queued-chat-input/contracts/queue-channel-contract.md
	// §2 — submit⇒new depth, drain⇒0, abort⇒0). The QueueSignal(1)
	// proving the message entered the buffer was already read at queuing
	// time above; the ABSENCE of QueueSignal(0) in the stall-recovery
	// frames is the retention proof — a stall-induced termination must
	// not look like an abort.
	depths := queueSignalDepths(stallFrames)
	for _, d := range depths {
		if d == 0 {
			t.Errorf("QueueSignal depth 0 before the stall-recovery wait — the buffer was cleared, want retained (FR-006); depths: %v", depths)
		}
	}

	// then (4): the session is fully recovered (SC-004) — a new message
	// starts a normal turn. Its input combines "hello again" with the
	// retained queued message (FR-007 drain semantics), so the response
	// content is NOT pinned: the fake-LLM may match any template for the
	// combined input. The assertions are the ones the contract pins —
	// the turn completes (exactly one terminal wait) and produces output
	// frames, and the ListMessages check below proves the retained
	// message was delivered as the turn's input rather than dropped.
	sendText(t, conn, sessionID, "hello again")
	recoveryFrames := drainUntilWait(t, conn)
	textSeen := false
	for _, f := range recoveryFrames {
		if frameHasText(f) {
			textSeen = true
		}
	}
	if !textSeen {
		t.Errorf("no text frame in the %d recovery frames — the next turn did not produce a response", len(recoveryFrames))
	}
	if got := countWaitFrames(recoveryFrames); got != 1 {
		t.Errorf("recovery turn wait count = %d, want 1", got)
	}

	// then (5): the queued message reached the agent — ListMessages
	// surfaces it as a USER message, proving the retained buffer was
	// delivered as the next turn's input rather than dropped (FR-007 /
	// SC-002).
	lmr := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "player")
	if !messagesContainText(lmr.GetMessages(), "watch out for mines") {
		t.Errorf("ListMessages did not surface the queued message 'watch out for mines' — the stall recovery dropped it (FR-007)")
	}
}

// TestAgentStallToolExecutionNotFalselyDetected drives the
// no-false-stall contract for long tool execution (specs/043-llm-stream-
// stall-recovery US3; quickstart Scenario 6; FR-003/SC-003) on the
// PRODUCTION saolei MCP path (buildSaoleiMcpTools → MCP HTTP →
// bridge.dispatch — NOT the dead-code mouse tools):
//
//  1. A user turn matching "saolei-start" makes fake-LLM return a
//     saolei_init tool_call; the F2 dispatch is replied with a
//     recognizable screenshot, and the fixture chains one saolei_operate
//     BATCH call whose first op (click 3,4) dispatches to the desktop.
//  2. The test delays the desktop reply by 65s — LONGER than the stall
//     deploy's idle timeout (60000ms). The client-side heartbeat wrapper
//     (`withIdleHeartbeat` calling `config.heartbeat()` every
//     TOOL_HEARTBEAT_INTERVAL_MS=10s) keeps the idle timer alive for the
//     whole wait; without it the timer would elapse mid-tool and raise a
//     false NodeTimeoutError → warn + wait.
//  3. After the delayed reply, the test drains the stream for the
//     batch's second op and fails on ANY warn/wait frame: a false stall
//     during the wait would have terminated the turn at ~60s — before
//     the reply — queueing those artifacts for the first reads.
//  4. The batch's second op dispatches, one result returns, the fixture
//     terminates with text, and the turn reaches its terminal wait. No
//     warn frame may appear anywhere in the stream.
func TestAgentStallToolExecutionNotFalselyDetected(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	profileName := fmt.Sprintf("stall-tool-%s", uniqueSuffix())

	// given: a saolei-enabled profile and a connected session.
	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")
	conn := connectAgentWSTrace(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	screenshot := buildSaoleiFlowResultScreenshot(saoleiBoardInitPNG)

	// when (1): a user turn matching "saolei-start" makes fake-LLM return
	// a saolei_init tool_call; the tool dispatches F2 and blocks on the
	// desktop reply.
	sendText(t, conn, sessionID, "please start saolei game")
	initFrame := readOperationFrame(t, conn)
	kp := frameKeyboardPress(initFrame)
	if kp == nil {
		t.Fatalf("saolei_init did not dispatch a KeyboardPressPart FlowPart; frame parts: %v",
			initFrame.GetFlowParts().GetParts())
	}
	if kp.GetKey() != game.KeyboardKey_KEYBOARD_KEY_F2 {
		t.Errorf("saolei_init key = %v, want KEYBOARD_KEY_F2", kp.GetKey())
	}
	respondToOperationWithScreenshot(t, conn, sessionID, initFrame,
		game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED, "F2 pressed, new game started", screenshot)

	// then (1): the fixture chains ONE saolei_operate BATCH call; its
	// first op (click 3,4) dispatches and blocks on the desktop reply —
	// the long tool execution under test.
	opFrame := readOperationFrame(t, conn)
	mmc := frameMouseMoveAndClick(opFrame)
	if mmc == nil {
		t.Fatalf("saolei_operate op (3,4) did not dispatch a MouseMoveAndClickPart FlowPart; frame parts: %v",
			opFrame.GetFlowParts().GetParts())
	}
	if err := assertMouseMoveAndClick(mmc, saoleiClick1CenterX, saoleiClick1CenterY,
		game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK); err != nil {
		t.Errorf("saolei_operate op (3,4) dispatch mismatch: %v", err)
	}

	// when (2): delay the desktop reply BEYOND the configured idle
	// timeout. The stall deploy sets GAME_STREAM_IDLE_TIMEOUT_MS=60000;
	// this sleeps 65s, so without the heartbeat wrapper the idle timer
	// would elapse mid-tool and raise a false NodeTimeoutError
	// (specs/043-llm-stream-stall-recovery/research.md R7: no LangChain
	// callback events fire while `bridge.dispatch` awaits the desktop).
	time.Sleep(stallToolReplyDelay)

	// when (3): the desktop finally replies to op (3,4) after the delay;
	// the tool resolves and the model resumes streaming (FR-003
	// acceptance scenario 2).
	respondToOperationWithScreenshot(t, conn, sessionID, opFrame,
		game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED,
		fmt.Sprintf("cell at (%d,%d) revealed", saoleiClick1X, saoleiClick1Y), screenshot)

	// then (2)+(3): drain the stream following the delayed reply for the
	// batch's second op (click 5,6), failing on ANY warn/wait frame. A
	// false stall during the 65s wait would have fired at ~60s — BEFORE
	// the reply above — terminating the turn and queueing warn + wait,
	// which the first reads below would surface (FR-003/SC-003; the
	// normal state after the reply is display frames until the dispatch).
	//
	// The drain cannot use a short read deadline as a silence probe: a
	// timed-out read poisons the connection — gorilla/websocket caches
	// the first read error and every later read returns the same error
	// (https://pkg.go.dev/github.com/gorilla/websocket#Conn.NextReader —
	// "Errors returned from this method are permanent. Once this method
	// returns a non-nil error, all subsequent calls to this method return
	// the same error"), which is why the original 500ms probe made the
	// op(5,6) read fail with the cached "i/o timeout" despite the
	// dispatch having happened (verified via the operation bridge sink
	// logs). The reads below use the standard per-read window and are
	// satisfied by the reply-driven stream.
	var op2Frame *game.TeamFrame
	for i := 0; i < 20; i++ {
		f := readWSFrame(t, conn)
		switch {
		case frameWarn(f) != nil:
			t.Fatalf("false stall: warn frame arrived while the saolei tool was executing > idleTimeout (FR-003/SC-003 — the heartbeat wrapper must keep the idle timer alive)")
		case frameWait(f) != nil:
			t.Fatalf("turn terminated with a wait frame while the saolei tool was executing > idleTimeout (false stall, FR-003/SC-003)")
		case frameOperationToolID(f) != "":
			op2Frame = f
		}
		if op2Frame != nil {
			break
		}
	}
	if op2Frame == nil {
		t.Fatal("did not receive an operation FlowPart frame from the agent (model→tool_call→dispatch chain did not fire)")
	}

	// then (3): the batch's second op (click 5,6) dispatched — each op of
	// the batch is an independent bridge.dispatch that must be read and
	// replied IN ORDER (one saolei_operate call → N dispatches → one
	// result, FR-002; sample_saolei_tools.yaml
	// saolei-init-followup-operate chains operations [click{3,4},
	// click{5,6}]).
	op2MMC := frameMouseMoveAndClick(op2Frame)
	if op2MMC == nil {
		t.Fatalf("saolei_operate op (5,6) did not dispatch a MouseMoveAndClickPart FlowPart; frame parts: %v",
			op2Frame.GetFlowParts().GetParts())
	}
	if err := assertMouseMoveAndClick(op2MMC, saoleiClick2CenterX, saoleiClick2CenterY,
		game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK); err != nil {
		t.Errorf("saolei_operate op (5,6) dispatch mismatch: %v", err)
	}
	respondToOperationWithScreenshot(t, conn, sessionID, op2Frame,
		game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED,
		fmt.Sprintf("cell at (%d,%d) revealed", saoleiClick2X, saoleiClick2Y), screenshot)

	// then (4): ONE result returns for the whole batch, the fixture
	// terminates with text, and the turn reaches its terminal wait. No
	// warn frame may appear anywhere in the stream.
	rest := drainUntilWait(t, conn)
	textSeen := false
	for _, f := range rest {
		if frameWarn(f) != nil {
			t.Errorf("warn frame found after the tool reply — the stall recovery fired despite the heartbeat (FR-003)")
		}
		if frameHasText(f) {
			textSeen = true
		}
	}
	if !textSeen {
		t.Errorf("no text frame in the %d post-reply frames — the turn did not resume after the tool result", len(rest))
	}
	if got := countWaitFrames(rest); got != 1 {
		t.Errorf("post-reply wait frame count = %d, want 1 (the turn must complete normally)", got)
	}
}

// TestAgentStallDetectedWithinConfiguredWindow anchors the spec 044 US1
// regression (FR-001/SC-004) on the stall deploy's explicit
// GAME_STREAM_IDLE_TIMEOUT_MS=60000 — the 60s minimum that FR-001 raised
// the old 15s floor to. A genuine silent-stream dropout (connection
// alive, zero events) must still be detected, and the detection must
// happen at approximately the CONFIGURED window — neither prematurely (a
// regression to the pre-044 15s/30s windows) nor after the 120s default
// (a regression that ignores the explicit env value, FR-003):
//
//  1. A user turn matching the "stall now" keyword makes fake-LLM emit
//     its reasoning delta and then stop with the connection alive
//     (sample_stall.yaml) — the exact real-silence failure mode.
//  2. The elapsed time from the last streamed chunk (the thinking frame)
//     to the detection is bracketed by [stallDetectMin, stallDetectMax]:
//     the old windows fire below the lower bound, the 120s default above
//     the upper bound, and the 60s window fires inside it.
//
// The detection behavior itself (warn + wait frames, buffer retention,
// recovery) is covered by TestAgentStallRecoveryWithQueuedMessage; this
// case pins the WINDOW that 044 recalibrated without duplicating that
// scenario's assertions (specs/044-llm-stall-recovery-fix/tasks.md T011
// case b).
func TestAgentStallDetectedWithinConfiguredWindow(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	profileName := fmt.Sprintf("stall-window-%s", uniqueSuffix())

	// given: a saolei-enabled profile and a connected session (gpt-4 — a
	// non-reasoning model, so the effective timeout is the deploy's
	// explicit 60s env value, FR-003).
	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")
	conn := connectAgentWSTrace(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// when (1): a user turn whose text matches the "stall now" keyword
	// makes fake-LLM emit the opening reasoning delta and then stop
	// (connection alive, no further data — sample_stall.yaml): a genuine
	// silent dropout, not a slow reply.
	sendText(t, conn, sessionID, "please stall now")
	thinkingFrame := drainWSFrame(t, conn, func(f *game.TeamFrame) bool {
		return frameHasThinking(f) && f.GetAgent() == "player"
	})
	if thinkingFrame == nil {
		t.Fatal("did not receive the player's thinking frame — the stall must start AFTER partial reasoning output")
	}
	stallStart := time.Now()

	// then (1): the stall is detected — a warn frame (visible notice,
	// FR-005) followed by a wait frame (idle, FR-004) terminates the
	// turn.
	stallFrames := drainUntilWait(t, conn)
	warnSeen := false
	for _, f := range stallFrames {
		if frameWarn(f) != nil {
			warnSeen = true
		}
	}
	if !warnSeen {
		t.Errorf("no warn frame among the %d stall-recovery frames — the silent dropout was not surfaced to the desktop (spec 044 FR-001/SC-004)", len(stallFrames))
	}
	if got := countWaitFrames(stallFrames); got != 1 {
		t.Errorf("wait frame count = %d, want 1 (the stall must terminate the turn, FR-004)", got)
	}

	// then (2): the detection fired within the configured 60s window. The
	// idle timer starts at the last streamed chunk (the reasoning delta),
	// so a healthy deploy detects at ~60s; a regression to the pre-044
	// 15s/30s windows detects below stallDetectMin, and ignoring the
	// explicit env (falling back to the 120s default) detects above
	// stallDetectMax.
	elapsed := time.Since(stallStart)
	if elapsed < stallDetectMin || elapsed > stallDetectMax {
		t.Errorf("stall detected after %s, want within [%s, %s] — the deploy's configured 60s window (spec 044 FR-001/SC-004)", elapsed, stallDetectMin, stallDetectMax)
	}
}

// TestAgentStallPersistsPartialOutput validates spec 044 US3
// (FR-004/FR-005/FR-007; SC-002/SC-003): when a stall terminates a turn
// mid-stream, the already-streamed partial output is persisted to the
// checkpoint and survives reconnection — ListMessages returns it, and the
// content block that was mid-stream at the stall (the tail part) carries
// the interrupted marker:
//
//  1. A user turn matching the "stall now" keyword makes fake-LLM emit
//     its reasoning delta and then stop with the connection alive
//     (sample_stall.yaml). The reasoning delta IS the partial output
//     streamed to the frontend before the stall; the stall template's
//     text block is never delivered, so the partial is reasoning-only
//     (tail = thinking → the interrupted marker lands on the
//     ThinkingPart, partial-output-contract.md §3).
//  2. Within the configured idle window (60s in the stall deploy) the
//     stall is detected and the turn terminates (warn + wait — 043's
//     finishError, FR-010 unchanged).
//  3. ListMessages on the player partition (the stalled node, FR-007)
//     returns the partial reasoning — the streamed output is no longer
//     lost on stall (SC-002) — and the thinking part carries
//     completion = PART_COMPLETION_INTERRUPTED (FR-005/SC-003; the wire
//     marker survives every protojson hop,
//     projects/game/proto_test.go TestPartCompletionEnumRoundtrip).
func TestAgentStallPersistsPartialOutput(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	profileName := fmt.Sprintf("stall-partial-%s", uniqueSuffix())

	// given: a saolei-enabled profile and a connected session (gpt-4 — a
	// non-reasoning model, so the deploy's explicit 60s env value is the
	// effective timeout, FR-003).
	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")
	conn := connectAgentWSTrace(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// when (1): a user turn whose text matches the "stall now" keyword
	// makes fake-LLM emit its reasoning delta and then stop (connection
	// alive, no further data — sample_stall.yaml). The reasoning delta
	// arrives as a live thinking frame — the partial output the desktop
	// saw before the stall.
	sendText(t, conn, sessionID, "please stall now")
	thinkingFrame := drainWSFrame(t, conn, func(f *game.TeamFrame) bool {
		return frameHasThinking(f) && f.GetAgent() == "player"
	})
	if thinkingFrame == nil {
		t.Fatal("did not receive the player's thinking frame — the stall must start AFTER partial reasoning output")
	}
	if got := frameThinking(thinkingFrame); got != expectedStallReasoning {
		t.Errorf("thinking content = %q, want %q (the stall template's reasoning)", got, expectedStallReasoning)
	}

	// then (1): within the configured idle window the stall is detected
	// and the turn terminates (warn + wait — 043's finishError terminal,
	// FR-010 unchanged). Persistence runs BEFORE the re-throw that
	// triggers finishError (session-team.ts persistPartialOutput), so by
	// the time the wait frame arrives the checkpoint write is complete.
	stallFrames := drainUntilWait(t, conn)
	warnSeen := false
	for _, f := range stallFrames {
		if frameWarn(f) != nil {
			warnSeen = true
		}
	}
	if !warnSeen {
		t.Errorf("no warn frame among the %d stall-recovery frames — the stall was not surfaced to the desktop (FR-005)", len(stallFrames))
	}
	if got := countWaitFrames(stallFrames); got != 1 {
		t.Errorf("wait frame count = %d, want 1 (the stall must terminate the turn, FR-004)", got)
	}

	// then (2): re-enter the session's history (ListMessages reads the
	// checkpoint — no WS reconnect needed) — the partial reasoning
	// survived the stall (FR-004/SC-002).
	lmr := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "player")
	if !messagesContainThinking(lmr.GetMessages(), expectedStallReasoning) {
		t.Errorf("ListMessages did not return the stalled turn's partial reasoning — the streamed output was lost on stall (spec 044 FR-004/SC-002)")
	}

	// then (3): the mid-stream block carries the interrupted marker — the
	// tail thinking part of the persisted partial has
	// completion = PART_COMPLETION_INTERRUPTED (FR-005/SC-003); a normal
	// complete part carries the default UNSPECIFIED (protojson omits it).
	interruptedSeen := false
	for _, m := range lmr.GetMessages() {
		for _, c := range messageThinkingCompletions(m) {
			if c == game.PartCompletion_PART_COMPLETION_INTERRUPTED {
				interruptedSeen = true
			}
		}
	}
	if !interruptedSeen {
		t.Errorf("no thinking part carries PART_COMPLETION_INTERRUPTED — the persisted partial lacks the interrupted marker (spec 044 FR-005/SC-003)")
	}
}
