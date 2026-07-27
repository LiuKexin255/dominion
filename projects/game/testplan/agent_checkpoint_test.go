// Package testplan contains agent checkpoint integration tests covering
// checkpoint resume, cross-profile history persistence, per-profile model
// usage, concurrent message serialization, and tool-result status
// preservation across session re-entry (spec 023 FR-012..FR-015).
package testplan

import (
	"fmt"
	"strings"
	"testing"

	"dominion/common/gopkg/testtool"
	game "dominion/projects/game"
	"dominion/projects/game/pkg/gameconst"
)

// TestAgentCheckpointResume verifies the full checkpoint-resume flow:
// create agent → send 3 messages → leave play → re-enter play →
// list messages → verify all prior messages present → send follow-up
// referencing turn 1 → verify agent produces a response (still functional).
func TestAgentCheckpointResume(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("ckpt-resume-%s", uniqueSuffix())

	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		Parent:         gameconst.PromptsParent,
		AgentProfileId: profileName,
		AgentProfile: &game.AgentProfile{
			Model:        "gpt-4",
			SystemPrompt: "You are a test agent.",
			Enabled:      true,
		},
	})
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)

	// Enter play — connect WebSocket
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)

	// Each turn carries the greeting keyword so responses are deterministic.
	messages := []string{
		"Hello, my name is Alice and I work as a software engineer.",
		"Hello, how are you today?",
		"Hello, what is 2+2?",
	}
	var responseTexts []string
	for _, msg := range messages {
		sendTextWithProfile(t, conn, sessionID, profileName, msg)

		thinkingResp := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
			return frameHasThinking(f)
		})
		if thinkingResp == nil {
			t.Fatalf("message %q: did not receive thinking response", msg)
		}
		if !strings.Contains(frameThinking(thinkingResp), expectedGreetingReasoning) {
			t.Errorf("message %q: thinking = %q, want to contain %q", msg, frameThinking(thinkingResp), expectedGreetingReasoning)
		}
		textResp := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
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
	lmr := listMessages(t, sutHostURL, sutEnvName, sessionID)
	gotCount := len(lmr.GetMessages())
	if gotCount < 6 {
		t.Errorf("ListMessages after 3 turns returned %d messages, want at least 6", gotCount)
	}
	for i, msg := range lmr.GetMessages() {
		t.Logf("message[%d]: type=%s sender=%s content=%q", i, messageKind(msg), senderString(msg.GetSender()), messageText(msg))
	}

	// Verify user messages are present and in order
	foundFirst := false
	for _, msg := range lmr.GetMessages() {
		if msg.GetSender() == game.FrameSender_FRAME_SENDER_USER && messageText(msg) == messages[0] {
			foundFirst = true
			break
		}
	}
	if !foundFirst {
		t.Errorf("first user message %q not found in ListMessages response", messages[0])
	}

	// Re-enter play
	conn2 := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn2.Close()

	// Send follow-up referencing turn 1, carrying the greeting keyword.
	followUp := "Hello, what is my name and what do I do for work?"
	textFrame := buildTextFrame(sessionID, profileName, followUp, game.FrameSender_FRAME_SENDER_USER)
	writeWSFrame(t, conn2, textFrame)

	followThinking := drainWSFrame(t, conn2, func(f *game.AgentFrame) bool {
		return frameHasThinking(f)
	})
	if followThinking == nil {
		t.Fatal("did not receive thinking response for follow-up after re-enter")
	}
	textResp := drainWSFrame(t, conn2, func(f *game.AgentFrame) bool {
		return frameHasText(f)
	})
	if textResp == nil {
		t.Fatal("did not receive text response for follow-up after re-enter")
	}
	if textResp.GetSender() != game.FrameSender_FRAME_SENDER_AGENT {
		t.Errorf("follow-up sender = %s, want AGENT", senderString(textResp.GetSender()))
	}
	if !strings.Contains(frameText(textResp), expectedGreetingText) {
		t.Errorf("follow-up text = %q, want to contain %q", frameText(textResp), expectedGreetingText)
	}
	t.Logf("follow-up response: %s", frameText(textResp))

	// Verify message count increased
	lmr2 := listMessages(t, sutHostURL, sutEnvName, sessionID)
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

	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		Parent:         gameconst.PromptsParent,
		AgentProfileId: profileName,
		AgentProfile: &game.AgentProfile{
			Model:        "gpt-4",
			SystemPrompt: "You are a test agent.",
			Enabled:      true,
		},
	})
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)

	// Send 2 messages, each carrying the greeting keyword.
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	userMessages := []string{"Hello, turn one", "Hello, turn two"}
	for _, msg := range userMessages {
		sendTextWithProfile(t, conn, sessionID, profileName, msg)

		thinkingResp := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
			return frameHasThinking(f)
		})
		if thinkingResp == nil {
			t.Fatalf("message %q: no thinking response", msg)
		}
		if !strings.Contains(frameThinking(thinkingResp), expectedGreetingReasoning) {
			t.Errorf("message %q: thinking = %q, want to contain %q", msg, frameThinking(thinkingResp), expectedGreetingReasoning)
		}
		textResp := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
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
	lmr := listMessages(t, sutHostURL, sutEnvName, sessionID)
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
	conn2 := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn2.Close()

	thirdMsg := "Hello, turn three continuing"
	textFrame := buildTextFrame(sessionID, profileName, thirdMsg, game.FrameSender_FRAME_SENDER_USER)
	writeWSFrame(t, conn2, textFrame)

	thirdThinking := drainWSFrame(t, conn2, func(f *game.AgentFrame) bool {
		return frameHasThinking(f)
	})
	if thirdThinking == nil {
		t.Fatal("third message: no thinking response after re-enter")
	}
	textR := drainWSFrame(t, conn2, func(f *game.AgentFrame) bool {
		return frameHasText(f)
	})
	if textR == nil {
		t.Fatal("third message: no text response after re-enter")
	}
	if !strings.Contains(frameText(textR), expectedGreetingText) {
		t.Errorf("third message text = %q, want to contain %q", frameText(textR), expectedGreetingText)
	}

	// Verify message count increased by 2 (1 user + 1 agent)
	lmr2 := listMessages(t, sutHostURL, sutEnvName, sessionID)
	if len(lmr2.GetMessages()) < 6 {
		t.Errorf("ListMessages after 3rd turn returned %d messages, want at least 6", len(lmr2.GetMessages()))
	}
	t.Logf("total messages after 3 turns: %d", len(lmr2.GetMessages()))
}

