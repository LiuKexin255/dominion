// Package testplan contains agent operation-result integration tests.
// These tests validate the agent's handling of content PartBlock payloads
// carrying a ToolResultPart (simulating desktop-executed tool operations)
// through the WebSocket surface, verifying the connection survives both
// successful and failed results and remains usable for subsequent turns.
//
// Feature 015 split the single "mouse" tool into "mouse_move"
// (coordinates only) and "mouse_click" (click_type only, at current cursor
// position). The profiles below declare the split tool names so the agent
// compiles with the post-015 tool surface.
package testplan

import (
	"fmt"
	"testing"

	"dominion/common/gopkg/testtool"
	game "dominion/projects/game"
)

// mouseSplitToolNames is the post-015 mouse tool surface: mouse_move
// positions the cursor, mouse_click fires at the current position.
// Declaring both on a profile exercises the buildTools wiring that
// replaced the legacy single "mouse" name.
var mouseSplitToolNames = []string{"mouse_move", "mouse_click"}

// TestAgentOperationResultSuccess verifies that after a user turn, a content
// frame carrying a ToolResultPart with SUCCEEDED status (simulating a
// desktop-executed mouse click) is processed without crashing, and the
// connection remains usable for a subsequent turn.
func TestAgentOperationResultSuccess(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("op-suc-%s", uniqueSuffix())

	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		Parent:         "prompts",
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

	// Initial turn to ensure the adapter is bound before the result arrives.
	sendTextWithProfile(t, conn, sessionID, profileName, "hello before operation")
	_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return frameHasText(f) })

	// Simulate a desktop-executed tool result. After the US2 split a click
	// fires at the current cursor position (no coordinates carried).
	opResult := buildOperationResultFrame(
		sessionID, fmt.Sprintf("op-success-%s", uniqueSuffix()),
		game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED,
		"LEFT_CLICK at current position succeeded",
	)
	writeWSFrame(t, conn, opResult)

	// The subsequent turn must succeed — the connection survived.
	sendTextWithProfile(t, conn, sessionID, profileName, "hello after operation")
	afterResp := drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return frameHasText(f) })
	if afterResp == nil {
		t.Fatal("connection unusable after successful tool result — agent did not recover")
	}
	t.Logf("connection survived successful tool result: %q",
		frameText(afterResp))
}

// TestAgentOperationResultFailed verifies that a ToolResultPart with FAILED
// status (simulating an out-of-bounds mouse_move coordinate) is handled
// gracefully, and the connection remains usable for a subsequent turn.
func TestAgentOperationResultFailed(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("op-fail-%s", uniqueSuffix())

	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		Parent:         "prompts",
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

	// Simulate a failed mouse_move (out-of-bounds coordinate).
	failedResult := buildOperationResultFrame(
		sessionID, fmt.Sprintf("op-failed-%s", uniqueSuffix()),
		game.ToolResultStatus_TOOL_RESULT_STATUS_FAILED,
		"out of bounds: coordinate (99999,99999) exceeds screen",
	)
	writeWSFrame(t, conn, failedResult)

	// The subsequent turn must succeed — the connection survived the failure.
	sendTextWithProfile(t, conn, sessionID, profileName, "hello after failure")
	afterResp := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return frameHasText(f) || f.GetWarn() != nil
	})
	if afterResp == nil {
		t.Fatal("connection unusable after failed tool result — agent did not recover")
	}
	if afterResp.GetWarn() != nil {
		t.Logf("connection survived failed tool result (warn): %q",
			afterResp.GetWarn().GetMessage())
	} else {
		t.Logf("connection survived failed tool result: %q",
			frameText(afterResp))
	}
}

// TestAgentMouseSplitToolBinding verifies that a profile declaring the
// post-015 split tool names (mouse_move + mouse_click) binds successfully and
// the agent processes a text turn without error. This is a regression guard
// for the US2 buildTools wiring: the legacy single "mouse" name is no longer
// recognized, so a profile that still declared it would silently register
// zero tools.
//
// Note: fake-llm's stateless keyword-matched Message templates cannot
// initiate a tool_call (only tool-result responses chain further calls), so
// actual mouse_move/mouse_click dispatch is covered at the unit level
// (mouse-tool.test.ts) rather than here. This case confirms the split tool
// names are accepted end-to-end through the real AgentAdapterImpl compile
// path.
func TestAgentMouseSplitToolBinding(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("mouse-split-%s", uniqueSuffix())

	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		Parent:         "prompts",
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
