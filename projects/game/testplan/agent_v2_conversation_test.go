// Package testplan contains agent_v2 conversation integration tests. These
// tests validate the agent_v2 conversation surface end-to-end through the
// gateway /api/v2 NDJSON API (browser path: gateway → proxy owner affinity →
// agent_v2 stateful instance, specs/049-agent-v2-dsh-init/contracts/
// conversation-api.md §1 托管拓扑), with the deterministic fake /v1/responses
// endpoint replacing the real GLM endpoint (contracts/fake-responses-wire.md
// §4). Assertions anchor the quickstart §2 用例 2–9 scenario table
// (specs/049-agent-v2-dsh-init/quickstart.md).
package testplan

import (
	"context"
	"net/http"
	"testing"
	"time"

	"dominion/common/gopkg/testtool"
	gamev2 "dominion/projects/game/v2"
)

// agentV2TurnResult is what a drain goroutine reports back: the collected
// events plus the first read error, if any. t.Fatal must only run on the test
// goroutine, so async drains report through this channel instead.
type agentV2TurnResult struct {
	events []*gamev2.ChatEvent
	err    error
}

// drainAgentV2TurnAsync drains a Send stream to its turn_end on a reader
// goroutine and reports the events (or the read error) on the channel. The
// stream body is closed after the turn ends.
func drainAgentV2TurnAsync(stream *agentV2EventStream) <-chan agentV2TurnResult {
	ch := make(chan agentV2TurnResult, 1)
	go func() {
		var result agentV2TurnResult
		defer func() {
			stream.Close()
			ch <- result
		}()
		for {
			evt, err := nextAgentV2EventNoFatal(stream.Scanner)
			if err != nil {
				result.err = err
				return
			}
			result.events = append(result.events, evt)
			if evt.GetTurnEnd() != nil {
				return
			}
		}
	}()
	return ch
}

// agentV2Prep creates one saolei-template session and returns its full
// /api/v2 resource name — the shared arrange step of every conversation
// test (the /api/v2 surface keys sessions off the resource name alone).
func agentV2Prep(t *testing.T, sutHostURL, sutEnvName string) (ctx context.Context, sessionName string) {
	t.Helper()
	ctx = traceContext(t)
	sessionID, _ := createSession(t, sutHostURL, sutEnvName, saoleiTemplateID)
	return ctx, agentV2SessionName(sessionID)
}

