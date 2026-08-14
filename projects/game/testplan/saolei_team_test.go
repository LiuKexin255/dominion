// Package testplan contains the saolei TEAM large-test suite.
//
// saolei_team_test.go validates the saolei template's team behaviour
// end-to-end against the deployed stack (specs/031-team-template-mode
// quickstart.md §2.1 case table; spec FR-030): team connect + lifecycle
// (FR-003/FR-004/FR-033), TeamProfile CRUD (FR-006/FR-027), per-TeamProfile
// model resolution, player-exclusive desktop control (FR-010), planner
// trigger exactly once per game end (FR-011/D6), planner long-term memory
// persistence via the memory service + the `memory` tool agent conversion
// (specs/039-planner-memory-calibration FR-006..FR-012), the calibration
// instruction scenarios + message order (FR-014..FR-017/FR-019),
// RefreshTeam short-term clearing + the post-refresh instruction turn
// (FR-018 / 042 US3 — specs/042-planner-memory-fixup/contracts/
// refresh-instruction-trigger.md §2.3), and per-agent message
// partitioning (FR-005). The shared-strategy flow (update_strategy /
// StrategyStore, 031 FR-012..FR-015) is GONE — Phase 6 of spec 039 removed
// it (FR-013) and this suite asserts its absence (SC-005).
//
// The 041 real-time init delivery (specs/041-realtime-init-push — quickstart
// §B): the init instruction frames pushed through the live Connect stream
// with no user message (B1/B3 — FR-001/FR-006, frameId == messageId),
// the status probe reporting IDLE during the init turn (B2 — FR-003),
// RefreshTeam / profile-change rebuild rejected FAILED_PRECONDITION while
// the init is in flight (B4 — FR-007), and the no-duplicate re-entry after
// the init completed (B5 — FR-004).
//
// A "game" in these tests is one full team turn driven by the fake-LLM
// "saolei-start" keyword: the player agent's saolei_init recognizes an
// IN-PROGRESS board (contract saolei-sink-contract.md §3 — onGameStart only,
// no game-end detection on init), then its first saolei_operate batch reply
// carries a TERMINAL won/lost board — the batch's post-dispatch recognition
// fires onOperate + onGameEnd (the sink records the end event), the
// conditional edge routes to the planner exactly once, the planner runs the
// deterministic memory→memory→instruct_player review chain
// (sample_planner_memory.yaml / sample_planner_tools.yaml — one batch add,
// a 0-hit replace error, a multi-hit replace error, then the review
// instruction), and the graph loops back through the player, whose stateless
// fake-LLM re-matches saolei-start and opens one more in-progress game (no
// end event) so the turn converges to wait. The helper playTeamGameUntilWait
// drives the desktop role (replying to every dispatched operation) until the
// terminal wait frame; it lives in helpers_test.go (shared with the memory
// module suite, style/large_test.md §反模式3).
package testplan

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"dominion/common/gopkg/testtool"
	game "dominion/projects/game"

	"github.com/gorilla/websocket"
)

// findPlannerReviewFrame returns the content of the FIRST real-time frame
// carrying the planner's review input (the "本局游戏过程" HumanMessage emitted
// as a live frame by US1 — specs/037-saolei-team-optimize FR-001), or ""
// when no such frame was seen.
func findPlannerReviewFrame(frames []*game.TeamFrame) string {
	for _, f := range frames {
		if f.GetAgent() != "planner" || !frameHasText(f) {
			continue
		}
		if text := frameText(f); strings.Contains(text, reviewInputPrefix) {
			return text
		}
	}
	return ""
}

// findPlannerReviewMessage returns the content of the FIRST planner-partition
// Message carrying the review input (the reloaded ListMessages view of the
// same message — FR-002/FR-003), or "" when none was found.
func findPlannerReviewMessage(messages []*game.Message) string {
	for _, m := range messages {
		if text := messageText(m); strings.Contains(text, reviewInputPrefix) {
			return text
		}
	}
	return ""
}

// findInstructPlayerCallContent returns the content argument of the FIRST
// instruct_player tool_call whose content equals want, or "" when none
// exists. The planner partition legitimately holds instruct_player calls
// from MULTIPLE scenarios (the async init turn's write-back precedes later
// review/compact calls — 039 FR-015), so tests match by content rather than
// position. Used to assert the calibration instructions (review / init /
// compact scenarios — spec 039 FR-014/FR-015/FR-016) reached the expected
// channel with the fixture's content. Panics on a malformed args_json — a
// bug in the test itself.
func findInstructPlayerCallContent(messages []*game.Message, want string) string {
	for _, m := range messages {
		if args := messageToolCallArgsJSON(m, "instruct_player"); args != "" {
			var parsed map[string]any
			if err := json.Unmarshal([]byte(args), &parsed); err != nil {
				panic(fmt.Sprintf("instruct_player args_json is not a JSON object: %q", args))
			}
			content, _ := parsed["content"].(string)
			if content == want {
				return content
			}
		}
	}
	return ""
}

// messageIndex returns the index of the first Message whose content carries
// the given substring, or -1 when none does.
func messageIndex(messages []*game.Message, substring string) int {
	for i, m := range messages {
		if strings.Contains(messageText(m), substring) {
			return i
		}
		for _, p := range m.GetContent().GetParts() {
			if tr := p.GetToolResult(); tr != nil && strings.Contains(tr.GetMessage(), substring) {
				return i
			}
		}
	}
	return -1
}

// ─── Team connect + lifecycle (FR-003/FR-004/FR-033) ──────────────────────

// TestTeamConnectLifecycle verifies the team lifecycle contract
// (specs/040-team-singleton-conformance/contracts/api-contract.md §2.3):
// GetTeam/ListMessages on a session whose team was NOT materialized return
// NOT_FOUND (no lazy creation, FR-003); UpdateTeam(allow_missing=true)
// returns the Team resource whose agents come from the template schema
// ([player, accepts_user_input=true], [planner, accepts_user_input=false] —
// D3/FR-031); repeated UpdateTeam is idempotent for the SAME profile (FR-002)
// and rebuilds the team graph for a DIFFERENT profile (FR-005); after
// materialization the WebSocket Connect works and a text turn completes.
// The one-shot async initInstruction turn (039 FR-015 — triggered once after
// first materialization) delivers its instruction with the player's first
// activation; the greeting keyword still matches deterministically (the
// instruction is an extra USER message, not the last user message).
func TestTeamConnectLifecycle(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileID := fmt.Sprintf("team-conn-%s", uniqueSuffix())
	otherProfileID := fmt.Sprintf("team-conn-other-%s", uniqueSuffix())

	// given: a session with NO team materialized.
	sessionID, _ := createSession(t, sutHostURL, sutEnvName, saoleiTemplateID)

	// then: GetTeam and ListMessages require an existing team (FR-003 — no
	// implicit/lazy creation on read paths).
	if status, _ := getTeamWithStatus(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID); status != http.StatusNotFound {
		t.Errorf("GetTeam before UpdateTeam: status=%d, want 404 NOT_FOUND (FR-003)", status)
	}
	if status, _ := listMessagesWithStatus(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "player"); status != http.StatusNotFound {
		t.Errorf("ListMessages before UpdateTeam: status=%d, want 404 NOT_FOUND (FR-003)", status)
	}

	// given: two saolei TeamProfiles (the second backs the rebuild case).
	createTeamProfile(t, sutHostURL, sutEnvName, saoleiTemplateID, profileID, "gpt-4", "gpt-4")
	createTeamProfile(t, sutHostURL, sutEnvName, saoleiTemplateID, otherProfileID, "gpt-4", "gpt-4")

	// when: UpdateTeam(allow_missing=true) (the ONLY Team creation point,
	// AIP-134 create-or-update).
	team := updateTeam(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, profileID)

	// then: the Team resource carries the session-scoped name, the materialized
	// profile and the template-schema agents (D3 — typed, not hard-coded).
	wantName := fmt.Sprintf("templates/%s/sessions/%s/team", saoleiTemplateID, sessionID)
	if team.GetName() != wantName {
		t.Errorf("Team.name = %q, want %q", team.GetName(), wantName)
	}
	wantProfile := fmt.Sprintf("templates/%s/profiles/%s", saoleiTemplateID, profileID)
	if team.GetProfile() != wantProfile {
		t.Errorf("Team.profile = %q, want %q (FR-004)", team.GetProfile(), wantProfile)
	}
	if len(team.GetAgents()) != 2 {
		t.Fatalf("Team.agents = %d entries, want 2 (player+planner)", len(team.GetAgents()))
	}
	agentByName := map[string]bool{}
	for _, a := range team.GetAgents() {
		agentByName[a.GetName()] = a.GetAcceptsUserInput()
	}
	if agentByName["player"] != true {
		t.Errorf("Team.agents player accepts_user_input = %v, want true (FR-031)", agentByName["player"])
	}
	if agentByName["planner"] != false {
		t.Errorf("Team.agents planner accepts_user_input = %v, want false (FR-031)", agentByName["planner"])
	}

	// then: GetTeam returns the same resource.
	fetched := getTeam(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	if fetched.GetName() != wantName {
		t.Errorf("GetTeam name = %q, want %q", fetched.GetName(), wantName)
	}
	if len(fetched.GetAgents()) != 2 {
		t.Errorf("GetTeam agents = %d entries, want 2", len(fetched.GetAgents()))
	}

	// then: repeated UpdateTeam with the SAME profile is idempotent (returns
	// the existing team — FR-002); a DIFFERENT profile rebuilds the team
	// graph and succeeds (FR-005, no ALREADY_EXISTS — FR-007). The rebuild
	// must NOT re-run the initInstruction turn (040 FR-005 — init runs once
	// at first materialization only), which the session-team unit tests pin.
	if status, _ := updateTeamWithStatus(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, profileID); status != http.StatusOK {
		t.Errorf("repeated UpdateTeam (same profile): status=%d, want 200 idempotent (FR-002)", status)
	}
	if status, body := updateTeamWithStatus(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, otherProfileID); status != http.StatusOK {
		t.Errorf("repeated UpdateTeam (different profile): status=%d, want 200 rebuild success, body=%s (FR-005)", status, body)
	}
	rebuilt := getTeam(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	if rebuilt.GetProfile() != fmt.Sprintf("templates/%s/profiles/%s", saoleiTemplateID, otherProfileID) {
		t.Errorf("GetTeam after rebuild profile = %q, want %q (FR-005)", rebuilt.GetProfile(), fmt.Sprintf("templates/%s/profiles/%s", saoleiTemplateID, otherProfileID))
	}

	// then: Connect works after materialization and a text turn completes
	// (the team graph answers through the real pipeline).
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	sendText(t, conn, sessionID, "hello team lifecycle")
	thinkingFrame := drainWSFrame(t, conn, func(f *game.TeamFrame) bool { return frameHasThinking(f) })
	if thinkingFrame == nil {
		t.Fatal("did not receive a thinking frame after UpdateTeam+Connect")
	}
	if !strings.Contains(frameThinking(thinkingFrame), expectedGreetingReasoning) {
		t.Errorf("thinking = %q, want to contain %q", frameThinking(thinkingFrame), expectedGreetingReasoning)
	}
}

// TestTeamUpdateRebuildInFlightRejected verifies FR-006: a profile-change
// UpdateTeam is rejected with 400 FAILED_PRECONDITION while a turn is
// in-flight (the per-session turn mutex is held — same guard as RefreshTeam),
// and the existing team plus the in-flight turn are unaffected
// (specs/040-team-singleton-conformance/quickstart.md 场景 3). The gateway
// uses grpc-gateway's default error mapping (no custom error handler in
// projects/game/gateway/cmd/main.go), which maps codes.FailedPrecondition →
// HTTP 400 (https://github.com/grpc-ecosystem/grpc-gateway/blob/v2.27.6/runtime/errors.go#L58-L60);
// 409 is reserved for AlreadyExists/Aborted.
func TestTeamUpdateRebuildInFlightRejected(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileID := fmt.Sprintf("team-rebuild-%s", uniqueSuffix())
	otherProfileID := fmt.Sprintf("team-rebuild-other-%s", uniqueSuffix())

	// given: a materialized team (profile=P1) and a second profile to switch
	// to.
	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileID, "gpt-4", "gpt-4")
	createTeamProfile(t, sutHostURL, sutEnvName, saoleiTemplateID, otherProfileID, "gpt-4", "gpt-4")

	// given: a turn in-flight — the saolei_init dispatch blocks the turn
	// until the test (playing desktop) replies with a FlowResultPart (same
	// in-flight window as TestTeamStatusPingPong).
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()
	sendText(t, conn, sessionID, "please start saolei game")
	opFrame := readOperationFrame(t, conn)
	if frameKeyboardPress(opFrame) == nil {
		t.Fatalf("saolei_init did not dispatch a KeyboardPressPart FlowPart")
	}

	// when: a profile-change UpdateTeam while the turn is in-flight.
	status, body := updateTeamWithStatus(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, otherProfileID)

	// then: 400 FAILED_PRECONDITION (FR-006) and the existing team is
	// untouched (no half-rebuilt state).
	if status != http.StatusBadRequest {
		t.Errorf("in-flight UpdateTeam (different profile): status=%d, want 400 FAILED_PRECONDITION, body=%s (FR-006)", status, body)
	}
	team := getTeam(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	if team.GetProfile() != fmt.Sprintf("templates/%s/profiles/%s", saoleiTemplateID, profileID) {
		t.Errorf("team profile after rejected rebuild = %q, want %q (existing team unchanged, FR-006)",
			team.GetProfile(), fmt.Sprintf("templates/%s/profiles/%s", saoleiTemplateID, profileID))
	}

	// then: the in-flight turn completes unaffected — reply to every
	// dispatched operation (init + the operate batch's two clicks) and drain
	// to the final text.
	screenshot := buildSaoleiFlowResultScreenshot(saoleiBoardInitPNG)
	respondToOperationWithScreenshot(t, conn, sessionID, opFrame,
		game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED, "F2 pressed, new game started", screenshot)
	for i := 0; i < 2; i++ {
		clickFrame := readOperationFrame(t, conn)
		if frameMouseMoveAndClick(clickFrame) == nil {
			t.Fatalf("expected a saolei_operate dispatch, got: %v", clickFrame.GetFlowParts().GetParts())
		}
		respondToOperationWithScreenshot(t, conn, sessionID, clickFrame,
			game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED,
			fmt.Sprintf("cell at (%d,%d) revealed", saoleiClick1X, saoleiClick1Y), screenshot)
	}
	textFrame := drainWSFrame(t, conn, func(f *game.TeamFrame) bool {
		return frameHasText(f)
	})
	if textFrame == nil {
		t.Fatal("did not receive a final text frame after the rejected in-flight rebuild")
	}
	if !strings.Contains(frameText(textFrame), expectedSaoleiFinalText) {
		t.Errorf("final text = %q, want to contain %q", frameText(textFrame), expectedSaoleiFinalText)
	}
}

// TestTeamConnectWithoutCreateRejected verifies the FR-003 inverse: a
// WebSocket Connect for a session whose team was NOT materialized does not
// hang — the first frame is rejected and the connection is closed (the proxy
// reports the missing owner over the stream error channel, which the gateway
// surfaces as a close). The old on-demand creation behaviour is gone.
func TestTeamConnectWithoutCreateRejected(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	// given: a session with no team (no UpdateTeam materialization).
	sessionID, _ := createSession(t, sutHostURL, sutEnvName, saoleiTemplateID)

	// when: connecting and sending the first frame.
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()
	sendText(t, conn, sessionID, "hello without a team")

	// then: the connection is closed instead of producing a response
	// (readWSFrameNoFatal returns an error — timeout or close).
	frame, err := readWSFrameNoFatal(conn, 10*time.Second)
	if err == nil {
		t.Fatalf("expected the connection to close for a session without a team, got a frame: role=%s", roleString(frame.GetRole()))
	}
	t.Logf("connect without create correctly closed: %v", err)
}

// TestTeamConnectExclusiveEmit verifies the connection-exclusivity behaviour
// of the per-session TurnLoop (the post-031 equivalent of the former
// concurrent-connection semantics — spec 030 replaced the "second connect
// kicks first" model with a single emit sink per submission): with two
// WebSocket connections on the same session, a turn submitted on the second
// connection streams its output ONLY to that connection; the first receives
// nothing.
func TestTeamConnectExclusiveEmit(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, "team-xemit-"+uniqueSuffix(), "gpt-4", "gpt-4")

	// given: two connections on the same session.
	conn1 := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn1.Close()
	conn2 := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn2.Close()

	// when: a turn is submitted on conn2.
	sendText(t, conn2, sessionID, "hello exclusive")

	// then: conn2 receives the response (thinking + text)…
	thinkingFrame := drainWSFrame(t, conn2, func(f *game.TeamFrame) bool { return frameHasThinking(f) })
	if thinkingFrame == nil {
		t.Fatal("conn2: did not receive a thinking frame for the submitted turn")
	}

	// …and conn1 receives nothing (the per-session emit sink is bound to the
	// submitting connection, not broadcast).
	if frame, err := readWSFrameNoFatal(conn1, 3*time.Second); err == nil {
		t.Errorf("conn1 received a frame (role=%s) — turn output must be exclusive to the submitting connection", roleString(frame.GetRole()))
	} else {
		t.Logf("conn1 correctly silent: %v", err)
	}

	// Drain conn2's remaining turn output so the connection settles.
	_ = drainWSFrame(t, conn2, func(f *game.TeamFrame) bool { return frameHasText(f) })
	_ = drainWSFrame(t, conn2, func(f *game.TeamFrame) bool { return frameWait(f) != nil })
}

// TestTeamDisconnectReconnectHistory verifies that conversation history
// persists across WebSocket disconnect and reconnect (the team checkpoint is
// session-scoped, not connection-scoped): messages sent before disconnect are
// visible via ListMessages after reconnecting.
func TestTeamDisconnectReconnectHistory(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, "team-disc-"+uniqueSuffix(), "gpt-4", "gpt-4")

	// Connect, send 2 text exchanges. Each text must carry a fake-LLM
	// keyword ("hello" → greeting config): the 037 compression testdata
	// configs (sample_compression_{player,planner}.yaml) are reasoning-less
	// text Messages in the matcher's random fallback pool, so a no-keyword
	// text can return a summary WITHOUT a thinking frame and this test's
	// frameHasThinking drain would hang (projects/game/fake-llm/service/
	// matcher.go Match).
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)

	messages := []string{"Hello, first message", "Hello, second message"}
	for _, msg := range messages {
		sendText(t, conn, sessionID, msg)
		_ = drainWSFrame(t, conn, func(f *game.TeamFrame) bool { return frameHasThinking(f) })
		textResp := drainWSFrame(t, conn, func(f *game.TeamFrame) bool { return frameHasText(f) })
		if textResp == nil {
			t.Fatalf("message %q: no text response", msg)
		}
		t.Logf("exchange: %q → %q", msg, frameText(textResp))
	}

	// Disconnect.
	conn.Close()

	// Reconnect with the same session.
	conn2 := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn2.Close()

	// Verify all 4 messages (2 user + 2 agent) present in the player
	// partition history (FR-005). The first activation also injected the
	// 039 init instruction, so the partition is >= 5 — the >= 4 assertion
	// below holds either way.
	lmr := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "player")
	gotCount := len(lmr.GetMessages())
	if gotCount < 4 {
		t.Errorf("ListMessages after reconnect returned %d messages, want at least 4", gotCount)
	}

	foundFirst := false
	foundSecond := false
	for _, msg := range lmr.GetMessages() {
		if msg.GetRole() == game.MessageRole_MESSAGE_ROLE_USER {
			if messageText(msg) == messages[0] {
				foundFirst = true
			}
			if messageText(msg) == messages[1] {
				foundSecond = true
			}
		}
	}
	if !foundFirst {
		t.Errorf("first user message %q not found in ListMessages", messages[0])
	}
	if !foundSecond {
		t.Errorf("second user message %q not found in ListMessages", messages[1])
	}
}

