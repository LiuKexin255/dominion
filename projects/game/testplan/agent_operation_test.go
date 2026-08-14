// Package testplan contains agent operation-dispatch integration tests.
//
// agent_operation_test.go validates the OperationBridge dispatch loop
// end-to-end against the deployed saolei TEAM graph: a user turn makes the
// team's player agent emit a real saolei tool_call (spec
// 031-team-template-mode FR-010 — the player is the ONLY agent holding the
// saolei MCP tools; the tools are template-fixed per FR-028, the former
// per-profile mouse tools are gone), the agent emits a tool_call MessagePart
// frame (live conversation channel), dispatches a FlowPart through
// OperationBridge (control channel), and the test — playing the desktop —
// reads that operation Part and replies with a FlowResultPart (spec 025
// FR-023/FR-024 — control channel). The agent's bridge.handleResult resolves
// the pending dispatch from the FlowResultPart and the model continues.
//
// This suite is the post-031 equivalent of the former mouse-tool dispatch
// suite (spec 023 D10 decoupling is asserted: the dispatched FlowPart carries
// a bridge-minted operation-channel tool_id that is NOT the conversation-
// channel tool_call.id — research.md D10; data-model.md §4). The mouse_move
// tool itself no longer exists: the saolei template fixes the player's tools
// to the saolei MCP tools (FR-028), so the dispatch chain is driven with
// saolei_init + the merged dual-form saolei_operate (spec
// 039-planner-memory-calibration FR-001 — the fixture chains the init result
// into ONE operate batch whose two ops dispatch in order). The mouse-specific
// screenshot-forwarding behaviour (FlowResultPart.screenshot → display
// tool_result.screenshot, spec 025 FR-025) is intentionally NOT covered here
// — saolei tool results are TEXT boards and never carry screenshots (spec 025
// FR-022).
package testplan

import (
	"fmt"
	"strings"
	"testing"

	"dominion/common/gopkg/testtool"
	game "dominion/projects/game"
)

