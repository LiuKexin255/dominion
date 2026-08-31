// Package testplan contains the shared agent_v2 conversation helpers used by
// the agent_v2 conversation and web hosting large-test files. Kept separate
// from helpers_test.go (the /api/v1 + WebSocket helper set) so only the
// suites that drive /api/v2 pay its dependency closure — the same selective
// inclusion pattern as saolei_fixtures_test.go.
package testplan

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"dominion/common/gopkg/otel/tracecontext"
	game "dominion/projects/game"
	gamev2 "dominion/projects/game/v2"

	"google.golang.org/protobuf/encoding/protojson"
)

// ─── agent_v2 conversation helpers (the /api/v2 NDJSON surface) ─────────────
//
// The AgentService surface (specs/051-agent-v2-dsh-migration/contracts/
// agent-api.md) is exposed by the gateway under /api/v2: Send is a
// chunked NDJSON stream, ListAgentMessages is unary JSON. Shared by the
// agent_v2 conversation module tests and the web hosting smoke — shared
// helpers live here, never copied per test file (style/large_test.md
// §反模式3).

// agentV2PathPrefix is the /api/v2 gateway route prefix the AgentService
// handler is bound to (agent-api.md §1).
const agentV2PathPrefix = "/api/v2/"

// User-message keyword triggers of projects/game/fake-llm/service/testdata/
// agent_v2.yaml — every /v1/responses template is matched by ONE of these
// case-insensitive substrings of the last user message (responses.go
// matchResponses). Tests must keep each trigger out of unrelated turns' texts:
// no message may carry two triggers (the alphabetical lowest-name template
// would win) and none may contain the followup history keyword below.
const (
	agentV2TriggerThink = "agent-v2-think"
	agentV2TriggerPlain = "agent-v2-plain"
	agentV2TriggerSlow  = "agent-v2-slow"
	agentV2TriggerFail  = "agent-v2-fail"
)

// Expected /v1/responses contents pinned from testdata/agent_v2.yaml. MUST be
// kept in sync with that file (the same lockstep rule as the chat-completions
// constants in helpers_test.go — testplan/README.md §5): the reasoning
// pieces stream as separate THINK deltas (fake-responses-wire.md §2), the
// text arrives as a single TEXT delta.
const (
	agentV2GreetThink1  = "Analyzing the user's request."
	agentV2GreetThink2  = "Drafting a friendly reply."
	agentV2GreetText    = "Hello! I can help you play and manage your game sessions."
	agentV2FollowupText = "As I said when we started, I help you play and manage your game sessions."
	agentV2PlainText    = "Plain answer with no thinking this time."
	agentV2SlowThink1   = "Thinking slowly."
	agentV2SlowThink2   = "Still thinking."
	agentV2SlowText     = "Finally done thinking."
)

// agentV2SessionName builds the full game session resource name the
// AgentService session field carries
// (templates/{template}/sessions/{session}, agent-api.md §1).
func agentV2SessionName(sessionID string) string {
	return game.SessionName{TemplateID: saoleiTemplateID, SessionID: sessionID}.String()
}

// agentV2EventStream is an open Send stream: the NDJSON frame scanner plus
// the response handle it wraps (Close terminates the HTTP stream).
type agentV2EventStream struct {
	resp    *http.Response
	Scanner *bufio.Scanner
}

// Close releases the stream's HTTP body.
func (s *agentV2EventStream) Close() { s.resp.Body.Close() }

// startAgentV2Send issues POST /api/v2/{session}:send (conversation-api.md
// §2) and returns the open NDJSON event stream. The body is left
// unconsumed: the caller reads ChatEvents via nextAgentV2Event (test
// goroutine) or nextAgentV2EventNoFatal (reader goroutines) and Closes the
// stream when done. Only the HTTP status is checked here — a stream that
// never opens is a fatal request-level failure.
func startAgentV2Send(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, sessionName, text string) *agentV2EventStream {
	t.Helper()

	body, err := json.Marshal(struct {
		Text string `json:"text"`
	}{Text: text})
	if err != nil {
		t.Fatalf("marshal SendRequest: %v", err)
	}

	reqURL := fmt.Sprintf("%s%s%s:send", sutHostURL, agentV2PathPrefix, sessionName)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("http.NewRequestWithContext %s: %v", reqURL, err)
	}
	req.Header.Set(headerEnv, sutEnvName)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Transport: tracecontext.NewHTTPTransport(http.DefaultTransport)}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST %s:send: %v", sessionName, err)
	}
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("POST %s:send status=%d, body=%s", sessionName, resp.StatusCode, respBody)
	}

	scanner := bufio.NewScanner(resp.Body)
	// Frame bodies are tiny; the oversized buffer just rules out
	// bufio.ErrTooLong on pathological content.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	return &agentV2EventStream{resp: resp, Scanner: scanner}
}