// TestTeamStatusPingPong verifies the agent's status ping-pong on the team
// graph: a status probe returns IDLE when no turn is in-flight, and ACTIVE
// while a turn is in-flight (the saolei_init dispatch blocks the turn until
// the test — playing desktop — replies with a FlowResultPart; the
// per-session turn mutex is held for the entire dispatch wait, so the probe
// is deterministic — specs/021-agent-session-resync/spec.md US1 / SC-001).
func TestTeamStatusPingPong(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, "team-status-"+uniqueSuffix(), "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// given: run a text turn to completion so no turn is in-flight.
	sendText(t, conn, sessionID, "hello")
	_ = drainWSFrame(t, conn, func(f *game.TeamFrame) bool { return frameHasThinking(f) })
	_ = drainWSFrame(t, conn, func(f *game.TeamFrame) bool { return frameHasText(f) })
	_ = drainWSFrame(t, conn, func(f *game.TeamFrame) bool { return frameWait(f) != nil })

	// when: probe the status while idle. then: the response is IDLE.
	sendStatusFrame(t, conn, sessionID, game.StatusSignalStatus_STATUS_SIGNAL_STATUS_ACTIVE)
	idleResp := drainWSFrame(t, conn, func(f *game.TeamFrame) bool {
		return frameStatus(f) != nil
	})
	if idleResp == nil {
		t.Fatal("did not receive a status response while idle")
	}
	if frameStatus(idleResp).GetStatus() != game.StatusSignalStatus_STATUS_SIGNAL_STATUS_IDLE {
		t.Errorf("idle status = %v, want IDLE", frameStatus(idleResp).GetStatus())
	}
	t.Logf("idle status probe: %v", frameStatus(idleResp).GetStatus())

	// when: start a saolei_init turn; reading the dispatched F2 operation
	// frame proves the turn is in-flight and blocked awaiting the desktop
	// result (the per-session mutex is held for the entire dispatch wait).
	sendText(t, conn, sessionID, "please start saolei game")
	opFrame := readOperationFrame(t, conn)

	// then: a status probe while the turn is in-flight returns ACTIVE.
	sendStatusFrame(t, conn, sessionID, game.StatusSignalStatus_STATUS_SIGNAL_STATUS_ACTIVE)
	activeResp := drainWSFrame(t, conn, func(f *game.TeamFrame) bool {
		return frameStatus(f) != nil
	})
	if activeResp == nil {
		t.Fatal("did not receive a status response while a turn is in-flight")
	}
	if frameStatus(activeResp).GetStatus() != game.StatusSignalStatus_STATUS_SIGNAL_STATUS_ACTIVE {
		t.Errorf("in-flight status = %v, want ACTIVE", frameStatus(activeResp).GetStatus())
	}
	t.Logf("in-flight status probe: %v", frameStatus(activeResp).GetStatus())

	// Complete the turn so the connection settles: reply with the
	// recognizable in-progress board and drain through the final text.
	screenshot := buildSaoleiFlowResultScreenshot(saoleiBoardInitPNG)
	respondToOperationWithScreenshot(t, conn, sessionID, opFrame,
		game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED, "F2 pressed, new game started", screenshot)
	for i := 0; i < 2; i++ {
		clickFrame := readOperationFrame(t, conn)
		if frameMouseMoveAndClick(clickFrame) == nil {
			t.Fatalf("expected a saolei_operate dispatch, got: %v", clickFrame.GetFlowParts().GetParts())
		}
		respondToOperationWithScreenshot(t, conn, sessionID, clickFrame,
			game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED,
			fmt.Sprintf("cell at (%d,%d) revealed", saoleiClick1X, saoleiClick1Y), screenshot)
	}
	textFrame := drainWSFrame(t, conn, func(f *game.TeamFrame) bool {
		return frameHasText(f)
	})
	if textFrame == nil {
		t.Fatal("did not receive a final text frame after the in-flight turn")
	}
	if !strings.Contains(frameText(textFrame), expectedSaoleiFinalText) {
		t.Errorf("final text = %q, want to contain %q", frameText(textFrame), expectedSaoleiFinalText)
	}
}

// TestTeamReconnectDispatchReliability verifies that an operation dispatch
// succeeds after a WebSocket disconnect/reconnect cycle: the stream-scoped
// sink (compare-and-delete) ensures the closing stream's cleanup cannot
// clobber the fresh reconnect's sink, so a saolei_init dispatch on the new
// connection resolves and the turn completes
// (specs/021-agent-session-resync/spec.md US2 / SC-002 / SC-005).
func TestTeamReconnectDispatchReliability(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, "team-reconn-"+uniqueSuffix(), "gpt-4", "gpt-4")
	initScreenshot := buildSaoleiFlowResultScreenshot(saoleiBoardInitPNG)

	// given: connect and run a turn whose saolei_init dispatch resolves —
	// init operation + desktop reply + the init tool_result streaming in
	// real time (the streamed tool-finished event resolves the dispatch;
	// same streaming contract as agent_operation_test.go).
	conn1 := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	sendText(t, conn1, sessionID, "please start saolei game")
	opFrame1 := readOperationFrame(t, conn1)
	if frameKeyboardPress(opFrame1) == nil {
		t.Fatalf("saolei_init did not dispatch a KeyboardPressPart FlowPart")
	}
	respondToOperationWithScreenshot(t, conn1, sessionID, opFrame1,
		game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED, "F2 pressed, new game started", initScreenshot)
	// The init dispatch cycle is complete once its tool_result arrives. Do
	// NOT drain text: the stateless fake-LLM chains the operate batch after
	// the init result, the player blocks on that dispatch (the test stops
	// playing the desktop), and the turn only unwinds via the disconnect
	// abort below.
	initResultFrame := drainWSFrame(t, conn1, func(f *game.TeamFrame) bool { return frameHasToolResult(f) })
	if initResultFrame == nil {
		t.Fatal("conn1: no init tool_result frame — the saolei_init dispatch did not resolve")
	}

	// when: disconnect then reconnect.
	conn1.Close()
	conn2 := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn2.Close()

	// then: a new turn on the reconnected stream completes — the fresh
	// sink is live and every dispatch resolves (not "desktop
	// disconnected"). Reconnect continues from the checkpoint (turn 1's
	// init history is already in playerMessages), so the resumed player
	// may dispatch a cell op rather than re-init; playTeamGameUntilWait
	// replies to every dispatched operation until the turn completes
	// (wait), proving reconnect dispatch reliability end-to-end. Both
	// screenshots are 16×16 to match turn 1's init board
	// (dimension-mismatched replies would make recognize fail and skip
	// the sink events — saolei-sink-contract.md §3).
	playTeamGameUntilWait(t, conn2, sessionID, initScreenshot,
		buildSaoleiFlowResultScreenshot(saoleiBoardLossPNG))
}

// ─── TeamProfile CRUD (FR-006/FR-027 — merged from the prompt suite) ─────

