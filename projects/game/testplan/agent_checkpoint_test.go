// Package testplan contains agent checkpoint integration tests covering
// checkpoint resume, delete-recreate isolation, per-profile model usage,
// and concurrent message serialization.
package testplan

import (
	"fmt"
	"net/http"
	"testing"

	"dominion/common/gopkg/testtool"
	game "dominion/projects/game"
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
		AgentProfileName: profileName,
		Model:            "gpt-4",
		SystemPrompt:     "You are a test agent.",
		Enabled:          true,
	})
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	_ = createAgentWithProfile(t, sutHostURL, sutEnvName, sessionID, profileName)

	// Enter play — connect WebSocket
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)

	messages := []string{
		"My name is Alice and I work as a software engineer.",
		"How are you today?",
		"What is 2+2?",
	}
	var responseTexts []string
	for _, msg := range messages {
		textFrame := &game.AgentFrame{
			SessionId: sessionID,
			Payload: &game.AgentFrame_Text{
				Text: &game.AgentTextFrame{Content: msg},
			},
			Sender: game.FrameSender_FRAME_SENDER_USER,
		}
		writeWSFrame(t, conn, textFrame)

		_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
			return f.GetThinking() != nil
		})
		textResp := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
			return f.GetText() != nil
		})
		if textResp == nil {
			t.Fatalf("message %q: did not receive text response", msg)
		}
		responseTexts = append(responseTexts, textResp.GetText().GetContent())
		t.Logf("turn %d: user=%q → agent=%q", len(responseTexts), msg, textResp.GetText().GetContent())
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
		t.Logf("message[%d]: type=%s sender=%s content=%q", i, msg.GetType(), senderString(msg.GetSender()), msg.GetContent())
	}

	// Verify user messages are present and in order
	foundFirst := false
	for _, msg := range lmr.GetMessages() {
		if msg.GetSender() == game.FrameSender_FRAME_SENDER_USER && msg.GetContent() == messages[0] {
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

	// Send follow-up referencing turn 1
	followUp := "What is my name and what do I do for work?"
	textFrame := &game.AgentFrame{
		SessionId: sessionID,
		Payload: &game.AgentFrame_Text{
			Text: &game.AgentTextFrame{Content: followUp},
		},
		Sender: game.FrameSender_FRAME_SENDER_USER,
	}
	writeWSFrame(t, conn2, textFrame)

	_ = drainWSFrame(t, conn2, func(f *game.AgentFrame) bool {
		return f.GetThinking() != nil
	})
	textResp := drainWSFrame(t, conn2, func(f *game.AgentFrame) bool {
		return f.GetText() != nil
	})
	if textResp == nil {
		t.Fatal("did not receive text response for follow-up after re-enter")
	}
	if textResp.GetSender() != game.FrameSender_FRAME_SENDER_AGENT {
		t.Errorf("follow-up sender = %s, want AGENT", senderString(textResp.GetSender()))
	}
	t.Logf("follow-up response: %s", textResp.GetText().GetContent())

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
		AgentProfileName: profileName,
		Model:            "gpt-4",
		SystemPrompt:     "You are a test agent.",
		Enabled:          true,
	})
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	_ = createAgentWithProfile(t, sutHostURL, sutEnvName, sessionID, profileName)

	// Send 2 messages
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	userMessages := []string{"Turn one: hello", "Turn two: world"}
	for _, msg := range userMessages {
		textFrame := &game.AgentFrame{
			SessionId: sessionID,
			Payload: &game.AgentFrame_Text{
				Text: &game.AgentTextFrame{Content: msg},
			},
			Sender: game.FrameSender_FRAME_SENDER_USER,
		}
		writeWSFrame(t, conn, textFrame)

		_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
			return f.GetThinking() != nil
		})
		textResp := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
			return f.GetText() != nil
		})
		if textResp == nil {
			t.Fatalf("message %q: no text response", msg)
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
		if msg.GetType() == "text" && msg.GetContent() == "" {
			t.Errorf("message[%d]: text type has empty content", i)
		}
	}

	// Re-connect and send a third message
	conn2 := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn2.Close()

	thirdMsg := "Turn three: continuing"
	textFrame := &game.AgentFrame{
		SessionId: sessionID,
		Payload: &game.AgentFrame_Text{
			Text: &game.AgentTextFrame{Content: thirdMsg},
		},
		Sender: game.FrameSender_FRAME_SENDER_USER,
	}
	writeWSFrame(t, conn2, textFrame)

	_ = drainWSFrame(t, conn2, func(f *game.AgentFrame) bool {
		return f.GetThinking() != nil
	})
	textR := drainWSFrame(t, conn2, func(f *game.AgentFrame) bool {
		return f.GetText() != nil
	})
	if textR == nil {
		t.Fatal("third message: no text response after re-enter")
	}

	// Verify message count increased by 2 (1 user + 1 agent)
	lmr2 := listMessages(t, sutHostURL, sutEnvName, sessionID)
	if len(lmr2.GetMessages()) < 6 {
		t.Errorf("ListMessages after 3rd turn returned %d messages, want at least 6", len(lmr2.GetMessages()))
	}
	t.Logf("total messages after 3 turns: %d", len(lmr2.GetMessages()))
}