// postAgentV2SendStatus is startAgentV2Send for request-level failures
// (stream never opens): it consumes the whole response and returns the HTTP
// status with the raw body — used to assert the 400 INVALID_ARGUMENT mapping
// (conversation-api.md §2.1).
func postAgentV2SendStatus(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, sessionName, text string) (int, []byte) {
	t.Helper()

	body, err := json.Marshal(struct {
		Text string `json:"text"`
	}{Text: text})
	if err != nil {
		t.Fatalf("marshal SendRequest: %v", err)
	}
	reqURL := fmt.Sprintf("%s%s%s:send", sutHostURL, agentV2PathPrefix, sessionName)
	resp, respBody := doHTTPTrace(t, ctx, http.MethodPost, reqURL, sutEnvName, body)
	return resp.StatusCode, respBody
}

// nextAgentV2Event reads one NDJSON frame from a Send stream and decodes it
// into a ChatEvent. grpc-gateway v2 streams every message wrapped in a
// "result" key — one `{"result": <ChatEvent>} JSON object per "\n"-terminated
// line (the default streaming marshaler, grpc-gateway runtime/handler.go
// handleForwardResponseServerStream at the repo-pinned v2.27.6; conversation-
// api.md §2) — so the wrapper is mandatory and unwrapped here. Calls t.Fatal
// on transport, framing, or decode errors; reader goroutines must use
// nextAgentV2EventNoFatal instead.
func nextAgentV2Event(t *testing.T, scanner *bufio.Scanner) *gamev2.ChatEvent {
	t.Helper()

	if !scanner.Scan() {
		t.Fatalf("read send stream: %v", scanner.Err())
	}
	return decodeAgentV2Chunk(t, scanner.Bytes())
}

// nextAgentV2EventNoFatal is nextAgentV2Event without t.Fatal: it returns the
// decoded ChatEvent or an error, for drain goroutines (t.Fatal must only run
// on the test goroutine).
func nextAgentV2EventNoFatal(scanner *bufio.Scanner) (*gamev2.ChatEvent, error) {
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return nil, err
		}
		return nil, io.EOF
	}
	return decodeAgentV2ChunkNoFatal(scanner.Bytes())
}

// decodeAgentV2Chunk unwraps and decodes one NDJSON chunk on the test
// goroutine (fatal variant of decodeAgentV2ChunkNoFatal).
func decodeAgentV2Chunk(t *testing.T, line []byte) *gamev2.ChatEvent {
	t.Helper()

	evt, err := decodeAgentV2ChunkNoFatal(line)
	if err != nil {
		t.Fatalf("decode stream chunk %s: %v", line, err)
	}
	return evt
}

// decodeAgentV2ChunkNoFatal decodes one `{"result": <ChatEvent>}` NDJSON
// line into a ChatEvent (protojson camelCase projection, unknown fields
// ignored per proto3 forward-compat — conversation-api.md §2).
func decodeAgentV2ChunkNoFatal(line []byte) (*gamev2.ChatEvent, error) {
	var chunk struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(line, &chunk); err != nil {
		return nil, err
	}
	if len(chunk.Result) == 0 {
		return nil, fmt.Errorf("chunk lacks the grpc-gateway %q wrapper", "result")
	}
	evt := new(gamev2.ChatEvent)
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(chunk.Result, evt); err != nil {
		return nil, err
	}
	return evt, nil
}

// drainAgentV2Turn reads frames until the turn's turn_end (inclusive) and
// asserts the stream ends there — no frames may follow the terminal event
// (conversation-api.md §3 invariant 1). Test-goroutine only.
func drainAgentV2Turn(t *testing.T, stream *agentV2EventStream) []*gamev2.ChatEvent {
	t.Helper()

	var events []*gamev2.ChatEvent
	for {
		evt := nextAgentV2Event(t, stream.Scanner)
		events = append(events, evt)
		if evt.GetTurnEnd() != nil {
			break
		}
	}
	if stream.Scanner.Scan() {
		t.Fatalf("frames after turn_end: %s", stream.Scanner.Text())
	}
	if err := stream.Scanner.Err(); err != nil {
		t.Fatalf("drain send stream after turn_end: %v", err)
	}
	return events
}

