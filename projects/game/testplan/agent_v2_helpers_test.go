// Package testplan contains the shared agent_v2 team helpers used by the
// agent_v2 large-test files. Kept separate from helpers_test.go (the shared
// /api/v1 session + memory HTTP helper set) so only the suites that drive
// /api/v2 pay its dependency closure — the same selective inclusion pattern
// as saolei_fixtures_test.go.
//
// The surface is the team model of
// specs/059-agent-v2-team-mode/contracts/team-api.md as revised by
// specs/060-agent-v2-team-optimize/contracts/team-api.md: the team singleton
// (UpdateTeam/GetTeam/GetTeamMember, including the active_member merged
// value), the merged and member-view histories
// (ListTeamMessages/ListMemberMessages), the team Send stream (member event
// frames + team_message/member_view team-level frames until quiescence) and
// the Cancel custom method. Shared helpers live here, never copied per test
// file (style/large_test.md §反模式3).
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

	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// agentV2PathPrefix is the /api/v2 gateway route prefix both the session
// team face (through the proxy) and the preset configuration face are bound
// to (specs/059-agent-v2-team-mode/contracts/team-api.md §1).
const agentV2PathPrefix = "/api/v2/"

// ─── Fixture vocabulary: user-message triggers ──────────────────────────────
//
// The team fixtures (projects/game/fake-llm/service/testdata/team_planner.yaml
// and team_player.yaml) are keyword/system-keyword driven; the constants below
// are the suite-side anchors. Keep them aligned with the fixture comments
// (projects/game/testplan/README.md §6 lockstep).

const (
	// teamStartMessage carries one of the planner-opening startup tokens
	// (请/开始/工作/就绪/团队/扫雷/开局) so the first Send drives
	// team-planner-opening and yields the opening strategy.
	teamStartMessage = "请开始扫雷"
	// teamNextGameMessage carries the team-player-resume-start anchors
	// (下一局计划/开始下一局) — the structural continuation's next game.
	teamNextGameMessage = "开始下一局"
	// teamQueueMessage carries the queued-digest tokens (暂停/稍等/等待/继续)
	// shared by team-planner-user-reply and team-player-user-intake.
	teamQueueMessage = "稍等，继续按计划观察"
	// teamWaitMessage selects team-planner-wait — the long-running planner
	// turn (4s inter-chunk delay) the queue/cancel/refresh cases pivot on.
	teamWaitMessage = "planner-wait"
	// teamRoleLockMessage selects team-player-role-lock — its system keywords
	// require the saolei guidance heading in the player's assembled prompt
	// (T023 role-lock positive assertion).
	teamRoleLockMessage = "player-role-lock"
)

// ─── Fixture vocabulary: pinned expected contents ───────────────────────────
//
// Texts pinned from projects/game/fake-llm/service/testdata/agent_v2*.yaml and
// team_*.yaml; the message_store_test.go lockstep keeps the embedded store and
// these constants honest (projects/game/testplan/README.md §6).

const (
	// Team chain texts (team_planner.yaml / team_player.yaml):
	teamPlannerOpeningText        = "开局计划：优先从棋盘中心区域开始，逐步向边缘推进；遇到数字边界时先标记周边可疑格。@player 请按以下开局计划开始本局游戏。"
	teamPlannerReviewContinueText = "本局复盘：全部雷区排除，节奏正确；下一局仍从中心区域推进，数字密集处先推理再操作。@player 请按以下下一局计划开始下一局。"
	teamPlannerReviewStopText     = "本局复盘：触雷失败，边角判断有偏差。本局到此为止，不再安排新一局。@player 请保持待命。"
	teamPlannerUserReplyText      = "收到你的消息。本局到此为止，不再安排新一局。"
	teamPlannerWaitText           = "延迟排查完成，暂无新计划。"
	teamPlayerResumeStopText      = "收到复盘。本局到此为止，暂不开新局，保持待命。"
	teamPlayerUserIntakeText      = "收到你的消息，我这边情况正常，会继续关注棋盘。"
	// T023 role-lock / memory fixtures:
	teamPlayerRoleLockText  = "player 角色锁定：扫雷工具守则已加载。"
	teamMemoryReviewContent = "本局复盘观察：中心区域开局稳定，边角标记需谨慎。"
	teamMemorySnapshotText  = "长期记忆快照已生效：中心区域开局稳定，边角标记需谨慎。"
	// teamMemoryAddedResult is the memory tool's success text for the review's
	// add (common/js/dsh-plugins/memory/src/operations.ts ADDED_TEXT).
	teamMemoryAddedResult = "memory added"

	// agent_v2.yaml user-message triggers; every /v1/responses template is
	// matched by ONE of these case-insensitive substrings of the last user
	// message (responses.go matchResponses). Tests must keep each trigger out
	// of unrelated turns' texts.
	agentV2TriggerThink   = "agent-v2-think"
	agentV2TriggerPlain   = "agent-v2-plain"
	agentV2TriggerSlow    = "agent-v2-slow"
	agentV2TriggerFail    = "agent-v2-fail"
	agentV2TriggerFailMid = "agent-v2-midfail"

	// agent_v2.yaml expected contents (the same lockstep rule): the reasoning
	// pieces stream as separate THINK deltas, the text as one TEXT delta.
	agentV2GreetThink1  = "Analyzing the user's request."
	agentV2GreetThink2  = "Drafting a friendly reply."
	agentV2GreetText    = "Hello! I can help you play and manage your game sessions."
	agentV2FollowupText = "As I said when we started, I help you play and manage your game sessions."
	agentV2PlainText    = "Plain answer with no thinking this time."
	agentV2SlowText     = "Finally done thinking."
	agentV2FailMidThink = "Thinking about the request before it breaks."
	agentV2FailMidText  = "Partial answer streamed before the failure."
)

// ─── Fixture vocabulary: game-chain texts (agent_v2_saolei*) ────────────────

const (
	// agent_v2_saolei_tools.yaml rule texts for the deterministic tool chain.
	// won chain (9×9 boards): init playing/revealed board → operate terminal
	// win; progressive chain (16×16): init → two-op batch still playing;
	// lost chain: the operate receipt is the loss board.
	agentV2WonInitContains    = "new game started"
	agentV2WonBoardContains   = "board size 9*9"
	agentV2WonStatusContains  = "game status: won"
	agentV2WonRejectContains  = "stopped at click(0,0) (game_won)"
	agentV2WonSummaryText     = "本局扫雷已完成：全部雷区排除，游戏获胜。"
	agentV2LostStatusContains = "game status: lost"
	agentV2LostSummaryText    = "本局扫雷已结束：触雷失败，可重新开局再试。"

	agentV2ProgInitContains   = "new game started"
	agentV2ProgBoardContains  = "board size 16*16"
	agentV2ProgStatusContains = "game status: playing"
	agentV2ProgExecContains   = "executed 2 ops"
	agentV2ProgSummaryText    = "已完成一轮扫雷操作：点击揭示与标记旗子均已执行，棋盘已刷新。"

	// Desktop-absent / mid-game-disconnect chain: the bridge's FAILED receipt
	// becomes a tool ERROR result whose text names the cause.
	agentV2DisconnectedContain = "desktop disconnected"
	agentV2NodesktopSummary    = "桌面未连接，无法开局。请先连接桌面后再试。"
	agentV2DisconnectSummary   = "桌面连接中断，操作未能完成。请等待桌面重连后再试。"
)

// Fixed caller-id sessions the deployed fake-desktop executor binds: the won
// session is bound by projects/game/testplan/deploy_agent_v2.yaml and the drop
// session by deploy_agent_v2_drop.yaml (one executor instance per deploy).
const (
	agentV2DesktopWonSessionID = "desktop-e2e-won"
	agentV2DesktopDropID       = "desktop-e2e-drop"
)

// ─── Resource-name helpers ──────────────────────────────────────────────────

// agentV2SessionName builds the full game session resource name
// (templates/{template}/sessions/{session}, team-api.md §1).
func agentV2SessionName(sessionID string) string {
	return game.SessionName{TemplateID: saoleiTemplateID, SessionID: sessionID}.String()
}

// agentV2TeamName builds the session's team singleton resource name
// (AIP-156: https://google.aip.dev/156).
func agentV2TeamName(sessionName string) string {
	return sessionName + "/team"
}

// agentV2MemberName builds one fixed member resource name (members/player or
// members/planner — the role IS the id, data-model.md §1).
func agentV2MemberName(sessionName, member string) string {
	return agentV2TeamName(sessionName) + "/members/" + member
}

// sessionIDFromName extracts the session id segment from a session resource
// name (the inverse of agentV2SessionName; the flow WebSocket path addresses
// the session by its raw id).
func sessionIDFromName(t *testing.T, sessionName string) string {
	t.Helper()

	parsed, err := game.ParseSessionName(sessionName)
	if err != nil {
		t.Fatalf("parse session resource name %q: %v", sessionName, err)
	}
	return parsed.SessionID
}