// TestTeamProfileCrud verifies the saolei TeamProfile CRUD surface
// (game.proto PromptService, contracts/api-contract.md §2.3): create/get/
// list/update (update_mask with oneof-member paths, AIP-161)/delete, the
// template↔parent consistency validation, and the FR-027 shape — the saolei
// profile carries ONLY the player/planner model selection (no tools/mcp/skill
// fields exist on the resource at all).
func TestTeamProfileCrud(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileID := fmt.Sprintf("tp-crud-%s", uniqueSuffix())
	wantName := fmt.Sprintf("templates/%s/profiles/%s", saoleiTemplateID, profileID)

	// given: create the saolei TeamProfile.
	created := createTeamProfile(t, sutHostURL, sutEnvName, saoleiTemplateID, profileID, "gpt-4", "gpt-4-turbo")

	// then: the resource carries the template-scoped name and the typed
	// saolei spec (FR-027 — only models). The template is carried by the name
	// path segment (TeamProfile.template was removed,
	// specs/035-proto-contract-refine/data-model.md §1.2).
	if created.GetName() != wantName {
		t.Errorf("created Name = %q, want %q", created.GetName(), wantName)
	}
	if created.GetSaolei() == nil {
		t.Fatal("created spec.saolei is nil — the saolei oneof variant must be set")
	}
	if created.GetSaolei().GetPlayerModel() != "gpt-4" {
		t.Errorf("created player_model = %q, want gpt-4", created.GetSaolei().GetPlayerModel())
	}
	if created.GetSaolei().GetPlannerModel() != "gpt-4-turbo" {
		t.Errorf("created planner_model = %q, want gpt-4-turbo", created.GetSaolei().GetPlannerModel())
	}

	// when: get + list the profile.
	fetched := getTeamProfile(t, sutHostURL, sutEnvName, saoleiTemplateID, profileID)
	if fetched.GetName() != wantName {
		t.Errorf("fetched Name = %q, want %q", fetched.GetName(), wantName)
	}
	if fetched.GetSaolei().GetPlayerModel() != "gpt-4" {
		t.Errorf("fetched player_model = %q, want gpt-4", fetched.GetSaolei().GetPlayerModel())
	}
	listed := listTeamProfiles(t, sutHostURL, sutEnvName, saoleiTemplateID, 100)
	found := false
	for _, p := range listed.GetTeamProfiles() {
		if p.GetName() == wantName {
			found = true
		}
	}
	if !found {
		t.Errorf("ListTeamProfiles did not surface %q", wantName)
	}

	// when: PATCH player_model via update_mask (AIP-161 oneof-member path).
	status, body := updateTeamProfile(t, sutHostURL, sutEnvName, saoleiTemplateID, profileID,
		"saolei.player_model", "gpt-4o", "")
	if status != http.StatusOK {
		t.Fatalf("UpdateTeamProfile(saolei.player_model) status=%d, body=%s", status, body)
	}

	// then: GET reflects the update; the unmasked planner_model is untouched.
	updated := getTeamProfile(t, sutHostURL, sutEnvName, saoleiTemplateID, profileID)
	if updated.GetSaolei().GetPlayerModel() != "gpt-4o" {
		t.Errorf("updated player_model = %q, want gpt-4o", updated.GetSaolei().GetPlayerModel())
	}
	if updated.GetSaolei().GetPlannerModel() != "gpt-4-turbo" {
		t.Errorf("updated planner_model = %q, want gpt-4-turbo (unmasked field preserved)", updated.GetSaolei().GetPlannerModel())
	}

	// when: PATCH planner_model via update_mask.
	status, body = updateTeamProfile(t, sutHostURL, sutEnvName, saoleiTemplateID, profileID,
		"saolei.planner_model", "", "gpt-3.5-turbo")
	if status != http.StatusOK {
		t.Fatalf("UpdateTeamProfile(saolei.planner_model) status=%d, body=%s", status, body)
	}
	updated = getTeamProfile(t, sutHostURL, sutEnvName, saoleiTemplateID, profileID)
	if updated.GetSaolei().GetPlannerModel() != "gpt-3.5-turbo" {
		t.Errorf("updated planner_model = %q, want gpt-3.5-turbo", updated.GetSaolei().GetPlannerModel())
	}

	// when: delete the profile, then get it again.
	if status := deleteTeamProfile(t, sutHostURL, sutEnvName, saoleiTemplateID, profileID); status != http.StatusOK && status != http.StatusNoContent {
		t.Fatalf("DeleteTeamProfile status=%d, want 200 or 204", status)
	}
	if status, _ := getTeamProfileWithStatus(t, sutHostURL, sutEnvName, saoleiTemplateID, profileID); status != http.StatusNotFound {
		t.Errorf("GetTeamProfile after delete: status=%d, want 404", status)
	}
}

// TestTeamProfileTemplateConsistency verifies the handler's no-implicit-rules
// validation (contracts/api-contract.md §2.3 — FR 禁潜规则): the oneof spec
// variant MUST be consistent with the template derived from the parent path
// segment — a saolei parent requires the saolei variant. The former
// resource-body template double-check is removed: the template is carried by
// the parent path segment only (specs/035-proto-contract-refine/contracts/
// resource-fields.md §2.3).
func TestTeamProfileTemplateConsistency(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	// given: a create request for the saolei template whose body carries NO
	// oneof spec variant (the parent path segment says saolei, but the spec
	// is absent — the oneof inconsistency the handler must reject).
	body, err := json.Marshal(map[string]any{})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	reqURL := fmt.Sprintf("%s%stemplates/%s/profiles?team_profile_id=inconsistent-%s",
		sutHostURL, pathPrefix, saoleiTemplateID, uniqueSuffix())
	resp, respBody := doHTTP(t, http.MethodPost, reqURL, sutEnvName, body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("CreateTeamProfile without the saolei spec variant: status=%d, want 400 INVALID_ARGUMENT, body=%s", resp.StatusCode, respBody)
	}

	// given: a create request whose body carries the matching saolei oneof
	// variant — the consistency check passes and the profile is created
	// (the rejection above is variant-based, not vacuous).
	body, err = json.Marshal(map[string]any{
		"saolei": map[string]any{"playerModel": "gpt-4", "plannerModel": "gpt-4"},
	})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	consistentURL := fmt.Sprintf("%s%stemplates/%s/profiles?team_profile_id=consistent-%s",
		sutHostURL, pathPrefix, saoleiTemplateID, uniqueSuffix())
	resp, respBody = doHTTP(t, http.MethodPost, consistentURL, sutEnvName, body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("CreateTeamProfile with the matching saolei spec variant: status=%d, want 200, body=%s", resp.StatusCode, respBody)
	}
}

// ─── Per-TeamProfile model (merged from the per-profile-model suite) ──────

// TestTeamPerProfileModel verifies that teams materialized from different
// TeamProfiles each resolve the models configured in their profile: the
// UpdateTeam request carries the TeamProfile resource name, the agent reads
// player_model/planner_model from it (prompt-client.getTeamProfile), and a
// turn on each session completes — proving the model specs were parsed and
// routed through the provider (fake-llm ignores the model field, so both
// respond with the same template-matched content; the assertion is that the
// per-profile model RESOLUTION succeeded end-to-end).
func TestTeamPerProfileModel(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profile1ID := fmt.Sprintf("model-gpt4-%s", uniqueSuffix())
	profile2ID := fmt.Sprintf("model-gpt4turbo-%s", uniqueSuffix())

	// given: two sessions, each with a team built from a different profile.
	// setupTeamSession creates the TeamProfiles internally (session →
	// TeamProfile → UpdateTeam materialization), so GetTeamProfile below must
	// run AFTER it — the profiles do not exist before that point.
	sessionID1 := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profile1ID, "gpt-4", "gpt-4-turbo")
	sessionID2 := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profile2ID, "gpt-4-turbo", "gpt-4")

	// Verify the created profiles carry the configured models via
	// GetTeamProfile (the source of truth the agent reads at materialization).
	fetched1 := getTeamProfile(t, sutHostURL, sutEnvName, saoleiTemplateID, profile1ID)
	if fetched1.GetSaolei().GetPlayerModel() != "gpt-4" || fetched1.GetSaolei().GetPlannerModel() != "gpt-4-turbo" {
		t.Errorf("fetched profile1 models = (%q, %q), want (gpt-4, gpt-4-turbo)",
			fetched1.GetSaolei().GetPlayerModel(), fetched1.GetSaolei().GetPlannerModel())
	}
	fetched2 := getTeamProfile(t, sutHostURL, sutEnvName, saoleiTemplateID, profile2ID)
	if fetched2.GetSaolei().GetPlayerModel() != "gpt-4-turbo" || fetched2.GetSaolei().GetPlannerModel() != "gpt-4" {
		t.Errorf("fetched profile2 models = (%q, %q), want (gpt-4-turbo, gpt-4)",
			fetched2.GetSaolei().GetPlayerModel(), fetched2.GetSaolei().GetPlannerModel())
	}

	// when: a turn on each session (both carry the greeting keyword so the
	// response content is deterministic).
	conn1 := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID1)
	defer conn1.Close()
	sendText(t, conn1, sessionID1, "Hello from session one")
	_ = drainWSFrame(t, conn1, func(f *game.TeamFrame) bool { return frameHasThinking(f) })
	resp1 := drainWSFrame(t, conn1, func(f *game.TeamFrame) bool { return frameHasText(f) })
	if resp1 == nil {
		t.Fatal("session1 (gpt-4 player profile): no text response")
	}
	if !strings.Contains(frameText(resp1), expectedGreetingText) {
		t.Errorf("session1 text = %q, want to contain %q", frameText(resp1), expectedGreetingText)
	}
	t.Logf("session1 (profile1) responded: %s", frameText(resp1))

	conn2 := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID2)
	defer conn2.Close()
	sendText(t, conn2, sessionID2, "Hello from session two")
	_ = drainWSFrame(t, conn2, func(f *game.TeamFrame) bool { return frameHasThinking(f) })
	resp2 := drainWSFrame(t, conn2, func(f *game.TeamFrame) bool { return frameHasText(f) })
	if resp2 == nil {
		t.Fatal("session2 (gpt-4-turbo player profile): no text response")
	}
	if !strings.Contains(frameText(resp2), expectedGreetingText) {
		t.Errorf("session2 text = %q, want to contain %q", frameText(resp2), expectedGreetingText)
	}
	t.Logf("session2 (profile2) responded: %s", frameText(resp2))

	// Both teams' profiles resolved their configured models.
	t.Logf("profile1 models=(%s, %s), profile2 models=(%s, %s)",
		fetched1.GetSaolei().GetPlayerModel(), fetched1.GetSaolei().GetPlannerModel(),
		fetched2.GetSaolei().GetPlayerModel(), fetched2.GetSaolei().GetPlannerModel())
}

// ─── Team graph behaviour (FR-005/FR-010/FR-011/FR-013..018/FR-020) ───────

// TestTeamMessagePartitionByAgent verifies FR-005: ListMessages partitions
// the session history per team agent. After a game whose terminal move
// triggered the planner, the player partition carries the user input and the
// saolei tool calls/results (agent="player"), the planner partition carries
// the memory review chain (agent="planner"), and neither partition leaks
// into the other. The 039 rewrite (FR-013/FR-009): the planner's channel
// carries `memory`/`instruct_player` calls (never update_strategy — the
// StrategyStore is gone — and never saolei tools), and the player's channel
// carries NO memory tool (the planner's review is invisible to the player,
// FR-017).
func TestTeamMessagePartitionByAgent(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, "team-part-"+uniqueSuffix(), "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// when: one full game — init recognizes an in-progress board, the first
	// operate batch's first click reply is a terminal lost board (planner
	// triggers).
	playTeamGameUntilWait(t, conn, sessionID, buildSaoleiFlowResultScreenshot(saoleiBoardInitPNG),
		buildSaoleiFlowResultScreenshot(saoleiBoardLossPNG))

	// then: the player partition carries the user input + saolei tool calls,
	// all stamped agent="player", and NO memory tool (FR-009 — the memory
	// tool is planner-only; the planner's review must stay in the planner
	// partition, FR-017).
	playerLmr := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "player")
	if len(playerLmr.GetMessages()) == 0 {
		t.Fatal("player partition is empty after a full game")
	}
	playerUserFound := false
	playerSaoleiFound := false
	for _, m := range playerLmr.GetMessages() {
		if m.GetAgent() != "player" {
			t.Errorf("player-partition message has agent=%q, want player (FR-005)", m.GetAgent())
		}
		if m.GetRole() == game.MessageRole_MESSAGE_ROLE_USER {
			playerUserFound = true
		}
		if messageHasToolCall(m, "saolei_init") || messageHasToolCall(m, "saolei_operate") {
			playerSaoleiFound = true
		}
		if messageHasToolCall(m, "memory") {
			t.Error("player partition carries a memory tool_call — the planner's memory review must stay in the planner partition (FR-005/FR-009)")
		}
		if messageHasToolCall(m, "update_strategy") {
			t.Error("player partition carries an update_strategy tool_call — the shared strategy tool is gone (spec 039 FR-013)")
		}
	}
	if !playerUserFound {
		t.Error("player partition did not surface the user input message")
	}
	if !playerSaoleiFound {
		t.Error("player partition did not surface a saolei tool_call — the player drives the game (FR-010)")
	}

	// then: the planner partition carries the memory review chain, all
	// stamped agent="planner", with no saolei/desktop operations (FR-010 —
	// the planner holds no desktop tools) and no update_strategy (FR-013).
	plannerLmr := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "planner")
	if len(plannerLmr.GetMessages()) == 0 {
		t.Fatal("planner partition is empty — the planner did not trigger for the terminal-move game (FR-011)")
	}
	plannerMemoryFound := false
	for _, m := range plannerLmr.GetMessages() {
		if m.GetAgent() != "planner" {
			t.Errorf("planner-partition message has agent=%q, want planner (FR-005)", m.GetAgent())
		}
		if messageHasToolCall(m, "memory") {
			plannerMemoryFound = true
		}
		if messageHasToolCall(m, "update_strategy") {
			t.Error("planner partition carries an update_strategy tool_call — the shared strategy tool is gone (spec 039 FR-013)")
		}
		for _, name := range messageToolCallNames(m) {
			if strings.HasPrefix(name, "saolei_") {
				t.Errorf("planner partition carries a saolei_%s tool_call — the planner MUST NOT hold desktop tools (FR-010)", name)
			}
		}
	}
	if !plannerMemoryFound {
		t.Error("planner partition did not surface a memory tool_call (FR-008)")
	}
}

// TestTeamPlayerExclusiveControl verifies FR-010 end-to-end: during a team
// turn only the player agent drives the desktop — every dispatched operation
// FlowPart on the WS corresponds to the player's saolei tool calls, and the
// planner contributes no operations at all (its only tools, memory +
// instruct_player, never dispatch). The frame stream of a full game (init
// in-progress → terminal-move operate batch) is checked: every operation
// frame belongs to a player tool (F2 init / cell ops), the planner's tool_call
// frames carry only memory/instruct_player, and the review chain runs
// exactly once per game end (FR-011).
func TestTeamPlayerExclusiveControl(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, "team-excl-"+uniqueSuffix(), "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// when: one full game — init recognizes an in-progress board, the first
	// operate batch's first click reply is a terminal lost board (planner
	// triggers inside the turn — D6).
	frames := playTeamGameUntilWait(t, conn, sessionID, buildSaoleiFlowResultScreenshot(saoleiBoardInitPNG),
		buildSaoleiFlowResultScreenshot(saoleiBoardLossPNG))

	// then: every operation frame is a player dispatch (keyboard F2 or cell
	// click) — the planner never dispatches.
	for _, f := range frames {
		if opID := frameOperationToolID(f); opID != "" {
			if kp := frameKeyboardPress(f); kp != nil {
				if kp.GetKey() != game.KeyboardKey_KEYBOARD_KEY_F2 {
					t.Errorf("player dispatched key %v, want F2 (saolei_init)", kp.GetKey())
				}
				continue
			}
			if frameMouseMoveAndClick(f) == nil {
				t.Errorf("operation frame with unknown part: %v", f.GetFlowParts().GetParts())
			}
		}
	}

	// then: the planner triggered exactly once per game end (FR-011/D6) —
	// signalled by the review input frame — and its deterministic review
	// chain ran (one memory batch-add + two old_text-location replaces +
	// one instruct_player; sample_planner_memory.yaml / sample_planner_tools.
	// yaml — FR-008/FR-014).
	if got := countPlannerReviewFrames(frames); got != 1 {
		t.Errorf("planner review frames = %d, want exactly 1 per game end (FR-011/D6)", got)
	}
	if got := countMemoryCalls(frames); got != 3 {
		t.Errorf("memory calls = %d, want exactly 3 per review (batch add + 0-hit replace + multi-hit replace — FR-008)", got)
	}

	// then: the planner's live tool_calls never include saolei tools, and
	// the reloaded planner channel carries no saolei tool_calls either
	// (FR-018 — the description injection added text, not tools).
	plannerLmr := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "planner")
	for _, m := range plannerLmr.GetMessages() {
		for _, name := range messageToolCallNames(m) {
			if strings.HasPrefix(name, "saolei_") {
				t.Errorf("planner channel carries saolei tool_call %q — only the player holds desktop tools (FR-010)", name)
			}
			if name == "update_strategy" {
				t.Errorf("planner channel carries update_strategy tool_call %q — the shared strategy tool is gone (spec 039 FR-013)", name)
			}
		}
	}
}