// TestAgentV2StreamedTurnSequenceAndBlocks covers quickstart §2 用例 2
// (US1-1/US2): a think+text turn streams the full event sequence
// turn_start → THINK deltas (progressive, multi-frame) → TEXT delta →
// turn_end{COMPLETED}, with THINK and TEXT as separately attributable blocks
// (SC-002) and usage folded into turn_end (§3 invariant 6). The greet
// template streams two reasoning pieces then the full text
// (testdata/agent_v2.yaml agent-v2-greet).
func TestAgentV2StreamedTurnSequenceAndBlocks(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx, sessionName := agentV2Prep(t, sutHostURL, sutEnvName)

	stream := startAgentV2Send(t, ctx, sutHostURL, sutEnvName, sessionName,
		agentV2TriggerThink+" introduce yourself")
	defer stream.Close()
	events := drainAgentV2Turn(t, stream)
	assertAgentV2TurnWellFormed(t, sessionName, events)

	// Frame shape: turn_start first (no queue — the session was idle), THINK
	// block with the two greet reasoning pieces as separate progressive
	// deltas, then the TEXT block with the greet text. The reasoning item
	// carries no done event on the fake wire (fake-responses-wire.md §2), so
	// only the TEXT block gets a block_end.
	if events[0].GetTurnStart() == nil {
		t.Fatalf("first frame payload = %T, want turn_start", events[0].GetPayload())
	}
	if start := events[1].GetBlockStart(); start == nil || start.GetType() != gamev2.BlockType_BLOCK_TYPE_THINK {
		t.Fatalf("frame 2 payload = %T, want block_start{THINK}", events[1].GetPayload())
	}
	if delta := events[2].GetDelta(); delta == nil || delta.GetText() != agentV2GreetThink1 {
		t.Fatalf("frame 3 = %v, want THINK delta %q", delta, agentV2GreetThink1)
	}
	if delta := events[3].GetDelta(); delta == nil || delta.GetText() != agentV2GreetThink2 {
		t.Fatalf("frame 4 = %v, want THINK delta %q (multi-frame progressive think)", delta, agentV2GreetThink2)
	}
	if start := events[4].GetBlockStart(); start == nil || start.GetType() != gamev2.BlockType_BLOCK_TYPE_TEXT {
		t.Fatalf("frame 5 payload = %T, want block_start{TEXT}", events[4].GetPayload())
	}
	if delta := events[5].GetDelta(); delta == nil || delta.GetText() != agentV2GreetText {
		t.Fatalf("frame 6 = %v, want TEXT delta %q", delta, agentV2GreetText)
	}
	end := events[6].GetBlockEnd()
	if end == nil || end.GetBlock().GetText() == nil {
		t.Fatalf("frame 7 payload = %T, want block_end{text}", events[6].GetPayload())
	}
	turnEnd := events[7].GetTurnEnd()
	if turnEnd == nil || turnEnd.GetStatus() != gamev2.TurnStatus_TURN_STATUS_COMPLETED {
		t.Fatalf("frame 8 = %v, want turn_end{COMPLETED}", turnEnd)
	}
	if got := end.GetBlock().GetText().GetContent(); got != agentV2GreetText {
		t.Errorf("block_end text = %q, want the delta concatenation %q (§3 invariant 4)", got, agentV2GreetText)
	}

	// THINK content is distinct from TEXT content (US2, SC-002): the think
	// deltas assemble the reasoning, the text deltas the reply body.
	term := agentV2TerminalBlocksFromEvents(events)
	if term.think != agentV2GreetThink1+agentV2GreetThink2 {
		t.Errorf("terminal think = %q, want %q", term.think, agentV2GreetThink1+agentV2GreetThink2)
	}
	if term.text != agentV2GreetText {
		t.Errorf("terminal text = %q, want %q", term.text, agentV2GreetText)
	}

	// usage rides turn_end (§3 invariant 6); the fake derives deterministic
	// positive numbers from the template lengths (fake-responses-wire.md §2).
	if turnEnd.GetUsage() == nil || turnEnd.GetUsage().GetOutputTokens() <= 0 {
		t.Errorf("turn_end usage = %v, want non-nil with positive output_tokens", turnEnd.GetUsage())
	}
}

// TestAgentV2PlainTextTurnHasNoThink covers quickstart §2 用例 2 second half
// (US2 场景 2): a turn served by the pure-text template (agent-v2-plain)
// streams zero THINK blocks — no empty thinking region may exist.
func TestAgentV2PlainTextTurnHasNoThink(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx, sessionName := agentV2Prep(t, sutHostURL, sutEnvName)

	stream := startAgentV2Send(t, ctx, sutHostURL, sutEnvName, sessionName,
		agentV2TriggerPlain+", no reasoning please")
	defer stream.Close()
	events := drainAgentV2Turn(t, stream)
	assertAgentV2TurnWellFormed(t, sessionName, events)

	for i, e := range events {
		if start := e.GetBlockStart(); start != nil && start.GetType() == gamev2.BlockType_BLOCK_TYPE_THINK {
			t.Fatalf("frame %d starts a THINK block — the pure-text turn must stream zero THINK blocks (US2 场景 2)", i)
		}
	}
	term := agentV2TerminalBlocksFromEvents(events)
	if term.think != "" {
		t.Errorf("terminal think = %q, want empty", term.think)
	}
	if term.text != agentV2PlainText {
		t.Errorf("terminal text = %q, want %q", term.text, agentV2PlainText)
	}
	if events[0].GetTurnStart() == nil {
		t.Fatalf("first frame payload = %T, want turn_start", events[0].GetPayload())
	}
}

