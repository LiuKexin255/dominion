// Package testplan contains agent operation-result integration tests.
// These tests validate the agent's handling of AgentOperationResultFrame
// payloads (simulating desktop-executed mouse operations) through the
// WebSocket surface, verifying the connection survives both successful
// and failed operation results and remains usable for subsequent turns.
package testplan

import (
	"fmt"
	"testing"

	"dominion/common/gopkg/testtool"
	game "dominion/projects/game"
)

// TestAgentOperationResultSuccess verifies that after a user turn, an
// operation_result frame with SUCCEEDED status (simulating a desktop-
// executed mouse click) is processed without crashing, and the
// connection remains usable for a subsequent turn.
func TestAgentOperationResultSuccess(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("op-suc-%s", uniqueSuffix())

	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		AgentProfileName: profileName,
		Model:            "gpt-4",
		SystemPrompt:     "Operation result test agent.",
		ToolNames:        []string{"mouse"},
		Enabled:          true,
	})
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	// Initial turn to ensure the adapter is bound before the result arrives.
	sendTextWithProfile(t, conn, sessionID, profileName, "hello before operation")
	_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return f.GetText() != nil })

	// Simulate a desktop-executed operation result.
	opResult := buildOperationResultFrame(
		sessionID, fmt.Sprintf("op-success-%s", uniqueSuffix()),
		game.AgentOperationResultStatus_AGENT_OPERATION_RESULT_STATUS_SUCCEEDED,
		"LEFT_CLICK at (100,200) succeeded",
	)
	writeWSFrame(t, conn, opResult)

	// The subsequent turn must succeed — the connection survived.
	sendTextWithProfile(t, conn, sessionID, profileName, "hello after operation")
	afterResp := drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return f.GetText() != nil })
	if afterResp == nil {
		t.Fatal("connection unusable after successful operation_result — agent did not recover")
	}
	t.Logf("connection survived successful operation_result: %q",
		afterResp.GetText().GetContent())
}

// TestAgentOperationResultFailed verifies that an operation_result with
// FAILED status (simulating an out-of-bounds mouse coordinate) is
// handled gracefully, and the connection remains usable for a subsequent
// turn.
func TestAgentOperationResultFailed(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("op-fail-%s", uniqueSuffix())

	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		AgentProfileName: profileName,
		Model:            "gpt-4",
		SystemPrompt:     "Operation failure test agent.",
		ToolNames:        []string{"mouse"},
		Enabled:          true,
	})
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	// Simulate a failed operation (e.g., out-of-bounds coordinate).
	failedResult := buildOperationResultFrame(
		sessionID, fmt.Sprintf("op-failed-%s", uniqueSuffix()),
		game.AgentOperationResultStatus_AGENT_OPERATION_RESULT_STATUS_FAILED,
		"out of bounds: coordinate (99999,99999) exceeds screen",
	)
	writeWSFrame(t, conn, failedResult)

	// The subsequent turn must succeed — the connection survived the failure.
	sendTextWithProfile(t, conn, sessionID, profileName, "hello after failure")
	afterResp := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return f.GetText() != nil || f.GetWarn() != nil
	})
	if afterResp == nil {
		t.Fatal("connection unusable after failed operation_result — agent did not recover")
	}
	if afterResp.GetWarn() != nil {
		t.Logf("connection survived failed operation_result (warn): %q",
			afterResp.GetWarn().GetMessage())
	} else {
		t.Logf("connection survived failed operation_result: %q",
			afterResp.GetText().GetContent())
	}
}
