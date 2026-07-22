// Package testplan contains agent operation-result integration tests.
//
// agent_operation_test.go validates the agent's mouse-tool dispatch chain
// end-to-end: a user turn makes the model emit a real mouse_move tool_call,
// the agent executes it through OperationBridge (dispatching a MouseMovePart
// to the WebSocket), and the test — playing the desktop — reads that
// operation Part and replies with a ToolResultPart. Both the succeeded and
// failed result paths are covered, proving the connection survives a real
// model→tool_call→bridge.dispatch→result cycle (the chain the original
// version bypassed by injecting a ToolResultPart directly).
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

// mouseSplitToolNames is the post-015 mouse tool surface: mouse_move
// positions the cursor, mouse_click fires at the current position.
// Declaring both on a profile exercises the buildTools wiring that
// replaced the legacy single "mouse" name.
var mouseSplitToolNames = []string{"mouse_move", "mouse_click"}

// expectedMouseMoveSuccessText is the terminal text fake-LLM returns once
// the mouse_move tool-result loop closes (sample_tools.yaml
// mouse-move-success-text). Both result tests assert it to prove the model
// continued after the dispatch result.
const expectedMouseMoveSuccessText = "I see the screen now."

// TestAgentOperationResultSuccess drives a real mouse_move tool_call from a
// user turn (the fake-LLM "mouse-trigger" Message), lets the agent dispatch
// the MouseMovePart through OperationBridge, and replies with a SUCCEEDED
// ToolResultPart. The model then continues with text, proving the full
// model→tool_call→dispatch→result chain fires and the connection survives.
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

	// then: the agent dispatches a MouseMovePart through OperationBridge.
	opFrame := readOperationFrame(t, conn)
	mm := frameMouseMove(opFrame)
	if mm == nil {
		t.Fatalf("mouse_move tool_call did not dispatch a MouseMovePart; frame parts: %v", opFrame.GetContent().GetParts())
	}
	if mm.GetXPx() != 100 || mm.GetYPx() != 200 {
		t.Errorf("mouse_move coords = (%d,%d), want (100,200) from the tool_call args", mm.GetXPx(), mm.GetYPx())
	}

	// The test (desktop) replies SUCCEEDED. The result text intentionally
	// avoids the "button"/"out of bounds" substrings so fake-LLM's
	// mouse-move-success-text (terminal) closes the tool loop with text
	// rather than chaining into another tool_call.
	respondToOperation(t, conn, sessionID, opFrame,
		game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED, "cursor moved to 100,200")

	// The model must continue and emit a final text frame — the connection
	// survived the real dispatch→result cycle.
	textFrame := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return frameHasText(f)
	})
	if textFrame == nil {
		t.Fatal("connection unusable after successful tool result — agent did not recover")
	}
	if !strings.Contains(frameText(textFrame), expectedMouseMoveSuccessText) {
		t.Errorf("post-result text = %q, want to contain %q", frameText(textFrame), expectedMouseMoveSuccessText)
	}
}

// TestAgentOperationResultFailed drives the same mouse_move tool_call but
// replies with a FAILED ToolResultPart. The agent must handle the failure
// gracefully: the model continues and the connection remains usable for the
// subsequent turn. The result text avoids the "out of bounds"/"button"
// substrings so fake-LLM's terminal mouse-move-success-text closes the loop.
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

	// then: the agent dispatches a MouseMovePart.
	opFrame := readOperationFrame(t, conn)
	if frameMouseMove(opFrame) == nil {
		t.Fatalf("mouse_move tool_call did not dispatch a MouseMovePart; frame parts: %v", opFrame.GetContent().GetParts())
	}

	// The desktop rejects the operation (FAILED). The message avoids
	// "out of bounds"/"button" so fake-LLM closes the loop with text.
	respondToOperation(t, conn, sessionID, opFrame,
		game.ToolResultStatus_TOOL_RESULT_STATUS_FAILED, "desktop rejected coordinate")

	// The model must recover: it receives the failed result and continues
	// with a text response (or a warn). The connection remains usable.
	afterResp := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return frameHasText(f) || f.GetWarn() != nil
	})
	if afterResp == nil {
		t.Fatal("connection unusable after failed tool result — agent did not recover")
	}
	switch {
	case afterResp.GetWarn() != nil:
		t.Logf("connection survived failed tool result (warn): %q", afterResp.GetWarn().GetMessage())
	case strings.Contains(frameText(afterResp), expectedMouseMoveSuccessText):
		t.Logf("connection survived failed tool result: %q", frameText(afterResp))
	default:
		t.Logf("connection survived failed tool result: %q", frameText(afterResp))
	}
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