// assertAgentV2TurnWellFormed checks the event-order invariants every turn
// stream must satisfy regardless of content (conversation-api.md §3):
// exactly one turn_end and it is the last frame (1); every event carries the
// requested session resource name and one shared non-empty turn_id (§1);
// queued, when present, is the first frame and precedes turn_start (2/7);
// turn_start precedes all block events (2); each block_start occurs exactly
// once, every delta is announced by its block_start, deltas of one index form
// a contiguous run (blocks never interleave — 3), each block_end arrives at
// most once per index and its block content equals the delta concatenation
// (4); zero-tool stage — no TOOL_CALL block may appear (FR-006, §3 invariant
// 5). A provider block that never receives a done event (the fake's
// reasoning item — fake-responses-wire.md §2) simply has no block_end and is
// not an interleaving violation.
func assertAgentV2TurnWellFormed(t *testing.T, sessionName string, events []*gamev2.ChatEvent) {
	t.Helper()

	if len(events) == 0 {
		t.Fatal("empty event stream")
	}
	if last := events[len(events)-1]; last.GetTurnEnd() == nil {
		t.Fatalf("last frame is not turn_end (payload = %T)", last.GetPayload())
	}
	turnEnds := 0
	for _, e := range events {
		if e.GetTurnEnd() != nil {
			turnEnds++
		}
	}
	if turnEnds != 1 {
		t.Fatalf("turn_end frame count = %d, want exactly 1", turnEnds)
	}

	turnID := events[0].GetTurnId()
	if turnID == "" {
		t.Fatal("first frame carries an empty turn_id")
	}
	for i, e := range events {
		if e.GetSession() != sessionName {
			t.Errorf("frame %d session = %q, want %q", i, e.GetSession(), sessionName)
		}
		if e.GetTurnId() != turnID {
			t.Errorf("frame %d turn_id = %q, want constant %q across the turn", i, e.GetTurnId(), turnID)
		}
	}

	sawTurnStart := false
	announced := map[int32]bool{}
	deltas := map[int32][]string{}
	closedRuns := map[int32]bool{}
	lastDeltaIdx := int32(-1)
	blockEnds := map[int32]*gamev2.ContentBlock{}
	for i, e := range events {
		switch {
		case e.GetQueued() != nil:
			if i != 0 {
				t.Errorf("queued at frame %d is not the first frame (§3 invariant 2/7)", i)
			}
			if pos := e.GetQueued().GetPosition(); pos < 1 {
				t.Errorf("queued position = %d, want >= 1 (1-based queue slot)", pos)
			}
		case e.GetTurnStart() != nil:
			if sawTurnStart {
				t.Errorf("duplicate turn_start at frame %d", i)
			}
			sawTurnStart = true
		case e.GetBlockStart() != nil:
			if !sawTurnStart {
				t.Errorf("block_start at frame %d precedes turn_start (§3 invariant 2)", i)
			}
			start := e.GetBlockStart()
			if announced[start.GetIndex()] {
				t.Errorf("duplicate block_start for index %d", start.GetIndex())
			}
			announced[start.GetIndex()] = true
			if start.GetType() == gamev2.BlockType_BLOCK_TYPE_TOOL_CALL {
				t.Errorf("TOOL_CALL block_start at frame %d — agent_v2 is zero-tool this stage (FR-006)", i)
			}
		case e.GetDelta() != nil:
			if !sawTurnStart {
				t.Errorf("delta at frame %d precedes turn_start (§3 invariant 2)", i)
			}
			idx := e.GetDelta().GetIndex()
			if !announced[idx] {
				t.Errorf("delta at frame %d carries index %d without a block_start (§3 invariant 3)", i, idx)
				continue
			}
			if lastDeltaIdx != -1 && lastDeltaIdx != idx {
				closedRuns[lastDeltaIdx] = true
			}
			if closedRuns[idx] {
				t.Errorf("delta at frame %d reopens index %d — block deltas interleave (§3 invariant 3)", i, idx)
			}
			deltas[idx] = append(deltas[idx], e.GetDelta().GetText())
			lastDeltaIdx = idx
		case e.GetBlockEnd() != nil:
			idx := e.GetBlockEnd().GetIndex()
			if !announced[idx] {
				t.Errorf("block_end at frame %d carries index %d without a block_start (§3 invariant 3)", i, idx)
				continue
			}
			if _, dup := blockEnds[idx]; dup {
				t.Errorf("duplicate block_end for index %d", idx)
				continue
			}
			blockEnds[idx] = e.GetBlockEnd().GetBlock()
		case e.GetTurnEnd() != nil:
			// Position/count already asserted above.
		default:
			t.Errorf("frame %d carries an unknown payload (proto3 oneof branch %T)", i, e.GetPayload())
		}
	}
	if !sawTurnStart {
		t.Error("no turn_start frame in the stream (§3 invariant 2)")
	}

	// §3 invariant 4: for every block that DID terminate, the delta
	// concatenation equals the terminal block content.
	for idx, block := range blockEnds {
		joined := strings.Join(deltas[idx], "")
		switch {
		case block.GetText() != nil:
			if got := block.GetText().GetContent(); got != joined {
				t.Errorf("block %d: block_end text = %q, want delta concatenation %q", idx, got, joined)
			}
		case block.GetThink() != nil:
			if got := block.GetThink().GetContent(); got != joined {
				t.Errorf("block %d: block_end think = %q, want delta concatenation %q", idx, got, joined)
			}
		}
	}
}