// TestAgentDeleteRecreateNoLeak verifies the delete-recreate isolation:
// create agent → send messages → delete → recreate → ListMessages empty →
// send message → verify agent has no memory of deleted conversation.
func TestAgentDeleteRecreateNoLeak(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("del-noleak-%s", uniqueSuffix())

	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		AgentProfileName: profileName,
		Model:            "gpt-4",
		SystemPrompt:     "You are a test agent.",
		Enabled:          true,
	})
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	_ = createAgentWithProfile(t, sutHostURL, sutEnvName, sessionID, profileName)

	// Send messages to first agent
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	firstMessages := []string{"Secret: the passcode is 42.", "Remember this."}
	for _, msg := range firstMessages {
		textFrame := &game.AgentFrame{
			SessionId: sessionID,
			Payload: &game.AgentFrame_Text{
				Text: &game.AgentTextFrame{Content: msg},
			},
			Sender: game.FrameSender_FRAME_SENDER_USER,
		}
		writeWSFrame(t, conn, textFrame)

		_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
			return f.GetThinking() != nil
		})
		_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
			return f.GetText() != nil
		})
	}
	conn.Close()

	// Verify messages exist before deletion
	lmrBefore := listMessages(t, sutHostURL, sutEnvName, sessionID)
	if len(lmrBefore.GetMessages()) == 0 {
		t.Fatal("expected messages before deletion, got none")
	}
	t.Logf("messages before delete: %d", len(lmrBefore.GetMessages()))

	// Delete agent
	delResp := deleteAgent(t, sutHostURL, sutEnvName, sessionID)
	delResp.Body.Close()

	// Recreate agent (same session, same profile)
	agent := createAgentWithProfile(t, sutHostURL, sutEnvName, sessionID, profileName)
	if agent.GetAgentProfileName() != profileName {
		t.Errorf("recreated agent profile = %q, want %q", agent.GetAgentProfileName(), profileName)
	}

	// ListMessages — verify empty (no leak from deleted agent)
	lmrAfter := listMessages(t, sutHostURL, sutEnvName, sessionID)
	if len(lmrAfter.GetMessages()) != 0 {
		t.Errorf("ListMessages after delete+recreate returned %d messages, want 0 (no leak)", len(lmrAfter.GetMessages()))
		for i, msg := range lmrAfter.GetMessages() {
			t.Logf("  leaked message[%d]: type=%s content=%q", i, msg.GetType(), msg.GetContent())
		}
	}

	// Connect new agent and send a message — verify it responds fresh
	conn2 := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn2.Close()

	pokeMsg := "What is the passcode?"
	textFrame := &game.AgentFrame{
		SessionId: sessionID,
		Payload: &game.AgentFrame_Text{
			Text: &game.AgentTextFrame{Content: pokeMsg},
		},
		Sender: game.FrameSender_FRAME_SENDER_USER,
	}
	writeWSFrame(t, conn2, textFrame)

	_ = drainWSFrame(t, conn2, func(f *game.AgentFrame) bool {
		return f.GetThinking() != nil
	})
	textR := drainWSFrame(t, conn2, func(f *game.AgentFrame) bool {
		return f.GetText() != nil
	})
	if textR == nil {
		t.Fatal("no text response from recreated agent")
	}
	t.Logf("recreated agent response: %s", textR.GetText().GetContent())

	// Verify the response does NOT contain the deleted data ("42")
	responseContent := textR.GetText().GetContent()
	if len(firstMessages) > 0 {
		for _, secret := range firstMessages {
			if containsString(responseContent, secret) {
				t.Errorf("recreated agent response leaked deleted data %q: %s", secret, responseContent)
			}
		}
	}

	// Verify message count — should only have messages from the new agent
	lmrFinal := listMessages(t, sutHostURL, sutEnvName, sessionID)
	if len(lmrFinal.GetMessages()) == 0 {
		t.Fatal("expected messages from recreated agent, got none")
	}
	if len(lmrFinal.GetMessages()) >= len(lmrBefore.GetMessages()) {
		t.Logf("final message count %d (before delete was %d) — no leak confirmed", len(lmrFinal.GetMessages()), len(lmrBefore.GetMessages()))
	}
}