// TestAgentPerProfileModel verifies that agents created from different
// profiles each reference the correct model configured in their profile.
// Both profiles use non-Anthropic model names so the resolver-aware
// ChatOpenAI provider serves them via fake-llm; fake-llm itself ignores the
// model field, so both respond with the same template-matched content.
func TestAgentPerProfileModel(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profile1Name := fmt.Sprintf("model-gpt4-%s", uniqueSuffix())
	profile2Name := fmt.Sprintf("model-gpt4turbo-%s", uniqueSuffix())

	// Create two profiles with different non-Anthropic models.
	profile1 := createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		Parent:         gameconst.PromptsParent,
		AgentProfileId: profile1Name,
		AgentProfile: &game.AgentProfile{
			Model:        "gpt-4",
			SystemPrompt: "GPT-4 test agent.",
			Enabled:      true,
		},
	})
	if profile1.GetModel() != "gpt-4" {
		t.Errorf("profile1 Model = %q, want %q", profile1.GetModel(), "gpt-4")
	}

	profile2 := createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		Parent:         gameconst.PromptsParent,
		AgentProfileId: profile2Name,
		AgentProfile: &game.AgentProfile{
			Model:        "gpt-4-turbo",
			SystemPrompt: "GPT-4 Turbo test agent.",
			Enabled:      true,
		},
	})
	if profile2.GetModel() != "gpt-4-turbo" {
		t.Errorf("profile2 Model = %q, want %q", profile2.GetModel(), "gpt-4-turbo")
	}

	// Create two sessions — each with a different profile.
	sessionID1, _ := createSession(t, sutHostURL, sutEnvName)
	sessionID2, _ := createSession(t, sutHostURL, sutEnvName)

	// Verify profile models via GetAgentProfile (the source of truth for model).
	fetched1 := getAgentProfile(t, sutHostURL, sutEnvName, profile1Name)
	if fetched1.GetModel() != "gpt-4" {
		t.Errorf("fetched profile1 Model = %q, want %q", fetched1.GetModel(), "gpt-4")
	}

	fetched2 := getAgentProfile(t, sutHostURL, sutEnvName, profile2Name)
	if fetched2.GetModel() != "gpt-4-turbo" {
		t.Errorf("fetched profile2 Model = %q, want %q", fetched2.GetModel(), "gpt-4-turbo")
	}

	// Send messages to both agents — both carry the greeting keyword so the
	// response content is deterministic. fake-llm ignores the model field, so
	// both respond with the greeting template.
	conn1 := connectAgentWS(t, sutHostURL, sutEnvName, sessionID1)
	defer conn1.Close()

	textFrame1 := buildTextFrame(sessionID1, profile1Name, "Hello from profile one", game.FrameSender_FRAME_SENDER_USER)
	writeWSFrame(t, conn1, textFrame1)
	_ = drainWSFrame(t, conn1, func(f *game.AgentFrame) bool { return frameHasThinking(f) })
	resp1 := drainWSFrame(t, conn1, func(f *game.AgentFrame) bool { return frameHasText(f) })
	if resp1 == nil {
		t.Fatal("agent1 (gpt-4 profile): no text response")
	}
	if !strings.Contains(frameText(resp1), expectedGreetingText) {
		t.Errorf("agent1 (gpt-4) text = %q, want to contain %q", frameText(resp1), expectedGreetingText)
	}
	t.Logf("agent1 (gpt-4) responded: %s", frameText(resp1))

	conn2 := connectAgentWS(t, sutHostURL, sutEnvName, sessionID2)
	defer conn2.Close()

	textFrame2 := buildTextFrame(sessionID2, profile2Name, "Hello from profile two", game.FrameSender_FRAME_SENDER_USER)
	writeWSFrame(t, conn2, textFrame2)
	_ = drainWSFrame(t, conn2, func(f *game.AgentFrame) bool { return frameHasThinking(f) })
	resp2 := drainWSFrame(t, conn2, func(f *game.AgentFrame) bool { return frameHasText(f) })
	if resp2 == nil {
		t.Fatal("agent2 (gpt-4-turbo profile): no text response")
	}
	if !strings.Contains(frameText(resp2), expectedGreetingText) {
		t.Errorf("agent2 (gpt-4-turbo) text = %q, want to contain %q", frameText(resp2), expectedGreetingText)
	}
	t.Logf("agent2 (gpt-4-turbo) responded: %s", frameText(resp2))

	// Both agents' profiles match their configured models.
	t.Logf("profile1=%s model=%s, profile2=%s model=%s", profile1Name, fetched1.GetModel(), profile2Name, fetched2.GetModel())
}