// TestTeamPlannerTriggersOncePerGame verifies FR-011/D6: the planner is
// triggered EXACTLY ONCE per game end (won/lost) and never per move — two
// consecutive games on the same session produce exactly two review inputs
// (one per game), and a single game never repeats the trigger (the graph
// clears gameEnded after the planner node — team-graph-contract.md §2.2/§4).
func TestTeamPlannerTriggersOncePerGame(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, "team-trigger-"+uniqueSuffix(), "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// when: game 1 — init recognizes an in-progress board, the first operate
	// click's reply is a terminal lost board (the move fires onGameEnd →
	// planner).
	frames1 := playTeamGameUntilWait(t, conn, sessionID, buildSaoleiFlowResultScreenshot(saoleiBoardInitPNG),
		buildSaoleiFlowResultScreenshot(saoleiBoardLossPNG))

	// then: the planner triggered exactly once (FR-011 — not per move).
	if got := countPlannerReviewFrames(frames1); got != 1 {
		t.Errorf("game 1: planner review frames = %d, want exactly 1 (FR-011/D6)", got)
	}

	// when: game 2 on the same session (a second turn, same fixture boards).
	frames2 := playTeamGameUntilWait(t, conn, sessionID, buildSaoleiFlowResultScreenshot(saoleiBoardInitPNG),
		buildSaoleiFlowResultScreenshot(saoleiBoardLossPNG))

	// then: exactly one more planner trigger — one per game, accumulated.
	if got := countPlannerReviewFrames(frames2); got != 1 {
		t.Errorf("game 2: planner review frames = %d, want exactly 1 (one per game — FR-011)", got)
	}
	plannerLmr := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "planner")
	total := 0
	for _, m := range plannerLmr.GetMessages() {
		if strings.Contains(messageText(m), reviewInputPrefix) {
			total++
		}
	}
	if total != 2 {
		t.Errorf("planner partition total review inputs = %d, want 2 (one per game across two games)", total)
	}
}

// TestTeamMemoryPersistsAcrossGames verifies the planner's long-term memory
// flow (specs/039-planner-memory-calibration FR-006/FR-008): the `memory`
// tool calls made during the review persist in the memory SERVICE (not the
// checkpoint) across games of the SAME session — verified through the
// gateway's public HTTP entry (ListMemories) — and are isolated per session
// (FR-012 resource scope templates/{template}/sessions/{session}/memories/...).
// The second game's batch add dedupes (hermes "no duplicate added" — FR-008),
// so the entries are never duplicated.
func TestTeamMemoryPersistsAcrossGames(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	profileID := fmt.Sprintf("team-mem-%s", uniqueSuffix())
	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileID, "gpt-4", "gpt-4")
	conn := connectAgentWSTrace(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// when: game 1 — init recognizes an in-progress board, the first operate
	// click's reply is a terminal lost board (the planner writes memory via
	// the memory tool's batch add).
	frames1 := playTeamGameUntilWait(t, conn, sessionID, buildSaoleiFlowResultScreenshot(saoleiBoardInitPNG),
		buildSaoleiFlowResultScreenshot(saoleiBoardLossPNG))

	// then: the memory tool_call chain ran the fixture's deterministic forms —
	// the BATCH add carrying both fixture entries (FR-008 operations form —
	// the write argument) plus the two SINGLE-OP replace calls (FR-008 allows
	// single-op OR batch; the 0-hit / multi-hit old_text locators). Only the
	// batch call must carry the fixture entries; the replaces carry their own
	// locator contents.
	foundBatch := false
	for _, f := range frames1 {
		for _, p := range frameMessageParts(f).GetParts() {
			tc := p.GetToolCall()
			if tc == nil || tc.GetName() != "memory" {
				continue
			}
			args := tc.GetArgsJson()
			if strings.Contains(args, "operations") {
				// The batch add form (FR-008 — operations array): the
				// fixture's two entries are the add contents.
				if !strings.Contains(args, expectedPlannerMemoryE1) || !strings.Contains(args, expectedPlannerMemoryE2) {
					t.Errorf("memory batch args_json = %q, want to contain both fixture entries %q / %q",
						args, expectedPlannerMemoryE1, expectedPlannerMemoryE2)
				}
				foundBatch = true
				continue
			}
			// The single-op replace form (FR-008 — action + old_text +
			// content): the 0-hit / multi-hit old_text locator calls.
			if !strings.Contains(args, `"action":"replace"`) || !strings.Contains(args, "old_text") {
				t.Errorf("memory tool_call args_json = %q, want the single-op replace form (action/old_text/content — FR-008)", args)
			}
		}
	}
	if !foundBatch {
		t.Fatal("no memory batch add tool_call frame seen in game 1 — the planner did not write memory")
	}

	// then: the entries are PERSISTED in the memory service (agent-side
	// conversion → CreateMemory through the gateway; FR-006 — durable
	// storage, not the checkpoint). The memory_ids are content digests
	// (generateMemoryId), so the ListMemories order is compared as a set.
	contents := listMemoryContents(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	if len(contents) != 2 {
		t.Fatalf("ListMemories after game 1 = %d entries, want 2 (the batch add wrote both fixture entries)", len(contents))
	}
	if !slices.Contains(contents, expectedPlannerMemoryE1) || !slices.Contains(contents, expectedPlannerMemoryE2) {
		t.Errorf("ListMemories contents = %q, want both fixture entries %q and %q", contents, expectedPlannerMemoryE1, expectedPlannerMemoryE2)
	}

	// when: game 2 on the same session — the batch add dedupes (equivalent
	// content already present = success, hermes "no duplicate added" —
	// FR-008), so "applied 0 operation(s)" and the entries stay unique.
	frames2 := playTeamGameUntilWait(t, conn, sessionID, buildSaoleiFlowResultScreenshot(saoleiBoardInitPNG),
		buildSaoleiFlowResultScreenshot(saoleiBoardLossPNG))
	foundDedupe := false
	for _, f := range frames2 {
		for _, p := range frameMessageParts(f).GetParts() {
			if tr := p.GetToolResult(); tr != nil && strings.Contains(tr.GetMessage(), "memory: applied 0 operation(s)") {
				foundDedupe = true
			}
		}
	}
	if !foundDedupe {
		t.Error("game 2: no 'memory: applied 0 operation(s)' tool_result — the duplicate add was not deduped (FR-008)")
	}
	contents = listMemoryContents(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	if len(contents) != 2 {
		t.Errorf("ListMemories after game 2 = %d entries, want 2 (no duplicates — FR-008)", len(contents))
	}

	// when: a SECOND session runs its own game (fresh memory scope).
	otherSessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileID+"-b", "gpt-4", "gpt-4")

	// then: the fresh session's memory scope is EMPTY before its first game
	// (per-session resource scope — FR-012).
	if got := len(listMemoryContents(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, otherSessionID)); got != 0 {
		t.Errorf("fresh session ListMemories = %d entries, want 0 (per-session memory scope — FR-012)", got)
	}
	connB := connectAgentWSTrace(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, otherSessionID)
	defer connB.Close()
	framesB := playTeamGameUntilWait(t, connB, otherSessionID, buildSaoleiFlowResultScreenshot(saoleiBoardInitPNG),
		buildSaoleiFlowResultScreenshot(saoleiBoardLossPNG))

	// then: the second session's game wrote ITS OWN entries (the planner
	// triggered normally and the memory flow is healthy).
	if got := countPlannerReviewFrames(framesB); got != 1 {
		t.Errorf("second session: planner review frames = %d, want exactly 1 (per-session memory isolation — FR-012)", got)
	}
	if got := len(listMemoryContents(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, otherSessionID)); got != 2 {
		t.Errorf("second session ListMemories = %d entries, want 2 (its own memory scope — FR-012)", got)
	}
}

// TestTeamRefreshClearsShortTermKeepsMemory verifies FR-018/D8 (039 contract
// §7) + the 042 US3 post-refresh instruction turn (specs/042-planner-memory-
// fixup/contracts/refresh-instruction-trigger.md §2.3): RefreshTeam clears
// the session's SHORT-TERM memory — the old game's review chain and history
// must be GONE from both per-agent message channels — while the long-term
// memory (the entries in the memory service) is unaffected: the next game's
// planner still triggers and the memory flow continues. 042 US3: after the
// clear, refresh ALSO starts a fresh no-game-history instruction turn
// (fire-and-forget, FR-009 — the RPC returns right after the channel clear,
// NOT waiting for the LLM), so the partitions are NOT empty afterwards: the
// player partition holds the fresh instruction write-back and the planner
// partition the instruction turn's request/response. The old-content
// assertions below are race-free (the clear is synchronous inside the
// refresh RPC), and the fresh-content assertions wait for the turn's
// deterministic write-back before counting (the fake-LLM matches the init
// request's "团队初始化" keyword — sample_init_instruction.yaml).
func TestTeamRefreshClearsShortTermKeepsMemory(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, "team-refresh-"+uniqueSuffix(), "gpt-4", "gpt-4")
	conn := connectAgentWSTrace(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// given: one full game so both channels hold messages (player + planner)
	// and the memory service holds the fixture entries.
	playTeamGameUntilWait(t, conn, sessionID, buildSaoleiFlowResultScreenshot(saoleiBoardInitPNG),
		buildSaoleiFlowResultScreenshot(saoleiBoardLossPNG))
	if got := len(listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "player").GetMessages()); got == 0 {
		t.Fatal("precondition: player partition is empty before RefreshTeam")
	}
	if got := len(listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "planner").GetMessages()); got == 0 {
		t.Fatal("precondition: planner partition is empty before RefreshTeam — the planner did not trigger")
	}
	if got := len(listMemoryContents(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)); got != 2 {
		t.Fatalf("precondition: memory entries = %d, want 2 (the review wrote them)", got)
	}

	// when: RefreshTeam (FR-018 — clears short-term memory; 039 contract §7
	// also covers instructions — they live IN the playerMessages channel, so
	// the channel clear removes them; 042 US3 — after the clear, refresh
	// ALSO triggers a fresh no-game-history instruction turn, fire-and-forget
	// (FR-009 — specs/042-planner-memory-fixup/contracts/
	// refresh-instruction-trigger.md §2.3: the RPC returns right after the
	// channel clear, NOT waiting for the LLM).
	refreshTeam(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)

	// then: the OLD game's short-term messages are gone from both partitions.
	// The channel clear is synchronous inside the refresh RPC, so this holds
	// IMMEDIATELY after it returns — no matter whether the fire-and-forget
	// post-refresh instruction turn has landed yet (that turn only ADDS
	// fresh instruction content; it never reintroduces old game content).
	// Assert by content, not by count: the post-refresh turn may already
	// have written fresh messages when the read happens (the team-init
	// instruction text is NOT a marker here — the post-refresh turn
	// re-produces the identical instruction, FR-013).
	playerLmr := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "player")
	for _, old := range []string{
		"please start saolei game",     // the game-driving user text (playTeamGameUntilWait)
		"stopped at click(3,4) (lost)", // the game-ending operate tool_result
		expectedReviewInstructionText,  // the review instruction written into the player channel (FR-017)
	} {
		if got := messageIndex(playerLmr.GetMessages(), old); got != -1 {
			t.Errorf("player partition after RefreshTeam still carries old game content %q at index %d — the channel was not cleared (FR-018)", old, got)
		}
	}
	for _, m := range playerLmr.GetMessages() {
		for _, name := range messageToolCallNames(m) {
			if strings.HasPrefix(name, "saolei_") {
				t.Errorf("player partition after RefreshTeam still carries the old game's saolei_%s tool_call — the channel was not cleared (FR-018)", name)
			}
		}
	}
	plannerLmr := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "planner")
	if got := messageIndex(plannerLmr.GetMessages(), reviewInputPrefix); got != -1 {
		t.Errorf("planner partition after RefreshTeam still carries the old game's review input at index %d — the channel was not cleared (FR-018)", got)
	}
	if got := countMemoryCallsInMessages(plannerLmr.GetMessages()); got != 0 {
		t.Errorf("planner partition after RefreshTeam carries %d old memory tool_calls — the review chain was not cleared (FR-018)", got)
	}
	if got := findInstructPlayerCallContent(plannerLmr.GetMessages(), expectedReviewInstructionText); got != "" {
		t.Error("planner partition after RefreshTeam still carries the old game's review instruct_player call — the channel was not cleared (FR-018)")
	}

	// then: the 042 US3 post-refresh instruction turn completes — the fresh
	// no-game-history instruction lands in the CLEARED player channel
	// (deterministic: the init request's "团队初始化" keyword matches
	// sample_init_instruction.yaml → instruct_player with
	// expectedInitInstructionText — specs/042-planner-memory-fixup/contracts/
	// refresh-instruction-trigger.md §2.3/§6). Wait for the write-back so
	// the count assertions below cannot race the fire-and-forget turn.
	waitForInitInstructionPersisted(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)

	// then: both partitions hold EXACTLY the fresh instruction turn's
	// messages — the player channel = [the instruction write-back] (FR-008/
	// FR-013 — one fresh instruction in the cleared channel), the planner
	// channel = [init request HumanMessage, instruct_player tool_call, its
	// tool_result, the terminal "指令已发送。" text] (the same input-included
	// write-back as the review/compact nodes — the request joins the channel
	// like the review input, 037 FR-001 pattern) — with no old-game residue.
	playerLmr = listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "player")
	if got := len(playerLmr.GetMessages()); got != 1 {
		t.Fatalf("player partition after the post-refresh instruction turn = %d messages, want exactly 1 (the fresh instruction write-back — FR-008)", got)
	}
	if got := messageText(playerLmr.GetMessages()[0]); !strings.Contains(got, expectedInitInstructionText) {
		t.Errorf("player partition after RefreshTeam = %q, want to contain the fresh instruction %q (042 US3 — FR-008)", got, expectedInitInstructionText)
	}
	plannerLmr = listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "planner")
	if got := len(plannerLmr.GetMessages()); got != 4 {
		t.Errorf("planner partition after the post-refresh instruction turn = %d messages, want 4 (init request + instruct_player call/result + final text — FR-008)", got)
	}
	if got := messageIndex(plannerLmr.GetMessages(), initRequestPrefix); got == -1 {
		t.Error("planner partition after RefreshTeam does not carry the post-refresh init request — the instruction turn did not run (042 US3 — FR-008)")
	}
	if got := findInstructPlayerCallContent(plannerLmr.GetMessages(), expectedInitInstructionText); got != expectedInitInstructionText {
		t.Errorf("planner partition instruct_player content = %q, want %q (the post-refresh turn produced the pinned initial instruction — FR-008)", got, expectedInitInstructionText)
	}
	if !messagesContainText(plannerLmr.GetMessages(), "指令已发送。") {
		t.Error("planner partition does not carry the instruct_player final text \"指令已发送。\" — the post-refresh instruction loop did not terminate deterministically")
	}

	// then: the long-term memory is unaffected — the entries stay in the
	// memory service (FR-006 — durable storage, not the checkpoint) and the
	// next game still triggers the planner and the memory flow completes.
	if got := len(listMemoryContents(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)); got != 2 {
		t.Errorf("memory entries after RefreshTeam = %d, want 2 (long-term memory survived — FR-006/039 contract §7)", got)
	}
	frames := playTeamGameUntilWait(t, conn, sessionID, buildSaoleiFlowResultScreenshot(saoleiBoardInitPNG),
		buildSaoleiFlowResultScreenshot(saoleiBoardLossPNG))
	if got := countPlannerReviewFrames(frames); got != 1 {
		t.Errorf("post-refresh game: planner review frames = %d, want exactly 1 (memory survived RefreshTeam — FR-018)", got)
	}
}

