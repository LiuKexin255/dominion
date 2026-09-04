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
	game "dominion/projects/game"
)

// agentV2Prep creates one saolei-template session, materializes its agent
// (preset + UpdateAgent — Send has no lazy materialization since FR-007),
// and returns the full /api/v2 resource name — the shared arrange step of
// every conversation test (the /api/v2 surface keys sessions off the
// resource name alone).
func agentV2Prep(t *testing.T, sutHostURL, sutEnvName string) (ctx context.Context, sessionName string) {
	t.Helper()
	ctx = traceContext(t)
	sessionID, _ := createSession(t, sutHostURL, sutEnvName, saoleiTemplateID)
	sessionName = agentV2SessionName(sessionID)
	preset := createAgentV2Preset(t, ctx, sutHostURL, sutEnvName, "conv-"+uniqueSuffix(), "conversation persona")
	updateAgentV2Agent(t, ctx, sutHostURL, sutEnvName, sessionName, preset.GetName(), "")
	return ctx, sessionName
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
	if start := events[1].GetBlockStart(); start == nil || start.GetType() != game.BlockType_BLOCK_TYPE_THINK {
		t.Fatalf("frame 2 payload = %T, want block_start{THINK}", events[1].GetPayload())
	}
	if delta := events[2].GetDelta(); delta == nil || delta.GetText() != agentV2GreetThink1 {
		t.Fatalf("frame 3 = %v, want THINK delta %q", delta, agentV2GreetThink1)
	}
	if delta := events[3].GetDelta(); delta == nil || delta.GetText() != agentV2GreetThink2 {
		t.Fatalf("frame 4 = %v, want THINK delta %q (multi-frame progressive think)", delta, agentV2GreetThink2)
	}
	if start := events[4].GetBlockStart(); start == nil || start.GetType() != game.BlockType_BLOCK_TYPE_TEXT {
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
	if turnEnd == nil || turnEnd.GetStatus() != game.TurnStatus_TURN_STATUS_COMPLETED {
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
		if start := e.GetBlockStart(); start != nil && start.GetType() == game.BlockType_BLOCK_TYPE_THINK {
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
	// Both sessions materialize their own agent before the turns (FR-007).
	preset1 := createAgentV2Preset(t, ctx, sutHostURL, sutEnvName, "conv-iso-1-"+uniqueSuffix(), "isolation one")
	preset2 := createAgentV2Preset(t, ctx, sutHostURL, sutEnvName, "conv-iso-2-"+uniqueSuffix(), "isolation two")
	updateAgentV2Agent(t, ctx, sutHostURL, sutEnvName, name1, preset1.GetName(), "")
	updateAgentV2Agent(t, ctx, sutHostURL, sutEnvName, name2, preset2.GetName(), "")

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
	var events1, events2 []*game.ChatEvent
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
	assertAgentV2TurnWellFormed(t, name1, append([]*game.ChatEvent{first1}, events1...))
	assertAgentV2TurnWellFormed(t, name2, append([]*game.ChatEvent{first2}, events2...))
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
		if m.GetRole() == game.Role_ROLE_USER && agentV2MessageText(m) == text1 {
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
	var eventsA, eventsB []*game.ChatEvent
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
	fullA := append([]*game.ChatEvent{firstA}, eventsA...)
	fullB := append([]*game.ChatEvent{firstB}, eventsB...)
	assertAgentV2TurnWellFormed(t, sessionName, fullA)
	assertAgentV2TurnWellFormed(t, sessionName, fullB)

	// Turn A completed and turn B was answered in order (queued → automatic
	// start → completion, FR-012); assertAgentV2TurnWellFormed already pins
	// queued-first and turn_start-before-blocks within each stream.
	if eventsA[len(eventsA)-1].GetTurnEnd().GetStatus() != game.TurnStatus_TURN_STATUS_COMPLETED {
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
// (FR-014): a never-materialized session reads its history as 404 NOT_FOUND
// — the read paths only look the owner up and never allocate one
// (specs/051-agent-v2-dsh-migration/contracts/agent-api.md §2.2/§2.3); after
// a greet turn, ListAgentMessages returns the user message plus the agent
// reply whose blocks equal the streamed terminal state (think = the two
// reasoning pieces, text = the reply body) with per-message ids.
func TestAgentV2HistoryBackfillMatchesStream(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	// A session that never materialized an agent: 404 NOT_FOUND — the read
	// path must not allocate an owner (agent-api.md §2.2/§2.3).
	ghostName := "templates/" + saoleiTemplateID + "/sessions/ghost-" + uniqueSuffix()
	status, _ := listAgentV2MessagesWithStatus(t, ctx, sutHostURL, sutEnvName, ghostName)
	if status != http.StatusNotFound {
		t.Fatalf("never-materialized session history status = %d, want 404 NOT_FOUND (agent-api.md §2.3)", status)
	}

	sessionID, _ := createSession(t, sutHostURL, sutEnvName, saoleiTemplateID)
	sessionName := agentV2SessionName(sessionID)
	userText := agentV2TriggerThink + " remember this turn"

	// Materialize before the turn (FR-007: no lazy creation).
	preset := createAgentV2Preset(t, ctx, sutHostURL, sutEnvName, "conv-backfill-"+uniqueSuffix(), "backfill persona")
	updateAgentV2Agent(t, ctx, sutHostURL, sutEnvName, sessionName, preset.GetName(), "")

	stream := startAgentV2Send(t, ctx, sutHostURL, sutEnvName, sessionName, userText)
	defer stream.Close()
	events := drainAgentV2Turn(t, stream)
	assertAgentV2TurnWellFormed(t, sessionName, events)
	term := agentV2TerminalBlocksFromEvents(events)

	hist := listAgentV2Messages(t, ctx, sutHostURL, sutEnvName, sessionName)
	messages := hist.GetMessages()
	if len(messages) != 2 {
		t.Fatalf("history messages = %d, want 2 (user turn + agent reply)", len(messages))
	}
	if messages[0].GetRole() != game.Role_ROLE_USER || agentV2MessageText(messages[0]) != userText {
		t.Errorf("history[0] role = %s text = %q, want USER %q", messages[0].GetRole(), agentV2MessageText(messages[0]), userText)
	}
	if messages[1].GetRole() != game.Role_ROLE_AGENT {
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
	if end.GetStatus() != game.TurnStatus_TURN_STATUS_ERROR {
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
	if events2[len(events2)-1].GetTurnEnd().GetStatus() != game.TurnStatus_TURN_STATUS_COMPLETED {
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
		// The proxy validates empty text (agent.go Send) before any
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
	if events[len(events)-1].GetTurnEnd().GetStatus() != game.TurnStatus_TURN_STATUS_COMPLETED {
		t.Fatalf("post-rejection turn ended %v, want COMPLETED", events[len(events)-1].GetTurnEnd().GetStatus())
	}
}

// agentV2BlockSpan is one streamed block's segmentation facts: the block
// index, the model-output step it belongs to (agent-api-changes.md §1), and
// the block type.
type agentV2BlockSpan struct {
	index int32
	step  int32
	kind  game.BlockType
}

// agentV2BlockSpansFromEvents folds a turn's block_start frames into the
// per-block segmentation facts, in stream order.
func agentV2BlockSpansFromEvents(events []*game.ChatEvent) []agentV2BlockSpan {
	var spans []agentV2BlockSpan
	for _, e := range events {
		if start := e.GetBlockStart(); start != nil {
			spans = append(spans, agentV2BlockSpan{index: start.GetIndex(), step: start.GetStep(), kind: start.GetType()})
		}
	}
	return spans
}

// TestAgentV2StepSegmentedBlocksAndHistory covers the step extension
// (agent-api-changes.md §1) end to end on a multi-step game chain: every
// block event carries the step of the model output that produced it, the
// steps are non-decreasing and segment the turn (one tool call per model
// request, the terminal text on its own step), and the history backfill
// holds ONE assistant message per step (specs/054-agent-v2-bugfixes/
// data-model.md §6). The chain runs on the test's own session with the test
// answering the init dispatch over its own flow connection — the
// desktop_flow suite's serving pattern — so the game suite's executor
// session history is untouched.
func TestAgentV2StepSegmentedBlocksAndHistory(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	sessionID := "conv-steps-" + uniqueSuffix()
	ctx, sessionName, _ := agentV2GamePrep(t, sutHostURL, sutEnvName,
		sessionID, "conv-steps-"+uniqueSuffix(), "step segmentation")

	flow, _ := dialAgentV2FlowProbed(t, ctx, sutHostURL, sutEnvName, sessionID)
	defer flow.Close()

	stream := startAgentV2Send(t, ctx, sutHostURL, sutEnvName, sessionName,
		agentV2TriggerSaoleiGame+" across model steps")
	ch := drainAgentV2TurnAsync(stream)

	serveWonInitReceipt(t, flow, sessionID, wsReadTimeout)

	var events []*game.ChatEvent
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("send stream: %v", r.err)
		}
		events = r.events
	case <-time.After(wsReadTimeout):
		t.Fatal("multi-step game turn did not complete within the read window")
	}
	assertAgentV2TurnWellFormed(t, sessionName, events)
	if end := events[len(events)-1].GetTurnEnd(); end.GetStatus() != game.TurnStatus_TURN_STATUS_COMPLETED {
		t.Fatalf("game turn ended %v, want COMPLETED", end.GetStatus())
	}

	// Chain segmentation: init call (step 1) → operate call (step 2) →
	// summary text (step 3) — the won chain's three model requests (the
	// server step loop numbers steps from 1; agent-api-changes.md §1).
	spans := agentV2BlockSpansFromEvents(events)
	wantSpans := []agentV2BlockSpan{
		{index: 0, step: 1, kind: game.BlockType_BLOCK_TYPE_TOOL_CALL},
		{index: 1, step: 2, kind: game.BlockType_BLOCK_TYPE_TOOL_CALL},
		{index: 2, step: 3, kind: game.BlockType_BLOCK_TYPE_TEXT},
	}
	if len(spans) != len(wantSpans) {
		t.Fatalf("block count = %d (%v), want %d blocks on steps 1/2/3", len(spans), spans, len(wantSpans))
	}
	for i, span := range spans {
		if span != wantSpans[i] {
			t.Errorf("block[%d] = %+v, want %+v (the chain's step segmentation)", i, span, wantSpans[i])
		}
	}

	// Every delta and block_end of a block carries that block's step —
	// consumers group the events into per-step segments by this field.
	stepByIndex := map[int32]int32{}
	for _, span := range spans {
		stepByIndex[span.index] = span.step
	}
	lastStep := int32(-1)
	for i, e := range events {
		var index, step int32
		switch {
		case e.GetDelta() != nil:
			index, step = e.GetDelta().GetIndex(), e.GetDelta().GetStep()
		case e.GetBlockEnd() != nil:
			index, step = e.GetBlockEnd().GetIndex(), e.GetBlockEnd().GetStep()
		default:
			continue
		}
		if want := stepByIndex[index]; step != want {
			t.Errorf("frame %d carries step %d for block %d, want the block's step %d", i, step, index, want)
		}
		if step < lastStep {
			t.Errorf("frame %d step = %d, below the previous event's step %d (steps must be non-decreasing)", i, step, lastStep)
		}
		lastStep = step
	}

	// History backfill: user + ONE assistant message per step, each holding
	// exactly that step's settled blocks (the game suite pins the same
	// per-step shape on the executor session — data-model.md §6).
	hist := listAgentV2Messages(t, ctx, sutHostURL, sutEnvName, sessionName)
	messages := hist.GetMessages()
	if len(messages) != 4 {
		t.Fatalf("history messages = %d, want 4 (user + one assistant message per step)", len(messages))
	}
	if messages[0].GetRole() != game.Role_ROLE_USER {
		t.Errorf("history[0] role = %s, want USER", messages[0].GetRole())
	}
	wantBlocks := []struct {
		role    game.Role
		name    string
		tool    bool
		summary string
	}{
		{role: game.Role_ROLE_AGENT, name: "saolei_init", tool: true},
		{role: game.Role_ROLE_AGENT, name: "saolei_operate", tool: true},
		{role: game.Role_ROLE_AGENT, summary: agentV2WonSummaryText},
	}
	for i, want := range wantBlocks {
		m := messages[i+1]
		if m.GetRole() != want.role {
			t.Errorf("history[%d] role = %s, want %s", i+1, m.GetRole(), want.role)
		}
		if m.GetInterrupted() {
			t.Errorf("history[%d] interrupted = true, want false (the chain completed)", i+1)
		}
		blocks := m.GetBlocks()
		if len(blocks) != 1 {
			t.Errorf("history[%d] blocks = %d, want 1 (one block per step on this chain)", i+1, len(blocks))
			continue
		}
		if want.tool {
			call := blocks[0].GetToolCall()
			if call == nil {
				t.Errorf("history[%d] block = %T, want a tool-call block", i+1, blocks[0].GetKind())
				continue
			}
			if call.GetName() != want.name {
				t.Errorf("history[%d] tool name = %q, want %q", i+1, call.GetName(), want.name)
			}
			if call.GetStatus() != game.ToolStatus_TOOL_STATUS_SUCCEEDED {
				t.Errorf("history[%d] tool status = %v, want SUCCEEDED", i+1, call.GetStatus())
			}
		} else if got := agentV2MessageText(m); got != want.summary {
			t.Errorf("history[%d] text = %q, want %q", i+1, got, want.summary)
		}
	}
}

// TestAgentV2FailedTurnBackfillsInterruptedTail covers the failed-turn
// content fixation end to end (agent-api-changes.md §6): the partial-content
// failure template (agent-v2-fail-mid) streams think+text and only then
// response.failed, so the turn ends turn_end{ERROR} with the produced
// blocks on the stream and ListAgentMessages backfills the same content as
// the tail assistant message with interrupted=true — the FR-005 signal that
// keeps failed turns unfolded.
func TestAgentV2FailedTurnBackfillsInterruptedTail(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx, sessionName := agentV2Prep(t, sutHostURL, sutEnvName)

	stream := startAgentV2Send(t, ctx, sutHostURL, sutEnvName, sessionName,
		agentV2TriggerFailMid+" produce content, then break")
	defer stream.Close()
	events := drainAgentV2Turn(t, stream)
	assertAgentV2TurnWellFormed(t, sessionName, events)

	// The produced content reached the stream before the failure.
	term := agentV2TerminalBlocksFromEvents(events)
	if term.think != agentV2FailMidThink {
		t.Errorf("streamed think = %q, want %q", term.think, agentV2FailMidThink)
	}
	if term.text != agentV2FailMidText {
		t.Errorf("streamed text = %q, want %q", term.text, agentV2FailMidText)
	}
	end := events[len(events)-1].GetTurnEnd()
	if end.GetStatus() != game.TurnStatus_TURN_STATUS_ERROR {
		t.Fatalf("turn ended %v, want ERROR (the injected provider failure)", end.GetStatus())
	}
	if end.GetError() == nil || end.GetError().GetMessage() == "" {
		t.Errorf("turn_end.error = %+v, want a structured error payload", end.GetError())
	}

	// Backfill: user + the interrupted assistant tail, content equal to the
	// streamed prefix and interrupted=true (data-model.md §1.5).
	hist := listAgentV2Messages(t, ctx, sutHostURL, sutEnvName, sessionName)
	messages := hist.GetMessages()
	if len(messages) != 2 {
		t.Fatalf("history messages = %d, want 2 (user turn + interrupted assistant tail)", len(messages))
	}
	if messages[1].GetRole() != game.Role_ROLE_AGENT {
		t.Errorf("history[1] role = %s, want AGENT", messages[1].GetRole())
	}
	if !messages[1].GetInterrupted() {
		t.Errorf("history[1].interrupted = false, want true (the step never settled — FR-005)")
	}
	if got := agentV2MessageThink(messages[1]); got != term.think {
		t.Errorf("history[1] think = %q, want the streamed prefix %q", got, term.think)
	}
	if got := agentV2MessageText(messages[1]); got != term.text {
		t.Errorf("history[1] text = %q, want the streamed prefix %q", got, term.text)
	}
}

// TestAgentV2CancelTerminatesRunningTurn covers the :cancel in-flight half
// (agent-api-changes.md §3): canceling a running turn answers 200 and ends
// the turn's stream with turn_end{TURN_STATUS_CANCELED}, and the session
// accepts a new Send immediately (no cooldown — the CANCELED settlement
// keeps the entry live, data-model.md §1.3).
func TestAgentV2CancelTerminatesRunningTurn(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx, sessionName := agentV2Prep(t, sutHostURL, sutEnvName)

	// Confirm the turn is running before canceling (the slow template's 3s
	// inter-chunk window — testdata/agent_v2.yaml agent-v2-slow — is the
	// controllable cancellation target).
	stream := startAgentV2Send(t, ctx, sutHostURL, sutEnvName, sessionName,
		agentV2TriggerSlow+" to be canceled")
	first := nextAgentV2Event(t, stream.Scanner)
	if first.GetTurnStart() == nil {
		stream.Close()
		t.Fatalf("first frame payload = %T, want turn_start", first.GetPayload())
	}

	if status, body := postAgentV2Cancel(t, ctx, sutHostURL, sutEnvName, sessionName); status != http.StatusOK {
		t.Fatalf("cancel status = %d (body: %s), want 200", status, body)
	}

	ch := drainAgentV2TurnAsync(stream)
	var events []*game.ChatEvent
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("canceled stream: %v", r.err)
		}
		events = r.events
	case <-time.After(wsReadTimeout):
		t.Fatal("canceled turn did not settle within the read window")
	}
	full := append([]*game.ChatEvent{first}, events...)
	assertAgentV2TurnWellFormed(t, sessionName, full)
	if last := events[len(events)-1].GetTurnEnd(); last.GetStatus() != game.TurnStatus_TURN_STATUS_CANCELED {
		t.Fatalf("canceled turn ended %v, want CANCELED", last.GetStatus())
	}

	// 后置: a follow-up Send starts and completes right away.
	stream2 := startAgentV2Send(t, ctx, sutHostURL, sutEnvName, sessionName,
		agentV2TriggerPlain+" right after the cancel")
	defer stream2.Close()
	events2 := drainAgentV2Turn(t, stream2)
	assertAgentV2TurnWellFormed(t, sessionName, events2)
	if events2[len(events2)-1].GetTurnEnd().GetStatus() != game.TurnStatus_TURN_STATUS_COMPLETED {
		t.Fatalf("post-cancel turn ended %v, want COMPLETED", events2[len(events2)-1].GetTurnEnd().GetStatus())
	}
}

// TestAgentV2CancelLandsQueuedMessages covers the :cancel queue half
// (agent-api-changes.md §3): every queued stream receives
// turn_end{TURN_STATUS_CANCELED} and closes, and the queued messages stay
// in the history as user messages without triggering a turn (the user
// ruling that departs from the official client's kept queue — research.md
// D2, data-model.md §3).
func TestAgentV2CancelLandsQueuedMessages(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx, sessionName := agentV2Prep(t, sutHostURL, sutEnvName)

	// Turn A runs; turn B queues behind it.
	streamA := startAgentV2Send(t, ctx, sutHostURL, sutEnvName, sessionName,
		agentV2TriggerSlow+" running when the cancel fires")
	firstA := nextAgentV2Event(t, streamA.Scanner)
	if firstA.GetTurnStart() == nil {
		streamA.Close()
		t.Fatalf("turn A first frame payload = %T, want turn_start", firstA.GetPayload())
	}
	streamB := startAgentV2Send(t, ctx, sutHostURL, sutEnvName, sessionName,
		agentV2TriggerPlain+" queued when the cancel fires")
	firstB := nextAgentV2Event(t, streamB.Scanner)
	if firstB.GetQueued() == nil {
		streamA.Close()
		streamB.Close()
		t.Fatalf("turn B first frame payload = %T, want queued{position}", firstB.GetPayload())
	}

	if status, body := postAgentV2Cancel(t, ctx, sutHostURL, sutEnvName, sessionName); status != http.StatusOK {
		t.Fatalf("cancel status = %d (body: %s), want 200", status, body)
	}

	chA := drainAgentV2TurnAsync(streamA)
	chB := drainAgentV2TurnAsync(streamB)
	var eventsA, eventsB []*game.ChatEvent
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
			t.Fatal("the canceled turns did not both settle within the read window")
		}
	}
	fullA := append([]*game.ChatEvent{firstA}, eventsA...)
	assertAgentV2TurnWellFormed(t, sessionName, fullA)
	if eventsA[len(eventsA)-1].GetTurnEnd().GetStatus() != game.TurnStatus_TURN_STATUS_CANCELED {
		t.Fatalf("running turn A ended %v, want CANCELED", eventsA[len(eventsA)-1].GetTurnEnd().GetStatus())
	}
	// The queued stream is exactly queued{position} → turn_end{CANCELED}:
	// the cancel closes it without a turn_start — the stream-level proof
	// that the queued message never triggered a turn.
	if len(eventsB) != 1 || eventsB[0].GetTurnEnd().GetStatus() != game.TurnStatus_TURN_STATUS_CANCELED {
		t.Fatalf("queued stream frames after the queued frame = %v, want exactly one turn_end{CANCELED}", eventsB)
	}

	// The queued message stays in the history as a user message (enqueue
	// appended it; the cancel cleared the queue without running a turn).
	queuedText := agentV2TriggerPlain + " queued when the cancel fires"
	hist := listAgentV2Messages(t, ctx, sutHostURL, sutEnvName, sessionName)
	landed := false
	for _, m := range hist.GetMessages() {
		if m.GetRole() == game.Role_ROLE_USER && agentV2MessageText(m) == queuedText {
			landed = true
			break
		}
	}
	if !landed {
		t.Errorf("queued message %q never landed as a history user message", queuedText)
	}

	// The session stays live after draining the queue: a follow-up Send
	// starts and completes (agent-api-changes.md §3 后置).
	stream2 := startAgentV2Send(t, ctx, sutHostURL, sutEnvName, sessionName,
		agentV2TriggerPlain+" right after the cancel")
	defer stream2.Close()
	events2 := drainAgentV2Turn(t, stream2)
	assertAgentV2TurnWellFormed(t, sessionName, events2)
	if events2[len(events2)-1].GetTurnEnd().GetStatus() != game.TurnStatus_TURN_STATUS_COMPLETED {
		t.Fatalf("post-cancel turn ended %v, want COMPLETED", events2[len(events2)-1].GetTurnEnd().GetStatus())
	}
}

// TestAgentV2CancelNoopAndPrecondition covers the :cancel edge semantics
// (agent-api-changes.md §3), layered like Send's rejection family: a never-
// materialized session has no owner and answers 404 NOT_FOUND (the proxy's
// routing layer — no agent to cancel), an owner-without-agent session
// reaches agent_v2 and answers 400 FAILED_PRECONDITION, and an idle
// materialized agent answers 200 as a no-op (idempotent — repeating it
// stays 200).
func TestAgentV2CancelNoopAndPrecondition(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx, sessionName := agentV2Prep(t, sutHostURL, sutEnvName)

	// owner-without-agent: the fail-fast unknown-model UpdateAgent
	// allocates the owner, then rejects without materializing (US2 场景 7).
	unmatName := ensureAgentV2Session(t, sutHostURL, sutEnvName, "cancel-unmat-"+uniqueSuffix())
	unmatPreset := createAgentV2Preset(t, ctx, sutHostURL, sutEnvName, "cancel-unmat-"+uniqueSuffix(), "cancel precondition")
	updateAgentV2AgentWithStatus(t, ctx, sutHostURL, sutEnvName, unmatName, unmatPreset.GetName(), "no-such-model")

	tests := []struct {
		name    string
		session string
		want    int
	}{
		{name: "idle materialized agent", session: sessionName, want: http.StatusOK},
		{name: "never-materialized session has no owner", session: "templates/" + saoleiTemplateID + "/sessions/ghost-" + uniqueSuffix(), want: http.StatusNotFound},
		{name: "owner without a materialized agent", session: unmatName, want: http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := postAgentV2Cancel(t, ctx, sutHostURL, sutEnvName, tt.session)
			if status != tt.want {
				t.Fatalf("cancel status = %d (body: %s), want %d", status, body, tt.want)
			}
		})
	}

	// Idempotence: the second no-op cancel on the idle session succeeds.
	if status, body := postAgentV2Cancel(t, ctx, sutHostURL, sutEnvName, sessionName); status != http.StatusOK {
		t.Errorf("second cancel status = %d (body: %s), want 200 (idempotent no-op)", status, body)
	}
}

// TestAgentV2GetAgentDesktopConnected covers the GetAgent connection fact
// (agent-api-changes.md §4): a materialized session with no flow
// connection reports desktop_connected=false, and attaching a flow
// connection flips it to true. The "connected" half attaches the test's own
// flow to the executor session (the won topology's fake-desktop binding) —
// the read is pinned to the test's own live connection, not to the
// executor's boot timing.
func TestAgentV2GetAgentDesktopConnected(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)
	preset := createAgentV2Preset(t, ctx, sutHostURL, sutEnvName, "conv-conn-"+uniqueSuffix(), "connection status")

	// 无连接: a materialized session that never saw a flow connection.
	lonelyName := ensureAgentV2Session(t, sutHostURL, sutEnvName, "conv-conn-"+uniqueSuffix())
	updateAgentV2Agent(t, ctx, sutHostURL, sutEnvName, lonelyName, preset.GetName(), "")
	if lonely := getAgentV2Agent(t, ctx, sutHostURL, sutEnvName, lonelyName); lonely.GetDesktopConnected() {
		t.Errorf("desktop_connected = true with no flow connection, want false")
	}

	// 有连接: attach the test's flow to the executor session and read true.
	wonName := ensureAgentV2Session(t, sutHostURL, sutEnvName, agentV2DesktopWonSessionID)
	updateAgentV2Agent(t, ctx, sutHostURL, sutEnvName, wonName, preset.GetName(), "")
	flow, _ := dialAgentV2FlowProbed(t, ctx, sutHostURL, sutEnvName, agentV2DesktopWonSessionID)
	defer flow.Close()
	connected := getAgentV2Agent(t, ctx, sutHostURL, sutEnvName, wonName)
	if !connected.GetDesktopConnected() {
		t.Errorf("desktop_connected = false with a live flow connection attached, want true")
	}
}