// TestAgentV2MultiTurnContinuity covers quickstart §2 用例 3 (US1-2): the
// second turn of a session is served by the agent-v2-followup template — it
// fires only because the turn-1 assistant reply already sits in the model
// input history (history_keywords condition, responses.go
// matchResponsesMultiTurn), so its content proves the reply depends on the
// earlier exchange.
func TestAgentV2MultiTurnContinuity(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx, sessionName := agentV2Prep(t, sutHostURL, sutEnvName)

	// Turn 1 (greet): establishes the history the followup condition needs.
	stream1 := startAgentV2Send(t, ctx, sutHostURL, sutEnvName, sessionName,
		agentV2TriggerThink+" what can you do")
	events1 := drainAgentV2Turn(t, stream1)
	stream1.Close()
	assertAgentV2TurnWellFormed(t, sessionName, events1)
	if got := agentV2TerminalBlocksFromEvents(events1).text; got != agentV2GreetText {
		t.Fatalf("turn 1 text = %q, want %q (greet template must seed the history)", got, agentV2GreetText)
	}

	// Turn 2 (same trigger word, same session): the followup template wins
	// over greet because its history keyword hits the turn-1 reply.
	stream2 := startAgentV2Send(t, ctx, sutHostURL, sutEnvName, sessionName,
		agentV2TriggerThink+" say that again")
	defer stream2.Close()
	events2 := drainAgentV2Turn(t, stream2)
	assertAgentV2TurnWellFormed(t, sessionName, events2)

	if got := agentV2TerminalBlocksFromEvents(events2).text; got != agentV2FollowupText {
		t.Errorf("turn 2 text = %q, want %q — the reply must reference the turn-1 exchange (US1-2)", got, agentV2FollowupText)
	}
	if events1[0].GetTurnId() == events2[0].GetTurnId() {
		t.Errorf("turn 1 and turn 2 share turn_id %q — each turn must mint its own identity", events1[0].GetTurnId())
	}
}

// TestAgentV2ConcurrentSessionIsolation covers quickstart §2 用例 4 (US1-3):
// two sessions stream slow turns CONCURRENTLY on the shared agent_v2
// instance. The second session's first frame is turn_start — had the sessions
// shared a queue it would be queued{position} behind the first session's
// turn (§3 invariant 7 is scoped per session). Each session's history holds
// only its own marker.
func TestAgentV2ConcurrentSessionIsolation(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	id1, _ := createSession(t, sutHostURL, sutEnvName, saoleiTemplateID)
	id2, _ := createSession(t, sutHostURL, sutEnvName, saoleiTemplateID)
	name1 := agentV2SessionName(id1)
	name2 := agentV2SessionName(id2)

	text1 := agentV2TriggerSlow + " session one isolation marker"
	text2 := agentV2TriggerSlow + " session two isolation marker"

	// Open both streams before draining either so the turns overlap: the
	// slow template's 3s inter-chunk delay keeps both in flight
	// (testdata/agent_v2.yaml agent-v2-slow).
	stream1 := startAgentV2Send(t, ctx, sutHostURL, sutEnvName, name1, text1)
	first1 := nextAgentV2Event(t, stream1.Scanner)
	if first1.GetTurnStart() == nil {
		stream1.Close()
		t.Fatalf("session 1 first frame payload = %T, want turn_start", first1.GetPayload())
	}
	stream2 := startAgentV2Send(t, ctx, sutHostURL, sutEnvName, name2, text2)
	first2 := nextAgentV2Event(t, stream2.Scanner)
	if first2.GetQueued() != nil {
		stream1.Close()
		stream2.Close()
		t.Fatalf("session 2 first frame is queued{%d} — a session must not queue behind ANOTHER session's turn (US1-3, FR-012)", first2.GetQueued().GetPosition())
	}
	if first2.GetTurnStart() == nil {
		stream1.Close()
		stream2.Close()
		t.Fatalf("session 2 first frame payload = %T, want turn_start", first2.GetPayload())
	}

	// Drain both concurrently and wait for both — a shared serialization
	// would surface as one stream starving the other.
	ch1 := drainAgentV2TurnAsync(stream1)
	ch2 := drainAgentV2TurnAsync(stream2)
	var events1, events2 []*gamev2.ChatEvent
	for events1 == nil || events2 == nil {
		select {
		case r := <-ch1:
			if r.err != nil {
				t.Fatalf("session 1 stream: %v", r.err)
			}
			events1 = r.events
		case r := <-ch2:
			if r.err != nil {
				t.Fatalf("session 2 stream: %v", r.err)
			}
			events2 = r.events
		case <-time.After(wsReadTimeout):
			t.Fatal("concurrent turns did not both complete within the read window")
		}
	}
	assertAgentV2TurnWellFormed(t, name1, append([]*gamev2.ChatEvent{first1}, events1...))
	assertAgentV2TurnWellFormed(t, name2, append([]*gamev2.ChatEvent{first2}, events2...))
	if got := agentV2TerminalBlocksFromEvents(events1).text; got != agentV2SlowText {
		t.Errorf("session 1 text = %q, want %q", got, agentV2SlowText)
	}
	if got := agentV2TerminalBlocksFromEvents(events2).text; got != agentV2SlowText {
		t.Errorf("session 2 text = %q, want %q", got, agentV2SlowText)
	}
	if events1[0].GetTurnId() == events2[0].GetTurnId() {
		t.Errorf("both sessions report turn_id %q — turn identity leaked across sessions", events1[0].GetTurnId())
	}

	// Each history carries only its own marker (no cross-session bleed).
	hist1 := listAgentV2Messages(t, ctx, sutHostURL, sutEnvName, name1)
	hist2 := listAgentV2Messages(t, ctx, sutHostURL, sutEnvName, name2)
	if len(hist1.GetMessages()) == 0 || agentV2MessageText(hist1.GetMessages()[0]) != text1 {
		t.Errorf("session 1 history[0] = %+v, want the session-1 user marker %q", hist1.GetMessages()[0], text1)
	}
	for i, m := range hist2.GetMessages() {
		if m.GetRole() == gamev2.Role_ROLE_USER && agentV2MessageText(m) == text1 {
			t.Errorf("session 2 history[%d] carries session 1's marker — histories are not isolated (US1-3)", i)
		}
	}
}

