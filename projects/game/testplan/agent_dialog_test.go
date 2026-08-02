// Package testplan contains agent dialog integration tests.
// These tests validate the team's player-agent text dialog capability
// through the gateway HTTP + WebSocket surface, using the fake LLM test
// artifact that returns deterministic responses. Each test sets up the team
// stack via setupTeamSession (session → saolei TeamProfile → CreateTeam)
// before connecting — CreateTeam MUST precede Connect (no lazy creation,
// spec 031-team-template-mode FR-033).
package testplan

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"dominion/common/gopkg/testtool"
	game "dominion/projects/game"
)

// ─── Tests ───────────────────────────────────────────────────────────────────

// TestAgentDialogCreateAndConnect verifies the setup flow:
// create saolei TeamProfile → create session → CreateTeam → connect WebSocket.
func TestAgentDialogCreateAndConnect(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("ad-cc-%s", uniqueSuffix())

	// Create the TeamProfile, session, and Team (FR-033 — CreateTeam is the
	// only Team creation point and MUST precede Connect).
	profile := createTeamProfile(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")
	if profile.GetName() != "templates/"+saoleiTemplateID+"/profiles/"+profileName {
		t.Errorf("profile name = %q, want %q", profile.GetName(), "templates/"+saoleiTemplateID+"/profiles/"+profileName)
	}

	sessionID, _ := createSession(t, sutHostURL, sutEnvName, saoleiTemplateID)
	if sessionID == "" {
		t.Fatal("sessionID is empty")
	}
	createTeam(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, profileName)

	// Connect WebSocket
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	conn.Close()
}

// TestAgentDialogTextToResponse verifies the core dialog flow:
// send a content text frame → receive thinking frame → receive text frame
// → verify FrameSender.AGENT on response frames.
func TestAgentDialogTextToResponse(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("ad-ttr-%s", uniqueSuffix())

	// Setup
	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// Send a content frame carrying a TextPart with sender=USER.
	sendText := "Hello, agent!"
	textFrame := buildTextFrame(sessionID, "player", sendText, game.FrameSender_FRAME_SENDER_USER)
	writeWSFrame(t, conn, textFrame)

	// Receive thinking frame
	thinkingFrame := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return frameHasThinking(f)
	})
	if thinkingFrame == nil {
		t.Fatal("did not receive thinking frame")
	}
	// "Hello, agent!" carries the greeting keyword "hello" so fake-llm
	// deterministically returns the greeting template (see README §4).
	if thinkingFrame.GetSender() != game.FrameSender_FRAME_SENDER_AGENT {
		t.Errorf("thinking sender = %s, want AGENT", senderString(thinkingFrame.GetSender()))
	}
	if !strings.Contains(frameThinking(thinkingFrame), expectedGreetingReasoning) {
		t.Errorf("thinking = %q, want to contain %q", frameThinking(thinkingFrame), expectedGreetingReasoning)
	}
	t.Logf("thinking: %q (sender=%s)", frameThinking(thinkingFrame), senderString(thinkingFrame.GetSender()))

	// Receive text frame
	textRespFrame := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return frameHasText(f)
	})
	if textRespFrame == nil {
		t.Fatal("did not receive text frame")
	}
	if textRespFrame.GetSender() != game.FrameSender_FRAME_SENDER_AGENT {
		t.Errorf("text sender = %s, want AGENT", senderString(textRespFrame.GetSender()))
	}
	if !strings.Contains(frameText(textRespFrame), expectedGreetingText) {
		t.Errorf("text = %q, want to contain %q", frameText(textRespFrame), expectedGreetingText)
	}
	t.Logf("text: %q (sender=%s)", frameText(textRespFrame), senderString(textRespFrame.GetSender()))
}

// TestAgentDialogThinkingBeforeText verifies that the thinking frame arrives
// before the text frame — the ordering guarantee from the handler.
func TestAgentDialogThinkingBeforeText(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("ad-tbt-%s", uniqueSuffix())

	// Setup
	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// Send text carrying the greeting keyword so the response is deterministic.
	textFrame := buildTextFrame(sessionID, "player", "Hello ordering test", game.FrameSender_FRAME_SENDER_USER)
	writeWSFrame(t, conn, textFrame)

	// Read frames in order — first must be thinking, second must be text
	frame1 := readWSFrame(t, conn)
	if !frameHasThinking(frame1) {
		t.Fatal("frame 1: expected thinking, got something else")
	}
	if !strings.Contains(frameThinking(frame1), expectedGreetingReasoning) {
		t.Errorf("frame 1 thinking = %q, want to contain %q", frameThinking(frame1), expectedGreetingReasoning)
	}
	frame2 := readWSFrame(t, conn)
	if !frameHasText(frame2) {
		t.Fatal("frame 2: expected text, got something else")
	}
	if !strings.Contains(frameText(frame2), expectedGreetingText) {
		t.Errorf("frame 2 text = %q, want to contain %q", frameText(frame2), expectedGreetingText)
	}

	t.Logf("frame 1 thinking: %q", frameThinking(frame1))
	t.Logf("frame 2 text: %q", frameText(frame2))
}

