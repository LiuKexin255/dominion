// Package testplan contains the saolei MCP large-test suite.
//
// agent_saolei_test.go validates the deployed agent's saolei MCP path
// (specs/023-saolei-mcp-refine) end-to-end: a saolei-enabled profile drives
// the model→tool_call→loopback-MCP→OperationBridge→desktop-WS chain across a
// stateless init→click→click sequence (back-to-back, no `saolei_update` —
// spec 023 FR-016/FR-021). The test "plays the desktop" — it reads the
// operation FlowParts the agent dispatches (a KeyboardPressPart for
// saolei_init's F2, MouseMoveAndClickPart for saolei_click's cell clicks)
// and echoes each back as a ToolResultPart so the supervised tool-call loop
// continues. This covers quickstart.md Scenario 5 / SC-004.
//
// Organised by MODULE per style/large_test.md (not by scenario/spec-id); it
// reuses the shared helpers in helpers_test.go.
package testplan

import (
	"fmt"
	"strings"
	"testing"

	"dominion/common/gopkg/testtool"
	game "dominion/projects/game"
	"dominion/projects/game/pkg/gameconst"
)

// saoleiMcpNames is the profile MCP selection that triggers the agent's
// saolei adapter path (llm.ts builds the loopback MultiServerMCPClient for a
// saolei profile — spec 023 FR-016; the four stateless saolei tools are
// registered inside createSaoleiMcpServer).
var saoleiMcpNames = []string{"saolei"}

// saolei cell geometry constants. The fake-LLM fixture drives
// saolei_click{3,4} then saolei_click{5,6}; their window-client centres per
// the fixed formula in projects/game/agent/src/mcp/saolei/geometry.ts
// (centerX(x) = 24 + x*32 + 16, centerY(y) = 200 + y*32 + 16) are asserted on
// the dispatched MouseMoveAndClickPart (specs/018-saolei-mcp/contracts/proto-operation-contract.md
// §3; data-model.md §7).
const (
	saoleiClick1X = 3
	saoleiClick1Y = 4
	saoleiClick2X = 5
	saoleiClick2Y = 6

	saoleiClick1CenterX = 136 // 24 + 3*32 + 16
	saoleiClick1CenterY = 344 // 200 + 4*32 + 16
	saoleiClick2CenterX = 200 // 24 + 5*32 + 16
	saoleiClick2CenterY = 408 // 200 + 6*32 + 16
)

// expectedSaoleiFinalText is the terminal text fake-LLM returns once the
// second saolei_click result reaches the model
// (sample_saolei_tools.yaml saolei-click-5-6-final-text). The test asserts
// it to prove the whole init→click→click chain completed.
const expectedSaoleiFinalText = "Minesweeper sequence complete."