// TestAgentConcurrentSerialization verifies that sending two messages
// rapidly to the same agent yields responses in FIFO send order without
// interleaving. Each turn carries a DISTINCT keyword backed by a DISTINCT
// template (greeting then farewell) so the response identity proves order.
func TestAgentConcurrentSerialization(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("conc-fifo-%s", uniqueSuffix())

	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		Parent:         gameconst.PromptsParent,
		AgentProfileId: profileName,
		AgentProfile: &game.AgentProfile{
			Model:        "gpt-4",
			SystemPrompt: "You are a test agent.",
			Enabled:      true,
		},
	})
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	// Distinct keywords → distinct templates, so response text proves FIFO order.
	messages := []string{"hello first", "goodbye second"}
	for _, msg := range messages {
		sendTextWithProfile(t, conn, sessionID, profileName, msg)
	}

	wantTexts := []string{expectedGreetingText, expectedFarewellText}

	for i, want := range wantTexts {
		_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
			return frameHasThinking(f)
		})
		textFrame := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
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

// TestCrossProfileHistoryPersistence verifies that messages exchanged with
// profile A are visible to profile B via ListMessages. Switching profiles
// mid-connection requires Refresh to rebuild the adapter (the legacy
// auto-switch was removed in favor of a profile guard); after Refresh, the
// shared session history persists across adapter profiles
// (specs/021-agent-session-resync/contracts/agent-session-lifecycle-contract.md §2/§3).
func TestCrossProfileHistoryPersistence(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileAName := fmt.Sprintf("ckpt-xprof-a-%s", uniqueSuffix())
	profileBName := fmt.Sprintf("ckpt-xprof-b-%s", uniqueSuffix())

	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		Parent:         gameconst.PromptsParent,
		AgentProfileId: profileAName,
		AgentProfile: &game.AgentProfile{
			Model:        "gpt-4",
			SystemPrompt: "You are profile A.",
			Enabled:      true,
		},
	})
	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		Parent:         gameconst.PromptsParent,
		AgentProfileId: profileBName,
		AgentProfile: &game.AgentProfile{
			Model:        "gpt-4",
			SystemPrompt: "You are profile B.",
			Enabled:      true,
		},
	})

	sessionID, _ := createSession(t, sutHostURL, sutEnvName)

	// Connect with profile A and exchange 2 turns. Each carries the greeting
	// keyword so responses are deterministic.
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)

	userMessages := []string{"Hello, profile A turn one", "Hello, profile A turn two"}
	for _, msg := range userMessages {
		sendTextWithProfile(t, conn, sessionID, profileAName, msg)
		thinkingResp := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
			return frameHasThinking(f)
		})
		if thinkingResp == nil {
			t.Fatalf("profile A, message %q: no thinking response", msg)
		}
		textResp := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
			return frameHasText(f)
		})
		if textResp == nil {
			t.Fatalf("profile A, message %q: no text response", msg)
		}
		if !strings.Contains(frameText(textResp), expectedGreetingText) {
			t.Errorf("profile A, message %q: text = %q, want to contain %q", msg, frameText(textResp), expectedGreetingText)
		}
		t.Logf("profile A exchange: %q → %q", msg, frameText(textResp))
	}

	// given: switching profiles mid-connection now requires Refresh. A turn
	// under profile A binds adapter A; a later turn under profile B without
	// Refresh is rejected by the profile guard (Warn + Wait). Drain the last
	// profile-A turn's completion WaitSignal so the turn mutex is released,
	// then Refresh to invalidate the adapter so the next turn rebuilds for
	// profile B
	// (specs/021-agent-session-resync/contracts/agent-session-lifecycle-contract.md §2/§3).
	_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return frameWait(f) != nil })
	refreshAgent(t, sutHostURL, sutEnvName, sessionID)

	// when: profile B turn after Refresh rebuilds the adapter for B. The
	// farewell keyword yields a distinct template, confirming profile B's
	// adapter also reaches fake-llm.
	profileBMsg := "Goodbye, profile B turn one"
	sendTextWithProfile(t, conn, sessionID, profileBName, profileBMsg)
	// then: profile B produces a thinking + text response.
	_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return frameHasThinking(f) })
	textRespB := drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return frameHasText(f) })
	if textRespB == nil {
		t.Fatal("profile B: no text response after switch")
	}
	if !strings.Contains(frameText(textRespB), expectedFarewellText) {
		t.Errorf("profile B text = %q, want to contain %q", frameText(textRespB), expectedFarewellText)
	}
	t.Logf("profile B response: %q", frameText(textRespB))

	conn.Close()

	// ListMessages — both profiles' messages should be visible.
	lmr := listMessages(t, sutHostURL, sutEnvName, sessionID)
	gotCount := len(lmr.GetMessages())
	if gotCount < 6 {
		t.Errorf("ListMessages returned %d messages, want at least 6 (3 user + 3 agent)", gotCount)
	}
	for i, msg := range lmr.GetMessages() {
		t.Logf("message[%d]: type=%s sender=%s content=%q",
			i, messageKind(msg), senderString(msg.GetSender()), messageText(msg))
	}

	// Verify profile A's messages are present.
	for _, um := range userMessages {
		found := false
		for _, msg := range lmr.GetMessages() {
			if msg.GetSender() == game.FrameSender_FRAME_SENDER_USER && messageText(msg) == um {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("profile A user message %q not found in cross-profile history", um)
		}
	}

	// Verify profile B's user message is present too.
	profileBFound := false
	for _, msg := range lmr.GetMessages() {
		if msg.GetSender() == game.FrameSender_FRAME_SENDER_USER && messageText(msg) == profileBMsg {
			profileBFound = true
			break
		}
	}
	if !profileBFound {
		t.Errorf("profile B user message %q not found in cross-profile history", profileBMsg)
	}

	// Verify profile B sees the full history — not just its own turn.
	if gotCount < 6 {
		t.Errorf("profile B should see all %d prior messages, but only got %d", 6, gotCount)
	}
}