// TestAgentDialogDeterministicContent verifies that fake-llm returns the
// template-matched content deterministically: a prompt carrying the greeting
// keyword yields the greeting reasoning + text from the embedded testdata.
func TestAgentDialogDeterministicContent(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("ad-det-%s", uniqueSuffix())

	// Setup
	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// "Hello world" carries the greeting keyword "hello".
	textFrame := buildTextFrame(sessionID, "player", "Hello world", game.FrameSender_FRAME_SENDER_USER)
	writeWSFrame(t, conn, textFrame)

	// Read and verify thinking content
	thinkingFrame := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return frameHasThinking(f)
	})
	if thinkingFrame == nil {
		t.Fatal("did not receive thinking frame")
	}
	if !strings.Contains(frameThinking(thinkingFrame), expectedGreetingReasoning) {
		t.Errorf("thinking = %q, want to contain %q", frameThinking(thinkingFrame), expectedGreetingReasoning)
	}

	// Read and verify text content
	textRespFrame := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return frameHasText(f)
	})
	if textRespFrame == nil {
		t.Fatal("did not receive text frame")
	}
	if !strings.Contains(frameText(textRespFrame), expectedGreetingText) {
		t.Errorf("text = %q, want to contain %q", frameText(textRespFrame), expectedGreetingText)
	}
}

// TestAgentDialogMessageContentDisplayOnly verifies spec 023 FR-005 in the
// dialog module: after a plain text turn, ListMessages returns Messages
// whose content.parts carry ONLY display-only MessagePart kinds
// (text/thinking/image/toolCall/toolResult) — no FlowPart (mouse/keyboard
// operation or wait/warn/status signal) ever appears in Message.content.
// The content-model split is structural (Message.content is typed
// MessageParts), so this test guards a future regression that reintroduces
// an operation-shaped entry. It reuses the shared helpers in helpers_test.go
// (style/large_test.md §反模式3 — do not copy helpers).
func TestAgentDialogMessageContentDisplayOnly(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("ad-DispOnly-%s", uniqueSuffix())

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// Run a text turn carrying the greeting keyword so the response is
	// deterministic. The wait FlowPart the agent emits at turn end is a
	// flow_parts frame on the live socket (control channel); it MUST NOT
	// be reconstructed into any Message.content.
	sendText(t, conn, sessionID, "Hello display-only test")
	_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return frameHasThinking(f) })
	_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return frameHasText(f) })
	// Drain the terminal wait FlowPart so the turn settles before listing.
	_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return frameWait(f) != nil })

	// then: ListMessages returns Messages whose content is display-only.
	lmr := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "player")
	assertMessageContentDisplayOnly(t, lmr.GetMessages())

	// Sanity: the user text survived in history (a regression that
	// dropped text would silently pass the display-only guard).
	if !messagesContainText(lmr.GetMessages(), "Hello display-only test") {
		t.Errorf("ListMessages did not surface the user text 'Hello display-only test' — history reconstruction regressed")
	}
}

