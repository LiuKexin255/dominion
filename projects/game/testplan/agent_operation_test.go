// Package testplan contains agent operation-result integration tests.
//
// agent_operation_test.go validates the agent's mouse-tool dispatch chain
// end-to-end against the deployed agent (quickstart.md Scenario 7 / SC-001):
// a user turn makes the model emit a real mouse_move tool_call, the agent
// emits a tool_call MessagePart frame (live conversation channel), dispatches
// a MouseMovePart FlowPart through OperationBridge (control channel), and the
// test — playing the desktop — reads that operation Part and replies with a
// ToolResultPart. The agent then emits a tool_result MessagePart frame whose
// status is the REAL outcome (native mouse tool — D4). Both the succeeded and
// failed result paths are covered.
//
// spec 023 D10 decoupling is asserted: the dispatched FlowPart carries a
// bridge-minted operation-channel tool_id that is NOT the conversation-channel
// tool_call.id (research.md D10; data-model.md §4).
//
// Feature 015 split the single "mouse" tool into "mouse_move"
// (coordinates only) and "mouse_click" (click_type only, at current cursor
// position). The profiles below declare the split tool names so the agent
// compiles with the post-015 tool surface.
package testplan

import (
	"fmt"
	"strings"
	"testing"

	"dominion/common/gopkg/testtool"
	game "dominion/projects/game"
	"dominion/projects/game/pkg/gameconst"
)

// mouseSplitToolNames and expectedMouseMoveSuccessText live in
// helpers_test.go — shared by the agent_operation and agent_checkpoint
// suites (style/large_test.md §反模式3 — do not copy helpers).