// ─── 037 saolei-team optimize (US1/US2/US3/US5 — FR-034) ───────────────────

// TestTeamPlannerReviewRealtimeVisible verifies US1 (specs/037-saolei-team-
// optimize/spec.md FR-001/FR-002/FR-003): the planner's review input — a
// non-model-produced channel message carrying the full game history — is
// emitted as a REAL-TIME frame (agent="planner") while the game ends, so the
// desktop planner tab shows it without a reload (bug fix — the input never
// produced a streamEvents protocol event, specs/031-team-template-mode/
// bug-analysis.md Issue 2). The reloaded planner partition carries the
// identical content, proving the live and history paths are consistent
// (FR-002/FR-003 — the desktop dedups by frameId/messageId). The 039 update
// (spec 039-planner-memory-calibration FR-004): the gameLog renders
// saolei_operate entries with their full operation lists — the fixture batch
// [click{3,4}, click{5,6}] stopped at click(3,4) by the terminal reply.
func TestTeamPlannerReviewRealtimeVisible(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, "team-review-live-"+uniqueSuffix(), "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// when: one full game — init recognizes an in-progress board, the first
	// operate click's reply is a terminal lost board (planner triggered).
	frames := playTeamGameUntilWait(t, conn, sessionID, buildSaoleiFlowResultScreenshot(saoleiBoardInitPNG),
		buildSaoleiFlowResultScreenshot(saoleiBoardLossPNG))

	// then: the review input arrived as a REAL-TIME frame stamped
	// agent="planner" (FR-001/FR-005 — the frame carries the planner-tab
	// attribution).
	liveReview := findPlannerReviewFrame(frames)
	if liveReview == "" {
		t.Fatal("no real-time planner frame carrying the review input (本局游戏过程) — FR-001")
	}

	// then: the live content carries the full game process — each step's
	// tool, operations and status, plus the board renders (FR-002 — the
	// review input renders every gameLog entry, specs/036-team-mode-bugfix/
	// contracts/team-graph-fix-contract.md §2.2). The 039 batch render is
	// ONE entry carrying BOTH operations of the operate call (FR-004 — the
	// batch stopped at click(3,4) on the terminal reply, but the recorded entry
	// carries the full operation list).
	for _, want := range []string{
		"1. saolei_init → playing",
		"2. saolei_operate(click(3,4), click(5,6)) → lost",
		"3. (game-end) → lost",
		"board size 16*16",
	} {
		if !strings.Contains(liveReview, want) {
			t.Errorf("live review input missing %q (FR-002)", want)
		}
	}

	// then: the reloaded planner partition carries the SAME review content —
	// real-time emission and history loading are consistent (FR-002/FR-003).
	plannerLmr := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "planner")
	reloadedReview := findPlannerReviewMessage(plannerLmr.GetMessages())
	if reloadedReview == "" {
		t.Fatal("reloaded planner partition does not carry the review input — FR-003")
	}
	if reloadedReview != liveReview {
		t.Errorf("reloaded review input differs from the live frame (FR-002/FR-003):\nlive:     %q\nreloaded: %q", liveReview, reloadedReview)
	}
}

// TestTeamInitInstructionScenario verifies the team-INIT calibration
// scenario (specs/039-planner-memory-calibration US3 — FR-015/R2, contract
// §2.3/§6): UpdateTeam(allow_missing=true) materialization asynchronously
// triggers the one-shot initInstruction turn, whose prompt-guided planner
// call (sample_init_instruction.yaml keyword "团队初始化") writes a
// no-game-history instruction DIRECTLY into the playerMessages channel (a
// HumanMessage — same channel write-back as the review node, no pending
// slot) — WITHOUT invoking the player. The first user message's player
// activation then consumes it as plain channel history: the instruction is
// already in the channel BEFORE the user text (FR-015 — 异步产出期间到达的
// user message 排在指令之后), and the greeting still matches
// deterministically. The player partition order pins the delivery:
// instruction → user text → player response.
func TestTeamInitInstructionScenario(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, "team-init-instr-"+uniqueSuffix(), "gpt-4", "gpt-4")

	// given: connect after materialization — the one-shot async
	// initInstruction turn has been triggered by the UpdateTeam (R2 — 物化
	// 即返回, 不等 LLM) and the TurnLoop awaits it before the first user
	// turn (session-team.ts runTeamTurn — FR-015: 异步产出期间到达的 user
	// message 排在指令之后).
	conn := connectAgentWSTrace(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// when: the first user message (the player's first activation).
	sendText(t, conn, sessionID, "hello from first activation")
	_ = drainWSFrame(t, conn, func(f *game.TeamFrame) bool { return frameHasThinking(f) })
	textFrame := drainWSFrame(t, conn, func(f *game.TeamFrame) bool { return frameHasText(f) })
	if textFrame == nil {
		t.Fatal("did not receive a text response for the first user turn")
	}
	if !strings.Contains(frameText(textFrame), expectedGreetingText) {
		t.Errorf("first-activation text = %q, want to contain %q (the greeting keyword still matches)", frameText(textFrame), expectedGreetingText)
	}
	_ = drainWSFrame(t, conn, func(f *game.TeamFrame) bool { return frameWait(f) != nil })

	// then: the player partition carries the init instruction — written
	// into the channel by the initInstruction node and consumed with the
	// first activation as plain history (FR-015 — 随首次激活注入, 累积可引用).
	// The playerMessages channel order is instruction FIRST, then the user
	// message, then the player's response: the init turn wrote the
	// instruction into the channel before the user turn input landed
	// (instruction-node.ts writes playerMessages directly; runTeamTurn
	// awaits the init turn before appending the user input — contract
	// §2.3/§6, session-team.test.ts "queues a user message arriving during
	// the async init AFTER the instruction").
	playerLmr := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "player")
	instructionIdx := messageIndex(playerLmr.GetMessages(), expectedInitInstructionText)
	userIdx := messageIndex(playerLmr.GetMessages(), "hello from first activation")
	responseIdx := messageIndex(playerLmr.GetMessages(), expectedGreetingText)
	if instructionIdx == -1 {
		t.Errorf("player partition did not surface the init instruction %q — the instruction was not written into the player channel by the init turn (FR-015)", expectedInitInstructionText)
	}
	if userIdx == -1 {
		t.Error("player partition did not surface the first user message")
	}
	if responseIdx == -1 {
		t.Error("player partition did not surface the greeting response")
	}
	if instructionIdx != -1 && userIdx != -1 && instructionIdx > userIdx {
		t.Errorf("init instruction index %d follows the user message index %d — the instruction must precede the user input in the channel (FR-015 — user message 排在指令之后)", instructionIdx, userIdx)
	}
	if instructionIdx != -1 && responseIdx != -1 && instructionIdx > responseIdx {
		t.Errorf("init instruction index %d follows the response index %d — the instruction must precede the player's output of the same activation (FR-015)", instructionIdx, responseIdx)
	}

	// then: the player partition holds NO instruct_player tool_call (the
	// player does not hold the tool — FR-013/FR-009) and no memory call.
	for _, m := range playerLmr.GetMessages() {
		for _, name := range messageToolCallNames(m) {
			if name == "instruct_player" || name == "memory" {
				t.Errorf("player partition carries tool_call %q — calibration/memory tools are planner-only (FR-009/FR-013)", name)
			}
		}
	}

	// then: the planner partition carries the init turn's write-back — the
	// instruct_player tool_call with the fixture's init instruction content
	// (the initInstruction node ran, produced the instruction, and the
	// planner channel persisted it — FR-015). No other scenario has run yet,
	// so the init call is the only instruct_player call in the partition.
	plannerLmr := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "planner")
	initCall := findInstructPlayerCallContent(plannerLmr.GetMessages(), expectedInitInstructionText)
	if initCall != expectedInitInstructionText {
		t.Errorf("planner partition instruct_player content = %q, want %q (the init scenario produced the pinned initial instruction)", initCall, expectedInitInstructionText)
	}
}

// TestTeamReviewInstructionOrder verifies the normal-game-end calibration
// scenario (specs/039-planner-memory-calibration US3 — FR-014/FR-017): after
// the game-ending move, the planner reviews (plannerMessages — invisible to
// the player) and MAY send an instruction via instruct_player; the fixture
// chain always sends one, and it MUST land in the player channel IMMEDIATELY
// AFTER the game-ending tool_result and BEFORE the player's next output — the
// visible order tool_calling → tool_result → planner 指令 → player message
// output (FR-017). The planner's review content (memory calls, review input)
// stays in the planner partition.
func TestTeamReviewInstructionOrder(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, "team-instr-order-"+uniqueSuffix(), "gpt-4", "gpt-4")
	conn := connectAgentWSTrace(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// when: one full game — the first operate click's terminal reply ends
	// the game (the batch stops at op 1), the planner reviews and sends the
	// review instruction (FR-014 — the fixture chain decides to send).
	playTeamGameUntilWait(t, conn, sessionID, buildSaoleiFlowResultScreenshot(saoleiBoardInitPNG),
		buildSaoleiFlowResultScreenshot(saoleiBoardLossPNG))

	// then: the player partition order pins FR-017 — the game-ending
	// tool_result ("stopped at click(3,4) (lost)") is IMMEDIATELY followed by the
	// review instruction, which precedes the player's post-planner output
	// (the next game's init tool_call).
	playerLmr := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "player")
	endResultIdx := messageIndex(playerLmr.GetMessages(), "stopped at click(3,4) (lost)")
	instructionIdx := messageIndex(playerLmr.GetMessages(), expectedReviewInstructionText)
	nextInitIdx := -1
	for i := instructionIdx + 1; i < len(playerLmr.GetMessages()); i++ {
		if messageHasToolCall(playerLmr.GetMessages()[i], "saolei_init") {
			nextInitIdx = i
			break
		}
	}
	if endResultIdx == -1 {
		t.Fatalf("player partition did not surface the game-ending operate tool_result 'stopped at click(3,4) (lost)'")
	}
	if instructionIdx == -1 {
		t.Fatalf("player partition did not surface the review instruction %q (FR-014/FR-017)", expectedReviewInstructionText)
	}
	if instructionIdx != endResultIdx+1 {
		t.Errorf("review instruction index = %d, want %d (immediately after the game-ending tool_result — FR-017 order)", instructionIdx, endResultIdx+1)
	}
	if nextInitIdx == -1 {
		t.Error("player partition did not surface the post-instruction saolei_init output (the player's next activation)")
	} else if instructionIdx > nextInitIdx {
		t.Errorf("review instruction index %d follows the player's next saolei_init output index %d — the instruction must precede the player's next output (FR-017)", instructionIdx, nextInitIdx)
	}

	// then: the planner's review stayed INVISIBLE to the player — the player
	// partition carries no memory tool_call (FR-017: 复盘在 plannerMessages,
	// 对 player 不可见).
	for _, m := range playerLmr.GetMessages() {
		for _, name := range messageToolCallNames(m) {
			if name == "memory" {
				t.Errorf("player partition carries memory tool_call %q — the planner's review must stay invisible to the player (FR-017)", name)
			}
		}
	}

	// then: the planner partition carries the review chain — the memory
	// tool_calls (batch add + old_text replaces) and the instruct_player
	// call whose content is the pinned review instruction (FR-008/FR-014).
	// The partition ALSO carries the async init turn's instruct_player
	// write-back (FR-015), so the review call is matched by content, not by
	// position.
	plannerLmr := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "planner")
	if got := countMemoryCallsInMessages(plannerLmr.GetMessages()); got != 3 {
		t.Errorf("planner partition memory tool_calls = %d, want 3 (batch add + 0-hit + multi-hit — FR-008)", got)
	}
	reviewCall := findInstructPlayerCallContent(plannerLmr.GetMessages(), expectedReviewInstructionText)
	if reviewCall != expectedReviewInstructionText {
		t.Errorf("planner partition instruct_player content = %q, want %q (the review scenario produced the pinned instruction)", reviewCall, expectedReviewInstructionText)
	}
}

// countMemoryCallsInMessages returns the number of `memory` tool_call
// MessageParts across the given Messages (the partition view of
// countMemoryCalls).
func countMemoryCallsInMessages(messages []*game.Message) int {
	count := 0
	for _, m := range messages {
		for _, p := range m.GetContent().GetParts() {
			if tc := p.GetToolCall(); tc != nil && tc.GetName() == "memory" {
				count++
			}
		}
	}
	return count
}