// TestAgentDialogFIFOQueue verifies that messages sent one at a time — each
// followed by a full turn drain (thinking + text + wait) before the next is
// submitted — are processed independently and in FIFO order. Because fake-llm
// matches by keyword, each turn carries a DISTINCT keyword backed by a DISTINCT
// template so the response identity proves the processing order: greeting,
// farewell, greeting.
//
// This is the sequential-submit form of FIFO ordering: each message starts
// only after the previous turn's wait returned the loop to idle. The
// rapid-submit form — messages arriving mid-run, merged by the TurnLoop into
// one aggregated turn — is covered by TestAgentDialogQueueMultipleCombine
// (specs/030-queued-chat-input/spec.md FR-005).
func TestAgentDialogFIFOQueue(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("ad-fifo-%s", uniqueSuffix())

	// Setup
	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// Each message triggers a different template via a distinct keyword so the
	// response text proves which input was processed.
	messages := []string{
		"hello world",   // greeting
		"goodbye world", // farewell
		"hi friend",     // greeting again (hi is a greeting keyword)
	}
	wantTexts := []string{expectedGreetingText, expectedFarewellText, expectedGreetingText}

	// Submit each message sequentially — drain the full turn (thinking + text +
	// wait) before sending the next — so each message is processed as an
	// independent turn and the responses prove FIFO order. A rapid burst would
	// instead be merged by the per-session TurnLoop into one aggregated turn
	// (specs/030-queued-chat-input/spec.md FR-005).
	for i, msg := range messages {
		sendText(t, conn, sessionID, msg)
		_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
			return frameHasThinking(f)
		})
		textFrame := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
			return frameHasText(f)
		})
		if textFrame == nil {
			t.Fatalf("turn %d: did not receive text response frame", i)
		}
		if !strings.Contains(frameText(textFrame), wantTexts[i]) {
			t.Errorf("response %d = %q, want to contain %q (FIFO order violated)", i, frameText(textFrame), wantTexts[i])
		}
		// Drain the terminal wait so the loop returns to idle before the next
		// submit; otherwise the next message would land mid-run and be combined.
		_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
			return frameWait(f) != nil
		})
	}
}

// TestAgentDialogDeleteTeamProfileStillResponds verifies the loose coupling
// design: after the team is created, deleting the saolei TeamProfile does
// not prevent subsequent messages from being processed, because the team's
// player/planner models were resolved at CreateTeam time (server.ts
// SessionTeamStore factory reads the profile once).
func TestAgentDialogDeleteTeamProfileStillResponds(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("ad-delp-%s", uniqueSuffix())

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")

	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// Turn before deletion carries the greeting keyword.
	sendText(t, conn, sessionID, "Hello before delete")
	_ = drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return frameHasThinking(f) })
	firstResp := drainWSFrame(t, conn, func(f *game.AgentFrame) bool { return frameHasText(f) })
	if firstResp == nil {
		t.Fatal("no response before TeamProfile deletion")
	}

	delStatus := deleteTeamProfile(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName)
	if delStatus != http.StatusOK && delStatus != http.StatusNoContent {
		t.Fatalf("DELETE team profile status = %d, want 200 or 204", delStatus)
	}

	// Turn after deletion carries the farewell keyword so the content assertion
	// is deterministic (no random fallback).
	sendText(t, conn, sessionID, "Goodbye after delete")

	textRespFrame := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
		return frameHasText(f)
	})
	if textRespFrame == nil {
		t.Fatal("did not receive text response after TeamProfile deletion")
	}
	if textRespFrame.GetSender() != game.FrameSender_FRAME_SENDER_AGENT {
		t.Errorf("response sender = %s, want AGENT", senderString(textRespFrame.GetSender()))
	}
	if !strings.Contains(frameText(textRespFrame), expectedFarewellText) {
		t.Errorf("response text = %q, want to contain %q", frameText(textRespFrame), expectedFarewellText)
	}
}

// ─── Queue-while-running tests (specs/030-queued-chat-input) ────────────────
//
// These tests cover quickstart.md Scenarios 1-4: input during a run does not
// disturb the in-flight turn (specs/030-queued-chat-input/spec.md FR-001/FR-002),
// the queued message auto-becomes the next turn (specs/030-queued-chat-input/spec.md
// FR-005/FR-006), multiple queued messages combine into one aggregated turn
// (specs/030-queued-chat-input/spec.md FR-004/FR-005), and QueueSignal drives
// pending visibility (specs/030-queued-chat-input/spec.md FR-008/FR-009).
//
// The per-session TurnLoop (projects/game/agent/src/turn-loop.ts) is the
// single-flight + queue owner. When the test sends two frames in rapid
// succession, the first submit starts a turn (running=true) and the second
// submit arrives while the first turn is still in the fake-llm HTTP round-trip,
// so it is buffered (not processed concurrently). The fake-llm round-trip
// (localhost HTTP + JSON + SSE) provides the in-flight window.