// TestAgentSaoleiMcpStatelessFlow drives a full stateless saolei
// init→click→click sequence through the deployed agent and verifies each
// link of the chain (quickstart.md Scenario 5 / SC-004; spec 023 FR-016..FR-022):
//
//  1. A user turn matching the fake-LLM "saolei-start" Message triggers a
//     saolei_init tool_call (the dispatch fix — Message.tool_call).
//  2. The agent executes saolei_init via the loopback MCP server, which
//     dispatches a KeyboardPressPart{F2} through OperationBridge to the WS
//     as a flow_parts frame (control channel).
//  3. The test (playing desktop) reads that operation FlowPart and replies
//     with a SUCCEEDED ToolResultPart.
//  4. fake-LLM then returns saolei_click{3,4}; the agent dispatches a
//     MouseMoveAndClickPart{LEFT_CLICK, WINDOW_MESSAGE} at the cell centre.
//  5. The test replies SUCCEEDED; fake-LLM returns saolei_click{5,6}
//     (back-to-back — no `saolei_update`, no rejection; FR-021). The agent
//     dispatches the second MouseMoveAndClickPart and the test replies
//     SUCCEEDED. fake-LLM then returns the final text response.
//  6. ListMessages returns Messages whose content.parts include a
//     tool_call MessagePart (name + args_json) and a tool_result MessagePart
//     for each saolei tool invocation. Per spec 023 D12 the saolei (MCP)
//     tool_result status is neutral (TOOL_RESULT_STATUS_UNSPECIFIED), never
//     FAILED — the original spurious-"failed" bug is fixed.
//  7. No operation FlowPart (KeyboardPress / MouseMoveAndClick) appears in
//     Message.content — operations are control-only (FR-005 / FR-004).
func TestAgentSaoleiMcpStatelessFlow(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("saolei-%s", uniqueSuffix())

	// given: a saolei-enabled profile. The model name is non-Anthropic so
	// ModelProviderCache routes to the OpenAI platform (fake-llm). The four
	// stateless saolei tools are surfaced via the loopback MCP client.
	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		Parent:         gameconst.PromptsParent,
		AgentProfileId: profileName,
		AgentProfile: &game.AgentProfile{
			Model:        "gpt-4",
			SystemPrompt: "You operate minesweeper via saolei tools.",
			McpNames:     saoleiMcpNames,
			Enabled:      true,
		},
	})
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	// when: a user turn whose text matches the "saolei-start" keyword,
	// making fake-LLM return the first saolei_init tool_call.
	sendTextWithProfile(t, conn, sessionID, profileName, "please start saolei game")

	// then (1): saolei_init dispatches an F2 KeyboardPressPart FlowPart. The
	// test plays desktop: read the flow_parts frame, assert F2, reply SUCCEEDED.
	initFrame := readOperationFrame(t, conn)
	kp := frameKeyboardPress(initFrame)
	if kp == nil {
		t.Fatalf("saolei_init did not dispatch a KeyboardPressPart FlowPart; frame parts: %v",
			initFrame.GetFlowParts().GetParts())
	}
	if kp.GetKey() != game.KeyboardKey_KEYBOARD_KEY_F2 {
		t.Errorf("saolei_init key = %v, want KEYBOARD_KEY_F2 (spec 023 FR-019)", kp.GetKey())
	}
	respondToOperation(t, conn, sessionID, initFrame,
		game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED, "F2 pressed, new game started")

	// then (2): saolei_click(3,4) dispatches a MouseMoveAndClickPart at the
	// cell centre with LEFT_CLICK + WINDOW_MESSAGE (spec 023 FR-020;
	// research.md D5 / specs/018-saolei-mcp/contracts/proto-operation-contract.md §3).
	click1Frame := readOperationFrame(t, conn)
	mmc1 := frameMouseMoveAndClick(click1Frame)
	if mmc1 == nil {
		t.Fatalf("saolei_click(3,4) did not dispatch a MouseMoveAndClickPart FlowPart; frame parts: %v",
			click1Frame.GetFlowParts().GetParts())
	}
	if err := assertMouseMoveAndClick(mmc1, saoleiClick1CenterX, saoleiClick1CenterY,
		game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK); err != nil {
		t.Errorf("saolei_click(3,4) dispatch mismatch: %v", err)
	}
	// Tag the result message with the coordinate so fake-LLM can chain into
	// the second click (sample_saolei_tools.yaml saolei-click-3-4-followup-click
	// matches on the "(3,4)" substring).
	respondToOperation(t, conn, sessionID, click1Frame,
		game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED, "cell at (3,4) revealed")

	// then (3): saolei_click(5,6) is accepted back-to-back — no
	// "must update first" rejection (spec 023 FR-021 / research.md D7 — the
	// stateless MCP removed the operate→update alternation).
	click2Frame := readOperationFrame(t, conn)
	mmc2 := frameMouseMoveAndClick(click2Frame)
	if mmc2 == nil {
		t.Fatalf("saolei_click(5,6) did not dispatch a MouseMoveAndClickPart FlowPart; frame parts: %v",
			click2Frame.GetFlowParts().GetParts())
	}
	if err := assertMouseMoveAndClick(mmc2, saoleiClick2CenterX, saoleiClick2CenterY,
		game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK); err != nil {
		t.Errorf("saolei_click(5,6) dispatch mismatch: %v", err)
	}
	respondToOperation(t, conn, sessionID, click2Frame,
		game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED, "cell at (5,6) revealed")

	// then (4): the model emits the final text response, proving the whole
	// init→click→click chain completed and the connection remains usable.
	textFrame := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return frameHasText(f)
	})
	if textFrame == nil {
		t.Fatal("did not receive a final text frame — saolei init→click→click chain did not complete")
	}
	if !strings.Contains(frameText(textFrame), expectedSaoleiFinalText) {
		t.Errorf("final text = %q, want to contain %q", frameText(textFrame), expectedSaoleiFinalText)
	}

	// then (5): ListMessages returns the conversation history. Each saolei
	// tool invocation surfaces as a tool_call MessagePart (name + args_json)
	// plus its tool_result MessagePart (spec 023 FR-002/FR-006/FR-009).
	lmr := listMessages(t, sutHostURL, sutEnvName, sessionID)
	if !messagesContainToolCall(lmr.GetMessages(), "saolei_init") {
		t.Errorf("ListMessages did not surface a saolei_init tool_call MessagePart (spec 023 FR-006)")
	}
	if !messagesContainToolCall(lmr.GetMessages(), "saolei_click") {
		t.Errorf("ListMessages did not surface a saolei_click tool_call MessagePart (spec 023 FR-006)")
	}
	// tool_call args_json carries the model's arguments verbatim (research.md D3).
	if got := firstToolCallArgsJSON(lmr.GetMessages(), "saolei_click"); got == "" {
		t.Errorf("ListMessages saolei_click tool_call args_json is empty (spec 023 FR-002 / research.md D3)")
	}

	// then (6): per spec 023 D12, saolei is an MCP tool — the adapter-built
	// ToolMessage carries no additional_kwargs.toolResultStatus, so the
	// reconstructed tool_result status is neutral (TOOL_RESULT_STATUS_UNSPECIFIED),
	// NEVER FAILED. This is the original spurious-"failed" bug fix. The
	// status is asserted to be exactly UNSPECIFIED — not merely "any
	// non-FAILED value" — so a regression that flips saolei to SUCCEEDED
	// is also caught (that would mean the adapter started carrying real
	// status for MCP tools, contradicting D12).
	for i, m := range lmr.GetMessages() {
		for _, status := range messageToolResultStatuses(m) {
			if status != game.ToolResultStatus_TOOL_RESULT_STATUS_UNSPECIFIED {
				t.Errorf("message[%d]: saolei tool_result status = %v, want UNSPECIFIED (neutral) per spec 023 D12", i, status)
			}
		}
	}

	// then (7): no operation FlowPart appears in Message.content. Operations
	// are control-only (spec 023 FR-004/FR-005) — the KeyboardPress /
	// MouseMoveAndClick Parts the test read off the WS are flow_parts frames,
	// never reconstructed into Message.content. Message.content is typed as
	// MessageParts so this is structural, but the shared helper asserts the
	// rendered kinds so a future regression that leaks operations into
	// history is caught (data-model.md §4).
	assertMessageContentDisplayOnly(t, lmr.GetMessages())
}

// assertMouseMoveAndClick verifies a MouseMoveAndClickPart carries the
// expected centre coordinates, LEFT_CLICK action, and WINDOW_MESSAGE method
// (the desktop-facing saolei contract — spec 023 FR-020 / spec 018 FR-004b).
func assertMouseMoveAndClick(p *game.MouseMoveAndClickPart, wantX, wantY int32, wantClick game.MouseClickAction) error {
	if p.GetXPx() != wantX || p.GetYPx() != wantY {
		return fmt.Errorf("coords = (%d,%d), want (%d,%d)", p.GetXPx(), p.GetYPx(), wantX, wantY)
	}
	if p.GetClick() != wantClick {
		return fmt.Errorf("click = %v, want %v", p.GetClick(), wantClick)
	}
	if p.GetMethod() != game.MouseInputMethod_MOUSE_INPUT_METHOD_WINDOW_MESSAGE {
		return fmt.Errorf("method = %v, want WINDOW_MESSAGE", p.GetMethod())
	}
	return nil
}