// TestAgentOperationDispatchLoopSuccess drives a real saolei_operate dispatch
// loop from a user turn (the fake-LLM "saolei-start" Message): the player
// agent executes saolei_init (F2 dispatch), the test replies with a
// recognizable in-progress board screenshot, the fake-LLM chains the init
// result into ONE saolei_operate BATCH call (spec 039-planner-memory-
// calibration FR-001/FR-002 — operations [click{3,4}, click{5,6}] dispatching
// two MouseMoveAndClickParts IN ORDER), and the chain closes with the final
// text. The assertions focus on the DISPATCH MECHANICS (spec 023 D10
// decoupling + conversation-channel grouping) rather than the text-board
// return contract (covered by agent_saolei_test.go):
//
//  1. The live tool_call MessagePart frame and the dispatched operation
//     FlowPart frame both arrive (they race on the WS — collected in a
//     single pass by readToolCallAndOperation).
//  2. D10 decoupling: the dispatched FlowPart's bridge-minted tool_id
//     differs from the conversation-channel tool_call.id.
//  3. The display tool_result frame groups by the conversation tool_call.id
//     (LangChain auto-wires ToolMessage.tool_call_id).
//  4. The model continues with the terminal text — the dispatch→result loop
//     completed and the connection survived.
//  5. ListMessages surfaces the saolei tool_call/tool_result MessageParts
//     with no operation FlowPart leaking into Message.content (FR-005).
func TestAgentOperationDispatchLoopSuccess(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := "op-suc-" + uniqueSuffix()

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// when: a user turn matching the "saolei-start" keyword makes fake-LLM
	// return a saolei_init tool_call.
	sendText(t, conn, sessionID, "please start saolei game")

	// then (1)+(2): the player emits a tool_call MessagePart frame AND
	// dispatches the F2 KeyboardPressPart FlowPart through OperationBridge.
	// The two frames race on the WS (dispatch sink-writes synchronously
	// inside the tool fn while stream.toolCalls yields asynchronously), so a
	// single read pass collects both without dropping either.
	//
	// The player-scoped read skips the 041 real-time init frames: when the
	// one-shot init turn is still in flight at Connect, it pushes a planner
	// instruct_player toolCall frame (agent=planner — contract §2.2) that
	// would otherwise shadow the user turn's saolei_init (agent=player) in
	// the first toolCall slot (specs/041-realtime-init-push/
	// contracts/realtime-channel-contract.md §2.2).
	toolCallFrame, initOpFrame := readPlayerToolCallAndOperation(t, conn)
	toolCall := frameToolCall(toolCallFrame)
	if toolCall.GetName() != "saolei_init" {
		t.Errorf("tool_call.name = %q, want saolei_init (FR-002)", toolCall.GetName())
	}
	if toolCall.GetToolId() == "" {
		t.Error("tool_call.tool_id is empty — the conversation-channel id (LangChain tool_call.id) must be present for bubble grouping (FR-008)")
	}
	kp := frameKeyboardPress(initOpFrame)
	if kp == nil {
		t.Fatalf("saolei_init did not dispatch a KeyboardPressPart FlowPart; frame parts: %v",
			initOpFrame.GetFlowParts().GetParts())
	}
	if kp.GetKey() != game.KeyboardKey_KEYBOARD_KEY_F2 {
		t.Errorf("saolei_init key = %v, want KEYBOARD_KEY_F2 (spec 025 FR-019 retained)", kp.GetKey())
	}

	// D10 decoupling: the dispatched FlowPart's tool_id is a bridge-minted
	// operation-channel UUID, NOT the conversation-channel tool_call.id
	// (research.md D10; data-model.md §4).
	if kp.GetToolId() == "" {
		t.Error("dispatched FlowPart.tool_id is empty — the bridge must mint an operation-channel id (D10)")
	}
	if kp.GetToolId() == toolCall.GetToolId() {
		t.Errorf("decoupling violated (D10): FlowPart.tool_id (%q) == tool_call.id (%q); the two channels MUST NOT share an id",
			kp.GetToolId(), toolCall.GetToolId())
	}

	// The test (desktop) replies with a recognizable in-progress board so
	// the recognition engine seeds/updates the state.
	screenshot := buildSaoleiFlowResultScreenshot(saoleiBoardInitPNG)
	respondToOperationWithScreenshot(t, conn, sessionID, initOpFrame,
		game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED, "F2 pressed, new game started", screenshot)

	// then (3): the display tool_result frame groups by the conversation
	// tool_call.id of the FIRST saolei_init call (bubble grouping by
	// LangChain tool_call.id). Under the streaming contract (spec
	// 031-team-template-mode FR-034; team-graph-contract.md §2.1) the init
	// tool_result is emitted in real time as soon as the desktop
	// FlowResultPart resolves the dispatch, so it MUST be drained
	// immediately after the init reply — draining later (after the click
	// loop below) would only find a stale saolei_operate tool_result whose
	// tool_id differs from the init tool_call.id. Same pattern as
	// agent_checkpoint_test.go TestAgentCheckpointToolResultStatusPersists.
	toolResultFrame := drainWSFrame(t, conn, frameHasToolResult)
	if toolResultFrame == nil {
		t.Fatal("did not receive a tool_result MessagePart frame after the desktop reply (FR-006)")
	}

	// The fake-LLM fixture chains saolei_init → one saolei_operate batch
	// (operations [click{3,4}, click{5,6}] — sample_saolei_tools.yaml). Play
	// the desktop through both cell dispatches, asserting each dispatch
	// carries the centre of the cell the model targeted (saolei_fixtures_test.go —
	// WM client-space centres: (3,4)→(136,248), (5,6)→(200,312)). The operate
	// tool_results stream in real time alongside the loop's
	// readOperationFrame reads and are discarded there — this test asserts
	// the init tool_result only.
	clickSteps := []struct {
		cellX, cellY     int32
		centerX, centerY int32
	}{
		{saoleiClick1X, saoleiClick1Y, saoleiClick1CenterX, saoleiClick1CenterY},
		{saoleiClick2X, saoleiClick2Y, saoleiClick2CenterX, saoleiClick2CenterY},
	}
	for _, step := range clickSteps {
		clickFrame := readOperationFrame(t, conn)
		mmc := frameMouseMoveAndClick(clickFrame)
		if mmc == nil {
			t.Fatalf("saolei_operate op (%d,%d) did not dispatch a MouseMoveAndClickPart FlowPart; frame parts: %v",
				step.cellX, step.cellY, clickFrame.GetFlowParts().GetParts())
		}
		if err := assertMouseMoveAndClick(mmc, step.centerX, step.centerY,
			game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK); err != nil {
			t.Errorf("saolei_operate op (%d,%d) dispatch mismatch: %v", step.cellX, step.cellY, err)
		}
		respondToOperationWithScreenshot(t, conn, sessionID, clickFrame,
			game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED,
			fmt.Sprintf("cell at (%d,%d) revealed", step.cellX, step.cellY), screenshot)
	}

	// then (3) continued: the init tool_result drained right after the init
	// reply groups by the conversation tool_call.id of the FIRST saolei_init
	// call.
	toolResult := frameToolResult(toolResultFrame)
	if toolResult.GetToolId() != toolCall.GetToolId() {
		t.Errorf("tool_result.tool_id = %q, want %q (conversation-channel grouping by LangChain tool_call.id)",
			toolResult.GetToolId(), toolCall.GetToolId())
	}
	// saolei is an MCP tool — the reconstructed status is neutral
	// (TOOL_RESULT_STATUS_UNSPECIFIED, never FAILED) per spec 023 D12.
	if toolResult.GetStatus() != game.ToolResultStatus_TOOL_RESULT_STATUS_UNSPECIFIED {
		t.Errorf("saolei tool_result.status = %v, want UNSPECIFIED (neutral — spec 023 D12)",
			toolResult.GetStatus())
	}

	// then (4): the model continues with the terminal text frame — the
	// connection survived the real dispatch→result cycles.
	textFrame := drainWSFrame(t, conn, func(f *game.TeamFrame) bool {
		return frameHasText(f)
	})
	if textFrame == nil {
		t.Fatal("connection unusable after saolei dispatch loop — agent did not recover")
	}
	if !strings.Contains(frameText(textFrame), expectedSaoleiFinalText) {
		t.Errorf("post-result text = %q, want to contain %q", frameText(textFrame), expectedSaoleiFinalText)
	}

	// then (5): ListMessages returns the history with the saolei tool_call
	// and tool_result MessageParts (FR-009) and no operation FlowPart leaks
	// into Message.content (FR-005).
	lmr := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "player")
	if !messagesContainToolCall(lmr.GetMessages(), "saolei_init") {
		t.Errorf("ListMessages did not surface a saolei_init tool_call MessagePart (FR-006/FR-009)")
	}
	if !messagesContainToolCall(lmr.GetMessages(), "saolei_operate") {
		t.Errorf("ListMessages did not surface a saolei_operate tool_call MessagePart (FR-006/FR-009)")
	}
	assertMessageContentDisplayOnly(t, lmr.GetMessages())
}