// TestAgentDialogQueueAutoHandoff verifies quickstart.md Scenario 2
// (specs/030-queued-chat-input/spec.md FR-005/FR-006): a message queued during
// a run becomes the next turn automatically, with exactly one terminal wait at
// the end. Also covers Scenario 4 (specs/030-queued-chat-input/spec.md
// FR-008/FR-009): QueueSignal depth changes drive pending visibility
// (submit⇒1, drain⇒0).
//
// Two messages are sent in rapid succession. The first starts a turn; the
// second is buffered (QueueSignal depth 1). On the first turn's completion,
// the buffer drains into the next turn (QueueSignal depth 0) WITHOUT an
// intervening wait — only a single terminal wait fires when the loop reaches
// idle.
func TestAgentDialogQueueAutoHandoff(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("ad-qah-%s", uniqueSuffix())

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// Send two messages in rapid succession. Both carry distinct keywords so
	// the fake-llm responses are deterministic and distinguishable.
	// msg-1: greeting keyword "hello" → greeting template.
	// msg-2: farewell keyword "goodbye" → farewell template.
	sendText(t, conn, sessionID, "hello world")
	sendText(t, conn, sessionID, "goodbye world")

	// Collect ALL frames until the terminal wait (loop idle).
	frames := drainUntilWait(t, conn)

	// specs/030-queued-chat-input/spec.md FR-008/FR-009: QueueSignal depth
	// sequence is [1, 0] — submit-while-running grew the buffer to 1, then
	// drain-into-next-turn cleared it
	// (specs/030-queued-chat-input/contracts/queue-channel-contract.md §2).
	depths := queueSignalDepths(frames)
	if len(depths) < 2 {
		t.Fatalf("expected at least 2 QueueSignal frames (submit+drain), got %d: %v", len(depths), depths)
	}
	// The first signal MUST be depth 1 (the second message was buffered).
	if depths[0] != 1 {
		t.Errorf("first QueueSignal depth = %d, want 1 (second message buffered)", depths[0])
	}
	// A depth-0 signal MUST appear (buffer drained into the next turn).
	foundZero := false
	for _, d := range depths {
		if d == 0 {
			foundZero = true
		}
	}
	if !foundZero {
		t.Errorf("no QueueSignal depth 0 found in sequence %v (buffer drain signal missing)", depths)
	}

	// specs/030-queued-chat-input/spec.md FR-006: exactly one terminal wait —
	// the loop continued from turn 1 to turn 2 WITHOUT an intervening wait
	// (only QueueSignal(0) between them).
	waitCount := countWaitFrames(frames)
	if waitCount != 1 {
		t.Errorf("wait frame count = %d, want 1 (single terminal wait after full drain); "+
			"if >1 the messages were processed sequentially rather than queued — "+
			"see specs/030-queued-chat-input/spec.md FR-006", waitCount)
	}

	// Both messages produced responses: greeting for msg-1, farewell for msg-2.
	texts := collectTextContents(frames)
	if len(texts) < 2 {
		t.Fatalf("expected at least 2 agent text responses, got %d: %v", len(texts), texts)
	}
	if !strings.Contains(texts[0], expectedGreetingText) {
		t.Errorf("turn 1 response = %q, want to contain %q (greeting)", texts[0], expectedGreetingText)
	}
	if !strings.Contains(texts[1], expectedFarewellText) {
		t.Errorf("turn 2 response = %q, want to contain %q (farewell — the queued message)", texts[1], expectedFarewellText)
	}
}