// TestTeamCompressionAtFiveGames verifies US2 (specs/037-saolei-team-optimize/
// spec.md FR-006..FR-012/FR-015) + the 039 compact-instruction scenario
// (specs/039-planner-memory-calibration FR-016): after the 5th game's
// planner returns, the conditional edge routes to the compress node — each
// short-term channel is replaced by exactly ONE summary agent message
// (FR-008), live summary frames are emitted for both tabs (FR-011/SC-004),
// the player STOPS (FR-010 — the turn ends without opening another game),
// the long-term memory (memory service entries + frozen snapshot) survives
// (FR-009 — game 6 still triggers the planner from the summary context), and
// the live summary frame's frame_id equals the reloaded summary message's
// message_id (the desktop dedup anchor, data-model.md §4). The
// postCompactInstruction node then writes the no-game-history compact
// instruction (sample_compact_instruction.yaml keyword "上下文刚被压缩")
// DIRECTLY into the player channel — the turn ends WITHOUT the player being
// invoked (FR-016, 037"压缩后自动停下"一致), and the instruction is consumed
// with the player's NEXT activation (game 6) as plain channel history.
func TestTeamCompressionAtFiveGames(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, "team-compress-"+uniqueSuffix(), "gpt-4", "gpt-4")
	conn := connectAgentWSTrace(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	initScreenshot := buildSaoleiFlowResultScreenshot(saoleiBoardInitPNG)
	terminalScreenshot := buildSaoleiFlowResultScreenshot(saoleiBoardLossPNG)

	// given: 4 completed games — the counter stays below 5, so NO compression
	// fires (FR-006) and the player keeps looping back after each planner.
	for i := 0; i < 4; i++ {
		frames := playTeamGameUntilWait(t, conn, sessionID, initScreenshot, terminalScreenshot)
		if got := countPlannerReviewFrames(frames); got != 1 {
			t.Fatalf("game %d: planner review frames = %d, want 1 (planner trigger precondition)", i+1, got)
		}
	}

	// then: no premature compression after game 4 (the player partition holds
	// 4 turns of messages, not the single compression summary — FR-006).
	if got := len(listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "player").GetMessages()); got <= 1 {
		t.Fatalf("player partition after game 4 = %d messages, want > 1 (compression must NOT fire before counter 5 — FR-006)", got)
	}

	// when: the 5th game ends and the planner returns (counter == 5 — the
	// conditional edge routes to compress instead of back to player, FR-007).
	frames5 := playTeamGameUntilWait(t, conn, sessionID, initScreenshot, terminalScreenshot)
	if got := countPlannerReviewFrames(frames5); got != 1 {
		t.Errorf("game 5: planner review frames = %d, want exactly 1 (the planner still reviewed the 5th game before compression)", got)
	}

	// then: live summary frames were emitted for BOTH channels (FR-011/SC-004)
	// — agent="player" and agent="planner" with the pinned summary content.
	var playerSummaryFrame, plannerSummaryFrame *game.TeamFrame
	for _, f := range frames5 {
		if !frameHasText(f) {
			continue
		}
		switch {
		case f.GetAgent() == "player" && strings.Contains(frameText(f), expectedPlayerCompressionSummary):
			playerSummaryFrame = f
		case f.GetAgent() == "planner" && strings.Contains(frameText(f), expectedPlannerCompressionSummary):
			plannerSummaryFrame = f
		}
	}
	if playerSummaryFrame == nil {
		t.Error("no live player summary frame after game 5 (FR-011)")
	}
	if plannerSummaryFrame == nil {
		t.Error("no live planner summary frame after game 5 (FR-011)")
	}

	// then: the PLAYER channel was shrunk to exactly ONE summary message
	// (FR-008) and the player STOPPED — no new game was opened after the
	// compression (FR-010). The compact instruction is already IN the
	// player partition right after compression: the postCompactInstruction
	// node wrote it directly into playerMessages (same channel write-back
	// as the review node — no pending slot), where it awaits the player's
	// NEXT activation as plain history (FR-016 — 压缩后 turn 结束, player
	// 停下; the instruction is visible to ListMessages immediately).
	playerLmr := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "player")
	if got := len(playerLmr.GetMessages()); got != 2 {
		t.Fatalf("player partition after compression = %d messages, want exactly 2 (compressed summary + compact instruction — FR-008/FR-010/FR-016)", got)
	}
	if got := messageText(playerLmr.GetMessages()[0]); !strings.Contains(got, expectedPlayerCompressionSummary) {
		t.Errorf("player summary message = %q, want to contain %q (FR-008/FR-012)", got, expectedPlayerCompressionSummary)
	}
	if strings.Contains(messageText(playerLmr.GetMessages()[0]), expectedCompactInstructionText) {
		t.Errorf("player summary message = %q — the compact instruction must be its OWN channel message, not merged into the summary (FR-016)", messageText(playerLmr.GetMessages()[0]))
	}
	if got := messageText(playerLmr.GetMessages()[1]); !strings.Contains(got, expectedCompactInstructionText) {
		t.Errorf("player compact instruction message = %q, want to contain %q — the postCompactInstruction node must write the instruction into playerMessages (FR-016)", messageText(playerLmr.GetMessages()[1]))
	}
	// The PLANNER channel is NOT "exactly 1" after compression: the
	// postCompactInstruction node (039 Phase 6 — FR-016, contract §2.3) runs
	// AFTER the compress node and invokes the planner agent again, appending
	// its full message list to plannerMessages — the same input-included
	// write-back as the review node (the compact request HumanMessage joins
	// the channel like the review input, 037 FR-001/FR-003 pattern). The
	// post-compression planner partition is therefore, in order:
	//   [0] the compressed summary (FR-008)
	//   [1] the compact request HumanMessage ("上下文刚被压缩：…")
	//   [2] the instruct_player tool_call (compact instruction content)
	//   [3] its tool_result
	//   [4] the terminal "指令已发送。" text
	// = 5 messages, with NO memory/saolei calls (the instruction agent holds
	// only instruct_player, contract §2.3).
	plannerLmr := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "planner")
	if got := len(plannerLmr.GetMessages()); got != 5 {
		t.Fatalf("planner partition after compression = %d messages, want 5 (summary + compact request + instruct_player call/result + final text — FR-008 + 039 FR-016)", got)
	}
	if got := messageText(plannerLmr.GetMessages()[0]); !strings.Contains(got, expectedPlannerCompressionSummary) {
		t.Errorf("planner summary message = %q, want to contain %q (FR-008/FR-012)", got, expectedPlannerCompressionSummary)
	}
	if got := messageIndex(plannerLmr.GetMessages(), "上下文刚被压缩"); got == -1 {
		t.Error("planner partition does not carry the compact request HumanMessage — the postCompactInstruction input should join the channel (039 FR-016)")
	}
	compactCall := findInstructPlayerCallContent(plannerLmr.GetMessages(), expectedCompactInstructionText)
	if compactCall != expectedCompactInstructionText {
		t.Errorf("planner partition instruct_player content = %q, want %q (the compact scenario produced the pinned instruction — FR-016)", compactCall, expectedCompactInstructionText)
	}
	if !messagesContainText(plannerLmr.GetMessages(), "指令已发送。") {
		t.Error("planner partition does not carry the instruct_player final text \"指令已发送。\" — the compact instruction loop did not terminate deterministically")
	}
	for _, m := range plannerLmr.GetMessages() {
		for _, name := range messageToolCallNames(m) {
			if name == "memory" || strings.HasPrefix(name, "saolei_") {
				t.Errorf("planner partition carries tool_call %q after compression — the instruction agent holds only instruct_player (contract §2.3)", name)
			}
		}
	}

	// then: the live summary frame's frame_id equals the reloaded summary
	// message's message_id — the desktop dedup anchor (data-model.md §4 /
	// research.md D9; the FR-003 dedup rule applied to compression summaries).
	if playerSummaryFrame != nil && playerSummaryFrame.GetFrameId() != "" {
		if got := playerLmr.GetMessages()[0].GetMessageId(); got != playerSummaryFrame.GetFrameId() {
			t.Errorf("player summary message_id = %q, want frame_id %q (dedup anchor)", got, playerSummaryFrame.GetFrameId())
		}
	}
	if plannerSummaryFrame != nil && plannerSummaryFrame.GetFrameId() != "" {
		if got := plannerLmr.GetMessages()[0].GetMessageId(); got != plannerSummaryFrame.GetFrameId() {
			t.Errorf("planner summary message_id = %q, want frame_id %q (dedup anchor)", got, plannerSummaryFrame.GetFrameId())
		}
	}

	// when: a 6th game (user input after compression).
	frames6 := playTeamGameUntilWait(t, conn, sessionID, initScreenshot, terminalScreenshot)

	// then: the long-term memory survived the compression (FR-009) — the
	// planner still triggers and the memory flow completes (the batch add
	// dedupes: "applied 0 operation(s)" — FR-008)...
	if got := countPlannerReviewFrames(frames6); got != 1 {
		t.Errorf("game 6: planner review frames = %d, want exactly 1 (memory layer survived compression — FR-009)", got)
	}
	if got := countMemoryCalls(frames6); got != 3 {
		t.Errorf("game 6: memory calls = %d, want exactly 3 (the review chain ran after compression — FR-008)", got)
	}
	foundDedupe := false
	for _, f := range frames6 {
		for _, p := range frameMessageParts(f).GetParts() {
			if tr := p.GetToolResult(); tr != nil && strings.Contains(tr.GetMessage(), "memory: applied 0 operation(s)") {
				foundDedupe = true
			}
		}
	}
	if !foundDedupe {
		t.Error("game 6: no 'memory: applied 0 operation(s)' tool_result — the entries did not survive compression (FR-009/FR-008)")
	}
	if got := len(listMemoryContents(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)); got != 2 {
		t.Errorf("memory entries after game 6 = %d, want 2 (long-term memory survived compression — FR-009)", got)
	}

	// ...the compact instruction was consumed with the player's NEXT
	// activation (FR-016): the player partition grew beyond the summary +
	// instruction and the instruction still sits right after the summary
	// (already visible since the compression, now followed by game 6's
	// messages).
	playerLmr = listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "player")
	if got := len(playerLmr.GetMessages()); got <= 2 {
		t.Errorf("player partition after game 6 = %d messages, want > 2 (player resumed with the summary context — FR-010)", got)
	}
	compactIdx := messageIndex(playerLmr.GetMessages(), expectedCompactInstructionText)
	if compactIdx == -1 {
		t.Errorf("player partition after game 6 did not surface the compact instruction %q — the instruction was not delivered with the next activation (FR-016)", expectedCompactInstructionText)
	} else if compactIdx != 1 {
		t.Errorf("compact instruction index = %d — it must sit right after the compressed summary at index 1 (FR-016)", compactIdx)
	}
}

// TestTeamPlannerToolSurfaceAfterStrategyRemoval verifies the planner's tool
// surface after the shared-strategy removal (specs/039-planner-memory-
// calibration US2/US3 — FR-008/FR-009/FR-013/FR-018): the planner's actual
// tool set is exactly `memory` + `instruct_player` — it never calls a
// saolei_* tool (the description injection added text, NOT tools, FR-018)
// and never calls update_strategy (the StrategyStore is gone, FR-013); the
// player never calls memory/instruct_player (FR-009).
//
// The prompt-content half (US3 FR-016 — the "## Player 可用工具" description
// section listing saolei_operate + click/flag/chord; 039 FR-020 — the memory
// skill body injected via appendSkillBodyToPrompt) is NOT directly
// observable from the WS/HTTP surface: the keyword matcher only reads user
// text (testplan/README.md §4). It is verified via LOG/TRACE instead — the
// deployed fake-llm logs every request's system messages at INFO ("system
// prompt received" — projects/game/fake-llm/service/handler.go
// logSystemPrompts), so after this test drives a game an operator can query
// the fake-llm logs (signoz, trace_id printed by traceContext) for the
// injected memory skill body and the player-tool description section — the
// same verification channel the 037 test documented
// (specs/037-saolei-team-optimize FR-016).
func TestTeamPlannerToolSurfaceAfterStrategyRemoval(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, "team-tools-"+uniqueSuffix(), "gpt-4", "gpt-4")
	conn := connectAgentWSTrace(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// when: one full game — the planner's createAgent request (carrying the
	// system prompt with the memory skill body + the tool-description
	// section) is sent to fake-llm.
	frames := playTeamGameUntilWait(t, conn, sessionID, buildSaoleiFlowResultScreenshot(saoleiBoardInitPNG),
		buildSaoleiFlowResultScreenshot(saoleiBoardLossPNG))

	// then: every planner tool_call in the live stream is memory or
	// instruct_player — no player tool was injected into the planner's tool
	// set (FR-018) and no update_strategy remains (FR-013).
	for _, f := range frames {
		if f.GetAgent() != "planner" {
			continue
		}
		for _, p := range frameMessageParts(f).GetParts() {
			if tc := p.GetToolCall(); tc != nil {
				switch tc.GetName() {
				case "memory", "instruct_player":
				default:
					t.Errorf("planner live tool_call %q — the planner MUST hold only memory + instruct_player (spec 039 FR-008/FR-013/FR-018)", tc.GetName())
				}
			}
		}
	}

	// then: the reloaded planner partition carries no saolei / update_strategy
	// tool_call either (FR-018/FR-013), and the player partition carries no
	// memory / instruct_player tool_call (FR-009).
	plannerLmr := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "planner")
	for _, m := range plannerLmr.GetMessages() {
		for _, name := range messageToolCallNames(m) {
			if strings.HasPrefix(name, "saolei_") || name == "update_strategy" {
				t.Errorf("planner partition carries tool_call %q — the planner MUST hold only memory + instruct_player (spec 039 FR-013/FR-018)", name)
			}
		}
	}
	playerLmr := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "player")
	for _, m := range playerLmr.GetMessages() {
		for _, name := range messageToolCallNames(m) {
			if name == "memory" || name == "instruct_player" {
				t.Errorf("player partition carries tool_call %q — memory/calibration tools are planner-only (FR-009)", name)
			}
		}
	}
}