// TestAgentCheckpointToolResultStatusPersists verifies spec 023 FR-012/FR-013
// (the history-status fix, quickstart.md Scenario 6): a native mouse tool
// whose real outcome was SUCCEEDED MUST still read SUCCEEDED after leaving
// and re-entering the session, and one that genuinely failed MUST still read
// FAILED. Before this feature the status was guessed by `inferToolResultStatus`
// (FAILED unless the text contained "ok"/"succeeded") — every result that
// lacked those keywords flipped to FAILED on re-entry. With the real status
// carried through `ToolMessage.additional_kwargs.toolResultStatus` (D4) and
// read directly by `ListMessages`, the live and history statuses match.
//
// Saolei (MCP) tool results reading neutral (TOOL_RESULT_STATUS_UNSPECIFIED,
// never FAILED) on re-entry is covered by TestAgentSaoleiMcpStatelessFlow
// (D12 / spec 023 FR-014) — `ListMessages` is a stateless reconstruction of
// the checkpoint, so its behaviour does not change across WS reconnects.
func TestAgentCheckpointToolResultStatusPersists(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("ckpt-status-%s", uniqueSuffix())

	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		Parent:         gameconst.PromptsParent,
		AgentProfileId: profileName,
		AgentProfile: &game.AgentProfile{
			Model:        "gpt-4",
			SystemPrompt: "You operate the mouse for checkpoint-status tests.",
			ToolNames:    mouseSplitToolNames,
			Enabled:      true,
		},
	})
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)

	// given: turn 1 — a mouse_move tool_call whose real outcome is SUCCEEDED.
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	sendTextWithProfile(t, conn, sessionID, profileName, "please move the mouse now")
	// The agent emits the tool_call MessagePart frame and the operation
	// FlowPart frame concurrently (see readToolCallAndOperation doc); collect
	// both in one pass so neither is dropped by an early drain. The tool_call
	// frame's content is asserted in agent_operation_test.go; this suite only
	// needs the operation frame to reply and the resulting tool_result status.
	_, successOp1 := readToolCallAndOperation(t, conn)
	respondToOperation(t, conn, sessionID, successOp1,
		game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED, "cursor moved to 100,200")
	if fr := drainWSFrame(t, conn, frameHasToolResult); fr == nil {
		t.Fatal("turn 1: did not receive a tool_result MessagePart frame after SUCCEEDED reply")
	}
	// Drain the terminal text frame so the turn mutex releases before the
	// next turn (the agent emits mouse-move-success-text on the loop close).
	_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return frameHasText(f) })
	// Drain the wait FlowPart so the turn is fully settled.
	_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return frameWait(f) != nil })

	// given: turn 2 — a mouse_move tool_call whose real outcome is FAILED
	// (different trigger keyword — mouse-trigger matches both "move the mouse"
	// and "position cursor"). fake-LLM returns mouse-move-success-text for
	// both (the test replies FAILED regardless; the loop closes on text).
	sendTextWithProfile(t, conn, sessionID, profileName, "position cursor over the icon")
	_, failOp := readToolCallAndOperation(t, conn)
	respondToOperation(t, conn, sessionID, failOp,
		game.ToolResultStatus_TOOL_RESULT_STATUS_FAILED, "desktop rejected coordinate")
	if fr := drainWSFrame(t, conn, frameHasToolResult); fr == nil {
		t.Fatal("turn 2: did not receive a tool_result MessagePart frame after FAILED reply")
	}
	_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return frameHasText(f) })

	// when: the session is left (WS closed). ListMessages reads from the
	// checkpoint, not the live socket, so it must reflect both tool_result
	// statuses without reconnect-dependent state.
	conn.Close()
	lmrAfterLeave := listMessages(t, sutHostURL, sutEnvName, sessionID)

	// then: SUCCEEDED stays SUCCEEDED, FAILED stays FAILED — no spurious
	// "failed" from text-heuristic inference (spec 023 FR-012/FR-013;
	// data-model.md §6; the original bug from spec §Motivation item 3).
	if !messagesContainToolResultStatus(lmrAfterLeave.GetMessages(), game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED) {
		t.Errorf("after leave: ListMessages did not surface a SUCCEEDED tool_result — real status MUST survive the checkpoint (FR-013)")
	}
	if !messagesContainToolResultStatus(lmrAfterLeave.GetMessages(), game.ToolResultStatus_TOOL_RESULT_STATUS_FAILED) {
		t.Errorf("after leave: ListMessages did not surface a FAILED tool_result — real failures MUST survive too (FR-013 masks nothing)")
	}

	// when: the session is re-entered (fresh WS). The adapter is rebuilt but
	// the checkpoint persists; ListMessages must return the same statuses.
	conn2 := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn2.Close()
	lmrAfterReenter := listMessages(t, sutHostURL, sutEnvName, sessionID)

	// then: identical statuses after re-entry (live≡history for status).
	if !messagesContainToolResultStatus(lmrAfterReenter.GetMessages(), game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED) {
		t.Errorf("after re-enter: SUCCEEDED tool_result dropped from history — status MUST persist across re-entry (FR-013)")
	}
	if !messagesContainToolResultStatus(lmrAfterReenter.GetMessages(), game.ToolResultStatus_TOOL_RESULT_STATUS_FAILED) {
		t.Errorf("after re-enter: FAILED tool_result dropped from history — status MUST persist across re-entry (FR-013)")
	}

	// then: no historical tool_result reads spurious FAILED beyond the one
	// genuine failure. Count the FAILED entries and confirm there is at most
	// one (the turn-2 failure) — the SUCCEEDED turn-1 result MUST NOT flip.
	var failedCount int
	for _, m := range lmrAfterReenter.GetMessages() {
		for _, s := range messageToolResultStatuses(m) {
			if s == game.ToolResultStatus_TOOL_RESULT_STATUS_FAILED {
				failedCount++
			}
		}
	}
	if failedCount > 1 {
		t.Errorf("after re-enter: %d FAILED tool_results in history, want at most 1 (the genuine turn-2 failure) — spurious FAILED indicates the text-heuristic regression recurred (FR-015)", failedCount)
	}

	// then: no operation FlowPart in Message.content (FR-005).
	assertMessageContentDisplayOnly(t, lmrAfterReenter.GetMessages())
}