// ─── Send stream (the team NDJSON surface) ──────────────────────────────────

// teamStream is an open Send stream: the NDJSON frame scanner plus the
// response handle it wraps (Close terminates the HTTP stream). The stream
// runs from Send acceptance until the team quiesces (team-api.md §3.1).
type teamStream struct {
	resp    *http.Response
	Scanner *bufio.Scanner
}

// Close releases the stream's HTTP body.
func (s *teamStream) Close() { s.resp.Body.Close() }

// startTeamSend issues POST /api/v2/{session}:send (AIP-136 custom method,
// team-api.md §1/§3) and returns the open NDJSON stream. The body is left
// unconsumed: the caller reads ChatEvents via nextTeamEvent (test goroutine)
// or drainTeamStreamAsync (reader goroutines) and Closes the stream when
// done. Only the HTTP status is checked here — a stream that never opens is a
// fatal request-level failure.
func startTeamSend(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, sessionName, text string) *teamStream {
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
	return &teamStream{resp: resp, Scanner: scanner}
}

// postTeamSendStatus is startTeamSend for request-level failures (stream
// never opens): it consumes the whole response and returns the HTTP status
// with the raw body — used to assert the INVALID_ARGUMENT / NOT_FOUND /
// FAILED_PRECONDITION rejection family (team-api.md §3/§6).
func postTeamSendStatus(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, sessionName, text string) (int, []byte) {
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

// nextTeamEvent reads one NDJSON frame from a Send stream and decodes it into
// a ChatEvent. grpc-gateway v2 streams every message wrapped in a "result"
// key — one `{"result": <ChatEvent>} JSON object per "\n"-terminated line
// (the default streaming marshaler, grpc-gateway runtime/handler.go
// handleForwardResponseServerStream at the repo-pinned v2.27.6). Calls
// t.Fatal on transport, framing, or decode errors; reader goroutines must use
// nextTeamEventNoFatal instead.
func nextTeamEvent(t *testing.T, scanner *bufio.Scanner) *game.ChatEvent {
	t.Helper()

	if !scanner.Scan() {
		t.Fatalf("read send stream: %v", scanner.Err())
	}
	return decodeAgentV2Chunk(t, scanner.Bytes())
}

// nextTeamEventNoFatal is nextTeamEvent without t.Fatal: it returns the
// decoded ChatEvent or an error, for drain goroutines (t.Fatal must only run
// on the test goroutine). io.EOF means the server ended the stream at the
// team static point (team-api.md §3.1).
func nextTeamEventNoFatal(scanner *bufio.Scanner) (*game.ChatEvent, error) {
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
func decodeAgentV2Chunk(t *testing.T, line []byte) *game.ChatEvent {
	t.Helper()

	evt, err := decodeAgentV2ChunkNoFatal(line)
	if err != nil {
		t.Fatalf("decode stream chunk %s: %v", line, err)
	}
	return evt
}

// decodeAgentV2ChunkNoFatal decodes one `{"result": <ChatEvent>}` NDJSON
// line into a ChatEvent (protojson camelCase projection, unknown fields
// ignored per proto3 forward-compat).
func decodeAgentV2ChunkNoFatal(line []byte) (*game.ChatEvent, error) {
	var chunk struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(line, &chunk); err != nil {
		return nil, err
	}
	if len(chunk.Result) == 0 {
		return nil, fmt.Errorf("chunk lacks the grpc-gateway %q wrapper", "result")
	}
	evt := new(game.ChatEvent)
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(chunk.Result, evt); err != nil {
		return nil, err
	}
	return evt, nil
}

// teamStreamResult is what a drain goroutine reports back: the collected
// frames plus the first non-EOF read error, if any.
type teamStreamResult struct {
	events []*game.ChatEvent
	err    error
}

// drainTeamStreamAsync drains a Send stream to its natural end (the team
// static point, team-api.md §3.1) on a reader goroutine and reports the
// frames (or the read error) on the channel. io.EOF is the expected terminal
// (err stays nil); the stream body is closed after the drain.
func drainTeamStreamAsync(stream *teamStream) <-chan teamStreamResult {
	ch := make(chan teamStreamResult, 1)
	go func() {
		var result teamStreamResult
		defer func() {
			stream.Close()
			ch <- result
		}()
		for {
			evt, err := nextTeamEventNoFatal(stream.Scanner)
			if err != nil {
				if err != io.EOF {
					result.err = err
				}
				return
			}
			result.events = append(result.events, evt)
		}
	}()
	return ch
}

// drainTeamStream drains a stream to its natural end on the test goroutine
// and returns the frames. Use it when no other flow work must progress
// concurrently; otherwise run drainTeamStreamAsync and waitTeamStream.
func drainTeamStream(t *testing.T, stream *teamStream) []*game.ChatEvent {
	t.Helper()

	events := waitTeamStream(t, drainTeamStreamAsync(stream), "team stream")
	return events
}

// waitTeamStream waits for a drain goroutine to finish within the shared read
// window and returns the frames; a non-EOF read error or a timeout fails the
// case.
func waitTeamStream(t *testing.T, ch <-chan teamStreamResult, what string) []*game.ChatEvent {
	t.Helper()

	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("%s: %v", what, r.err)
		}
		return r.events
	case <-time.After(wsReadTimeout):
		t.Fatalf("%s did not quiesce within %s", what, wsReadTimeout)
		return nil
	}
}