// TestAgentV2QueuedTurnAutoResumes covers quickstart §2 用例 5 (FR-012): a
// Send arriving while the session's turn is still running receives
// queued{position} as its first frame, stays silent until the running turn's
// turn_end, then starts automatically and completes in order (§3 invariant
// 7). The slow template's 3s inter-chunk delay is the controllable window.
func TestAgentV2QueuedTurnAutoResumes(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx, sessionName := agentV2Prep(t, sutHostURL, sutEnvName)

	// Turn A: confirm it is running by reading its turn_start before
	// submitting the queued message.
	streamA := startAgentV2Send(t, ctx, sutHostURL, sutEnvName, sessionName,
		agentV2TriggerSlow+" queue window probe")
	firstA := nextAgentV2Event(t, streamA.Scanner)
	if firstA.GetTurnStart() == nil {
		streamA.Close()
		t.Fatalf("turn A first frame payload = %T, want turn_start", firstA.GetPayload())
	}

	// Turn B arrives mid-turn: its first frame must be queued (1-based
	// position ≥ 1, §3 invariant 7).
	streamB := startAgentV2Send(t, ctx, sutHostURL, sutEnvName, sessionName,
		agentV2TriggerSlow+" queued second message")
	firstB := nextAgentV2Event(t, streamB.Scanner)
	if firstB.GetQueued() == nil {
		streamA.Close()
		streamB.Close()
		t.Fatalf("turn B first frame payload = %T, want queued{position} while turn A runs (FR-012)", firstB.GetPayload())
	}
	if pos := firstB.GetQueued().GetPosition(); pos < 1 {
		t.Errorf("queued position = %d, want >= 1", pos)
	}

	chA := drainAgentV2TurnAsync(streamA)
	chB := drainAgentV2TurnAsync(streamB)
	var eventsA, eventsB []*gamev2.ChatEvent
	for eventsA == nil || eventsB == nil {
		select {
		case r := <-chA:
			if r.err != nil {
				t.Fatalf("turn A stream: %v", r.err)
			}
			eventsA = r.events
		case r := <-chB:
			if r.err != nil {
				t.Fatalf("turn B stream: %v", r.err)
			}
			eventsB = r.events
		case <-time.After(wsReadTimeout):
			t.Fatal("queued turn did not auto-resume within the read window")
		}
	}
	fullA := append([]*gamev2.ChatEvent{firstA}, eventsA...)
	fullB := append([]*gamev2.ChatEvent{firstB}, eventsB...)
	assertAgentV2TurnWellFormed(t, sessionName, fullA)
	assertAgentV2TurnWellFormed(t, sessionName, fullB)

	// Turn A completed and turn B was answered in order (queued → automatic
	// start → completion, FR-012); assertAgentV2TurnWellFormed already pins
	// queued-first and turn_start-before-blocks within each stream.
	if eventsA[len(eventsA)-1].GetTurnEnd().GetStatus() != gamev2.TurnStatus_TURN_STATUS_COMPLETED {
		t.Fatalf("turn A ended %v, want COMPLETED", eventsA[len(eventsA)-1].GetTurnEnd().GetStatus())
	}
	if got := agentV2TerminalBlocksFromEvents(eventsA).text; got != agentV2SlowText {
		t.Errorf("turn A text = %q, want %q", got, agentV2SlowText)
	}
	if got := agentV2TerminalBlocksFromEvents(eventsB).text; got != agentV2SlowText {
		t.Errorf("turn B text = %q, want %q (the queued message was sent and answered, FR-012)", got, agentV2SlowText)
	}
	if eventsA[0].GetTurnId() == eventsB[0].GetTurnId() {
		t.Errorf("turns A and B share turn_id %q", eventsA[0].GetTurnId())
	}
	// The queue was consumed in order: both user messages persisted.
	hist := listAgentV2Messages(t, ctx, sutHostURL, sutEnvName, sessionName)
	if len(hist.GetMessages()) != 4 {
		t.Fatalf("history messages = %d, want 4 (two user turns + two agent replies)", len(hist.GetMessages()))
	}
}