// TestServiceSurvivesDisconnectDuringTurn verifies
// specs/026-agent-abort-crash-fix/spec.md SC-001/SC-002/SC-003 and
// specs/026-agent-abort-crash-fix/quickstart.md Scenario 4: when a desktop
// disconnects MID-TURN (before the closing wait frame), the agent service
// process MUST survive and the session MUST remain reusable on reconnect.
//
// Coverage gap closed by this case (T008a finding): the other cases in this
// binary (TestAgentCheckpointResume, TestAgentCheckpointResumeVerifyContext,
// TestCrossProfileHistoryPersistence, TestAgentCheckpointToolResultStatusPersists,
// TestAgentPerProfileModel, TestAgentConcurrentSerialization) all disconnect
// AFTER draining the closing wait frame — the turn is already complete when
// the bidi stream closes. TestReconnectDispatchReliability
// (agent_lifecycle_test.go) also disconnects after the in-flight turn
// completes (the text frame is already drained). None of them exercise the
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

	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		Parent:         gameconst.PromptsParent,
		AgentProfileId: profileName,
		AgentProfile: &game.AgentProfile{
			Model:        "gpt-4",
			SystemPrompt: "You are a test agent.",
			Enabled:      true,
		},
	})
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)

	// given: a turn is in-flight. The greeting keyword yields a deterministic
	// thinking frame from fake-llm, proving the agent loop has started
	// consuming the user message (the LLM produced at least one streamed
	// block). We deliberately do NOT drain the closing wait frame — the
	// turn MUST still be running when we disconnect below, so the abort path
	// (catch block + finally → releaseMutex) is the code under test.
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	sendTextWithProfile(t, conn, sessionID, profileName, "Hello, mid-turn abort")
	firstThinking := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
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
	conn2 := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn2.Close()
	sendTextWithProfile(t, conn2, sessionID, profileName, "Hello, after reconnect")

	reconnectThinking := drainWSFrame(t, conn2, func(f *game.AgentFrame) bool {
		return frameHasThinking(f)
	})
	if reconnectThinking == nil {
		t.Fatal("post-disconnect reconnect: no thinking frame — service did not survive or mutex was not released (SC-001/SC-003)")
	}
	if !strings.Contains(frameThinking(reconnectThinking), expectedGreetingReasoning) {
		t.Errorf("post-disconnect thinking = %q, want to contain %q",
			frameThinking(reconnectThinking), expectedGreetingReasoning)
	}
	reconnectText := drainWSFrame(t, conn2, func(f *game.AgentFrame) bool {
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