// TestAgentOperationResultSuccess drives a real mouse_move tool_call from a
// user turn (the fake-LLM "mouse-trigger" Message), lets the agent dispatch
// the MouseMovePart through OperationBridge, and replies with a SUCCEEDED
// ToolResultPart. The model then continues with text, proving the full
// model→tool_call→dispatch→result chain fires and the connection survives.
//
// Live emission (spec 023 FR-006) is asserted: the agent emits a tool_call
// MessagePart frame before the operation and a tool_result MessagePart frame
// after the reply. The dispatched FlowPart's bridge-minted tool_id MUST
// differ from the conversation-channel tool_call.id (decoupling, research.md
// D10). The native mouse tool carries the REAL status (D4) — SUCCEEDED here.
func TestAgentOperationResultSuccess(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("op-suc-%s", uniqueSuffix())

	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		Parent:         gameconst.PromptsParent,
		AgentProfileId: profileName,
		AgentProfile: &game.AgentProfile{
			Model:        "gpt-4",
			SystemPrompt: "Operation result test agent.",
			ToolNames:    mouseSplitToolNames,
			Enabled:      true,
		},
	})
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	// when: a user turn matching the "mouse-trigger" keyword makes fake-LLM
	// return a mouse_move tool_call (the dispatch fix — Message.tool_call).
	sendTextWithProfile(t, conn, sessionID, profileName, "please move the mouse now")

	// then (1)+(2): the agent emits a tool_call MessagePart frame (live
	// conversation channel — spec 023 FR-006/D5) AND dispatches a
	// MouseMovePart FlowPart through OperationBridge (control channel). The
	// two frames race on the WS (dispatch sink-writes synchronously inside
	// the tool fn while stream.toolCalls yields asynchronously), so a single
	// read pass collects both without dropping either (see readToolCallAndOperation
	// doc).
	toolCallFrame, opFrame := readToolCallAndOperation(t, conn)
	toolCall := frameToolCall(toolCallFrame)
	if toolCall.GetName() != "mouse_move" {
		t.Errorf("tool_call.name = %q, want mouse_move (FR-002)", toolCall.GetName())
	}
	if toolCall.GetToolId() == "" {
		t.Error("tool_call.tool_id is empty — the conversation-channel id (LangChain tool_call.id) must be present for bubble grouping (FR-008)")
	}
	if toolCall.GetArgsJson() == "" {
		t.Error("tool_call.args_json is empty — the model's arguments must be carried verbatim (research.md D3)")
	}
	mm := frameMouseMove(opFrame)
	if mm == nil {
		t.Fatalf("mouse_move tool_call did not dispatch a MouseMovePart FlowPart; frame parts: %v",
			opFrame.GetFlowParts().GetParts())
	}
	if mm.GetXPx() != 100 || mm.GetYPx() != 200 {
		t.Errorf("mouse_move coords = (%d,%d), want (100,200) from the tool_call args", mm.GetXPx(), mm.GetYPx())
	}

	// then (3): D10 decoupling — the dispatched FlowPart's tool_id is a
	// bridge-minted operation-channel UUID, NOT the conversation-channel
	// tool_call.id (research.md D10; data-model.md §4).
	if mm.GetToolId() == "" {
		t.Error("dispatched FlowPart.tool_id is empty — the bridge must mint an operation-channel id (D10)")
	}
	if mm.GetToolId() == toolCall.GetToolId() {
		t.Errorf("decoupling violated (D10): FlowPart.tool_id (%q) == tool_call.id (%q); the two channels MUST NOT share an id",
			mm.GetToolId(), toolCall.GetToolId())
	}

	// The test (desktop) replies SUCCEEDED. The result text intentionally
	// avoids the "button"/"out of bounds" substrings so fake-LLM's
	// mouse-move-success-text (terminal) closes the tool loop with text
	// rather than chaining into another tool_call.
	respondToOperation(t, conn, sessionID, opFrame,
		game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED, "cursor moved to 100,200")

	// then (4): the agent emits a tool_result MessagePart frame carrying
	// the REAL status (native mouse tool — D4; spec 023 FR-006/D5). The
	// conversation-channel tool_id matches the earlier tool_call.id
	// (LangChain auto-wires ToolMessage.tool_call_id — bubble grouping).
	toolResultFrame := drainWSFrame(t, conn, frameHasToolResult)
	if toolResultFrame == nil {
		t.Fatal("did not receive a tool_result MessagePart frame after the desktop reply (FR-006)")
	}
	toolResult := frameToolResult(toolResultFrame)
	if toolResult.GetToolId() != toolCall.GetToolId() {
		t.Errorf("tool_result.tool_id = %q, want %q (conversation-channel grouping by LangChain tool_call.id)",
			toolResult.GetToolId(), toolCall.GetToolId())
	}
	if toolResult.GetStatus() != game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED {
		t.Errorf("tool_result.status = %v, want SUCCEEDED (native mouse tool carries the REAL status — D4)",
			toolResult.GetStatus())
	}

	// then (5): the model continues with the terminal text frame — the
	// connection survived the real dispatch→result cycle.
	textFrame := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return frameHasText(f)
	})
	if textFrame == nil {
		t.Fatal("connection unusable after successful tool result — agent did not recover")
	}
	if !strings.Contains(frameText(textFrame), expectedMouseMoveSuccessText) {
		t.Errorf("post-result text = %q, want to contain %q", frameText(textFrame), expectedMouseMoveSuccessText)
	}

	// then (6): ListMessages returns the history with the mouse_move
	// tool_call and the SUCCEEDED tool_result MessageParts (FR-009).
	lmr := listMessages(t, sutHostURL, sutEnvName, sessionID)
	if !messagesContainToolCall(lmr.GetMessages(), "mouse_move") {
		t.Errorf("ListMessages did not surface a mouse_move tool_call MessagePart (FR-006/FR-009)")
	}
	if !messagesContainToolResultStatus(lmr.GetMessages(), game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED) {
		t.Errorf("ListMessages did not surface a SUCCEEDED tool_result MessagePart (FR-013 / D4)")
	}
	// No operation FlowPart appears in Message.content (FR-005).
	assertMessageContentDisplayOnly(t, lmr.GetMessages())
}

