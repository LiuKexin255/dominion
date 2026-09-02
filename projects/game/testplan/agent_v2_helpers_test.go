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
	"net/url"
	"strings"
	"testing"
	"time"

	"dominion/common/gopkg/otel/tracecontext"
	game "dominion/projects/game"
	gamev2 "dominion/projects/game/v2"

	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// ─── agent_v2 conversation helpers (the /api/v2 NDJSON surface) ─────────────
//
// The AgentService surface (specs/051-agent-v2-dsh-migration/contracts/
// agent-api.md) is exposed by the gateway under /api/v2: Send is a
// chunked NDJSON stream, ListAgentMessages is unary JSON. Shared by the
// agent_v2 conversation module tests and the web hosting smoke — shared
// helpers live here, never copied per test file (style/large_test.md
// §反模式3).
//
// Every helper here dials straight into the surface with no startup
// probing: the deployment's startup probe (the /healthz:38080 readiness
// contract, specs/052-deploy-health-probe/contracts/deploy-probe.md) keeps
// the environment from turning ready before the agent_v2 instances can
// serve, and guitar holds postDeploySettle (60s) after a successful apply
// before running any case (tools/test/guitar/pkg/run/run.go) — so the
// proxy's instance discovery has settled by the time the first request
// fires.

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
// stream must satisfy regardless of content (conversation-api.md §3, plus
// the 051 tool extension of specs/051-agent-v2-dsh-migration/data-model.md
// §2.4): exactly one turn_end and it is the last frame (1); every event
// carries the requested session resource name and one shared non-empty
// turn_id (§1); queued, when present, is the first frame and precedes
// turn_start (2/7); turn_start precedes all block events (2); each
// block_start occurs exactly once, every delta is announced by its
// block_start, deltas of one index form a contiguous run (blocks never
// interleave — 3), each block_end arrives at most once per index and its
// block content equals the delta concatenation (4); every tool_result frame
// carries a non-empty tool_id and a terminal status, and settles the most
// recent unsettled tool-call block with that id — the tool identity first
// surfaces on the closing block (the dsh block-start chunk carries no id;
// the web store correlates from block_end too, chat.ts blockEndTerminal) —
// so a COMPLETED turn ends with no tool call
// left unsettled. A provider block that never receives a done event (the
// fake's reasoning item — fake-responses-wire.md §2) simply has no block_end
// and is not an interleaving violation.
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
	kindByIndex := map[int32]gamev2.BlockType{}
	deltas := map[int32][]string{}
	closedRuns := map[int32]bool{}
	lastDeltaIdx := int32(-1)
	blockEnds := map[int32]*gamev2.ContentBlock{}
	// tool pairing bookkeeping: the tool-call blocks seen (by tool_id, most
	// recent unsettled last) and the tool_result frames observed.
	unsettledToolCalls := []string{}
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
			kindByIndex[start.GetIndex()] = start.GetType()
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
			// The tool identity first surfaces on the closing block (the dsh
			// block-start chunk carries no id): register the pending call
			// from there, so the tool_result pairing below has its key.
			if call := e.GetBlockEnd().GetBlock().GetToolCall(); call != nil {
				if call.GetToolId() == "" || call.GetName() == "" {
					t.Errorf("block_end at frame %d carries tool_id %q name %q, want both non-empty (data-model.md §2.3)", i, call.GetToolId(), call.GetName())
				}
				unsettledToolCalls = append(unsettledToolCalls, call.GetToolId())
			}
		case e.GetToolResult() != nil:
			result := e.GetToolResult()
			if result.GetToolId() == "" {
				t.Errorf("tool_result at frame %d carries an empty tool_id (§2.4)", i)
				continue
			}
			if result.GetStatus() != gamev2.ToolStatus_TOOL_STATUS_SUCCEEDED && result.GetStatus() != gamev2.ToolStatus_TOOL_STATUS_FAILED {
				t.Errorf("tool_result at frame %d status = %v, want a terminal SUCCEEDED/FAILED", i, result.GetStatus())
			}
			// Settle the most recent unsettled call with this id (the same
			// newest-match rule the history applies, data-model.md §2.3).
			for j := len(unsettledToolCalls) - 1; j >= 0; j-- {
				if unsettledToolCalls[j] == result.GetToolId() {
					unsettledToolCalls = append(unsettledToolCalls[:j], unsettledToolCalls[j+1:]...)
					break
				}
			}
		case e.GetTurnEnd() != nil:
			// Position/count already asserted above.
		default:
			t.Errorf("frame %d carries an unknown payload (proto3 oneof branch %T)", i, e.GetPayload())
		}
	}
	if !sawTurnStart {
		t.Error("no turn_start frame in the stream (§3 invariant 2)")
	}
	if events[len(events)-1].GetTurnEnd().GetStatus() == gamev2.TurnStatus_TURN_STATUS_COMPLETED && len(unsettledToolCalls) > 0 {
		t.Errorf("COMPLETED turn ends with %d tool call(s) without a tool_result: %v (data-model.md §4-2)", len(unsettledToolCalls), unsettledToolCalls)
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
		case block.GetToolCall() != nil:
			call := block.GetToolCall()
			if call.GetStatus() != gamev2.ToolStatus_TOOL_STATUS_RUNNING {
				t.Errorf("block %d: block_end tool_call status = %v, want RUNNING (the terminal status arrives via tool_result, data-model.md §2.3)", idx, call.GetStatus())
			}
			if got := call.GetArgsJson(); got != joined {
				t.Errorf("block %d: block_end args_json = %q, want delta concatenation %q", idx, got, joined)
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

// listAgentV2MessagesWithStatus is listAgentV2Messages without the 200
// fatality: it returns the HTTP status with the parsed response — used to
// assert the 404 NOT_FOUND of a never-materialized agent (agent-api.md §2.3:
// no owner → the read paths answer NOT_FOUND, specs/051-agent-v2-dsh-
// migration/contracts/agent-api.md §2.2).
func listAgentV2MessagesWithStatus(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, sessionName string) (int, *gamev2.ListAgentMessagesResponse) {
	t.Helper()

	reqURL := fmt.Sprintf("%s%s%s/agent/messages", sutHostURL, agentV2PathPrefix, sessionName)
	resp, respBody := doHTTPTrace(t, ctx, http.MethodGet, reqURL, sutEnvName, nil)
	messages := new(gamev2.ListAgentMessagesResponse)
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(respBody, messages); err != nil {
		t.Logf("ListAgentMessagesResponse body (status %d) is not a response proto: %s", resp.StatusCode, respBody)
	}
	return resp.StatusCode, messages
}

// ─── agent_v2 game helpers (preset / materialization / flow WS) ─────────────
//
// The US1 game suites drive the full loop: preset creation → UpdateAgent
// materialization → a fake-llm game chain over the /api/v2 Send stream, with
// the flow half observed either through the deployed fake-desktop executors
// or through a test-created /api/v2 WebSocket connection
// (specs/051-agent-v2-dsh-migration/contracts/desktop-bridge.md §1/§6).

// User-message keyword triggers of projects/game/fake-llm/service/testdata/
// agent_v2_saolei.yaml. Same matching rules as the agentV2Trigger* constants
// above — keep each trigger out of every other turn's text.
const (
	agentV2TriggerSaoleiGame        = "开始一局扫雷"
	agentV2TriggerSaoleiProgressive = "progressive minesweeper"
	agentV2TriggerSaoleiNodesktop   = "minesweeper without desktop"
)

// Expected /v1/responses game-chain contents pinned from
// testdata/agent_v2_saolei_tools.yaml (the same lockstep rule as the
// agentV2* constants above). The chain texts are what the tool_result frames
// and the terminal TEXT deltas must carry per fake-desktop scenario
// (testdata/agent_v2_saolei_tools.yaml rule set).
const (
	// won chain (fake-desktop-won, the 9×9 win board): init sees the already
	// won board, the operate batch stops pre-dispatch on game_won, and the
	// terminal summary closes the chain.
	agentV2WonInitContains   = "new game started"
	agentV2WonBoardContains  = "board size 9*9"
	agentV2WonStatusContains = "game status: won"
	agentV2WonRejectContains = "stopped at click(0,0) (game_won)"
	agentV2WonSummaryText    = "本局扫雷已完成：全部雷区排除，游戏获胜。"
	// progressive chain (fake-desktop-drop before its fault fires): two cell
	// ops land on the board model and the batch reports playing.
	agentV2ProgInitContains   = "new game started"
	agentV2ProgBoardContains  = "board size 16*16"
	agentV2ProgStatusContains = "game status: playing"
	agentV2ProgExecContains   = "executed 2 ops"
	agentV2ProgSummaryText    = "已完成一轮扫雷操作：点击揭示与标记旗子均已执行，棋盘已刷新。"
	// desktop-absent / mid-game-disconnect chain: the bridge's FAILED receipt
	// becomes a tool ERROR result whose text names the cause.
	agentV2DisconnectedContain = "desktop disconnected"
	agentV2NodesktopSummary    = "桌面未连接，无法开局。请先连接桌面后再试。"
	agentV2DisconnectSummary   = "桌面连接中断，操作未能完成。请等待桌面重连后再试。"
)

// Fixed caller-id sessions the deployed fake-desktop executors bind
// (deploy_agent_v2.yaml FAKE_DESKTOP_SESSION). The suites create these
// sessions idempotently and address the executors through them.
const (
	agentV2DesktopWonSessionID = "desktop-e2e-won"
	agentV2DesktopDropID       = "desktop-e2e-drop"
)

// ensureAgentV2Session creates a caller-id session, tolerating an
// ALREADY_EXISTS (409) from a prior case in the same deployment — the
// session resource itself is what matters, not who created it. Returns the
// full /api/v2 session resource name.
func ensureAgentV2Session(t *testing.T, sutHostURL, sutEnvName, sessionID string) string {
	t.Helper()

	reqURL := fmt.Sprintf("%s%stemplates/%s/sessions?session_id=%s",
		sutHostURL, pathPrefix, saoleiTemplateID, url.QueryEscape(sessionID))
	resp, respBody := doHTTP(t, http.MethodPost, reqURL, sutEnvName, []byte("{}"))
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusConflict:
		// Already created by a sibling case — verify it is still readable.
		if status, _ := getSessionWithStatus(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID); status != http.StatusOK {
			t.Fatalf("session %s reported already exists but is not readable: status=%d body=%s", sessionID, status, respBody)
		}
	default:
		t.Fatalf("POST ensure session %s status=%d, body=%s", sessionID, resp.StatusCode, respBody)
	}
	return agentV2SessionName(sessionID)
}

// createAgentV2Preset creates a preset through the gateway
// (POST /api/v2/templates/saolei/presets?preset_id=...) and returns the
// created resource (agent-api.md §1 CreatePreset; the caller-supplied id
// rides the query string per the body:"preset" binding). Calls t.Fatal on
// non-200 responses.
func createAgentV2Preset(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, presetID, playerPrompt string) *gamev2.Preset {
	t.Helper()

	preset := &gamev2.Preset{PlayerPrompt: playerPrompt}
	body, err := protojson.Marshal(preset)
	if err != nil {
		t.Fatalf("protojson.Marshal Preset: %v", err)
	}
	reqURL := fmt.Sprintf("%s%stemplates/%s/presets?preset_id=%s",
		sutHostURL, agentV2PathPrefix, saoleiTemplateID, url.QueryEscape(presetID))
	resp, respBody := doHTTPTrace(t, ctx, http.MethodPost, reqURL, sutEnvName, body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST create preset status=%d, body=%s", resp.StatusCode, respBody)
	}
	created := new(gamev2.Preset)
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(respBody, created); err != nil {
		t.Fatalf("Unmarshal Preset: %v (raw: %s)", err, respBody)
	}
	return created
}

// updateAgentV2Agent materializes (or refreshes) the session's agent
// singleton through the gateway (PATCH /api/v2/.../agent, agent-api.md §2.1
// — allow_missing=true as the web always sends) and returns the
// materialized Agent. The body carries only the mutable fields: grpc-gateway
// derives the update_mask from the body's set fields, so a name here would
// produce an invalid `name` mask path (the identity rides the URL path).
// Calls t.Fatal on non-200 responses.
func updateAgentV2Agent(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, sessionName, presetName, model string) *gamev2.Agent {
	t.Helper()

	agent := &gamev2.Agent{Preset: presetName}
	if model != "" {
		agent.Model = model
	}
	body, err := protojson.Marshal(agent)
	if err != nil {
		t.Fatalf("protojson.Marshal Agent: %v", err)
	}
	reqURL := fmt.Sprintf("%s%s%s/agent?allow_missing=true", sutHostURL, agentV2PathPrefix, sessionName)
	resp, respBody := doHTTPTrace(t, ctx, http.MethodPatch, reqURL, sutEnvName, body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH update agent status=%d, body=%s", resp.StatusCode, respBody)
	}
	materialized := new(gamev2.Agent)
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(respBody, materialized); err != nil {
		t.Fatalf("Unmarshal Agent: %v (raw: %s)", err, respBody)
	}
	return materialized
}

// getAgentV2AgentWithStatus issues GET /api/v2/.../agent and returns the
// HTTP status with the raw body — the materialization probe (200 with the
// Agent, 404 while unmaterialized, agent-api.md §2.2).
func getAgentV2AgentWithStatus(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, sessionName string) (int, []byte) {
	t.Helper()

	reqURL := fmt.Sprintf("%s%s%s/agent", sutHostURL, agentV2PathPrefix, sessionName)
	resp, respBody := doHTTPTrace(t, ctx, http.MethodGet, reqURL, sutEnvName, nil)
	return resp.StatusCode, respBody
}

// connectAgentV2Flow dials the gateway's /api/v2 flow WebSocket for one
// session (desktop-bridge.md §3: the gateway binds the connection from the
// URL path and injects the identity into the first frame). The dial runs on
// ctx with the traceparent propagation of traceContext, so the flow traffic
// joins the test trace. Calls t.Fatal on dial failures.
func connectAgentV2Flow(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, sessionID string) *websocket.Conn {
	t.Helper()

	wsPath := fmt.Sprintf("/api/v2/templates/%s/sessions/%s/connect", saoleiTemplateID, sessionID)
	wsURL := buildWSURL(sutHostURL, wsPath)

	header := http.Header{}
	header.Set(headerEnv, sutEnvName)
	for _, env := range tracecontext.Environ(ctx) {
		if strings.HasPrefix(env, tracecontext.EnvKey+"=") {
			header.Set("traceparent", strings.TrimPrefix(env, tracecontext.EnvKey+"="))
		}
	}

	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, resp, err := dialer.DialContext(ctx, wsURL, header)
	if err != nil {
		t.Fatalf("websocket.Dial %s: %v", wsURL, err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		conn.Close()
		t.Fatalf("flow WS upgrade status = %d, want %d", resp.StatusCode, http.StatusSwitchingProtocols)
	}
	return conn
}

// sendFlowUserFrame writes one binary-proto UserFrame on the flow stream.
func sendFlowUserFrame(t *testing.T, conn *websocket.Conn, frame *game.UserFrame) {
	t.Helper()

	data, err := proto.Marshal(frame)
	if err != nil {
		t.Fatalf("marshal UserFrame: %v", err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
		t.Fatalf("write flow UserFrame: %v", err)
	}
}

// flowProbeFrame builds the connect probe: the first UserFrame carrying the
// session identity (gateway-injected values are authoritative — desktop-
// bridge.md §1) with the ACTIVE status signal the bridge echoes back.
func flowProbeFrame(sessionID string) *game.UserFrame {
	return &game.UserFrame{
		SessionId:  sessionID,
		TemplateId: saoleiTemplateID,
		Payload: &game.UserFrame_FlowParts{FlowParts: &game.FlowParts{Parts: []*game.FlowPart{{
			Kind: &game.FlowPart_Status{Status: &game.StatusSignal{
				Status: game.StatusSignalStatus_STATUS_SIGNAL_STATUS_ACTIVE,
			}},
		}}}},
	}
}

// dialAgentV2FlowProbed dials the flow WebSocket and runs the connect probe
// to completion (probe frame out, status echo back). The environment is
// already serving when the case starts (the deployment startup probe +
// guitar's postDeploySettle — see the section note above), so a probe
// failure here is a plain case failure, not a startup race. Returns the
// live connection and the echo frame.
func dialAgentV2FlowProbed(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, sessionID string) (*websocket.Conn, *game.TeamFrame) {
	t.Helper()

	conn := connectAgentV2Flow(t, ctx, sutHostURL, sutEnvName, sessionID)
	sendFlowProbe(t, conn, sessionID)
	frame, err := readFlowTeamFrameNoFatal(conn, 10*time.Second)
	if err != nil || len(frame.GetFlowParts().GetParts()) == 0 {
		conn.Close()
		t.Fatalf("flow probe for %s failed: %v", sessionID, err)
	}
	return conn, frame
}

// sendFlowProbe writes the connect probe first frame.
func sendFlowProbe(t *testing.T, conn *websocket.Conn, sessionID string) {
	t.Helper()

	sendFlowUserFrame(t, conn, flowProbeFrame(sessionID))
}

// replyFlowReceipt writes the operation receipt the desktop half owes the
// bridge: the dispatched tool_id echoed back with the given status and, on
// success, the screenshot the agent's recognizer consumes
// (desktop-bridge.md §1 FlowResultPart row). The gateway injects the
// session identity from the connect URL, so the frame's own fields are
// advisory — they are filled in for wire conformance.
func replyFlowReceipt(t *testing.T, conn *websocket.Conn, sessionID, toolID string, status game.ToolResultStatus, screenshotPNG []byte) {
	t.Helper()

	result := &game.FlowResultPart{ToolId: toolID, Status: status}
	if screenshotPNG != nil {
		result.Screenshot = &game.ImagePart{
			Encoding: game.ImageEncoding_IMAGE_ENCODING_PNG,
			Data:     screenshotPNG,
		}
	}
	sendFlowUserFrame(t, conn, &game.UserFrame{
		SessionId:  sessionID,
		TemplateId: saoleiTemplateID,
		Payload: &game.UserFrame_FlowParts{FlowParts: &game.FlowParts{Parts: []*game.FlowPart{{
			Kind: &game.FlowPart_FlowResult{FlowResult: result},
		}}}},
	})
}

// readFlowTeamFrame reads one binary-proto TeamFrame with a deadline.
// t.Fatal is only safe on the test goroutine.
func readFlowTeamFrame(t *testing.T, conn *websocket.Conn, timeout time.Duration) *game.TeamFrame {
	t.Helper()

	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		t.Fatalf("set flow read deadline: %v", err)
	}
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read flow TeamFrame: %v", err)
	}
	frame := new(game.TeamFrame)
	if err := proto.Unmarshal(data, frame); err != nil {
		t.Fatalf("unmarshal flow TeamFrame: %v", err)
	}
	return frame
}

// readFlowTeamFrameNoFatal is readFlowTeamFrame for reader goroutines: it
// returns the frame or the error instead of calling t.Fatal.
func readFlowTeamFrameNoFatal(conn *websocket.Conn, timeout time.Duration) (*game.TeamFrame, error) {
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return nil, err
	}
	_, data, err := conn.ReadMessage()
	if err != nil {
		return nil, err
	}
	frame := new(game.TeamFrame)
	if err := proto.Unmarshal(data, frame); err != nil {
		return nil, err
	}
	return frame, nil
}