// TestTeamReviewInputGameStats verifies US5 (specs/037-saolei-team-optimize/
// spec.md FR-026..FR-033): the saolei MCP computes per-game stats first-hand
// and they flow onGameEnd → ephemeral buffer → the planner's review input
// (FR-031/FR-032), visible both as the real-time frame (US1) and in the
// reloaded planner partition.
//
// Expected values for the fixture pair (saolei_1 init → saolei_5 terminal):
//   - operationCount = 1 — exactly one successful cell op (the first op of
//     the operate batch whose post-dispatch recognition is terminal; the
//     batch stops at op 1 — init/re-init and skipped ops never fire onMove —
//     FR-027/FR-002).
//   - correctFlags = 40 − 42 − 1 = −3 — the init board saolei_1.png's mine
//     counter decodes to 40 (verified with `saolei-recognize --json`); the
//     terminal loss board saolei_5.png has 42 MINE + 1 HIT_MINE cells
//     (golden board, projects/game/pkg/saolei-board/testdata/saolei_5.golden.
//     txt — `M`=MINE, `X`=HIT_MINE per src/core/render.ts). The two
//     screenshots are DIFFERENT games (43 vs 40 mines), so the formula
//     yields a NEGATIVE value — the cross-board fixture is artificial, but
//     it pins the exact FR-028 computation end-to-end.
//   - avgOpsPerMine = "N/A" — the computeGameStats guard is `correctFlags >
//     0` (FR-029's division guard), so a non-positive correctFlags (0 or
//     negative, as here) degrades to "N/A" rather than NaN/Infinity. The
//     positive-value path (e.g. 2.33) is pinned by the saolei-mcp unit tests
//     (saolei-mcp.test.ts computeGameStats) with known boards.
func TestTeamReviewInputGameStats(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, "team-stats-"+uniqueSuffix(), "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// when: one full game (init in-progress → first operate click terminal
	// loss).
	frames := playTeamGameUntilWait(t, conn, sessionID, buildSaoleiFlowResultScreenshot(saoleiBoardInitPNG),
		buildSaoleiFlowResultScreenshot(saoleiBoardLossPNG))

	// then: the real-time review frame carries the stats section with the
	// exact computed values (FR-032/FR-001 — the stats are part of the
	// review input, hence of the live frame).
	liveReview := findPlannerReviewFrame(frames)
	if liveReview == "" {
		t.Fatal("no real-time planner frame carrying the review input")
	}
	for _, want := range []string{
		"本局统计数据：",
		"- 操作次数：1",
		"- 正确标记地雷数：-3",
		"- 每雷平均操作数：N/A",
	} {
		if !strings.Contains(liveReview, want) {
			t.Errorf("live review input missing %q (FR-032)", want)
		}
	}

	// then: the reloaded planner partition carries the same stats section
	// (FR-032 — the history path is identical to the live path).
	plannerLmr := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "planner")
	reloadedReview := findPlannerReviewMessage(plannerLmr.GetMessages())
	if reloadedReview == "" {
		t.Fatal("reloaded planner partition does not carry the review input")
	}
	for _, want := range []string{
		"本局统计数据：",
		"- 操作次数：1",
		"- 正确标记地雷数：-3",
		"- 每雷平均操作数：N/A",
	} {
		if !strings.Contains(reloadedReview, want) {
			t.Errorf("reloaded review input missing %q (FR-032)", want)
		}
	}
}

// ─── 041 real-time init delivery (FR-001..FR-007) ──────────────────────────

// initRequestPrefix keys the init scenario's planner request frame and its
// persisted plannerMessages entry: buildInstructionRequest("init") always
// starts with this text (projects/game/agent/src/team/instruction-node.ts:
// 104-112), and the fake-LLM matches it deterministically to the
// instruct_player tool_call (projects/game/fake-llm/service/testdata/
// sample_init_instruction.yaml — spec 039-planner-memory-calibration
// FR-015). No user-turn text the large tests send contains it, so no other
// suite can match it.
const initRequestPrefix = "团队初始化"

// The three init instruction frame kinds (specs/041-realtime-init-push/
// specs/041-realtime-init-push/contracts/realtime-channel-contract.md §2.2 / specs/041-realtime-init-push/data-model.md §3.3): the
// planner request (agent=planner, role=USER, text), the planner response
// (agent=planner, role=AGENT, toolCall instruct_player) and the player
// write-back (agent=player, role=USER, text). initFrameKind classifies a
// frame into one of these, or "".
const (
	initFrameRequest   = "planner-request"
	initFrameResponse  = "planner-response"
	initFrameWriteback = "player-writeback"
)

// TestTeamInitRealtimeDelivery verifies the real-time init instruction
// delivery over the live Connect stream (specs/041-realtime-init-push/
// quickstart.md §B B1/B2/B3; spec FR-001/FR-003/FR-006; SC-001/SC-002/
// SC-003): after UpdateTeam(allow_missing=true) materializes the team (the
// one-shot initInstruction turn is triggered fire-and-forget, FR-005), a
// Connect with NO user message must surface the instruction on first entry.
// Two timings are possible and both are verified (specs/041-realtime-init-push/research.md D1):
//
//   - init still in flight at Connect → the three frames are pushed through
//     the stream-bound sink in real time (specs/041-realtime-init-push/contracts/realtime-channel-contract.md §2.2 — planner request
//     USER / planner response AGENT toolCall / player write-back USER),
//     each with frameId == the persisted message id (specs/041-realtime-init-push/contracts/realtime-channel-contract.md §4 — the
//     FR-004 dedup anchor, verified via ListMessages);
//   - init completed before the sink bound → the emit was a no-op (contract
//     §1.2 / specs/041-realtime-init-push/research.md D9 — spec edge case 3) and the instruction is
//     delivered from history: both partitions hold it exactly once.
//
// Either way the status probe answers IDLE (B2 — FR-003/SC-002: isRunning
// deliberately excludes the init turn, projects/game/agent/src/session-team.ts:546
// (isRunning)) and no user interaction occurred (B3 — FR-002/SC-003:
// background delivery).
func TestTeamInitRealtimeDelivery(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	// given: a fresh session whose team was just materialized — the one-shot
	// async initInstruction turn was triggered by the UpdateTeam and is
	// likely still in flight (fire-and-forget, FR-005 — materialization
	// returns before the planner model call completes, specs/041-realtime-init-push/research.md D1).
	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, "team-init-live-"+uniqueSuffix(), "gpt-4", "gpt-4")

	// when: connect immediately (the first inbound frame — the status probe —
	// binds the stream display sink, specs/041-realtime-init-push/contracts/realtime-channel-contract.md §1.1) and probe, then read
	// frames WITHOUT any user message until the init delivery resolves
	// (real-time push or history, ≤ 10 s per SC-001).
	conn := connectAgentWSTrace(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// then: the status probe answers IDLE (B2 — FR-003/SC-002): the init
	// turn runs outside the TurnLoop and isRunning() excludes initInFlight,
	// so the typing indicator must not be driven by it
	// (projects/game/agent/src/session-team.ts:546 isRunning; specs/041-realtime-init-push/contracts/realtime-channel-contract.md §5).
	// IDLE holds whether the probe lands during the init or after it
	// completed — the init must never report ACTIVE.
	sendStatusFrame(t, conn, sessionID, game.StatusSignalStatus_STATUS_SIGNAL_STATUS_ACTIVE)
	probeResp := drainWSFrame(t, conn, func(f *game.TeamFrame) bool { return frameStatus(f) != nil })
	if probeResp == nil {
		t.Fatal("no status probe response on connect")
	}
	if got := frameStatus(probeResp).GetStatus(); got != game.StatusSignalStatus_STATUS_SIGNAL_STATUS_IDLE {
		t.Errorf("status probe on entry = %v, want IDLE (FR-003/SC-002)", got)
	}

	frames := collectInitDeliveryFrames(t, conn, sessionID, sutHostURL, sutEnvName, saoleiTemplateID)

	if initFramesComplete(frames) {
		// then: the three frames arrived through the stream IN ORDER, tagged
		// per specs/041-realtime-init-push/contracts/realtime-channel-contract.md §2.2 (B1/B3 — FR-001/FR-006/SC-001/SC-003 — the
		// real-time push path).
		if kinds := initFrameKindsInOrder(frames); !slices.Equal(kinds, []string{initFrameRequest, initFrameResponse, initFrameWriteback}) {
			t.Errorf("init frame order = %v, want [%s %s %s] (contract §2.2)", kinds, initFrameRequest, initFrameResponse, initFrameWriteback)
		}
		requestFrame := initFrameByKind(frames, initFrameRequest)
		responseFrame := initFrameByKind(frames, initFrameResponse)
		writebackFrame := initFrameByKind(frames, initFrameWriteback)

		// then: the init turn emitted no wait/status FlowPart (specs/041-realtime-init-push/contracts/realtime-channel-contract.md §2.4
		// — the init runs outside the TurnLoop and must not drive the typing
		// indicator; the probe response above was consumed before the
		// collection started).
		for _, f := range frames {
			if frameWait(f) != nil || frameStatus(f) != nil {
				t.Errorf("init turn emitted a wait/status control frame — contract §2.4 forbids it: %v", f.GetFlowParts().GetParts())
			}
		}

		// then: each frameId equals the persisted message id (FR-004 dedup
		// anchor, specs/041-realtime-init-push/contracts/realtime-channel-contract.md §4) and the persisted message carries the frame's
		// content — the real-time push and the ListMessages view are the
		// SAME messages (specs/041-realtime-init-push/research.md D3 faithful mirroring).
		persisted := waitForMessageID(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "planner", requestFrame.GetFrameId(), 5*time.Second)
		if persisted == nil {
			t.Errorf("planner request frameId %q not found in the planner partition — frameId must equal the persisted message id (FR-004)", requestFrame.GetFrameId())
		} else {
			if !strings.Contains(messageText(persisted), initRequestPrefix) {
				t.Errorf("planner request message text = %q, want to contain %q (contract §2.2)", messageText(persisted), initRequestPrefix)
			}
			if persisted.GetRole() != game.MessageRole_MESSAGE_ROLE_USER {
				t.Errorf("planner request message role = %v, want USER (contract §2.2)", persisted.GetRole())
			}
		}
		persisted = waitForMessageID(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "planner", responseFrame.GetFrameId(), 5*time.Second)
		if persisted == nil {
			t.Errorf("planner response frameId %q not found in the planner partition — frameId must equal the persisted message id (FR-004)", responseFrame.GetFrameId())
		} else {
			if !messageHasToolCall(persisted, "instruct_player") {
				t.Errorf("planner response message does not carry the instruct_player tool_call (contract §2.2)")
			}
			if args := messageToolCallArgsJSON(persisted, "instruct_player"); !strings.Contains(args, expectedInitInstructionText) {
				t.Errorf("planner response instruct_player args_json = %q, want to contain %q (contract §2.2)", args, expectedInitInstructionText)
			}
			if persisted.GetRole() != game.MessageRole_MESSAGE_ROLE_AGENT {
				t.Errorf("planner response message role = %v, want AGENT (contract §2.2)", persisted.GetRole())
			}
		}
		persisted = waitForMessageID(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "player", writebackFrame.GetFrameId(), 5*time.Second)
		if persisted == nil {
			t.Errorf("player write-back frameId %q not found in the player partition — frameId must equal the persisted message id (FR-004)", writebackFrame.GetFrameId())
		} else {
			if messageText(persisted) != expectedInitInstructionText {
				t.Errorf("player write-back message text = %q, want %q (contract §2.2)", messageText(persisted), expectedInitInstructionText)
			}
			if persisted.GetRole() != game.MessageRole_MESSAGE_ROLE_USER {
				t.Errorf("player write-back message role = %v, want USER (contract §2.2)", persisted.GetRole())
			}
		}
	} else {
		// History path (spec edge case 3 — specs/041-realtime-init-push/research.md D1/D7): the init
		// completed before the sink bound, so no real-time frame was ever
		// emitted (specs/041-realtime-init-push/contracts/realtime-channel-contract.md §1.2 — emitting through an unbound sink is a
		// no-op). The collector returned only once the instruction was
		// persisted, so a partial frame set here is a real bug: the init
		// emission is BATCH (post-invoke, specs/041-realtime-init-push/contracts/realtime-channel-contract.md §2.1), all-or-nothing.
		if len(frames) != 0 {
			t.Fatalf("init delivery unresolved: %d frame(s) arrived but the three init frames are incomplete (batch emission, contract §2.1): %v", len(frames), frames)
		}
		playerLmr := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "player")
		if !messagesContainText(playerLmr.GetMessages(), expectedInitInstructionText) {
			t.Fatalf("the init turn did not deliver within 10 s — no real-time frame and no persisted instruction (SC-001: the planner model must answer within 10 s)")
		}
		t.Logf("init completed before the sink bound — the instruction is delivered from history instead of a real-time push (spec edge case 3)")
	}

	// then: regardless of the delivery path, the instruction is persisted
	// EXACTLY once in each partition (FR-004 / US1 acceptance scenario 2 —
	// no duplicate rendering; the frameId == messageId anchor collapses
	// seed/history/push onto one id namespace, specs/041-realtime-init-push/contracts/realtime-channel-contract.md §4).
	playerLmr := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "player")
	playerInstructionCount := 0
	for _, m := range playerLmr.GetMessages() {
		if strings.Contains(messageText(m), expectedInitInstructionText) {
			playerInstructionCount++
		}
	}
	if playerInstructionCount != 1 {
		t.Errorf("player partition instruction count = %d, want exactly 1 (FR-004)", playerInstructionCount)
	}
	plannerLmr := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "planner")
	plannerRequestCount := 0
	plannerCallCount := 0
	for _, m := range plannerLmr.GetMessages() {
		if strings.Contains(messageText(m), initRequestPrefix) {
			plannerRequestCount++
		}
		if args := messageToolCallArgsJSON(m, "instruct_player"); strings.Contains(args, expectedInitInstructionText) {
			plannerCallCount++
		}
	}
	if plannerRequestCount != 1 {
		t.Errorf("planner partition request count = %d, want exactly 1 (FR-004)", plannerRequestCount)
	}
	if plannerCallCount != 1 {
		t.Errorf("planner partition instruct_player call count = %d, want exactly 1 (FR-004)", plannerCallCount)
	}
}