// TestAgentV2HistoryBackfillMatchesStream covers quickstart §2 用例 6
// (FR-014): a never-seen session reads an empty history through the proxy
// short-circuit (no owner allocation, conversation.go agent read paths); after a
// greet turn, ListAgentMessages returns the user message plus the agent reply whose
// blocks equal the streamed terminal state (think = the two reasoning
// pieces, text = the reply body) with per-message ids.
func TestAgentV2HistoryBackfillMatchesStream(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	// A session that never sent a message: 200 with an empty list — the
	// read path must not allocate an owner (conversation-api.md §2).
	ghostName := "templates/" + saoleiTemplateID + "/sessions/ghost-" + uniqueSuffix()
	hist := listAgentV2Messages(t, ctx, sutHostURL, sutEnvName, ghostName)
	if len(hist.GetMessages()) != 0 {
		t.Fatalf("never-used session history = %d messages, want 0", len(hist.GetMessages()))
	}

	sessionID, _ := createSession(t, sutHostURL, sutEnvName, saoleiTemplateID)
	sessionName := agentV2SessionName(sessionID)
	userText := agentV2TriggerThink + " remember this turn"

	stream := startAgentV2Send(t, ctx, sutHostURL, sutEnvName, sessionName, userText)
	defer stream.Close()
	events := drainAgentV2Turn(t, stream)
	assertAgentV2TurnWellFormed(t, sessionName, events)
	term := agentV2TerminalBlocksFromEvents(events)

	hist = listAgentV2Messages(t, ctx, sutHostURL, sutEnvName, sessionName)
	messages := hist.GetMessages()
	if len(messages) != 2 {
		t.Fatalf("history messages = %d, want 2 (user turn + agent reply)", len(messages))
	}
	if messages[0].GetRole() != gamev2.Role_ROLE_USER || agentV2MessageText(messages[0]) != userText {
		t.Errorf("history[0] role = %s text = %q, want USER %q", messages[0].GetRole(), agentV2MessageText(messages[0]), userText)
	}
	if messages[1].GetRole() != gamev2.Role_ROLE_AGENT {
		t.Errorf("history[1] role = %s, want AGENT", messages[1].GetRole())
	}
	// Backfill equals the streamed terminal state (FR-014: 回填内容与流式终态一致).
	if got := agentV2MessageThink(messages[1]); got != term.think {
		t.Errorf("history[1] think = %q, want streamed terminal %q", got, term.think)
	}
	if got := agentV2MessageText(messages[1]); got != term.text {
		t.Errorf("history[1] text = %q, want streamed terminal %q", got, term.text)
	}
	if messages[0].GetMessageId() == "" || messages[1].GetMessageId() == "" {
		t.Errorf("history message ids = %q / %q, want server-assigned non-empty ids", messages[0].GetMessageId(), messages[1].GetMessageId())
	}
	if messages[0].GetMessageId() == messages[1].GetMessageId() {
		t.Errorf("history message ids collide on %q", messages[0].GetMessageId())
	}
}