// TestAgentOperationDispatchFailureRecovers drives a saolei_init dispatch
// whose FlowResultPart reports FAILED with NO screenshot. The recognition
// engine cannot decode a board (no screenshot — FR-017 invalidates the
// session state), so saolei_init returns the "unable to recognize" outcome
// instead of a board. The test asserts:
//
//  1. The recognition-failure tool_result is surfaced (deterministic: the
//     init outcome is fixed).
//  2. The connection remains usable: a SECOND turn (greeting keyword)
//     completes normally — a failed dispatch must not wedge the per-session
//     TurnLoop.
//
// The post-failure chain is deliberately NOT asserted: with the state
// invalidated, the fake-LLM tool-result matcher finds no candidate for the
// follow-up click (the rejection text carries no "(3,4)"/"(5,6)"
// coordinates) and falls back to a random tool response, which is
// nondeterministic by design (matcher.go no-match fallback).
func TestAgentOperationDispatchFailureRecovers(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := "op-fail-" + uniqueSuffix()

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// when: a user turn triggers the saolei_init tool_call.
	sendText(t, conn, sessionID, "please start saolei game")

	// Collect the live tool_call frame and the dispatched F2 operation frame
	// in a single pass — the two race on the WS.
	_, opFrame := readToolCallAndOperation(t, conn)
	if frameKeyboardPress(opFrame) == nil {
		t.Fatalf("saolei_init did not dispatch a KeyboardPressPart FlowPart; frame parts: %v",
			opFrame.GetFlowParts().GetParts())
	}

	// The desktop rejects the operation (FAILED) and attaches NO screenshot.
	respondToOperation(t, conn, sessionID, opFrame,
		game.ToolResultStatus_TOOL_RESULT_STATUS_FAILED, "desktop rejected operation")

	// then (1): the recognition-failure tool_result is surfaced (FR-017 —
	// a failed/no-screenshot result invalidates the recognized state).
	failedResult := drainWSFrame(t, conn, frameHasToolResult)
	if failedResult == nil {
		t.Fatal("did not receive a tool_result MessagePart frame after the failed desktop reply (FR-006)")
	}
	if !strings.Contains(frameToolResult(failedResult).GetMessage(), "unable to recognize") {
		t.Errorf("failed tool_result message = %q, want to contain \"unable to recognize\" (FR-017)",
			frameToolResult(failedResult).GetMessage())
	}

	// then (2): the connection stays usable — a second turn (greeting
	// keyword) completes with the deterministic greeting response. drainWSFrame
	// skips any leftover frames from the failed turn.
	sendText(t, conn, sessionID, "hello after failed dispatch")
	thinkingFrame := drainWSFrame(t, conn, func(f *game.TeamFrame) bool {
		return frameHasThinking(f)
	})
	if thinkingFrame == nil {
		t.Fatal("post-failure turn: did not receive a thinking frame — the connection is unusable after the failed dispatch")
	}
	if !strings.Contains(frameThinking(thinkingFrame), expectedGreetingReasoning) {
		t.Errorf("post-failure thinking = %q, want to contain %q",
			frameThinking(thinkingFrame), expectedGreetingReasoning)
	}
	textResp := drainWSFrame(t, conn, func(f *game.TeamFrame) bool {
		return frameHasText(f)
	})
	if textResp == nil {
		t.Fatal("post-failure turn: did not receive a text frame")
	}
	if !strings.Contains(frameText(textResp), expectedGreetingText) {
		t.Errorf("post-failure text = %q, want to contain %q", frameText(textResp), expectedGreetingText)
	}
}