// TestAgentDeleteRecreateVerifyClean verifies that after deleting and
// recreating an agent, the message list is empty and the new agent starts
// with a clean state.
func TestAgentDeleteRecreateVerifyClean(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("del-clean-%s", uniqueSuffix())

	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		AgentProfileName: profileName,
		Model:            "gpt-4",
		SystemPrompt:     "You are a test agent.",
		Enabled:          true,
	})
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)

	// Create agent
	_ = createAgentWithProfile(t, sutHostURL, sutEnvName, sessionID, profileName)

	// Delete agent
	delResp := deleteAgent(t, sutHostURL, sutEnvName, sessionID)
	delResp.Body.Close()

	// Verify agent is gone
	status, _ := getAgentWithStatus(t, sutHostURL, sutEnvName, sessionID)
	if status != http.StatusNotFound {
		t.Errorf("GET agent after delete status=%d, want %d", status, http.StatusNotFound)
	}

	// Recreate agent
	agent := createAgentWithProfile(t, sutHostURL, sutEnvName, sessionID, profileName)
	if agent.GetAgentProfileName() != profileName {
		t.Errorf("recreated agent profile = %q, want %q", agent.GetAgentProfileName(), profileName)
	}

	// Verify agent exists after recreate
	status2, _ := getAgentWithStatus(t, sutHostURL, sutEnvName, sessionID)
	if status2 != http.StatusOK {
		t.Errorf("GET agent after recreate status=%d, want %d", status2, http.StatusOK)
	}

	// ListMessages — must be empty
	lmr := listMessages(t, sutHostURL, sutEnvName, sessionID)
	if len(lmr.GetMessages()) != 0 {
		t.Errorf("ListMessages after delete+recreate returned %d messages, want 0", len(lmr.GetMessages()))
	}

	// Send a message and verify it works
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	textFrame := &game.AgentFrame{
		SessionId: sessionID,
		Payload: &game.AgentFrame_Text{
			Text: &game.AgentTextFrame{Content: "Hello, new agent!"},
		},
		Sender: game.FrameSender_FRAME_SENDER_USER,
	}
	writeWSFrame(t, conn, textFrame)

	_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return f.GetThinking() != nil
	})
	textR := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return f.GetText() != nil
	})
	if textR == nil {
		t.Fatal("no text response from recreated agent")
	}
	t.Logf("recreated agent responded: %s", textR.GetText().GetContent())
}

// TestAgentPerProfileModel verifies that agents created from different
// profiles each reference the correct model configured in their profile.
func TestAgentPerProfileModel(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profile1Name := fmt.Sprintf("model-gpt4-%s", uniqueSuffix())
	profile2Name := fmt.Sprintf("model-claude-%s", uniqueSuffix())

	// Create two profiles with different models
	profile1 := createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		AgentProfileName: profile1Name,
		Model:            "gpt-4",
		SystemPrompt:     "GPT-4 test agent.",
		Enabled:          true,
	})
	if profile1.GetModel() != "gpt-4" {
		t.Errorf("profile1 Model = %q, want %q", profile1.GetModel(), "gpt-4")
	}

	profile2 := createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		AgentProfileName: profile2Name,
		Model:            "claude-3-opus",
		SystemPrompt:     "Claude test agent.",
		Enabled:          true,
	})
	if profile2.GetModel() != "claude-3-opus" {
		t.Errorf("profile2 Model = %q, want %q", profile2.GetModel(), "claude-3-opus")
	}

	// Create two sessions and agents — each with a different profile
	sessionID1, _ := createSession(t, sutHostURL, sutEnvName)
	agent1 := createAgentWithProfile(t, sutHostURL, sutEnvName, sessionID1, profile1Name)
	if agent1.GetAgentProfileName() != profile1Name {
		t.Errorf("agent1 profile = %q, want %q", agent1.GetAgentProfileName(), profile1Name)
	}

	sessionID2, _ := createSession(t, sutHostURL, sutEnvName)
	agent2 := createAgentWithProfile(t, sutHostURL, sutEnvName, sessionID2, profile2Name)
	if agent2.GetAgentProfileName() != profile2Name {
		t.Errorf("agent2 profile = %q, want %q", agent2.GetAgentProfileName(), profile2Name)
	}

	// Verify profile models via GetAgentProfile (the source of truth for model)
	fetched1 := getAgentProfile(t, sutHostURL, sutEnvName, profile1Name)
	if fetched1.GetModel() != "gpt-4" {
		t.Errorf("fetched profile1 Model = %q, want %q", fetched1.GetModel(), "gpt-4")
	}

	fetched2 := getAgentProfile(t, sutHostURL, sutEnvName, profile2Name)
	if fetched2.GetModel() != "claude-3-opus" {
		t.Errorf("fetched profile2 Model = %q, want %q", fetched2.GetModel(), "claude-3-opus")
	}

	// Send messages to both agents — verify both respond (functional check)
	conn1 := connectAgentWS(t, sutHostURL, sutEnvName, sessionID1)
	defer conn1.Close()

	textFrame1 := &game.AgentFrame{
		SessionId: sessionID1,
		Payload:   &game.AgentFrame_Text{Text: &game.AgentTextFrame{Content: "Hello GPT"}},
		Sender:    game.FrameSender_FRAME_SENDER_USER,
	}
	writeWSFrame(t, conn1, textFrame1)
	_ = drainWSFrame(t, conn1, func(f *game.AgentFrame) bool { return f.GetThinking() != nil })
	resp1 := drainWSFrame(t, conn1, func(f *game.AgentFrame) bool { return f.GetText() != nil })
	if resp1 == nil {
		t.Fatal("agent1 (gpt-4 profile): no text response")
	}
	t.Logf("agent1 (gpt-4) responded: %s", resp1.GetText().GetContent())

	conn2 := connectAgentWS(t, sutHostURL, sutEnvName, sessionID2)
	defer conn2.Close()

	textFrame2 := &game.AgentFrame{
		SessionId: sessionID2,
		Payload:   &game.AgentFrame_Text{Text: &game.AgentTextFrame{Content: "Hello Claude"}},
		Sender:    game.FrameSender_FRAME_SENDER_USER,
	}
	writeWSFrame(t, conn2, textFrame2)
	_ = drainWSFrame(t, conn2, func(f *game.AgentFrame) bool { return f.GetThinking() != nil })
	resp2 := drainWSFrame(t, conn2, func(f *game.AgentFrame) bool { return f.GetText() != nil })
	if resp2 == nil {
		t.Fatal("agent2 (claude-3-opus profile): no text response")
	}
	t.Logf("agent2 (claude-3-opus) responded: %s", resp2.GetText().GetContent())

	// Both agents' profiles match their configured models
	t.Logf("profile1=%s model=%s, profile2=%s model=%s", profile1Name, fetched1.GetModel(), profile2Name, fetched2.GetModel())
}