// collectAgentV2GameEvents folds a completed game turn's tool_result frames
// into the per-tool rendered results keyed by the fake's tool name order:
// it returns the tool_result payloads in arrival order.
func collectAgentV2GameEvents(events []*gamev2.ChatEvent) []*gamev2.ToolResultEvent {
	var results []*gamev2.ToolResultEvent
	for _, e := range events {
		if r := e.GetToolResult(); r != nil {
			results = append(results, r)
		}
	}
	return results
}

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

// agentV2GamePrep is the game suites' arrange step: ensure the (possibly
// fake-desktop-bound) session exists, create its preset, and materialize the
// agent on it. Returns the context, the session resource name, and the
// materialized Agent.
func agentV2GamePrep(t *testing.T, sutHostURL, sutEnvName, sessionID, presetID, playerPrompt string) (context.Context, string, *gamev2.Agent) {
	t.Helper()

	ctx := traceContext(t)
	sessionName := ensureAgentV2Session(t, sutHostURL, sutEnvName, sessionID)
	preset := createAgentV2Preset(t, ctx, sutHostURL, sutEnvName, presetID, playerPrompt)
	agent := updateAgentV2Agent(t, ctx, sutHostURL, sutEnvName, sessionName, preset.GetName(), "")
	return ctx, sessionName, agent
}

// serveWonInitReceipt reads flow frames until the saolei_init F2 dispatch
// arrives and answers it with a SUCCEEDED receipt carrying the recognizable
// win-board screenshot (the same fixture the deployed fake-desktop uses).
// It fails the test if no operation shows up within the timeout. Reading
// happens on the test goroutine — call this only when no other reader
// consumes the flow connection.
func serveWonInitReceipt(t *testing.T, flow *websocket.Conn, sessionID string, timeout time.Duration) {
	t.Helper()

	for {
		frame, err := readFlowTeamFrameNoFatal(flow, timeout)
		if err != nil {
			t.Fatalf("read flow dispatch: %v", err)
		}
		for _, part := range frame.GetFlowParts().GetParts() {
			press := part.GetKeyboardPress()
			if press == nil {
				continue
			}
			if press.GetKey() != game.KeyboardKey_KEYBOARD_KEY_F2 {
				t.Fatalf("dispatched key = %v, want F2 (the saolei_init new-game press)", press.GetKey())
			}
			replyFlowReceipt(t, flow, sessionID, press.GetToolId(), game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED, saoleiBoardWinPNG)
			return
		}
	}
}