// TestAgentOperationResultFailed drives the same mouse_move tool_call but
// replies with a FAILED ToolResultPart. The agent must handle the failure
// gracefully: the model continues and the connection remains usable for the
// subsequent turn. The result text avoids the "out of bounds"/"button"
// substrings so fake-LLM's terminal mouse-move-success-text closes the loop.
//
// The native mouse tool's REAL FAILED status is asserted both in the live
// tool_result frame and in ListMessages history (D4 — failed live ⇒ failed
// in history; spec 023 FR-013).
func TestAgentOperationResultFailed(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("op-fail-%s", uniqueSuffix())

	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		Parent:         gameconst.PromptsParent,
		AgentProfileId: profileName,
		AgentProfile: &game.AgentProfile{
			Model:        "gpt-4",
			SystemPrompt: "Operation failure test agent.",
			ToolNames:    mouseSplitToolNames,
			Enabled:      true,
		},
	})
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	// when: a user turn triggers the mouse_move tool_call.
	sendTextWithProfile(t, conn, sessionID, profileName, "position cursor over the icon")

	// Collect the live tool_call frame and the dispatched operation frame in
	// a single pass — the two race on the WS (see readToolCallAndOperation doc).
	toolCallFrame, opFrame := readToolCallAndOperation(t, conn)
	toolCall := frameToolCall(toolCallFrame)
	if frameMouseMove(opFrame) == nil {
		t.Fatalf("mouse_move tool_call did not dispatch a MouseMovePart FlowPart; frame parts: %v",
			opFrame.GetFlowParts().GetParts())
	}

	// D10 decoupling: bridge-minted op id ≠ conversation tool_call.id.
	if opToolID := frameOperationToolID(opFrame); opToolID == "" || opToolID == toolCall.GetToolId() {
		t.Errorf("decoupling violated (D10): op tool_id = %q, tool_call.id = %q", opToolID, toolCall.GetToolId())
	}

	// The desktop rejects the operation (FAILED). The message avoids
	// "out of bounds"/"button" so fake-LLM closes the loop with text.
	respondToOperation(t, conn, sessionID, opFrame,
		game.ToolResultStatus_TOOL_RESULT_STATUS_FAILED, "desktop rejected coordinate")

	// then: the live tool_result frame carries the REAL FAILED status (D4).
	toolResultFrame := drainWSFrame(t, conn, frameHasToolResult)
	if toolResultFrame == nil {
		t.Fatal("did not receive a tool_result MessagePart frame after the failed desktop reply (FR-006)")
	}
	if got := frameToolResult(toolResultFrame).GetStatus(); got != game.ToolResultStatus_TOOL_RESULT_STATUS_FAILED {
		t.Errorf("live tool_result.status = %v, want FAILED (native mouse tool carries the REAL status — D4)", got)
	}

	// The model must recover: it receives the failed result and continues
	// with a text response (or a warn). The connection remains usable.
	afterResp := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return frameHasText(f) || frameWarn(f) != nil
	})
	if afterResp == nil {
		t.Fatal("connection unusable after failed tool result — agent did not recover")
	}
	switch {
	case frameWarn(afterResp) != nil:
		t.Logf("connection survived failed tool result (warn): %q", frameWarn(afterResp).GetMessage())
	case strings.Contains(frameText(afterResp), expectedMouseMoveSuccessText):
		t.Logf("connection survived failed tool result: %q", frameText(afterResp))
	default:
		t.Logf("connection survived failed tool result: %q", frameText(afterResp))
	}

	// then: ListMessages history reflects the real FAILED status — failed
	// live ⇒ failed in history (spec 023 FR-013; data-model.md §6).
	lmr := listMessages(t, sutHostURL, sutEnvName, sessionID)
	if !messagesContainToolResultStatus(lmr.GetMessages(), game.ToolResultStatus_TOOL_RESULT_STATUS_FAILED) {
		t.Errorf("ListMessages did not surface a FAILED tool_result MessagePart — the real FAILED status must survive history (FR-013/D4)")
	}
	// No operation FlowPart appears in Message.content (FR-005).
	assertMessageContentDisplayOnly(t, lmr.GetMessages())
}

// TestAgentMouseSplitToolBinding verifies that a profile declaring the
// post-015 split tool names (mouse_move + mouse_click) binds successfully and
// the agent processes a text turn without error. This is a regression guard
// for the US2 buildTools wiring: the legacy single "mouse" name is no longer
// recognized, so a profile that still declared it would silently register
// zero tools. (mouse_move/mouse_click dispatch is now covered end-to-end by
// TestAgentOperationResultSuccess/Failed above, which drive real tool_calls.)
func TestAgentMouseSplitToolBinding(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("mouse-split-%s", uniqueSuffix())

	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		Parent:         gameconst.PromptsParent,
		AgentProfileId: profileName,
		AgentProfile: &game.AgentProfile{
			Model:        "gpt-4",
			SystemPrompt: "Mouse split tool binding test agent.",
			ToolNames:    mouseSplitToolNames,
			Enabled:      true,
		},
	})
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	// A text turn must produce a normal thinking + text response, proving
	// the adapter compiled with the split tools and the connection is usable.
	sendTextWithProfile(t, conn, sessionID, profileName, "hello mouse split")
	_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return frameHasThinking(f)
	})
	textResp := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return frameHasText(f)
	})
	if textResp == nil {
		t.Fatal("no text response — agent did not bind with split mouse tools")
	}
	t.Logf("agent bound with mouse_move+mouse_click, responded: %q",
		frameText(textResp))
}