// TestAgentV2ModelFailureRecovers covers quickstart §2 用例 8 (Edge-模型故障):
// the failure template emits response.failed, the turn ends turn_end{ERROR}
// with a structured error, and the session stays usable — a follow-up turn
// completes normally.
func TestAgentV2ModelFailureRecovers(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx, sessionName := agentV2Prep(t, sutHostURL, sutEnvName)

	stream := startAgentV2Send(t, ctx, sutHostURL, sutEnvName, sessionName,
		agentV2TriggerFail+" break this turn")
	defer stream.Close()
	events := drainAgentV2Turn(t, stream)
	assertAgentV2TurnWellFormed(t, sessionName, events)

	for i, e := range events {
		if e.GetBlockStart() != nil {
			t.Errorf("frame %d starts a block — a failed turn must produce no content blocks", i)
		}
	}
	end := events[len(events)-1].GetTurnEnd()
	if end.GetStatus() != gamev2.TurnStatus_TURN_STATUS_ERROR {
		t.Fatalf("failed turn ended %v, want ERROR (Edge-模型故障)", end.GetStatus())
	}
	if end.GetError() == nil || end.GetError().GetMessage() == "" {
		t.Errorf("turn_end.error = %+v, want a structured error payload", end.GetError())
	} else {
		t.Logf("turn error: code=%q message=%q", end.GetError().GetCode(), end.GetError().GetMessage())
	}

	// The session survives: a follow-up turn completes (回合内错误走事件，
	// HTTP 仍 200 — conversation-api.md §2).
	stream2 := startAgentV2Send(t, ctx, sutHostURL, sutEnvName, sessionName,
		agentV2TriggerPlain+" recover now")
	defer stream2.Close()
	events2 := drainAgentV2Turn(t, stream2)
	assertAgentV2TurnWellFormed(t, sessionName, events2)
	if events2[len(events2)-1].GetTurnEnd().GetStatus() != gamev2.TurnStatus_TURN_STATUS_COMPLETED {
		t.Fatalf("recovery turn ended %v, want COMPLETED", events2[len(events2)-1].GetTurnEnd().GetStatus())
	}
	if got := agentV2TerminalBlocksFromEvents(events2).text; got != agentV2PlainText {
		t.Errorf("recovery turn text = %q, want %q", got, agentV2PlainText)
	}
}

// TestAgentV2InvalidInputRejected covers quickstart §2 用例 9 (Edge-非法输入):
// request-level invalid input maps to 400 INVALID_ARGUMENT at the /api/v2
// surface and the service stays healthy afterwards.
func TestAgentV2InvalidInputRejected(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx, sessionName := agentV2Prep(t, sutHostURL, sutEnvName)

	tests := []struct {
		name    string
		session string
		text    string
	}{
		// The proxy validates empty text (conversation.go Send) before any
		// owner work; unknown templates fail resource-name parsing — both
		// INVALID_ARGUMENT → 400 (conversation-api.md §2.1).
		{name: "empty text", session: sessionName, text: ""},
		{name: "unknown template", session: "templates/unknown-template/sessions/" + uniqueSuffix(), text: "hello"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := postAgentV2SendStatus(t, ctx, sutHostURL, sutEnvName, tt.session, tt.text)
			if status != http.StatusBadRequest {
				t.Fatalf("invalid send status = %d (body: %s), want 400", status, body)
			}
		})
	}

	// The service must be unaffected: a valid turn completes.
	stream := startAgentV2Send(t, ctx, sutHostURL, sutEnvName, sessionName,
		agentV2TriggerPlain+" still alive")
	defer stream.Close()
	events := drainAgentV2Turn(t, stream)
	assertAgentV2TurnWellFormed(t, sessionName, events)
	if events[len(events)-1].GetTurnEnd().GetStatus() != gamev2.TurnStatus_TURN_STATUS_COMPLETED {
		t.Fatalf("post-rejection turn ended %v, want COMPLETED", events[len(events)-1].GetTurnEnd().GetStatus())
	}
}