// agentV2TerminalBlocks folds a completed turn's stream events into the
// per-type terminal block contents: each block's deltas joined in arrival
// order (conversation-api.md §3 invariant 4 — the delta concatenation IS the
// block content). THINK deltas are attributed via their block_start type;
// a THINK block whose provider wire never sends a done event (the fake's
// reasoning item emits no output_item.done — fake-responses-wire.md §2)
// still contributes its deltas. This is the expected history backfill of
// the same turn (FR-014 consistency).
type agentV2TerminalBlocks struct {
	think string
	text  string
}

func agentV2TerminalBlocksFromEvents(events []*gamev2.ChatEvent) agentV2TerminalBlocks {
	var out agentV2TerminalBlocks
	kindByIndex := map[int32]gamev2.BlockType{}
	for _, e := range events {
		if start := e.GetBlockStart(); start != nil {
			kindByIndex[start.GetIndex()] = start.GetType()
			continue
		}
		delta := e.GetDelta()
		if delta == nil {
			continue
		}
		switch kindByIndex[delta.GetIndex()] {
		case gamev2.BlockType_BLOCK_TYPE_THINK:
			out.think += delta.GetText()
		case gamev2.BlockType_BLOCK_TYPE_TEXT:
			out.text += delta.GetText()
		}
	}
	return out
}

// agentV2MessageThink returns the concatenated ThinkBlock contents of one
// history message; agentV2MessageText the TextBlock contents.
func agentV2MessageThink(m *gamev2.HistoryMessage) string {
	var s string
	for _, b := range m.GetBlocks() {
		if think := b.GetThink(); think != nil {
			s += think.GetContent()
		}
	}
	return s
}

func agentV2MessageText(m *gamev2.HistoryMessage) string {
	var s string
	for _, b := range m.GetBlocks() {
		if text := b.GetText(); text != nil {
			s += text.GetContent()
		}
	}
	return s
}

// listAgentV2Messages issues GET /api/v2/{session}/agent/messages and
// returns the parsed ListAgentMessagesResponse (agent-api.md §1). Calls
// t.Fatal on non-200 responses.
func listAgentV2Messages(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, sessionName string) *gamev2.ListAgentMessagesResponse {
	t.Helper()

	reqURL := fmt.Sprintf("%s%s%s/agent/messages", sutHostURL, agentV2PathPrefix, sessionName)
	resp, respBody := doHTTPTrace(t, ctx, http.MethodGet, reqURL, sutEnvName, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET agent messages status=%d, body=%s", resp.StatusCode, respBody)
	}
	messages := new(gamev2.ListAgentMessagesResponse)
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(respBody, messages); err != nil {
		t.Fatalf("Unmarshal ListAgentMessagesResponse: %v (raw: %s)", err, respBody)
	}
	return messages
}