// TestAgentDialogQueueMultipleCombine verifies quickstart.md Scenario 3
// (specs/030-queued-chat-input/spec.md FR-004/FR-005): multiple messages
// queued during a single run are combined into ONE next turn in FIFO order,
// not processed as separate turns.
//
// Three messages are sent in rapid succession. msg-1 starts the first turn.
// msg-2 and msg-3 are buffered while turn 1 runs. On turn 1's completion, the
// buffer [msg-2, msg-3] is merged into one aggregated HumanMessage (FIFO) and
// run as exactly ONE next turn — NOT two separate turns.
func TestAgentDialogQueueMultipleCombine(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("ad-qmc-%s", uniqueSuffix())

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// msg-1 starts turn 1 (greeting). msg-2 and msg-3 are queued while turn 1
	// runs and combined into one aggregated turn on drain.
	sendText(t, conn, sessionID, "hello world")
	sendText(t, conn, sessionID, "goodbye world")
	sendText(t, conn, sessionID, "hi friend")

	frames := drainUntilWait(t, conn)

	// QueueSignal depth sequence: [1, 2, 0] — msg-2 grew buffer to 1, msg-3
	// grew it to 2, then the combined drain cleared it to 0
	// (specs/030-queued-chat-input/quickstart.md Scenario 3).
	depths := queueSignalDepths(frames)
	if len(depths) < 3 {
		t.Fatalf("expected at least 3 QueueSignal frames (two submits + drain), got %d: %v", len(depths), depths)
	}
	if depths[0] != 1 {
		t.Errorf("first QueueSignal depth = %d, want 1 (msg-2 buffered)", depths[0])
	}
	if depths[1] != 2 {
		t.Errorf("second QueueSignal depth = %d, want 2 (msg-3 buffered)", depths[1])
	}
	// A depth-0 signal MUST appear (combined turn drained the buffer).
	foundZero := false
	for _, d := range depths {
		if d == 0 {
			foundZero = true
		}
	}
	if !foundZero {
		t.Errorf("no QueueSignal depth 0 found in sequence %v (combined drain signal missing)", depths)
	}

	// specs/030-queued-chat-input/spec.md FR-005: exactly one terminal wait —
	// the loop ran turn 1, then the combined turn (from the drained buffer),
	// with no intervening wait.
	waitCount := countWaitFrames(frames)
	if waitCount != 1 {
		t.Errorf("wait frame count = %d, want 1 (single terminal wait after combined drain); "+
			"see specs/030-queued-chat-input/spec.md FR-005", waitCount)
	}

	// Turn 1 response is the greeting (msg-1). The combined turn response
	// addresses the merged [msg-2, msg-3] text — the fake-llm joins text
	// parts with a space and keyword-matches; "goodbye world hi friend"
	// contains both "bye" (farewell) and "hi" (greeting), and alphabetical
	// tie-breaking picks farewell (f < g). The combined response is therefore
	// deterministic.
	texts := collectTextContents(frames)
	if len(texts) < 2 {
		t.Fatalf("expected at least 2 agent text responses (turn 1 + combined turn), got %d: %v", len(texts), texts)
	}
	if !strings.Contains(texts[0], expectedGreetingText) {
		t.Errorf("turn 1 response = %q, want to contain %q (greeting for msg-1)", texts[0], expectedGreetingText)
	}
	// The combined turn produced a response (its exact content depends on
	// fake-llm keyword matching of the merged text — see comment above).
	t.Logf("combined turn response: %q", texts[1])
}

// TestAgentDialogQueueInputDoesNotDisturb verifies quickstart.md Scenario 1
// (specs/030-queued-chat-input/spec.md FR-001/FR-002): submitting a message
// while a turn is in progress does not disturb the in-flight turn — its
// streamed response completes as if nothing was queued.
//
// The TurnLoop guarantees (specs/030-queued-chat-input/spec.md FR-002) that a
// submit-while-running only touches the buffer; the in-flight generateTurn is
// isolated. The observable proof is that turn 1's response content is complete
// and correct (greeting template), identical to a turn run with nothing queued.
func TestAgentDialogQueueInputDoesNotDisturb(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("ad-qnd-%s", uniqueSuffix())

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// Send msg-1 (greeting), then immediately msg-2 (farewell) while turn 1
	// is still in the fake-llm round-trip. The queued msg-2 MUST NOT alter
	// turn 1's output.
	sendText(t, conn, sessionID, "hello world")
	sendText(t, conn, sessionID, "goodbye world")

	frames := drainUntilWait(t, conn)

	// specs/030-queued-chat-input/spec.md FR-002: turn 1's response is
	// complete and correct — the greeting template with both reasoning and
	// text, identical to an undisturbed turn.
	thinkingFrames := 0
	textFrames := 0
	for _, f := range frames {
		if f.GetSender() != game.FrameSender_FRAME_SENDER_AGENT {
			continue
		}
		if frameHasThinking(f) {
			thinkingFrames++
		}
		if frameHasText(f) {
			textFrames++
		}
	}
	if thinkingFrames < 1 {
		t.Errorf("no agent thinking frame received (turn 1 reasoning missing)")
	}
	if textFrames < 2 {
		t.Errorf("agent text frame count = %d, want >= 2 (turn 1 greeting + queued turn response)", textFrames)
	}

	// Turn 1's text is the greeting (undisturbed by the queued msg-2).
	texts := collectTextContents(frames)
	if len(texts) == 0 {
		t.Fatal("no agent text responses received")
	}
	if !strings.Contains(texts[0], expectedGreetingText) {
		t.Errorf("turn 1 response = %q, want to contain %q (in-flight turn undisturbed by queued message; specs/030-queued-chat-input/spec.md FR-002)",
			texts[0], expectedGreetingText)
	}
}