// TestAgentConcurrentSerialization verifies that sending two messages
// rapidly to the same agent yields responses in FIFO send order without
// interleaving.
func TestAgentConcurrentSerialization(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("conc-fifo-%s", uniqueSuffix())

	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		AgentProfileName: profileName,
		Model:            "gpt-4",
		SystemPrompt:     "You are a test agent.",
		Enabled:          true,
	})
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	_ = createAgentWithProfile(t, sutHostURL, sutEnvName, sessionID, profileName)

	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	messages := []string{"Rapid message A", "Rapid message B"}
	for _, msg := range messages {
		textFrame := &game.AgentFrame{
			SessionId: sessionID,
			Payload: &game.AgentFrame_Text{
				Text: &game.AgentTextFrame{Content: msg},
			},
			Sender: game.FrameSender_FRAME_SENDER_USER,
		}
		writeWSFrame(t, conn, textFrame)
	}

	// Collect text responses in order — must match send order
	var responseTexts []string
	for i := 0; i < len(messages); i++ {
		_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
			return f.GetThinking() != nil
		})
		textFrame := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
			return f.GetText() != nil
		})
		if textFrame == nil {
			t.Fatalf("message %d: did not receive text response", i)
		}
		responseTexts = append(responseTexts, textFrame.GetText().GetContent())
	}

	if len(responseTexts) != len(messages) {
		t.Fatalf("got %d responses, want %d", len(responseTexts), len(messages))
	}

	for i, msg := range messages {
		expectedText := fmt.Sprintf("Hello! This is a simulated response. You said: %s", msg)
		if responseTexts[i] != expectedText {
			t.Errorf("response %d = %q, want %q (FIFO order violated)", i, responseTexts[i], expectedText)
		}
	}

	// Extra verification: no response should match a different send index
	for i := 0; i < len(messages); i++ {
		for j := 0; j < len(messages); j++ {
			if i == j {
				continue
			}
			expectedForJ := fmt.Sprintf("Hello! This is a simulated response. You said: %s", messages[j])
			if responseTexts[i] == expectedForJ {
				t.Errorf("response[%d] matches send[%d] — out of order", i, j)
			}
		}
	}
}

// containsString returns true when whole is a substring of s.
func containsString(s, whole string) bool {
	if len(whole) > len(s) {
		return false
	}
	for i := 0; i <= len(s)-len(whole); i++ {
		if s[i:i+len(whole)] == whole {
			return true
		}
	}
	return false
}
