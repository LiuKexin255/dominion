// Package testplan contains the saolei MCP large-test suite.
//
// agent_saolei_test.go validates the deployed agent's saolei MCP path
// (specs/018-saolei-mcp) end-to-end: a saolei-enabled profile makes the
// model drive the real model→tool_call→loopback-MCP→OperationBridge→
// desktop-WS chain. The test "plays the desktop" — it reads the operation
// Parts the agent dispatches (a KeyboardPressPart for saolei_init's F2, a
// MouseMoveAndClickPart for saolei_click's cell click) and echoes each back
// as a ToolResultPart so the supervised tool-call loop continues. This
// covers quickstart.md Scenario 7 / SC-002 / SC-003 / SC-007.
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
// saolei adapter path (llm.ts excludes mouse tools and builds the loopback
// MultiServerMCPClient for a saolei profile — spec 018 FR-012/FR-002b).
var saoleiMcpNames = []string{"saolei"}

// saoleiInitClickTarget is the grid coordinate the fake-LLM testdata drives
// (sample_saolei_tools.yaml saolei-init-followup-click → saolei_click{3,4}).
// Its window-client centre per data-model.md §5 is (24+3*32+16, 200+4*32+16)
// = (136, 344), asserted on the dispatched MouseMoveAndClickPart.
const (
	saoleiInitClickX       = 3
	saoleiInitClickY       = 4
	saoleiInitClickCenterX = 136 // 24 + 3*32 + 16
	saoleiInitClickCenterY = 344 // 200 + 4*32 + 16
)

// TestAgentSaoleiMcpToolFlow drives a full saolei init→click→update sequence
// through the deployed agent and verifies each link of the chain:
//
//  1. A user turn matching the fake-LLM "saolei-start" Message triggers a
//     saolei_init tool_call (the dispatch fix — Message.tool_call).
//  2. The agent executes saolei_init via the loopback MCP server, which
//     dispatches a KeyboardPressPart{F2} through OperationBridge to the WS.
//  3. The test (playing desktop) reads that operation Part and replies with a
//     SUCCEEDED ToolResultPart.
//  4. fake-LLM then returns saolei_click{3,4}; the agent dispatches a
//     MouseMoveAndClickPart{LEFT_CLICK, WINDOW_MESSAGE} at the cell centre.
//  5. The test replies SUCCEEDED; fake-LLM returns saolei_update (no dispatch
//     — state update only), then a final text response.
//
// Covers spec 018 FR-006/FR-007/FR-011, quickstart.md Scenario 7, SC-002
// (correct Part dispatch) and SC-007 (model reveal sequence).
func TestAgentSaoleiMcpToolFlow(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("saolei-%s", uniqueSuffix())

	// given: a saolei-enabled profile. mouse tools are omitted — a saolei
	// profile surfaces the five saolei tools via the MCP client instead
	// (FR-012). The model name is non-Anthropic so ModelProviderCache
	// routes to the OpenAI platform (fake-llm).
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

	// then (1): saolei_init dispatches an F2 KeyboardPressPart. The test
	// plays desktop: read the operation frame, assert F2, reply SUCCEEDED.
	initFrame := readOperationFrame(t, conn)
	kp := frameKeyboardPress(initFrame)
	if kp == nil {
		t.Fatalf("saolei_init did not dispatch a KeyboardPressPart; frame parts: %v", initFrame.GetContent().GetParts())
	}
	if kp.GetKey() != game.KeyboardKey_KEYBOARD_KEY_F2 {
		t.Errorf("saolei_init key = %v, want KEYBOARD_KEY_F2 (FR-006)", kp.GetKey())
	}
	respondToOperation(t, conn, sessionID, initFrame,
		game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED, "F2 pressed, new game started")

	// then (2): saolei_click dispatches a MouseMoveAndClickPart at the cell
	// centre with LEFT_CLICK + WINDOW_MESSAGE (FR-007 / research.md D5).
	clickFrame := readOperationFrame(t, conn)
	mmc := frameMouseMoveAndClick(clickFrame)
	if mmc == nil {
		t.Fatalf("saolei_click did not dispatch a MouseMoveAndClickPart; frame parts: %v", clickFrame.GetContent().GetParts())
	}
	if mmc.GetClick() != game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK {
		t.Errorf("saolei_click action = %v, want LEFT_CLICK (FR-007)", mmc.GetClick())
	}
	if mmc.GetMethod() != game.MouseInputMethod_MOUSE_INPUT_METHOD_WINDOW_MESSAGE {
		t.Errorf("saolei_click method = %v, want WINDOW_MESSAGE (research.md D5)", mmc.GetMethod())
	}
	if mmc.GetXPx() != saoleiInitClickCenterX || mmc.GetYPx() != saoleiInitClickCenterY {
		t.Errorf("saolei_click coords = (%d,%d), want (%d,%d) [centre of (%d,%d) per data-model.md §5]",
			mmc.GetXPx(), mmc.GetYPx(), saoleiInitClickCenterX, saoleiInitClickCenterY,
			saoleiInitClickX, saoleiInitClickY)
	}
	respondToOperation(t, conn, sessionID, clickFrame,
		game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED, "cell revealed")

	// then (3): saolei_update applies state without dispatching, then the
	// model emits the final text (saolei-update-final-text). Draining for a
	// text frame proves the whole init→click→update chain completed and the
	// connection remains usable (SC-007).
	textFrame := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return frameHasText(f)
	})
	if textFrame == nil {
		t.Fatal("did not receive a final text frame — saolei init→click→update chain did not complete")
	}
	if !strings.Contains(frameText(textFrame), "Minesweeper sequence complete.") {
		t.Errorf("final text = %q, want to contain the saolei completion text", frameText(textFrame))
	}
}