// waitTeamQuiescence polls ListTeamMessages until cond holds, up to the shared
// read window — the List 回填 anchor for a stream the client dropped
// (team-api.md §3.3).
func waitTeamQuiescence(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, sessionName string, cond func([]*game.TeamMessage) bool) []*game.TeamMessage {
	t.Helper()

	deadline := time.Now().Add(wsReadTimeout)
	for {
		entries := listTeamMessages(t, ctx, sutHostURL, sutEnvName, sessionName)
		if cond(entries) {
			return entries
		}
		if time.Now().After(deadline) {
			t.Fatalf("team history did not reach the expected state within %s; entries = %d", wsReadTimeout, len(entries))
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// ─── Stream frame analysis ──────────────────────────────────────────────────

// teamMemberTurn groups one member turn's frames (same turn_id) in arrival
// order. Team-level frames (queued / team_message) carry no turn_id and are
// not part of a group.
type teamMemberTurn struct {
	member string
	turnID string
	events []*game.ChatEvent
}

// groupTeamMemberTurns folds a stream's frames into member turns by turn_id,
// in first-arrival order (team-api.md §3.2: turn_id is minted per member
// turn, so it is the grouping key).
func groupTeamMemberTurns(events []*game.ChatEvent) []*teamMemberTurn {
	var turns []*teamMemberTurn
	byID := map[string]*teamMemberTurn{}
	for _, event := range events {
		if event.GetMember() == "" || event.GetTurnId() == "" {
			continue
		}
		turn := byID[event.GetTurnId()]
		if turn == nil {
			turn = &teamMemberTurn{member: event.GetMember(), turnID: event.GetTurnId()}
			byID[event.GetTurnId()] = turn
			turns = append(turns, turn)
		}
		turn.events = append(turn.events, event)
	}
	return turns
}

// teamTurnBlocks folds one member turn's TEXT and THINK deltas (the block
// content per conversation-api.md §3 invariant 4).
func teamTurnBlocks(turn *teamMemberTurn) (think, text string) {
	kindByIndex := map[int32]game.BlockType{}
	var thinkOut, textOut strings.Builder
	for _, event := range turn.events {
		if start := event.GetBlockStart(); start != nil {
			kindByIndex[start.GetIndex()] = start.GetType()
			continue
		}
		delta := event.GetDelta()
		if delta == nil {
			continue
		}
		switch kindByIndex[delta.GetIndex()] {
		case game.BlockType_BLOCK_TYPE_TEXT:
			textOut.WriteString(delta.GetText())
		case game.BlockType_BLOCK_TYPE_THINK:
			thinkOut.WriteString(delta.GetText())
		}
	}
	return thinkOut.String(), textOut.String()
}

// teamTurnToolResults returns one member turn's tool_result frames in order.
func teamTurnToolResults(turn *teamMemberTurn) []*game.ToolResultEvent {
	var results []*game.ToolResultEvent
	for _, event := range turn.events {
		if result := event.GetToolResult(); result != nil {
			results = append(results, result)
		}
	}
	return results
}

// teamTurnsForMember filters a stream's member turns by producer role.
func teamTurnsForMember(events []*game.ChatEvent, member string) []*teamMemberTurn {
	var turns []*teamMemberTurn
	for _, turn := range groupTeamMemberTurns(events) {
		if turn.member == member {
			turns = append(turns, turn)
		}
	}
	return turns
}

// teamTurnEndStatus returns the turn's terminal status.
func teamTurnEndStatus(turn *teamMemberTurn) game.TurnStatus {
	return turn.events[len(turn.events)-1].GetTurnEnd().GetStatus()
}

// teamStreamMessages returns a stream's team_message frames (the merged
// sequence entries with their seq anchors).
func teamStreamMessages(events []*game.ChatEvent) []*game.TeamMessage {
	var messages []*game.TeamMessage
	for _, event := range events {
		if message := event.GetTeamMessage(); message != nil {
			messages = append(messages, message)
		}
	}
	return messages
}

// assertTeamMemberTurnWellFormed checks one member turn's frame invariants:
// exactly one turn_start (first) and one turn_end (last), a constant turn_id
// and member, announced block starts with contiguous per-index delta runs and
// matching terminal content, and tool_result frames settling the tool calls
// (conversation-api.md §3; data-model.md §2.4).
func assertTeamMemberTurnWellFormed(t *testing.T, sessionName string, turn *teamMemberTurn) {
	t.Helper()

	if len(turn.events) == 0 {
		t.Fatalf("member turn %s has no frames", turn.turnID)
	}
	if turn.events[0].GetTurnStart() == nil {
		t.Fatalf("member turn %s first frame = %T, want turn_start", turn.turnID, turn.events[0].GetPayload())
	}
	if last := turn.events[len(turn.events)-1]; last.GetTurnEnd() == nil {
		t.Fatalf("member turn %s last frame is not turn_end (payload = %T)", turn.turnID, last.GetPayload())
	}
	turnStarts, turnEnds := 0, 0
	for i, event := range turn.events {
		if event.GetSession() != sessionName {
			t.Errorf("turn %s frame %d session = %q, want %q", turn.turnID, i, event.GetSession(), sessionName)
		}
		if event.GetTurnId() != turn.turnID {
			t.Errorf("turn %s frame %d turn_id = %q", turn.turnID, i, event.GetTurnId())
		}
		if event.GetMember() != turn.member {
			t.Errorf("turn %s frame %d member = %v, want %v", turn.turnID, i, event.GetMember(), turn.member)
		}
		switch {
		case event.GetTurnStart() != nil:
			turnStarts++
		case event.GetTurnEnd() != nil:
			turnEnds++
		}
	}
	if turnStarts != 1 || turnEnds != 1 {
		t.Fatalf("turn %s frame counts: turn_start=%d turn_end=%d, want exactly 1 each", turn.turnID, turnStarts, turnEnds)
	}

	announced := map[int32]bool{}
	kindByIndex := map[int32]game.BlockType{}
	deltas := map[int32][]string{}
	closedRuns := map[int32]bool{}
	lastDeltaIdx := int32(-1)
	blockEnds := map[int32]*game.ContentBlock{}
	unsettledToolCalls := []string{}
	for i, event := range turn.events {
		switch {
		case event.GetBlockStart() != nil:
			start := event.GetBlockStart()
			if announced[start.GetIndex()] {
				t.Errorf("turn %s: duplicate block_start for index %d", turn.turnID, start.GetIndex())
			}
			announced[start.GetIndex()] = true
			kindByIndex[start.GetIndex()] = start.GetType()
		case event.GetDelta() != nil:
			idx := event.GetDelta().GetIndex()
			if !announced[idx] {
				t.Errorf("turn %s frame %d: delta carries index %d without a block_start", turn.turnID, i, idx)
				continue
			}
			if lastDeltaIdx != -1 && lastDeltaIdx != idx {
				closedRuns[lastDeltaIdx] = true
			}
			if closedRuns[idx] {
				t.Errorf("turn %s frame %d: delta reopens index %d — block deltas interleave", turn.turnID, i, idx)
			}
			deltas[idx] = append(deltas[idx], event.GetDelta().GetText())
			lastDeltaIdx = idx
		case event.GetBlockEnd() != nil:
			idx := event.GetBlockEnd().GetIndex()
			if !announced[idx] {
				t.Errorf("turn %s frame %d: block_end carries index %d without a block_start", turn.turnID, i, idx)
				continue
			}
			if _, dup := blockEnds[idx]; dup {
				t.Errorf("turn %s: duplicate block_end for index %d", turn.turnID, idx)
				continue
			}
			blockEnds[idx] = event.GetBlockEnd().GetBlock()
			// The tool identity first surfaces on the closing block (the dsh
			// block-start chunk carries no id): register the pending call
			// from there, so the tool_result pairing below has its key.
			if call := event.GetBlockEnd().GetBlock().GetToolCall(); call != nil {
				if call.GetToolId() == "" || call.GetName() == "" {
					t.Errorf("turn %s: block_end carries tool_id %q name %q, want both non-empty", turn.turnID, call.GetToolId(), call.GetName())
				}
				unsettledToolCalls = append(unsettledToolCalls, call.GetToolId())
			}
		case event.GetToolResult() != nil:
			result := event.GetToolResult()
			if result.GetToolId() == "" {
				t.Errorf("turn %s frame %d: tool_result carries an empty tool_id", turn.turnID, i)
				continue
			}
			if result.GetStatus() != game.ToolStatus_TOOL_STATUS_SUCCEEDED && result.GetStatus() != game.ToolStatus_TOOL_STATUS_FAILED {
				t.Errorf("turn %s frame %d: tool_result status = %v, want a terminal SUCCEEDED/FAILED", turn.turnID, i, result.GetStatus())
			}
			for j := len(unsettledToolCalls) - 1; j >= 0; j-- {
				if unsettledToolCalls[j] == result.GetToolId() {
					unsettledToolCalls = append(unsettledToolCalls[:j], unsettledToolCalls[j+1:]...)
					break
				}
			}
		}
	}
	if teamTurnEndStatus(turn) == game.TurnStatus_TURN_STATUS_COMPLETED && len(unsettledToolCalls) > 0 {
		t.Errorf("turn %s: COMPLETED turn ends with %d unsettled tool call(s): %v", turn.turnID, len(unsettledToolCalls), unsettledToolCalls)
	}
	for idx, block := range blockEnds {
		joined := strings.Join(deltas[idx], "")
		switch {
		case block.GetText() != nil:
			if got := block.GetText().GetContent(); got != joined {
				t.Errorf("turn %s block %d: block_end text = %q, want delta concatenation %q", turn.turnID, idx, got, joined)
			}
		case block.GetThink() != nil:
			if got := block.GetThink().GetContent(); got != joined {
				t.Errorf("turn %s block %d: block_end think = %q, want delta concatenation %q", turn.turnID, idx, got, joined)
			}
		case block.GetToolCall() != nil:
			call := block.GetToolCall()
			if call.GetStatus() != game.ToolStatus_TOOL_STATUS_RUNNING {
				t.Errorf("turn %s block %d: block_end tool_call status = %v, want RUNNING", turn.turnID, idx, call.GetStatus())
			}
			if got := call.GetArgsJson(); got != joined {
				t.Errorf("turn %s block %d: block_end args_json = %q, want delta concatenation %q", turn.turnID, idx, got, joined)
			}
		}
	}
}

// assertTeamToolResultWireOrder checks the server's tool-call frame order and
// pairing contract (specs/060-agent-v2-team-optimize/contracts/team-api.md
// §3): for every tool call the stream carries, block_end surfaces the
// provider-issued tool id first, the step's team_message fixation (same tool
// id) follows, and only then does the tool_result frame settle it with the
// terminal status/result.
//
// The fixation's tool-call status is normally RUNNING. When a tool fails
// synchronously inside its plugin (no desktop/network round trip), its settle
// can precede grpc-js serializing the fixation write: the frame and
// ListTeamMessages share one entry object by design
// (projects/game/agent_v2/src/history.ts appendMerge + settleToolResult), so
// the serialized fixation may already carry the terminal status. The frame
// order is unaffected (tool_result still follows the fixation) and clients
// settle idempotently, so the checker accepts RUNNING or the terminal status
// the tool's own tool_result frame reports; the two MUST agree whenever the
// fixation carries a terminal status.
//
// Precondition: every member turn in the stream settled COMPLETED. A
// CANCELED/ABORTED turn may legitimately interrupt a tool call without its
// team_message fixation or tool_result settlement, which this checker would
// misreport.
func assertTeamToolResultWireOrder(t *testing.T, sessionName string, events []*game.ChatEvent) {
	t.Helper()

	blockEndAt := map[string]int{}
	fixationAt := map[string]int{}
	fixationStatus := map[string]game.ToolStatus{}
	settledAt := map[string]int{}
	settledStatus := map[string]game.ToolStatus{}
	for i, event := range events {
		if event.GetSession() != sessionName {
			t.Errorf("frame %d session = %q, want %q", i, event.GetSession(), sessionName)
		}
		switch {
		case event.GetBlockEnd() != nil:
			call := event.GetBlockEnd().GetBlock().GetToolCall()
			if call == nil {
				continue
			}
			if call.GetToolId() == "" {
				t.Errorf("frame %d: block_end tool_call lacks its tool_id", i)
				continue
			}
			if _, dup := blockEndAt[call.GetToolId()]; dup {
				t.Errorf("frame %d: duplicate block_end for tool %q", i, call.GetToolId())
				continue
			}
			blockEndAt[call.GetToolId()] = i
		case event.GetTeamMessage() != nil:
			for _, block := range event.GetTeamMessage().GetMessage().GetBlocks() {
				call := block.GetToolCall()
				if call == nil || call.GetToolId() == "" {
					continue
				}
				if _, seen := fixationAt[call.GetToolId()]; seen {
					t.Errorf("frame %d: duplicate team_message fixation for tool %q", i, call.GetToolId())
					continue
				}
				if _, ok := blockEndAt[call.GetToolId()]; !ok {
					t.Errorf("frame %d: team_message fixes tool %q before its block_end", i, call.GetToolId())
				}
				if status := call.GetStatus(); status != game.ToolStatus_TOOL_STATUS_RUNNING &&
					status != game.ToolStatus_TOOL_STATUS_SUCCEEDED &&
					status != game.ToolStatus_TOOL_STATUS_FAILED {
					t.Errorf("frame %d: team_message fixation for tool %q status = %v, want RUNNING or a terminal status", i, call.GetToolId(), status)
				}
				fixationAt[call.GetToolId()] = i
				fixationStatus[call.GetToolId()] = call.GetStatus()
			}
		case event.GetToolResult() != nil:
			result := event.GetToolResult()
			if _, seen := settledAt[result.GetToolId()]; seen {
				t.Errorf("frame %d: duplicate tool_result for tool %q", i, result.GetToolId())
				continue
			}
			if result.GetStatus() != game.ToolStatus_TOOL_STATUS_SUCCEEDED && result.GetStatus() != game.ToolStatus_TOOL_STATUS_FAILED {
				t.Errorf("frame %d: tool_result for %q status = %v, want a terminal SUCCEEDED/FAILED", i, result.GetToolId(), result.GetStatus())
			}
			if _, ok := fixationAt[result.GetToolId()]; !ok {
				t.Errorf("frame %d: tool_result for %q precedes its team_message fixation", i, result.GetToolId())
			}
			settledAt[result.GetToolId()] = i
			settledStatus[result.GetToolId()] = result.GetStatus()
		}
	}
	for id, endAt := range blockEndAt {
		fixation, fixed := fixationAt[id]
		if !fixed {
			t.Errorf("tool %q: block_end at frame %d has no team_message fixation", id, endAt)
			continue
		}
		settled, ok := settledAt[id]
		if !ok {
			t.Errorf("tool %q: block_end at frame %d has no tool_result settlement", id, endAt)
			continue
		}
		if fixation < endAt || settled < fixation {
			t.Errorf("tool %q frame order = block_end:%d team_message:%d tool_result:%d, want block_end < team_message < tool_result", id, endAt, fixation, settled)
		}
		if status := fixationStatus[id]; status != game.ToolStatus_TOOL_STATUS_RUNNING && status != settledStatus[id] {
			t.Errorf("tool %q: team_message fixation status = %v, want RUNNING or the tool_result status %v", id, status, settledStatus[id])
		}
	}
	for id := range settledAt {
		if _, ok := blockEndAt[id]; !ok {
			t.Errorf("tool_result for tool %q has no preceding block_end", id)
		}
	}
}

// assertTeamStreamWellFormed checks the stream-level invariants of a team
// stream established at its lifecycle start (team-api.md §3.2/§3.3; the
// member_view frame contract
// specs/060-agent-v2-team-optimize/contracts/team-api.md §2): every frame
// carries the session; a queued frame is the first frame only; the
// team_message frames carry a producer label, a message, and a strictly
// increasing seq; a member_view frame names its consuming member, the input
// source, and the projected message; and each member turn is well formed
// (assertTeamMemberTurnWellFormed).
func assertTeamStreamWellFormed(t *testing.T, sessionName string, events []*game.ChatEvent) {
	t.Helper()

	if len(events) == 0 {
		t.Fatal("empty team stream")
	}
	lastSeq := int64(0)
	for i, event := range events {
		if event.GetSession() != sessionName {
			t.Errorf("frame %d session = %q, want %q", i, event.GetSession(), sessionName)
		}
		switch {
		case event.GetQueued() != nil:
			if i != 0 {
				t.Errorf("queued at frame %d is not the first frame (team-api.md §3.3)", i)
			}
			if pos := event.GetQueued().GetPosition(); pos < 1 {
				t.Errorf("queued position = %d, want >= 1", pos)
			}
		case event.GetTeamMessage() != nil:
			frame := event.GetTeamMessage()
			if frame.GetMember() == "" {
				t.Errorf("team_message at frame %d lacks its producer label", i)
			}
			if frame.GetMessage() == nil {
				t.Errorf("team_message at frame %d lacks its message payload", i)
			}
			if frame.GetSeq() <= lastSeq {
				t.Errorf("team_message at frame %d seq = %d, want > previous %d", i, frame.GetSeq(), lastSeq)
			}
			lastSeq = frame.GetSeq()
		case event.GetMemberView() != nil:
			// Team-level frame (no outer member/turn_id): the payload names
			// the consuming member, the input source, and the projection.
			frame := event.GetMemberView()
			if frame.GetMember() == "" || frame.GetSender() == "" || frame.GetMessage() == nil {
				t.Errorf("member_view at frame %d lacks member/sender/message", i)
			}
		case event.GetPayload() == nil:
			t.Errorf("frame %d carries an empty payload", i)
		default:
			if event.GetMember() == "" || event.GetTurnId() == "" {
				t.Errorf("member frame %d lacks member/turn_id", i)
			}
		}
	}
	for _, turn := range groupTeamMemberTurns(events) {
		assertTeamMemberTurnWellFormed(t, sessionName, turn)
	}
}

// teamHistoryMessagesEquivalent compares one streamed team_message payload
// with the listed entry: role and every block's native content must agree. A
// tool-call block's terminal settlement is excluded — the frame carries the
// shared entry object (projects/game/agent_v2/src/history.ts appendMerge), so
// its status may read RUNNING or an already-settled terminal state depending
// on when the frame was serialized, while the List read observes the settled
// status/result backfilled by tool/result
// (specs/060-agent-v2-team-optimize/contracts/team-api.md §3); the comparison
// follows the entry's stable content — only id/name/args are compared here.
func teamHistoryMessagesEquivalent(streamed, listed *game.HistoryMessage) bool {
	if streamed.GetRole() != listed.GetRole() {
		return false
	}
	streamedBlocks, listedBlocks := streamed.GetBlocks(), listed.GetBlocks()
	if len(streamedBlocks) != len(listedBlocks) {
		return false
	}
	for i := range streamedBlocks {
		a, b := streamedBlocks[i], listedBlocks[i]
		switch {
		case a.GetText() != nil || b.GetText() != nil:
			if a.GetText().GetContent() != b.GetText().GetContent() {
				return false
			}
		case a.GetThink() != nil || b.GetThink() != nil:
			if a.GetThink().GetContent() != b.GetThink().GetContent() {
				return false
			}
		case a.GetToolCall() != nil || b.GetToolCall() != nil:
			ca, cb := a.GetToolCall(), b.GetToolCall()
			if ca.GetToolId() != cb.GetToolId() || ca.GetName() != cb.GetName() || ca.GetArgsJson() != cb.GetArgsJson() {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// assertTeamStreamMessagesMatchList asserts the Seq 锚 consistency the team
// stream and ListTeamMessages share (team-api.md §3.2): the frames a stream
// from the lifecycle start observed are exactly the listed entries, in order,
// with equal seq, producer label, and native message content (SC-003).
func assertTeamStreamMessagesMatchList(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, sessionName string, events []*game.ChatEvent) {
	t.Helper()

	frames := teamStreamMessages(events)
	listed := listTeamMessages(t, ctx, sutHostURL, sutEnvName, sessionName)
	if len(frames) != len(listed) {
		t.Fatalf("stream team_message frames = %d, ListTeamMessages entries = %d, want equal", len(frames), len(listed))
	}
	for i, frame := range frames {
		entry := listed[i]
		if frame.GetSeq() != entry.GetSeq() {
			t.Fatalf("frame[%d] seq = %d, listed seq = %d", i, frame.GetSeq(), entry.GetSeq())
		}
		if frame.GetMember() != entry.GetMember() {
			t.Errorf("frame[%d] member = %v, listed member = %v", i, frame.GetMember(), entry.GetMember())
		}
		if !teamHistoryMessagesEquivalent(frame.GetMessage(), entry.GetMessage()) {
			t.Errorf("frame[%d] (seq %d) message differs from the listed entry", i, frame.GetSeq())
		}
	}
}

// teamMessagesForMember filters merge entries by producer label.
func teamMessagesForMember(entries []*game.TeamMessage, member string) []*game.TeamMessage {
	var out []*game.TeamMessage
	for _, entry := range entries {
		if entry.GetMember() == member {
			out = append(out, entry)
		}
	}
	return out
}

// agentV2MessageThink returns the concatenated ThinkBlock contents of one
// history message; agentV2MessageText the TextBlock contents.
func agentV2MessageThink(m *game.HistoryMessage) string {
	var s string
	for _, b := range m.GetBlocks() {
		if think := b.GetThink(); think != nil {
			s += think.GetContent()
		}
	}
	return s
}

func agentV2MessageText(m *game.HistoryMessage) string {
	var s string
	for _, b := range m.GetBlocks() {
		if text := b.GetText(); text != nil {
			s += text.GetContent()
		}
	}
	return s
}

// assertMemberViewPerspective checks one member view's perspective contract
// (team-api.md §5): a user input is a USER-role message with sender "user";
// the member's own output is an AGENT-role message with sender = the member;
// another member's relayed output is a USER-role message with sender = that
// role (the `user: [sender] …` shape). viewName and member only label
// failures.
func assertMemberViewPerspective(t *testing.T, viewName string, view []*game.MemberViewMessage, member string) {
	t.Helper()

	for i, entry := range view {
		message := entry.GetMessage()
		switch sender := entry.GetSender(); sender {
		case "user":
			if message.GetRole() != game.Role_ROLE_USER {
				t.Errorf("%s view[%d] user input role = %v, want USER", viewName, i, message.GetRole())
			}
		case member:
			if message.GetRole() != game.Role_ROLE_AGENT {
				t.Errorf("%s view[%d] own output role = %v, want AGENT", viewName, i, message.GetRole())
			}
		case "player", "planner":
			if message.GetRole() != game.Role_ROLE_USER {
				t.Errorf("%s view[%d] relayed output from %q role = %v, want USER (the injected broadcast is a user-role message)", viewName, i, sender, message.GetRole())
			}
		default:
			t.Errorf("%s view[%d] sender = %q, want \"user\" or a member role", viewName, i, sender)
		}
	}
}

// assertTeamMemberViewLive checks one live consumption frame against the
// member_view contract
// (specs/060-agent-v2-team-optimize/contracts/team-api.md §2): the stream
// carries a frame naming the consuming member and the input source; the frame
// is team-level; its message is the member-view projection (ROLE_USER) for
// the same messageId ListMemberMessages serves; and the frame arrives with
// the consuming turn — no later than the member's first streamed content
// frame — so the live view never waits for the turn to settle (the fan-out
// happens at the member-log injection, which precedes the model request).
func assertTeamMemberViewLive(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, sessionName string, events []*game.ChatEvent, member, sender string) *game.MemberViewEvent {
	t.Helper()

	frameIndex, frame := -1, (*game.MemberViewEvent)(nil)
	for i, event := range events {
		view := event.GetMemberView()
		if view == nil || view.GetMember() != member || view.GetSender() != sender {
			continue
		}
		frameIndex, frame = i, view
		break
	}
	if frame == nil {
		t.Fatalf("stream carries no member_view{member=%q sender=%q}", member, sender)
	}
	if event := events[frameIndex]; event.GetMember() != "" || event.GetTurnId() != "" {
		t.Errorf("member_view at frame %d carries outer member/turn_id (%q/%q), want a team-level frame", frameIndex, event.GetMember(), event.GetTurnId())
	}
	if role := frame.GetMessage().GetRole(); role != game.Role_ROLE_USER {
		t.Errorf("member_view{member=%q sender=%q} message role = %v, want USER", member, sender, role)
	}
	if frame.GetMessage().GetMessageId() == "" {
		t.Errorf("member_view{member=%q sender=%q} message lacks its messageId anchor", member, sender)
	}
	for i, event := range events {
		if event.GetMember() != member {
			continue
		}
		if event.GetBlockStart() == nil && event.GetDelta() == nil {
			continue
		}
		if frameIndex > i {
			t.Errorf("member_view{member=%q sender=%q} at frame %d arrives after the member's first content frame at frame %d", member, sender, frameIndex, i)
		}
		break
	}
	matched := false
	for _, entry := range listMemberMessages(t, ctx, sutHostURL, sutEnvName, sessionName, member) {
		if entry.GetMessage().GetMessageId() != frame.GetMessage().GetMessageId() {
			continue
		}
		matched = true
		if entry.GetSender() != sender {
			t.Errorf("ListMemberMessages(%s) sender = %q, want %q", member, entry.GetSender(), sender)
		}
		if !proto.Equal(entry.GetMessage(), frame.GetMessage()) {
			t.Errorf("member_view{member=%q sender=%q} message differs from the ListMemberMessages projection (messageId %s)", member, sender, frame.GetMessage().GetMessageId())
		}
		break
	}
	if !matched {
		t.Errorf("ListMemberMessages(%s) lacks the message the member_view frame carried (messageId %s)", member, frame.GetMessage().GetMessageId())
	}
	return frame
}

// assertMergeMatchesMemberViews asserts every member output in the merged
// sequence is the SAME message as its entry in that member's own view — same
// messageId and equal body. The two projections share one message object
// (history.ts appendMemberOutput), so this pins the cross-view正文一致
// requirement (SC-003) the List faces expose.
func assertMergeMatchesMemberViews(t *testing.T, entries []*game.TeamMessage, views map[string][]*game.MemberViewMessage) {
	t.Helper()

	for i, entry := range entries {
		if entry.GetMember() == "user" {
			continue
		}
		found := false
		for _, viewEntry := range views[entry.GetMember()] {
			if viewEntry.GetMessage().GetMessageId() != entry.GetMessage().GetMessageId() {
				continue
			}
			found = true
			if !proto.Equal(viewEntry.GetMessage(), entry.GetMessage()) {
				t.Errorf("merge entry[%d] %s message %s differs from the same message in that member's view", i, entry.GetMember(), entry.GetMessage().GetMessageId())
			}
		}
		if !found {
			t.Errorf("merge entry[%d] %s message %s is missing from that member's view", i, entry.GetMember(), entry.GetMessage().GetMessageId())
		}
	}
}

// ─── Team resource helpers (the /api/v2 team singleton surface) ─────────────

// teamMember builds one members-list entry for the UpdateTeam input (the
// generalized wire shape: one {role, preset, model?} per member,
// specs/059-agent-v2-team-mode/contracts/team-api.md §2). An empty model is
// omitted from the wire body (empty = deployment default).
func teamMember(role, preset, model string) *game.TeamMember {
	member := &game.TeamMember{Role: role, Preset: preset}
	if model != "" {
		member.Model = model
	}
	return member
}

// updateAgentV2Team materializes (or refreshes) the session's team through
// the gateway with the scene's two-member roster (player + planner) and
// returns the stored Team. Calls t.Fatal on non-200 responses.
func updateAgentV2Team(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, sessionName, playerPreset, plannerPreset, playerModel, plannerModel string) *game.Team {
	t.Helper()

	team, status, respBody := updateAgentV2TeamWithStatus(t, ctx, sutHostURL, sutEnvName, sessionName, playerPreset, plannerPreset, playerModel, plannerModel)
	if status != http.StatusOK {
		t.Fatalf("PATCH update team status=%d, body=%s", status, respBody)
	}
	return team
}

// updateAgentV2TeamWithStatus is updateAgentV2Team without the 200 fatality:
// it builds the same scene roster and returns the HTTP status with the parsed
// resource (nil unless the body decodes as a Team) — the fail-fast matrix's
// convenience path (INVALID_ARGUMENT, team-api.md §2/§6).
func updateAgentV2TeamWithStatus(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, sessionName, playerPreset, plannerPreset, playerModel, plannerModel string) (*game.Team, int, []byte) {
	t.Helper()

	return updateAgentV2TeamMembers(t, ctx, sutHostURL, sutEnvName, sessionName, []*game.TeamMember{
		teamMember("player", playerPreset, playerModel),
		teamMember("planner", plannerPreset, plannerModel),
	}, "")
}

// updateAgentV2TeamMembers sends an exact members list as the materialization
// input (PATCH /api/v2/.../team?allow_missing=true, AIP-134 create-or-update)
// and returns the parsed Team with the HTTP status. The proto imposes no
// roster size or role vocabulary (scene-agnostic primitive), so this raw path
// is what the structural/scene validation cases use; updateMask is the raw
// mask value ("members" or "", the latter omitting the query parameter).
func updateAgentV2TeamMembers(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, sessionName string, members []*game.TeamMember, updateMask string) (*game.Team, int, []byte) {
	t.Helper()

	body, err := protojson.Marshal(&game.Team{Members: members})
	if err != nil {
		t.Fatalf("protojson.Marshal Team: %v", err)
	}
	reqURL := fmt.Sprintf("%s%s%s/team?allow_missing=true", sutHostURL, agentV2PathPrefix, sessionName)
	if updateMask != "" {
		reqURL += "&update_mask=" + url.QueryEscape(updateMask)
	}
	resp, respBody := doHTTPTrace(t, ctx, http.MethodPatch, reqURL, sutEnvName, body)
	stored := new(game.Team)
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(respBody, stored); err != nil {
		t.Logf("Team body (status %d) is not a response proto: %s", resp.StatusCode, respBody)
		return nil, resp.StatusCode, respBody
	}
	return stored, resp.StatusCode, respBody
}

// getAgentV2TeamWithStatus issues GET /api/v2/.../team and returns the HTTP
// status with the raw body — the materialization probe (200 with the Team,
// 404 while unmaterialized, team-api.md §1/§3).
func getAgentV2TeamWithStatus(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, sessionName string) (int, []byte) {
	t.Helper()

	reqURL := fmt.Sprintf("%s%s%s/team", sutHostURL, agentV2PathPrefix, sessionName)
	resp, respBody := doHTTPTrace(t, ctx, http.MethodGet, reqURL, sutEnvName, nil)
	return resp.StatusCode, respBody
}

// getAgentV2Team fetches the session's materialized team. Calls t.Fatal on
// non-200 responses (404 means not materialized — use
// getAgentV2TeamWithStatus to observe that branch).
func getAgentV2Team(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, sessionName string) *game.Team {
	t.Helper()

	reqURL := fmt.Sprintf("%s%s%s/team", sutHostURL, agentV2PathPrefix, sessionName)
	resp, respBody := doHTTPTrace(t, ctx, http.MethodGet, reqURL, sutEnvName, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET team status=%d, body=%s", resp.StatusCode, respBody)
	}
	team := new(game.Team)
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(respBody, team); err != nil {
		t.Fatalf("Unmarshal Team: %v (raw: %s)", err, respBody)
	}
	return team
}

// teamActiveMember reads the materialized team's output-only active_member —
// the single merged value
// (specs/060-agent-v2-team-optimize/contracts/team-api.md §1): the driving
// member while a member turn is in flight, else the activation owning the
// next input (initial planner; cancel/pause does not change it).
func teamActiveMember(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, sessionName string) string {
	t.Helper()

	return getAgentV2Team(t, ctx, sutHostURL, sutEnvName, sessionName).GetActiveMember()
}

// getAgentV2TeamMemberWithStatus issues GET .../team/members/{member} and
// returns the HTTP status with the parsed member (nil unless the body decodes
// as a TeamMember) — the fixed-roster probe (team-api.md §1).
func getAgentV2TeamMemberWithStatus(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, sessionName, member string) (*game.TeamMember, int, []byte) {
	t.Helper()

	reqURL := fmt.Sprintf("%s%s", sutHostURL, agentV2PathPrefix)
	reqURL += strings.TrimPrefix(agentV2MemberName(sessionName, member), "/")
	resp, respBody := doHTTPTrace(t, ctx, http.MethodGet, reqURL, sutEnvName, nil)
	parsed := new(game.TeamMember)
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(respBody, parsed); err != nil {
		t.Logf("TeamMember body (status %d) is not a response proto: %s", resp.StatusCode, respBody)
		return nil, resp.StatusCode, respBody
	}
	return parsed, resp.StatusCode, respBody
}

// getAgentV2TeamMember fetches one fixed member. Calls t.Fatal on non-200.
func getAgentV2TeamMember(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, sessionName, member string) *game.TeamMember {
	t.Helper()

	parsed, status, respBody := getAgentV2TeamMemberWithStatus(t, ctx, sutHostURL, sutEnvName, sessionName, member)
	if status != http.StatusOK {
		t.Fatalf("GET team member %s status=%d, body=%s", member, status, respBody)
	}
	return parsed
}

// listTeamMessagesWithStatus issues GET .../team/messages and returns the
// HTTP status with the raw body (404 while unmaterialized, team-api.md §5).
func listTeamMessagesWithStatus(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, sessionName string) (int, []byte) {
	t.Helper()

	reqURL := fmt.Sprintf("%s%s%s/team/messages", sutHostURL, agentV2PathPrefix, sessionName)
	resp, respBody := doHTTPTrace(t, ctx, http.MethodGet, reqURL, sutEnvName, nil)
	return resp.StatusCode, respBody
}

// listTeamMessagesResponse issues GET .../team/messages and returns the
// parsed response envelope — the raw ListTeamMessages read for assertions
// that need the wrapper (the pagination compat slot: next_page_token stays
// empty, the List face returns the whole sequence; team-api.md §5). Calls
// t.Fatal on non-200 responses.
func listTeamMessagesResponse(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, sessionName string) *game.ListTeamMessagesResponse {
	t.Helper()

	reqURL := fmt.Sprintf("%s%s%s/team/messages", sutHostURL, agentV2PathPrefix, sessionName)
	resp, respBody := doHTTPTrace(t, ctx, http.MethodGet, reqURL, sutEnvName, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET team messages status=%d, body=%s", resp.StatusCode, respBody)
	}
	listed := new(game.ListTeamMessagesResponse)
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(respBody, listed); err != nil {
		t.Fatalf("Unmarshal ListTeamMessagesResponse: %v (raw: %s)", err, respBody)
	}
	return listed
}

// listTeamMessages fetches the merged team sequence (ListTeamMessages,
// team-api.md §5). Calls t.Fatal on non-200 responses.
func listTeamMessages(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, sessionName string) []*game.TeamMessage {
	t.Helper()

	return listTeamMessagesResponse(t, ctx, sutHostURL, sutEnvName, sessionName).GetMessages()
}

// listMemberMessagesWithStatus issues GET .../team/members/{member}/messages
// and returns the HTTP status with the raw body.
func listMemberMessagesWithStatus(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, sessionName, member string) (int, []byte) {
	t.Helper()

	reqURL := fmt.Sprintf("%s%s", sutHostURL, agentV2PathPrefix)
	reqURL += strings.TrimPrefix(agentV2MemberName(sessionName, member), "/") + "/messages"
	resp, respBody := doHTTPTrace(t, ctx, http.MethodGet, reqURL, sutEnvName, nil)
	return resp.StatusCode, respBody
}

// listMemberMessagesResponse issues GET .../team/members/{member}/messages
// and returns the parsed response envelope — the raw ListMemberMessages read
// for assertions that need the wrapper (the pagination compat slot:
// next_page_token stays empty; team-api.md §5). Calls t.Fatal on non-200
// responses.
func listMemberMessagesResponse(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, sessionName, member string) *game.ListMemberMessagesResponse {
	t.Helper()

	reqURL := fmt.Sprintf("%s%s", sutHostURL, agentV2PathPrefix)
	reqURL += strings.TrimPrefix(agentV2MemberName(sessionName, member), "/") + "/messages"
	resp, respBody := doHTTPTrace(t, ctx, http.MethodGet, reqURL, sutEnvName, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET member messages (%s) status=%d, body=%s", member, resp.StatusCode, respBody)
	}
	listed := new(game.ListMemberMessagesResponse)
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(respBody, listed); err != nil {
		t.Fatalf("Unmarshal ListMemberMessagesResponse: %v (raw: %s)", err, respBody)
	}
	return listed
}

// listMemberMessages fetches one member's view history (ListMemberMessages,
// team-api.md §5). Calls t.Fatal on non-200 responses.
func listMemberMessages(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, sessionName, member string) []*game.MemberViewMessage {
	t.Helper()

	return listMemberMessagesResponse(t, ctx, sutHostURL, sutEnvName, sessionName, member).GetMessages()
}

// postTeamCancel issues POST /api/v2/{team}:cancel (AIP-136, team-api.md §4)
// and returns the HTTP status with the raw body: 200 on success — both the
// terminating and the idempotent no-op cancel — 404 for a never-materialized
// session, 400 FAILED_PRECONDITION for an owner without a materialized team.
func postTeamCancel(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, sessionName string) (int, []byte) {
	t.Helper()

	reqURL := fmt.Sprintf("%s%s%s/team:cancel", sutHostURL, agentV2PathPrefix, sessionName)
	resp, respBody := doHTTPTrace(t, ctx, http.MethodPost, reqURL, sutEnvName, []byte("{}"))
	return resp.StatusCode, respBody
}

// teamMemberByRole returns the team's member with the given scene role string
// ("player" / "planner"), or nil.
func teamMemberByRole(team *game.Team, role string) *game.TeamMember {
	for _, member := range team.GetMembers() {
		if member.GetRole() == role {
			return member
		}
	}
	return nil
}

// teamMemberPreset returns one scene member's preset snapshot from the
// members list (the generalized Team shape — the former player_preset /
// planner_preset scalar fields are gone, team-api.md §2). Empty when the role
// is absent.
func teamMemberPreset(team *game.Team, role string) string {
	if member := teamMemberByRole(team, role); member != nil {
		return member.GetPreset()
	}
	return ""
}

// teamMemberModel returns one scene member's effective model (members-list
// accessor, see teamMemberPreset). Empty when the role is absent.
func teamMemberModel(team *game.Team, role string) string {
	if member := teamMemberByRole(team, role); member != nil {
		return member.GetModel()
	}
	return ""
}

// waitTeamDesktopConnected polls GetTeam until the session's desktop bridge
// fact reads true (the deployed fake-desktop executors re-dial after their
// injected fault cycles, so a game case must not race the reconnect).
func waitTeamDesktopConnected(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, sessionName string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		if getAgentV2Team(t, ctx, sutHostURL, sutEnvName, sessionName).GetDesktopConnected() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("desktop did not reconnect for %s within %s", sessionName, timeout)
		}
		time.Sleep(time.Second)
	}
}

// ─── Preset helpers (the stateless configuration face) ──────────────────────

// createAgentV2TeamPreset creates a preset in the given pool (role REQUIRED on
// create, preset-api.md §2). The body:"preset" binding carries only the
// resource; the caller-supplied id and the role ride the URI query parameters
// (AIP-133). Calls t.Fatal on non-200 responses.
func createAgentV2TeamPreset(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, presetID, persona string, role string) *game.Preset {
	t.Helper()

	preset, status, respBody := createAgentV2PresetWithRole(t, ctx, sutHostURL, sutEnvName, presetID, persona, role)
	if status != http.StatusOK {
		t.Fatalf("POST create preset status=%d, body=%s", status, respBody)
	}
	return preset
}

// createAgentV2TeamPresetPair creates one player-pool and one planner-pool
// preset for a team materialization. Both personas carry the role's
// system-prompt anchor (你是扫雷 player / 你是扫雷 planner) so the fake-llm
// team fixtures match the materialized members (the cross-phase stable
// contract, specs/059-agent-v2-team-mode/tasks.md T004/T011). Calls t.Fatal
// on non-200 responses.
func createAgentV2TeamPresetPair(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, idPrefix, marker string) (*game.Preset, *game.Preset) {
	t.Helper()

	player := createAgentV2TeamPreset(t, ctx, sutHostURL, sutEnvName, idPrefix+"-player-"+uniqueSuffix(),
		"你是扫雷 player，"+marker, "player")
	planner := createAgentV2TeamPreset(t, ctx, sutHostURL, sutEnvName, idPrefix+"-planner-"+uniqueSuffix(),
		"你是扫雷 planner，"+marker, "planner")
	return player, planner
}

// createAgentV2Preset creates a player-pool preset (the common case for the
// single-purpose fixtures). Calls t.Fatal on non-200 responses.
func createAgentV2Preset(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, presetID, persona string) *game.Preset {
	t.Helper()

	return createAgentV2TeamPreset(t, ctx, sutHostURL, sutEnvName, presetID, persona, "player")
}

// createAgentV2PresetWithStatus is the role-aware preset creation without the
// 200 fatality: it returns the HTTP status with the parsed resource (nil
// unless the body decodes as a Preset) — used to assert the 409
// ALREADY_EXISTS and the missing-role 400 (preset-api.md §2).
func createAgentV2PresetWithRole(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, presetID, persona string, role string) (*game.Preset, int, []byte) {
	t.Helper()

	body, err := protojson.Marshal(&game.Preset{Persona: persona})
	if err != nil {
		t.Fatalf("protojson.Marshal Preset: %v", err)
	}
	reqURL := fmt.Sprintf("%s%stemplates/%s/presets?preset_id=%s&role=%s",
		sutHostURL, agentV2PathPrefix, saoleiTemplateID,
		url.QueryEscape(presetID),
		url.QueryEscape(role))
	resp, respBody := doHTTPTrace(t, ctx, http.MethodPost, reqURL, sutEnvName, body)
	created := new(game.Preset)
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(respBody, created); err != nil {
		t.Logf("Preset body (status %d) is not a response proto: %s", resp.StatusCode, respBody)
		return nil, resp.StatusCode, respBody
	}
	return created, resp.StatusCode, respBody
}

// postAgentV2PresetCreate issues a raw preset-create request with the given
// query values (roleQuery empty = the parameter is omitted) and returns the
// HTTP status with the raw body — the role-vocabulary reject path
// (missing/unknown/empty role; preset-api.md §2).
func postAgentV2PresetCreate(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, presetID, persona, roleQuery string) (int, []byte) {
	t.Helper()

	body, err := protojson.Marshal(&game.Preset{Persona: persona})
	if err != nil {
		t.Fatalf("protojson.Marshal Preset: %v", err)
	}
	reqURL := fmt.Sprintf("%s%stemplates/%s/presets?preset_id=%s",
		sutHostURL, agentV2PathPrefix, saoleiTemplateID, url.QueryEscape(presetID))
	if roleQuery != "" {
		reqURL += "&role=" + url.QueryEscape(roleQuery)
	}
	resp, respBody := doHTTPTrace(t, ctx, http.MethodPost, reqURL, sutEnvName, body)
	return resp.StatusCode, respBody
}

// listAgentV2Presets issues GET /api/v2/templates/saolei/presets and returns
// the parsed ListPresetsResponse. Calls t.Fatal on non-200 responses.
func listAgentV2Presets(t *testing.T, ctx context.Context, sutHostURL, sutEnvName string) *game.ListPresetsResponse {
	t.Helper()

	return listAgentV2PresetsByRole(t, ctx, sutHostURL, sutEnvName, "")
}

// listAgentV2PresetsByRole is listAgentV2Presets with an optional role filter
// (empty = the query parameter is omitted — no filtering; preset-api.md §1)
// and the 200 fatality. The filter value is a scene role string
// ("player" / "planner").
func listAgentV2PresetsByRole(t *testing.T, ctx context.Context, sutHostURL, sutEnvName string, role string) *game.ListPresetsResponse {
	t.Helper()

	resp, respBody := listAgentV2PresetsWithStatus(t, ctx, sutHostURL, sutEnvName, role)
	if resp != http.StatusOK {
		t.Fatalf("GET list presets status=%d, body=%s", resp, respBody)
	}
	list := new(game.ListPresetsResponse)
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(respBody, list); err != nil {
		t.Fatalf("Unmarshal ListPresetsResponse: %v (raw: %s)", err, respBody)
	}
	return list
}

// listAgentV2PresetsWithStatus issues the preset list with the raw role query
// value (empty = omitted) and returns the HTTP status with the raw body — the
// role-vocabulary filter reject path (preset-api.md §1).
func listAgentV2PresetsWithStatus(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, role string) (int, []byte) {
	t.Helper()

	reqURL := fmt.Sprintf("%s%stemplates/%s/presets", sutHostURL, agentV2PathPrefix, saoleiTemplateID)
	if role != "" {
		reqURL += "?role=" + url.QueryEscape(role)
	}
	resp, respBody := doHTTPTrace(t, ctx, http.MethodGet, reqURL, sutEnvName, nil)
	return resp.StatusCode, respBody
}

// getAgentV2PresetWithStatus issues GET /api/v2/{name} for a preset and
// returns the HTTP status with the parsed resource — the existence probe
// (200 while stored, 404 after delete; preset-api.md §1).
func getAgentV2PresetWithStatus(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, name string) (*game.Preset, int) {
	t.Helper()

	reqURL := fmt.Sprintf("%s%s%s", sutHostURL, agentV2PathPrefix, name)
	resp, respBody := doHTTPTrace(t, ctx, http.MethodGet, reqURL, sutEnvName, nil)
	preset := new(game.Preset)
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(respBody, preset); err != nil {
		t.Logf("Preset body (status %d) is not a response proto: %s", resp.StatusCode, respBody)
		return nil, resp.StatusCode
	}
	return preset, resp.StatusCode
}

// getAgentV2Preset is getAgentV2PresetWithStatus with the 200 fatality.
func getAgentV2Preset(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, name string) *game.Preset {
	t.Helper()

	preset, status := getAgentV2PresetWithStatus(t, ctx, sutHostURL, sutEnvName, name)
	if status != http.StatusOK {
		t.Fatalf("GET preset %s status=%d, want 200", name, status)
	}
	return preset
}

// updateAgentV2Preset patches a preset's persona through the gateway
// (PATCH /api/v2/{name}?update_mask=persona, AIP-134; persona is the only
// mutable field, preset-api.md §2). Calls t.Fatal on non-200 responses.
func updateAgentV2Preset(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, name, persona string) *game.Preset {
	t.Helper()

	body, err := protojson.Marshal(&game.Preset{Persona: persona})
	if err != nil {
		t.Fatalf("protojson.Marshal Preset: %v", err)
	}
	reqURL := fmt.Sprintf("%s%s%s?update_mask=persona", sutHostURL, agentV2PathPrefix, name)
	resp, respBody := doHTTPTrace(t, ctx, http.MethodPatch, reqURL, sutEnvName, body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH update preset status=%d, body=%s", resp.StatusCode, respBody)
	}
	updated := new(game.Preset)
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(respBody, updated); err != nil {
		t.Fatalf("Unmarshal Preset: %v (raw: %s)", err, respBody)
	}
	return updated
}

// deleteAgentV2PresetWithStatus issues DELETE /api/v2/{name} for a preset and
// returns the HTTP status with the raw body (no fan-out to materialized teams,
// preset-api.md §5).
func deleteAgentV2PresetWithStatus(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, name string) (int, []byte) {
	t.Helper()

	reqURL := fmt.Sprintf("%s%s%s", sutHostURL, agentV2PathPrefix, name)
	resp, respBody := doHTTPTrace(t, ctx, http.MethodDelete, reqURL, sutEnvName, nil)
	return resp.StatusCode, respBody
}

// listAgentV2Models fetches the deployment-level model catalog
// (GET /api/v2/models, preset-api.md §3) and returns the parsed response.
// Calls t.Fatal on non-200 responses.
func listAgentV2Models(t *testing.T, ctx context.Context, sutHostURL, sutEnvName string) *game.ListModelsResponse {
	t.Helper()

	reqURL := fmt.Sprintf("%s%smodels", sutHostURL, agentV2PathPrefix)
	resp, respBody := doHTTPTrace(t, ctx, http.MethodGet, reqURL, sutEnvName, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET list models status=%d, body=%s", resp.StatusCode, respBody)
	}
	list := new(game.ListModelsResponse)
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(respBody, list); err != nil {
		t.Fatalf("Unmarshal ListModelsResponse: %v (raw: %s)", err, respBody)
	}
	return list
}

// ─── Team arrange helpers ───────────────────────────────────────────────────

// ensureAgentV2Session creates a caller-id session, tolerating an
// ALREADY_EXISTS (409) from a prior case in the same deployment — the session
// resource itself is what matters, not who created it. Returns the full
// /api/v2 session resource name.
func ensureAgentV2Session(t *testing.T, sutHostURL, sutEnvName, sessionID string) string {
	t.Helper()

	reqURL := fmt.Sprintf("%s%stemplates/%s/sessions?session_id=%s",
		sutHostURL, pathPrefix, saoleiTemplateID, url.QueryEscape(sessionID))
	resp, respBody := doHTTP(t, http.MethodPost, reqURL, sutEnvName, []byte("{}"))
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusConflict:
		if status, _ := getSessionWithStatus(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID); status != http.StatusOK {
			t.Fatalf("session %s reported already exists but is not readable: status=%d body=%s", sessionID, status, respBody)
		}
	default:
		t.Fatalf("POST ensure session %s status=%d, body=%s", sessionID, resp.StatusCode, respBody)
	}
	return agentV2SessionName(sessionID)
}

// teamPrep is the team suites' arrange step: ensure the session exists, create
// its player/planner preset pair, and materialize the team (UpdateTeam,
// team-api.md §2). Returns the context, the session resource name, and the
// materialized Team.
func teamPrep(t *testing.T, sutHostURL, sutEnvName, sessionID, marker string) (context.Context, string, *game.Team) {
	t.Helper()

	ctx := traceContext(t)
	sessionName := ensureAgentV2Session(t, sutHostURL, sutEnvName, sessionID)
	playerPreset, plannerPreset := createAgentV2TeamPresetPair(t, ctx, sutHostURL, sutEnvName, "team-"+marker, marker)
	team := updateAgentV2Team(t, ctx, sutHostURL, sutEnvName, sessionName,
		playerPreset.GetName(), plannerPreset.GetName(), "", "")
	return ctx, sessionName, team
}

// teamPlayerActivation materializes a desktop-less team and runs its opening
// cycle — the first Send drives the planner's opening strategy, the
// structural continuation drives the player whose saolei_init fails with the
// desktop-absent error. The cycle leaves the player as the orchestrator's
// current activation, so a following Send drives the player directly (the
// member-turn fixtures' player-scope tests). Returns the context and the
// session resource name.
func teamPlayerActivation(t *testing.T, sutHostURL, sutEnvName, sessionID, marker string) (context.Context, string) {
	t.Helper()

	ctx, sessionName, _ := teamPrep(t, sutHostURL, sutEnvName, sessionID, marker)
	stream := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, teamStartMessage)
	events := drainTeamStream(t, stream)
	assertTeamStreamWellFormed(t, sessionName, events)
	turns := groupTeamMemberTurns(events)
	if len(turns) == 0 || turns[0].member != "planner" {
		t.Fatalf("opening cycle turns = %v, want the planner first", turns)
	}
	return ctx, sessionName
}

// ─── Flow-control stream helpers (the desktop's side) ───────────────────────

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
// session identity (gateway-injected values are authoritative —
// desktop-bridge.md §1) with the ACTIVE status signal the bridge echoes back.
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
// to completion (probe frame out, status echo back). Returns the live
// connection and the echo frame.
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

// sendFlowReceiptNoFatal writes the operation receipt the desktop half owes
// the bridge: the dispatched tool_id echoed back with the given status and,
// on success, the screenshot the agent's recognizer consumes
// (desktop-bridge.md §1 FlowResultPart row). The flow-script goroutine uses
// this variant because t.Fatal must only run on the test goroutine.
func sendFlowReceiptNoFatal(conn *websocket.Conn, sessionID, toolID string, status game.ToolResultStatus, screenshotPNG []byte) error {
	result := &game.FlowResultPart{ToolId: toolID, Status: status}
	if screenshotPNG != nil {
		result.Screenshot = &game.ImagePart{
			Encoding: game.ImageEncoding_IMAGE_ENCODING_PNG,
			Data:     screenshotPNG,
		}
	}
	frame := &game.UserFrame{
		SessionId:  sessionID,
		TemplateId: saoleiTemplateID,
		Payload: &game.UserFrame_FlowParts{FlowParts: &game.FlowParts{Parts: []*game.FlowPart{{
			Kind: &game.FlowPart_FlowResult{FlowResult: result},
		}}}},
	}
	data, err := proto.Marshal(frame)
	if err != nil {
		return fmt.Errorf("marshal flow receipt: %w", err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
		return fmt.Errorf("write flow receipt: %w", err)
	}
	return nil
}

// replyFlowReceipt is sendFlowReceiptNoFatal on the test goroutine.
func replyFlowReceipt(t *testing.T, conn *websocket.Conn, sessionID, toolID string, status game.ToolResultStatus, screenshotPNG []byte) {
	t.Helper()

	if err := sendFlowReceiptNoFatal(conn, sessionID, toolID, status, screenshotPNG); err != nil {
		t.Fatalf("reply flow receipt: %v", err)
	}
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

// teamFlowScript is the receipt sequence the test's desktop half serves: each
// F2 keyboard dispatch consumes the next initBoards entry, each cell dispatch
// the next stepBoards entry. The script ends when both lists are exhausted.
type teamFlowScript struct {
	initBoards [][]byte
	stepBoards [][]byte
}

// serveTeamFlowScript reads flow frames and answers every dispatch per the
// script, then reports nil on the returned channel. It runs on its own
// goroutine so the Send-stream drain can proceed concurrently; errors are
// reported instead of calling t.Fatal.
func serveTeamFlowScript(conn *websocket.Conn, sessionID string, script teamFlowScript, timeout time.Duration) <-chan error {
	ch := make(chan error, 1)
	go func() {
		initIdx, stepIdx := 0, 0
		for initIdx < len(script.initBoards) || stepIdx < len(script.stepBoards) {
			frame, err := readFlowTeamFrameNoFatal(conn, timeout)
			if err != nil {
				ch <- fmt.Errorf("read flow dispatch: %w", err)
				return
			}
			for _, part := range frame.GetFlowParts().GetParts() {
				switch {
				case part.GetKeyboardPress() != nil:
					if initIdx >= len(script.initBoards) {
						ch <- fmt.Errorf("unexpected keyboard dispatch after %d init replies", initIdx)
						return
					}
					if err := sendFlowReceiptNoFatal(conn, sessionID, part.GetKeyboardPress().GetToolId(), game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED, script.initBoards[initIdx]); err != nil {
						ch <- err
						return
					}
					initIdx++
				case part.GetMouseMoveAndClick() != nil:
					if stepIdx >= len(script.stepBoards) {
						ch <- fmt.Errorf("unexpected cell dispatch after %d step replies", stepIdx)
						return
					}
					if err := sendFlowReceiptNoFatal(conn, sessionID, part.GetMouseMoveAndClick().GetToolId(), game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED, script.stepBoards[stepIdx]); err != nil {
						ch <- err
						return
					}
					stepIdx++
				}
			}
		}
		ch <- nil
	}()
	return ch
}

// waitTeamFlowScript waits for the flow script to serve every scheduled
// receipt within the shared read window.
func waitTeamFlowScript(t *testing.T, ch <-chan error, timeout time.Duration) {
	t.Helper()

	select {
	case err := <-ch:
		if err != nil {
			t.Fatalf("flow script: %v", err)
		}
	case <-time.After(timeout):
		t.Fatal("flow script did not serve every scheduled receipt")
	}
}
