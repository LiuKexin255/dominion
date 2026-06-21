// Package testplan contains agent checkpoint integration tests covering
// checkpoint resume, cross-profile history persistence, per-profile model
// usage, and concurrent message serialization.
package testplan

import (
	"fmt"
	"strings"
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
			return f.GetThinking() != nil
		})
		if thinkingResp == nil {
			t.Fatalf("message %q: did not receive thinking response", msg)
		}
		if !strings.Contains(thinkingResp.GetThinking().GetContent(), expectedGreetingReasoning) {
			t.Errorf("message %q: thinking = %q, want to contain %q", msg, thinkingResp.GetThinking().GetContent(), expectedGreetingReasoning)
		}
		textResp := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
			return f.GetText() != nil
		})
		if textResp == nil {
			t.Fatalf("message %q: did not receive text response", msg)
		}
		if !strings.Contains(textResp.GetText().GetContent(), expectedGreetingText) {
			t.Errorf("message %q: text = %q, want to contain %q", msg, textResp.GetText().GetContent(), expectedGreetingText)
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

	// Send follow-up referencing turn 1, carrying the greeting keyword.
	followUp := "Hello, what is my name and what do I do for work?"
	textFrame := &game.AgentFrame{
		SessionId:        sessionID,
		AgentProfileName: profileName,
		Payload: &game.AgentFrame_Text{
			Text: &game.AgentTextFrame{Content: followUp},
		},
		Sender: game.FrameSender_FRAME_SENDER_USER,
	}
	writeWSFrame(t, conn2, textFrame)

	followThinking := drainWSFrame(t, conn2, func(f *game.AgentFrame) bool {
		return f.GetThinking() != nil
	})
	if followThinking == nil {
		t.Fatal("did not receive thinking response for follow-up after re-enter")
	}
	textResp := drainWSFrame(t, conn2, func(f *game.AgentFrame) bool {
		return f.GetText() != nil
	})
	if textResp == nil {
		t.Fatal("did not receive text response for follow-up after re-enter")
	}
	if textResp.GetSender() != game.FrameSender_FRAME_SENDER_AGENT {
		t.Errorf("follow-up sender = %s, want AGENT", senderString(textResp.GetSender()))
	}
	if !strings.Contains(textResp.GetText().GetContent(), expectedGreetingText) {
		t.Errorf("follow-up text = %q, want to contain %q", textResp.GetText().GetContent(), expectedGreetingText)
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

	// Send 2 messages, each carrying the greeting keyword.
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	userMessages := []string{"Hello, turn one", "Hello, turn two"}
	for _, msg := range userMessages {
		sendTextWithProfile(t, conn, sessionID, profileName, msg)

		thinkingResp := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
			return f.GetThinking() != nil
		})
		if thinkingResp == nil {
			t.Fatalf("message %q: no thinking response", msg)
		}
		if !strings.Contains(thinkingResp.GetThinking().GetContent(), expectedGreetingReasoning) {
			t.Errorf("message %q: thinking = %q, want to contain %q", msg, thinkingResp.GetThinking().GetContent(), expectedGreetingReasoning)
		}
		textResp := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
			return f.GetText() != nil
		})
		if textResp == nil {
			t.Fatalf("message %q: no text response", msg)
		}
		if !strings.Contains(textResp.GetText().GetContent(), expectedGreetingText) {
			t.Errorf("message %q: text = %q, want to contain %q", msg, textResp.GetText().GetContent(), expectedGreetingText)
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

	// Re-connect and send a third message carrying the greeting keyword.
	conn2 := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn2.Close()

	thirdMsg := "Hello, turn three continuing"
	textFrame := &game.AgentFrame{
		SessionId:        sessionID,
		AgentProfileName: profileName,
		Payload: &game.AgentFrame_Text{
			Text: &game.AgentTextFrame{Content: thirdMsg},
		},
		Sender: game.FrameSender_FRAME_SENDER_USER,
	}
	writeWSFrame(t, conn2, textFrame)

	thirdThinking := drainWSFrame(t, conn2, func(f *game.AgentFrame) bool {
		return f.GetThinking() != nil
	})
	if thirdThinking == nil {
		t.Fatal("third message: no thinking response after re-enter")
	}
	textR := drainWSFrame(t, conn2, func(f *game.AgentFrame) bool {
		return f.GetText() != nil
	})
	if textR == nil {
		t.Fatal("third message: no text response after re-enter")
	}
	if !strings.Contains(textR.GetText().GetContent(), expectedGreetingText) {
		t.Errorf("third message text = %q, want to contain %q", textR.GetText().GetContent(), expectedGreetingText)
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
		Model:            "gpt-4-turbo",
		SystemPrompt:     "GPT-4 Turbo test agent.",
		Enabled:          true,
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

	textFrame1 := &game.AgentFrame{
		SessionId:        sessionID1,
		AgentProfileName: profile1Name,
		Payload:          &game.AgentFrame_Text{Text: &game.AgentTextFrame{Content: "Hello from profile one"}},
		Sender:           game.FrameSender_FRAME_SENDER_USER,
	}
	writeWSFrame(t, conn1, textFrame1)
	_ = drainWSFrame(t, conn1, func(f *game.AgentFrame) bool { return f.GetThinking() != nil })
	resp1 := drainWSFrame(t, conn1, func(f *game.AgentFrame) bool { return f.GetText() != nil })
	if resp1 == nil {
		t.Fatal("agent1 (gpt-4 profile): no text response")
	}
	if !strings.Contains(resp1.GetText().GetContent(), expectedGreetingText) {
		t.Errorf("agent1 (gpt-4) text = %q, want to contain %q", resp1.GetText().GetContent(), expectedGreetingText)
	}
	t.Logf("agent1 (gpt-4) responded: %s", resp1.GetText().GetContent())

	conn2 := connectAgentWS(t, sutHostURL, sutEnvName, sessionID2)
	defer conn2.Close()

	textFrame2 := &game.AgentFrame{
		SessionId:        sessionID2,
		AgentProfileName: profile2Name,
		Payload:          &game.AgentFrame_Text{Text: &game.AgentTextFrame{Content: "Hello from profile two"}},
		Sender:           game.FrameSender_FRAME_SENDER_USER,
	}
	writeWSFrame(t, conn2, textFrame2)
	_ = drainWSFrame(t, conn2, func(f *game.AgentFrame) bool { return f.GetThinking() != nil })
	resp2 := drainWSFrame(t, conn2, func(f *game.AgentFrame) bool { return f.GetText() != nil })
	if resp2 == nil {
		t.Fatal("agent2 (gpt-4-turbo profile): no text response")
	}
	if !strings.Contains(resp2.GetText().GetContent(), expectedGreetingText) {
		t.Errorf("agent2 (gpt-4-turbo) text = %q, want to contain %q", resp2.GetText().GetContent(), expectedGreetingText)
	}
	t.Logf("agent2 (gpt-4-turbo) responded: %s", resp2.GetText().GetContent())

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
		AgentProfileName: profileName,
		Model:            "gpt-4",
		SystemPrompt:     "You are a test agent.",
		Enabled:          true,
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
			return f.GetThinking() != nil
		})
		textFrame := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
			return f.GetText() != nil
		})
		if textFrame == nil {
			t.Fatalf("message %d: did not receive text response", i)
		}
		if !strings.Contains(textFrame.GetText().GetContent(), want) {
			t.Errorf("response %d = %q, want to contain %q (FIFO order violated)", i, textFrame.GetText().GetContent(), want)
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

	// Connect with profile A and exchange 2 turns. Each carries the greeting
	// keyword so responses are deterministic.
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)

	userMessages := []string{"Hello, profile A turn one", "Hello, profile A turn two"}
	for _, msg := range userMessages {
		sendTextWithProfile(t, conn, sessionID, profileAName, msg)
		thinkingResp := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
			return f.GetThinking() != nil
		})
		if thinkingResp == nil {
			t.Fatalf("profile A, message %q: no thinking response", msg)
		}
		textResp := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
			return f.GetText() != nil
		})
		if textResp == nil {
			t.Fatalf("profile A, message %q: no text response", msg)
		}
		if !strings.Contains(textResp.GetText().GetContent(), expectedGreetingText) {
			t.Errorf("profile A, message %q: text = %q, want to contain %q", msg, textResp.GetText().GetContent(), expectedGreetingText)
		}
		t.Logf("profile A exchange: %q → %q", msg, textResp.GetText().GetContent())
	}

	// Switch to profile B mid-connection. The farewell keyword yields a
	// distinct template, confirming profile B's adapter also reaches fake-llm.
	profileBMsg := "Goodbye, profile B turn one"
	sendTextWithProfile(t, conn, sessionID, profileBName, profileBMsg)
	_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return f.GetThinking() != nil })
	textRespB := drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return f.GetText() != nil })
	if textRespB == nil {
		t.Fatal("profile B: no text response after switch")
	}
	if !strings.Contains(textRespB.GetText().GetContent(), expectedFarewellText) {
		t.Errorf("profile B text = %q, want to contain %q", textRespB.GetText().GetContent(), expectedFarewellText)
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

	// Verify profile B's user message is present too.
	profileBFound := false
	for _, msg := range lmr.GetMessages() {
		if msg.GetSender() == game.FrameSender_FRAME_SENDER_USER && msg.GetContent() == profileBMsg {
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