// TestTeamInitDestructiveOpsRejected verifies FR-007/SC-005 (quickstart §B
// B4; specs/041-realtime-init-push/contracts/realtime-channel-contract.md §5): while the one-shot init turn is in flight, the
// destructive operations — RefreshTeam and a profile-change rebuild
// (UpdateTeam with a different profile) — are rejected with
// FAILED_PRECONDITION (isBusy includes initInFlight while isRunning does
// not — projects/game/agent/src/session-team.ts:563 (isBusy); the gateway
// maps FailedPrecondition to HTTP 400, see TestTeamUpdateRebuildInFlightRejected).
// Once the init completes, the same operations succeed.
//
// The init window (the planner model call — seconds per spec Assumption line
// 105) is caught deterministically: each op is fired as the FIRST HTTP call
// after its own UpdateTeam materialization returned. The one-shot init
// starts BEFORE UpdateTeam responds (session-team.ts:925 —
// triggerInitInstruction inside factory.then) and its planner model call
// outlives the few ms the op's request needs to reach the agent, so the
// request lands while the init is certainly in flight. Each op runs on its
// OWN session (table case): a shared session would race the two requests
// against each other — the first op's round trip can consume the window the
// second op needs (the T011 flake: RefreshTeam caught the window, the
// profile-change rebuild missed it).
func TestTeamInitDestructiveOpsRejected(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	// given: per-case teams — one session per destructive op (each op fires
	// as its session's first post-materialization call, so the two ops never
	// race each other for the init window); the rebuild case also needs a
	// second profile to switch to.
	tests := []struct {
		name           string
		profileID      string
		otherProfileID string // profile-change rebuild target (empty for RefreshTeam)
		fire           func(t *testing.T, sutHostURL, sutEnvName, sessionID, otherProfileID string) (int, []byte)
	}{
		{
			name:      "RefreshTeam",
			profileID: fmt.Sprintf("team-init-busy-refresh-%s", uniqueSuffix()),
			fire: func(t *testing.T, host, env, sessionID, otherProfileID string) (int, []byte) {
				return refreshTeamWithStatus(t, host, env, saoleiTemplateID, sessionID)
			},
		},
		{
			name:           "profile-change rebuild",
			profileID:      fmt.Sprintf("team-init-busy-rebuild-%s", uniqueSuffix()),
			otherProfileID: fmt.Sprintf("team-init-busy-rebuild-other-%s", uniqueSuffix()),
			fire: func(t *testing.T, host, env, sessionID, otherProfileID string) (int, []byte) {
				return updateTeamWithStatus(t, host, env, saoleiTemplateID, sessionID, otherProfileID)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given: a materialized team — the one-shot init turn was
			// triggered fire-and-forget inside the UpdateTeam
			// (session-team.ts:925 — FR-005) and is in flight for the
			// duration of its planner model call. The rebuild case's second
			// profile is created BEFORE materialization: the op must fire as
			// the FIRST HTTP call after the UpdateTeam return, and any
			// intervening request would consume the init window.
			if tt.otherProfileID != "" {
				createTeamProfile(t, sutHostURL, sutEnvName, saoleiTemplateID, tt.otherProfileID, "gpt-4", "gpt-4")
			}
			sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, tt.profileID, "gpt-4", "gpt-4")

			// when: fire the destructive op as the FIRST HTTP call after
			// materialization (no connect — the init window is what is being
			// caught; nothing between the UpdateTeam return and this request
			// can consume the window).
			status, body := tt.fire(t, sutHostURL, sutEnvName, sessionID, tt.otherProfileID)

			// then: FAILED_PRECONDITION — grpc-gateway maps it to HTTP 400
			// (FR-007/SC-005; the mapping is pinned by
			// TestTeamUpdateRebuildInFlightRejected).
			if status != http.StatusBadRequest {
				t.Fatalf("%s during init: status=%d, want 400 FAILED_PRECONDITION (FR-007/SC-005), body=%s", tt.name, status, body)
			}
			t.Logf("%s rejected during init: %d %s", tt.name, status, body)

			// when: the init completes (the instruction write-back persists —
			// the init turn's completion marker, specs/041-realtime-init-push/contracts/realtime-channel-contract.md §2.2).
			waitForInitInstructionPersisted(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)

			// then: the same operation succeeds once the init is done
			// (FR-007 — the gate lifts).
			status, body = tt.fire(t, sutHostURL, sutEnvName, sessionID, tt.otherProfileID)
			if status != http.StatusOK {
				t.Fatalf("%s after init: status=%d, want 200 (FR-007 — the same operation must succeed once the init is done), body=%s", tt.name, status, body)
			}
			t.Logf("%s succeeded after init: %d %s", tt.name, status, body)
		})
	}
}

// TestTeamInitNoDuplicateReentry verifies FR-004 / US1 acceptance scenario 2
// (specs/041-realtime-init-push/quickstart.md §B B5; specs/041-realtime-init-push/contracts/realtime-channel-contract.md §4): after the one-shot init turn completed, a
// (re-)entry delivers the instruction EXACTLY once — from history, with no
// real-time re-push. The init runs once per session lifecycle
// (projects/game/agent/src/session-team.ts:337 (triggerInitInstruction) —
// never re-triggered by re-entry or rebuild), and its emission was a no-op
// while the sink was unbound (specs/041-realtime-init-push/research.md D1/D7), so a second Connect must
// receive no init frames; the frontend's renderedMessageIds dedup by
// frameId == messageId (specs/041-realtime-init-push/contracts/realtime-channel-contract.md §4) is the rendering guarantee, and this
// test pins its service-side half: the history holds the instruction exactly
// once and the connection pushes nothing.
func TestTeamInitNoDuplicateReentry(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	// given: a session whose init turn ALREADY completed — the instruction is
	// persisted in the player partition (spec edge case 3: init completes
	// before the user connects; the history seed delivers it on entry).
	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, "team-init-reentry-"+uniqueSuffix(), "gpt-4", "gpt-4")
	waitForInitInstructionPersisted(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)

	// when: enter the session (connect #1 — the sink binds AFTER the init
	// completed, so the real-time push is a no-op, specs/041-realtime-init-push/contracts/realtime-channel-contract.md §1.2)…
	conn1 := connectAgentWSTrace(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	sendStatusFrame(t, conn1, sessionID, game.StatusSignalStatus_STATUS_SIGNAL_STATUS_ACTIVE)
	probeResp := drainWSFrame(t, conn1, func(f *game.TeamFrame) bool { return frameStatus(f) != nil })
	if probeResp == nil {
		t.Fatal("connect #1: no status probe response")
	}
	if got := frameStatus(probeResp).GetStatus(); got != game.StatusSignalStatus_STATUS_SIGNAL_STATUS_IDLE {
		t.Errorf("connect #1 probe = %v, want IDLE (no turn running)", got)
	}
	if frame, err := readWSFrameNoFatal(conn1, 2*time.Second); err == nil {
		t.Errorf("connect #1 received an unexpected frame after the probe — the init completed before connect, no real-time push expected: agent=%s role=%s (FR-004)", frame.GetAgent(), roleString(frame.GetRole()))
	}
	conn1.Close()

	// …then re-enter the session (connect #2): the instruction must NOT be
	// pushed again (B5 — the one-shot init never re-emits; FR-004).
	conn2 := connectAgentWSTrace(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn2.Close()
	sendStatusFrame(t, conn2, sessionID, game.StatusSignalStatus_STATUS_SIGNAL_STATUS_ACTIVE)
	probeResp = drainWSFrame(t, conn2, func(f *game.TeamFrame) bool { return frameStatus(f) != nil })
	if probeResp == nil {
		t.Fatal("connect #2: no status probe response")
	}
	if got := frameStatus(probeResp).GetStatus(); got != game.StatusSignalStatus_STATUS_SIGNAL_STATUS_IDLE {
		t.Errorf("connect #2 probe = %v, want IDLE (no turn running)", got)
	}
	if frame, err := readWSFrameNoFatal(conn2, 2*time.Second); err == nil {
		t.Errorf("re-entry received an unexpected frame — no duplicate delivery on re-connect: agent=%s role=%s (FR-004/B5)", frame.GetAgent(), roleString(frame.GetRole()))
	}

	// then: the instruction is visible from history immediately (no delay,
	// US1 acceptance scenario 2) and exactly once per partition (no
	// duplicate rendering, FR-004).
	playerLmr := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "player")
	playerInstructionCount := 0
	for _, m := range playerLmr.GetMessages() {
		if strings.Contains(messageText(m), expectedInitInstructionText) {
			playerInstructionCount++
		}
	}
	if playerInstructionCount != 1 {
		t.Errorf("player partition instruction count = %d, want exactly 1 (FR-004)", playerInstructionCount)
	}
	plannerLmr := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "planner")
	plannerRequestCount := 0
	plannerCallCount := 0
	for _, m := range plannerLmr.GetMessages() {
		if strings.Contains(messageText(m), initRequestPrefix) {
			plannerRequestCount++
		}
		if args := messageToolCallArgsJSON(m, "instruct_player"); strings.Contains(args, expectedInitInstructionText) {
			plannerCallCount++
		}
	}
	if plannerRequestCount != 1 {
		t.Errorf("planner partition request count = %d, want exactly 1 (FR-004)", plannerRequestCount)
	}
	if plannerCallCount != 1 {
		t.Errorf("planner partition instruct_player call count = %d, want exactly 1 (FR-004)", plannerCallCount)
	}
}

// ─── 041 helpers (init delivery collection / classification) ───────────────

// collectInitDeliveryFrames reads WS frames until the init delivery is
// resolved, returning every frame read in order (the caller consumes the
// status probe response first — the init frames always follow it, because
// the sink binds only when the probe is processed, specs/041-realtime-init-push/contracts/realtime-channel-contract.md §1.1):
//
//   - all three real-time init frames arrived (specs/041-realtime-init-push/contracts/realtime-channel-contract.md §2.2 — the
//     real-time push path), or
//   - no init frame arrived AND the instruction is persisted in the player
//     partition (the init completed before the sink bound — the history
//     path, specs/041-realtime-init-push/contracts/realtime-channel-contract.md §1.2 / specs/041-realtime-init-push/research.md D9; a buffered real-time frame
//     would have been returned by the read BEFORE the history check, so a
//     persisted instruction proves no push is coming), or
//   - the 10 s cap elapses (SC-001 — the planner model's response time).
//
// The read deadline is short so the persisted-state check can interleave
// with the frame reads.
func collectInitDeliveryFrames(t *testing.T, conn *websocket.Conn, sessionID, sutHostURL, sutEnvName, template string) []*game.TeamFrame {
	t.Helper()
	const (
		initDeliveryCap = 10 * time.Second
		perReadDeadline = 1 * time.Second
	)
	stopAt := time.Now().Add(initDeliveryCap)
	var frames []*game.TeamFrame
	for time.Now().Before(stopAt) {
		remaining := time.Until(stopAt)
		if remaining > perReadDeadline {
			remaining = perReadDeadline
		}
		frame, err := readWSFrameNoFatal(conn, remaining)
		if err == nil {
			frames = append(frames, frame)
			if initFramesComplete(frames) {
				return frames
			}
			continue
		}
		if !framesContainInitFrame(frames) {
			playerLmr := listMessages(t, sutHostURL, sutEnvName, template, sessionID, "player")
			if messagesContainText(playerLmr.GetMessages(), expectedInitInstructionText) {
				return frames
			}
		}
	}
	return frames
}

// initFrameKind classifies a frame into one of the three init instruction
// frame kinds (specs/041-realtime-init-push/contracts/realtime-channel-contract.md §2.2), or "" when the frame carries none. The
// toolCall check comes first so a frame carrying both a toolCall and text
// still classifies as the planner response.
func initFrameKind(f *game.TeamFrame) string {
	if tc := frameToolCall(f); tc != nil && tc.GetName() == "instruct_player" &&
		f.GetAgent() == "planner" && f.GetRole() == game.MessageRole_MESSAGE_ROLE_AGENT {
		return initFrameResponse
	}
	if !frameHasText(f) {
		return ""
	}
	switch {
	case f.GetAgent() == "planner" && f.GetRole() == game.MessageRole_MESSAGE_ROLE_USER && strings.Contains(frameText(f), initRequestPrefix):
		return initFrameRequest
	case f.GetAgent() == "player" && f.GetRole() == game.MessageRole_MESSAGE_ROLE_USER && strings.Contains(frameText(f), expectedInitInstructionText):
		return initFrameWriteback
	}
	return ""
}

// initFramesComplete reports whether all three init instruction frames
// (specs/041-realtime-init-push/contracts/realtime-channel-contract.md §2.2) are present in the given frames.
func initFramesComplete(frames []*game.TeamFrame) bool {
	seen := map[string]bool{}
	for _, f := range frames {
		if kind := initFrameKind(f); kind != "" {
			seen[kind] = true
		}
	}
	return seen[initFrameRequest] && seen[initFrameResponse] && seen[initFrameWriteback]
}

// framesContainInitFrame reports whether any of the given frames is one of
// the three init instruction frames (specs/041-realtime-init-push/contracts/realtime-channel-contract.md §2.2).
func framesContainInitFrame(frames []*game.TeamFrame) bool {
	for _, f := range frames {
		if initFrameKind(f) != "" {
			return true
		}
	}
	return false
}

// initFrameByKind returns the first frame of the given init frame kind, or
// nil when none is present.
func initFrameByKind(frames []*game.TeamFrame, kind string) *game.TeamFrame {
	for _, f := range frames {
		if initFrameKind(f) == kind {
			return f
		}
	}
	return nil
}

// initFrameKindsInOrder returns the ordered sequence of init frame kinds
// carried by the given frames (non-init frames are skipped).
func initFrameKindsInOrder(frames []*game.TeamFrame) []string {
	var kinds []string
	for _, f := range frames {
		if kind := initFrameKind(f); kind != "" {
			kinds = append(kinds, kind)
		}
	}
	return kinds
}

// waitForMessageID polls the given agent partition until a Message with the
// given message id is persisted, returning it (nil on timeout). The init
// frames are emitted BEFORE the outer checkpoint save resolves (contract
// §2.1 — the node emits, then the graph saves the state), so the
// ListMessages view can lag the real-time frame by one superstep.
func waitForMessageID(t *testing.T, sutHostURL, sutEnvName, template, sessionID, agent, id string, timeout time.Duration) *game.Message {
	t.Helper()
	stopAt := time.Now().Add(timeout)
	for time.Now().Before(stopAt) {
		lmr := listMessages(t, sutHostURL, sutEnvName, template, sessionID, agent)
		for _, m := range lmr.GetMessages() {
			if m.GetMessageId() == id {
				return m
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}

// waitForInitInstructionPersisted polls the player partition until the init
// instruction write-back is persisted — the completion marker of the one-shot
// async init turn (the write-back lands in playerMessages at the checkpoint
// save; specs/041-realtime-init-push/contracts/realtime-channel-contract.md §2.2/§5, specs/041-realtime-init-push/research.md D1). Fails the test when the
// instruction does not persist within 10 s (SC-001 — the planner model call
// completes in seconds, spec Assumption line 105).
func waitForInitInstructionPersisted(t *testing.T, sutHostURL, sutEnvName, template, sessionID string) {
	t.Helper()
	stopAt := time.Now().Add(10 * time.Second)
	for time.Now().Before(stopAt) {
		lmr := listMessages(t, sutHostURL, sutEnvName, template, sessionID, "player")
		if messagesContainText(lmr.GetMessages(), expectedInitInstructionText) {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("init instruction %q did not persist within 10 s — the init turn did not complete (SC-001)", expectedInitInstructionText)
}
