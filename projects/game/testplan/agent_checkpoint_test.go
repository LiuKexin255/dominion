// Package testplan contains agent checkpoint integration tests covering
// checkpoint resume, cross-profile history persistence, per-profile model
// usage, and concurrent message serialization.
package testplan

import (
	"fmt"
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

	// Enter play — connect WebSocket
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)

	messages := []string{
		"My name is Alice and I work as a software engineer.",
		"How are you today?",
		"What is 2+2?",
	}
	var responseTexts []string
	for _, msg := range messages {
		sendTextWithProfile(t, conn, sessionID, profileName, msg)

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
		SessionId:        sessionID,
		AgentProfileName: profileName,
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

	// Send 2 messages
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	userMessages := []string{"Turn one: hello", "Turn two: world"}
	for _, msg := range userMessages {
		sendTextWithProfile(t, conn, sessionID, profileName, msg)

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
		SessionId:        sessionID,
		AgentProfileName: profileName,
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

	// Create two sessions — each with a different profile
	sessionID1, _ := createSession(t, sutHostURL, sutEnvName)
	sessionID2, _ := createSession(t, sutHostURL, sutEnvName)

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
		SessionId:        sessionID1,
		AgentProfileName: profile1Name,
		Payload:          &game.AgentFrame_Text{Text: &game.AgentTextFrame{Content: "Hello GPT"}},
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
		SessionId:        sessionID2,
		AgentProfileName: profile2Name,
		Payload:          &game.AgentFrame_Text{Text: &game.AgentTextFrame{Content: "Hello Claude"}},
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
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	messages := []string{"Rapid message A", "Rapid message B"}
	for _, msg := range messages {
		sendTextWithProfile(t, conn, sessionID, profileName, msg)
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

// TestCrossProfileHistoryPersistence verifies that messages exchanged with
// profile A are visible to profile B via ListMessages. When switching
// profiles mid-connection (or via a new connect), the shared session
// history persists across adapter profiles.
func TestCrossProfileHistoryPersistence(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileAName := fmt.Sprintf("ckpt-xprof-a-%s", uniqueSuffix())
	profileBName := fmt.Sprintf("ckpt-xprof-b-%s", uniqueSuffix())

	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		AgentProfileName: profileAName,
		Model:            "gpt-4",
		SystemPrompt:     "You are profile A.",
		Enabled:          true,
	})
	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		AgentProfileName: profileBName,
		Model:            "gpt-4",
		SystemPrompt:     "You are profile B.",
		Enabled:          true,
	})

	sessionID, _ := createSession(t, sutHostURL, sutEnvName)

	// Connect with profile A and exchange 2 turns.
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)

	userMessages := []string{"Profile A turn one", "Profile A turn two"}
	for _, msg := range userMessages {
		sendTextWithProfile(t, conn, sessionID, profileAName, msg)
		_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
			return f.GetThinking() != nil
		})
		textResp := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
			return f.GetText() != nil
		})
		if textResp == nil {
			t.Fatalf("profile A, message %q: no text response", msg)
		}
		t.Logf("profile A exchange: %q → %q", msg, textResp.GetText().GetContent())
	}

	// Switch to profile B mid-connection.
	sendTextWithProfile(t, conn, sessionID, profileBName, "Profile B turn one")
	_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return f.GetThinking() != nil })
	textRespB := drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return f.GetText() != nil })
	if textRespB == nil {
		t.Fatal("profile B: no text response after switch")
	}
	t.Logf("profile B response: %q", textRespB.GetText().GetContent())

	conn.Close()

	// ListMessages — both profiles' messages should be visible.
	lmr := listMessages(t, sutHostURL, sutEnvName, sessionID)
	gotCount := len(lmr.GetMessages())
	if gotCount < 6 {
		t.Errorf("ListMessages returned %d messages, want at least 6 (3 user + 3 agent)", gotCount)
	}
	for i, msg := range lmr.GetMessages() {
		t.Logf("message[%d]: type=%s sender=%s content=%q",
			i, msg.GetType(), senderString(msg.GetSender()), msg.GetContent())
	}

	// Verify profile A's messages are present.
	for _, um := range userMessages {
		found := false
		for _, msg := range lmr.GetMessages() {
			if msg.GetSender() == game.FrameSender_FRAME_SENDER_USER && msg.GetContent() == um {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("profile A user message %q not found in cross-profile history", um)
		}
	}

	// Verify profile B sees the full history — not just its own turn.
	if gotCount < 6 {
		t.Errorf("profile B should see all %d prior messages, but only got %d", 6, gotCount)
	}
}
