// Package testplan contains the saolei TEAM large-test suite.
//
// saolei_team_test.go validates the saolei template's team behaviour
// end-to-end against the deployed stack (specs/031-team-template-mode
// quickstart.md §2.1 case table; spec FR-030): team connect + lifecycle
// (FR-003/FR-004/FR-033), TeamProfile CRUD (FR-006/FR-027), per-TeamProfile
// model resolution, player-exclusive desktop control (FR-010), planner
// trigger exactly once per game end (FR-011/D6), strategy persistence and
// sharing (FR-013/FR-014/FR-015), RefreshTeam short-term clearing (FR-018),
// and per-agent message partitioning (FR-005).
//
// It replaces the three pre-031 suites that covered removed behaviour
// (specs/031-team-template-mode/tasks.md Phase 8 T029): the prompt suite
// (AgentProfile/Skill CRUD → TeamProfile CRUD), the session-agent lifecycle
// suite (profile switching/GetAgent → team lifecycle: CreateTeam/Connect/
// GetTeam/connection exclusivity), and the per-profile-model suite
// (→ per-TeamProfile model). The team turn (one user input = one graph
// invoke) is driven through the per-session TurnLoop exactly like the
// pre-team agent; each test sets up the team stack via setupTeamSession
// (session → saolei TeamProfile → CreateTeam) before connecting — CreateTeam
// MUST precede Connect (no lazy creation, FR-033).
//
// A "game" in these tests is one full team turn driven by the fake-LLM
// "saolei-start" keyword: the player agent's saolei_init recognizes an
// IN-PROGRESS board (contract saolei-sink-contract.md §3 — onGameStart only,
// no game-end detection on init), then its first saolei_click reply carries
// a TERMINAL won/lost board — the move's post-dispatch recognition fires
// onMove + onGameEnd (the sink records the end event), the conditional edge
// routes to the planner exactly once, the planner calls update_strategy
// (fake-LLM matches the fixed review prefix), and the graph loops back
// through the player, whose stateless fake-LLM re-matches saolei-start and
// opens one more in-progress game (no end event) so the turn converges to
// wait. The helper playTeamGameUntilWait drives the desktop role (replying
// to every dispatched operation) until the terminal wait frame.
package testplan

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"dominion/common/gopkg/testtool"
	game "dominion/projects/game"

	"github.com/gorilla/websocket"
)

// playTeamGameUntilWait drives one full team turn ("please start saolei
// game") while playing the desktop: every dispatched operation FlowPart is
// answered with a FlowResultPart carrying a recognizable board screenshot
// (the reply message is derived from the operation kind so the fake-LLM
// coordinate-tagged tool configs keep matching — sample_saolei_tools.yaml),
// and the loop drains frames until the terminal wait FlowPart (turn
// complete). Returns every frame read, in order, so callers can assert on
// the planner's update_strategy tool_calls and the per-agent frame stream.
//
// Screenshot semantics follow specs/031-team-template-mode/contracts/
// saolei-sink-contract.md §3 (onGameEnd fires only after a move whose
// post-dispatch recognition is terminal — NOT after saolei_init):
//
//   - `initScreenshot` answers every saolei_init (keyboard F2) dispatch —
//     an IN-PROGRESS board (saoleiBoardInitPNG), so the init recognize is
//     non-terminal (onGameStart only) and the following cell ops pass
//     pre-dispatch validation.
//   - `clickTerminalScreenshot` answers the FIRST cell-op (saolei_click)
//     dispatch — a TERMINAL board (saoleiBoardLossPNG), so the move's
//     post-dispatch recognition fires onMove + onGameEnd once (the sink
//     records the end event → planner). MUST share the init board's
//     dimensions: SaoleiBoard.updateFromScreenshot rejects a dimension
//     change (BoardDimensionMismatchError → "unable to recognize" → no sink
//     events), which is why saoleiBoardWinPNG (9×9) cannot back a 16×16 init
//     (saoleiBoardInitPNG) — both here are 16×16.
//   - LATER cell-op dispatches are answered with `initScreenshot` again:
//     after the planner the stateless fake-LLM re-matches the saolei-start
//     keyword and the player opens another game; replying in-progress keeps
//     that run end-event-free, so the turn converges to wait with the
//     planner triggered exactly once (FR-011).
func playTeamGameUntilWait(t *testing.T, conn *websocket.Conn, sessionID string, initScreenshot, clickTerminalScreenshot *game.ImagePart) []*game.TeamFrame {
	t.Helper()

	sendText(t, conn, sessionID, "please start saolei game")

	var frames []*game.TeamFrame
	clickReplies := 0
	for i := 0; i < 60; i++ {
		frame := readWSFrame(t, conn)
		frames = append(frames, frame)

		if opID := frameOperationToolID(frame); opID != "" {
			// Play the desktop: reply with the recognizable board. The reply
			// message must carry the coordinates for cell ops so the fake-LLM
			// coordinate-tagged configs match deterministically.
			if kp := frameKeyboardPress(frame); kp != nil {
				respondToOperationWithScreenshot(t, conn, sessionID, frame,
					game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED,
					"F2 pressed, new game started", initScreenshot)
				continue
			}
			if mmc := frameMouseMoveAndClick(frame); mmc != nil {
				// Invert the WM client-space centre formula
				// (geometry.ts centerX(x) = 24 + x*32 + 16, centerY(y) =
				// 104 + y*32 + 16) to recover the cell for the reply message.
				// First click reply = terminal board (triggers onGameEnd);
				// later clicks (the post-planner player run's clicks — the
				// stateless fake-LLM re-matches saolei-start) reply
				// in-progress so the turn converges to wait.
				x := (mmc.GetXPx() - 40) / 32
				y := (mmc.GetYPx() - 120) / 32
				screenshot := initScreenshot
				if clickReplies == 0 {
					screenshot = clickTerminalScreenshot
				}
				clickReplies++
				respondToOperationWithScreenshot(t, conn, sessionID, frame,
					game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED,
					fmt.Sprintf("cell at (%d,%d) revealed", x, y), screenshot)
				continue
			}
			t.Fatalf("operation frame with unknown operation kind: %v", frame.GetFlowParts().GetParts())
		}

		if frameWait(frame) != nil {
			return frames
		}
	}
	t.Fatal("playTeamGameUntilWait: no wait frame within 60 reads — the team turn did not complete")
	return nil
}

// countUpdateStrategyCalls returns the number of update_strategy tool_call
// MessageParts across the given frames (the planner fires exactly once per
// game end — FR-011 — so this counts games reviewed by the planner).
func countUpdateStrategyCalls(frames []*game.TeamFrame) int {
	count := 0
	for _, f := range frames {
		if f.GetRole() != game.MessageRole_MESSAGE_ROLE_AGENT {
			continue
		}
		for _, p := range frameMessageParts(f).GetParts() {
			if tc := p.GetToolCall(); tc != nil && tc.GetName() == "update_strategy" {
				count++
			}
		}
	}
	return count
}

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

// ─── Team connect + lifecycle (FR-003/FR-004/FR-033) ──────────────────────

// TestTeamConnectLifecycle verifies the team lifecycle contract
// (contracts/api-contract.md §2.2 / FR-033): GetTeam/ListMessages on a
// session whose team was NOT created return NOT_FOUND (no lazy creation);
// CreateTeam returns the Team resource whose agents come from the template
// schema ([player, accepts_user_input=true], [planner, accepts_user_input=
// false] — D3/FR-031); repeated CreateTeam is idempotent for the SAME
// profile and ALREADY_EXISTS for a DIFFERENT profile; after CreateTeam the
// WebSocket Connect works and a text turn completes.
func TestTeamConnectLifecycle(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileID := fmt.Sprintf("team-conn-%s", uniqueSuffix())
	otherProfileID := fmt.Sprintf("team-conn-other-%s", uniqueSuffix())

	// given: a session with NO team created.
	sessionID, _ := createSession(t, sutHostURL, sutEnvName, saoleiTemplateID)

	// then: GetTeam and ListMessages require an existing team (FR-033 — no
	// implicit/lazy creation on read paths).
	if status, _ := getTeamWithStatus(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID); status != http.StatusNotFound {
		t.Errorf("GetTeam before CreateTeam: status=%d, want 404 NOT_FOUND (FR-033)", status)
	}
	if status, _ := listMessagesWithStatus(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "player"); status != http.StatusNotFound {
		t.Errorf("ListMessages before CreateTeam: status=%d, want 404 NOT_FOUND (FR-033)", status)
	}

	// given: a saolei TeamProfile.
	createTeamProfile(t, sutHostURL, sutEnvName, saoleiTemplateID, profileID, "gpt-4", "gpt-4")

	// when: CreateTeam (the ONLY Team creation point, AIP-133).
	team := createTeam(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, profileID)

	// then: the Team resource carries the session-scoped name and the
	// template-schema agents (D3 — typed, not hard-coded).
	wantName := fmt.Sprintf("templates/%s/sessions/%s/team", saoleiTemplateID, sessionID)
	if team.GetName() != wantName {
		t.Errorf("Team.name = %q, want %q", team.GetName(), wantName)
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

	// then (FR-033): repeated CreateTeam with the SAME profile is idempotent
	// (returns the existing team); a DIFFERENT profile is ALREADY_EXISTS.
	if status, _ := createTeamWithStatus(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, profileID); status != http.StatusOK {
		t.Errorf("repeated CreateTeam (same profile): status=%d, want 200 idempotent (FR-033)", status)
	}
	if status, body := createTeamWithStatus(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, otherProfileID); status != http.StatusConflict {
		t.Errorf("repeated CreateTeam (different profile): status=%d, want 409 ALREADY_EXISTS, body=%s (FR-033)", status, body)
	}

	// then: Connect works after CreateTeam and a text turn completes
	// (the team graph answers through the real pipeline).
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	sendText(t, conn, sessionID, "hello team lifecycle")
	thinkingFrame := drainWSFrame(t, conn, func(f *game.TeamFrame) bool { return frameHasThinking(f) })
	if thinkingFrame == nil {
		t.Fatal("did not receive a thinking frame after CreateTeam+Connect")
	}
	if !strings.Contains(frameThinking(thinkingFrame), expectedGreetingReasoning) {
		t.Errorf("thinking = %q, want to contain %q", frameThinking(thinkingFrame), expectedGreetingReasoning)
	}
}

// TestTeamConnectWithoutCreateRejected verifies the FR-033 inverse: a
// WebSocket Connect for a session whose team was NOT created does not hang —
// the first frame is rejected and the connection is closed (the proxy
// reports the missing owner over the stream error channel, which the gateway
// surfaces as a close). The old on-demand creation behaviour is gone.
func TestTeamConnectWithoutCreateRejected(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	// given: a session with no team (no CreateTeam).
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

	// Connect, send 2 text exchanges.
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)

	messages := []string{"First exchange", "Second exchange"}
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
	// partition history (FR-005).
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
			t.Fatalf("expected a saolei_click dispatch, got: %v", clickFrame.GetFlowParts().GetParts())
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
	// NOT drain text: the stateless fake-LLM chains saolei_click{3,4} after
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

// TestTeamPerProfileModel verifies that teams created from different
// TeamProfiles each resolve the models configured in their profile: the
// CreateTeam request carries the TeamProfile resource name, the agent reads
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
	// TeamProfile → CreateTeam), so GetTeamProfile below must run AFTER it —
	// the profiles do not exist before that point.
	sessionID1 := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profile1ID, "gpt-4", "gpt-4-turbo")
	sessionID2 := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profile2ID, "gpt-4-turbo", "gpt-4")

	// Verify the created profiles carry the configured models via
	// GetTeamProfile (the source of truth the agent reads at CreateTeam).
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

// ─── Team graph behaviour (FR-005/FR-010/FR-011/FR-013..015/FR-018) ───────

// TestTeamMessagePartitionByAgent verifies FR-005: ListMessages partitions
// the session history per team agent. After a game whose terminal move
// triggered the planner, the player partition carries the user input and the
// saolei tool calls/results (agent="player"), the planner partition carries
// the update_strategy review (agent="planner"), and neither partition leaks
// into the other.
func TestTeamMessagePartitionByAgent(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, "team-part-"+uniqueSuffix(), "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// when: one full game — init recognizes an in-progress board, the first
	// click's reply is a terminal lost board (planner triggers).
	playTeamGameUntilWait(t, conn, sessionID, buildSaoleiFlowResultScreenshot(saoleiBoardInitPNG),
		buildSaoleiFlowResultScreenshot(saoleiBoardLossPNG))

	// then: the player partition carries the user input + saolei tool calls,
	// all stamped agent="player".
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
		if messageHasToolCall(m, "saolei_init") || messageHasToolCall(m, "saolei_click") {
			playerSaoleiFound = true
		}
		if messageHasToolCall(m, "update_strategy") {
			t.Error("player partition carries an update_strategy tool_call — the planner's messages must stay in the planner partition (FR-005)")
		}
	}
	if !playerUserFound {
		t.Error("player partition did not surface the user input message")
	}
	if !playerSaoleiFound {
		t.Error("player partition did not surface a saolei tool_call — the player drives the game (FR-010)")
	}

	// then: the planner partition carries the update_strategy review, all
	// stamped agent="planner", with no saolei/desktop operations (FR-010 —
	// the planner holds no desktop tools).
	plannerLmr := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "planner")
	if len(plannerLmr.GetMessages()) == 0 {
		t.Fatal("planner partition is empty — the planner did not trigger for the terminal-move game (FR-011)")
	}
	plannerUpdateFound := false
	for _, m := range plannerLmr.GetMessages() {
		if m.GetAgent() != "planner" {
			t.Errorf("planner-partition message has agent=%q, want planner (FR-005)", m.GetAgent())
		}
		if messageHasToolCall(m, "update_strategy") {
			plannerUpdateFound = true
		}
		for _, name := range messageToolCallNames(m) {
			if strings.HasPrefix(name, "saolei_") {
				t.Errorf("planner partition carries a saolei_%s tool_call — the planner MUST NOT hold desktop tools (FR-010)", name)
			}
		}
	}
	if !plannerUpdateFound {
		t.Error("planner partition did not surface an update_strategy tool_call (FR-012)")
	}
}

// TestTeamPlayerExclusiveControl verifies FR-010 end-to-end: during a team
// turn only the player agent drives the desktop — every dispatched operation
// FlowPart on the WS corresponds to the player's saolei tool calls, and the
// planner contributes no operations at all (its only tool, update_strategy,
// writes the strategy store without dispatching). The frame stream of a full
// game (init in-progress → terminal-move click) is checked: every operation
// frame belongs to a player tool (F2 init / cell clicks), while the
// planner's tool_call frames carry only update_strategy.
func TestTeamPlayerExclusiveControl(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, "team-excl-"+uniqueSuffix(), "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// when: one full game — init recognizes an in-progress board, the first
	// click's reply is a terminal lost board (planner triggers inside the
	// turn — D6).
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

	// then: the planner contributed exactly its update_strategy review — no
	// saolei tool_calls from the planner channel (partition assertion) and no
	// desktop operations originated outside the player's channel.
	if got := countUpdateStrategyCalls(frames); got != 1 {
		t.Errorf("update_strategy calls = %d, want exactly 1 per game end (FR-011/D6)", got)
	}
	plannerLmr := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "planner")
	for _, m := range plannerLmr.GetMessages() {
		for _, name := range messageToolCallNames(m) {
			if strings.HasPrefix(name, "saolei_") {
				t.Errorf("planner channel carries saolei tool_call %q — only the player holds desktop tools (FR-010)", name)
			}
		}
	}
}

// TestTeamPlannerTriggersOncePerGame verifies FR-011/D6: the planner is
// triggered EXACTLY ONCE per game end (won/lost) and never per move — two
// consecutive games on the same session produce exactly two update_strategy
// reviews (one per game), and a single game never repeats the trigger
// (the graph clears gameEnded after the planner node — team-graph-contract.md
// §2.2/§4).
func TestTeamPlannerTriggersOncePerGame(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, "team-trigger-"+uniqueSuffix(), "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// when: game 1 — init recognizes an in-progress board, the first click's
	// reply is a terminal lost board (the move fires onGameEnd → planner).
	frames1 := playTeamGameUntilWait(t, conn, sessionID, buildSaoleiFlowResultScreenshot(saoleiBoardInitPNG),
		buildSaoleiFlowResultScreenshot(saoleiBoardLossPNG))

	// then: the planner triggered exactly once (FR-011 — not per move; the
	// in-game cell rejections on the terminal board never re-trigger it).
	if got := countUpdateStrategyCalls(frames1); got != 1 {
		t.Errorf("game 1: update_strategy calls = %d, want exactly 1 (FR-011/D6)", got)
	}

	// when: game 2 on the same session (a second turn, same fixture boards).
	frames2 := playTeamGameUntilWait(t, conn, sessionID, buildSaoleiFlowResultScreenshot(saoleiBoardInitPNG),
		buildSaoleiFlowResultScreenshot(saoleiBoardLossPNG))

	// then: exactly one more planner trigger — one per game, accumulated.
	if got := countUpdateStrategyCalls(frames2); got != 1 {
		t.Errorf("game 2: update_strategy calls = %d, want exactly 1 (one per game — FR-011)", got)
	}
	plannerLmr := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "planner")
	total := 0
	for _, m := range plannerLmr.GetMessages() {
		for _, name := range messageToolCallNames(m) {
			if name == "update_strategy" {
				total++
			}
		}
	}
	if total != 2 {
		t.Errorf("planner partition total update_strategy calls = %d, want 2 (one per game across two games)", total)
	}
}

// TestTeamStrategyPersistsAcrossGames verifies FR-013/FR-014/FR-015: the
// strategy written by the planner's update_strategy call persists across
// games of the SAME session (the StrategyStore key is the session id) and is
// isolated between sessions.
//
// Black-box note: the strategy is injected into the player/planner prompts
// as a SystemMessage by the graph nodes (player.ts/planner.ts, FR-014/FR-015)
// and fake-LLM only keyword-matches the LAST user message, so the injected
// content itself is not directly observable from this layer. In particular,
// quickstart §2.1's `strategy-shared-persistent` expectation — "下一局
// player 作为当前态势读取" — is NOT directly verified here: the player's
// read of the persisted strategy is only exercised indirectly (the player
// run completes normally and the game flow proceeds). The observable
// contract asserted instead: (1) the update_strategy tool_call carries the
// fixture's strategy content verbatim (args_json — the write happened),
// (2) the planner keeps triggering across games (the strategy layer stays
// healthy), and (3) a second session's games are independent (per-session
// namespace — FR-013).
func TestTeamStrategyPersistsAcrossGames(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileID := fmt.Sprintf("team-strat-%s", uniqueSuffix())
	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileID, "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// when: game 1 — init recognizes an in-progress board, the first click's
	// reply is a terminal lost board (planner writes the strategy).
	frames1 := playTeamGameUntilWait(t, conn, sessionID, buildSaoleiFlowResultScreenshot(saoleiBoardInitPNG),
		buildSaoleiFlowResultScreenshot(saoleiBoardLossPNG))

	// then: the update_strategy tool_call carried the fixture content
	// verbatim (the write argument — FR-012).
	foundStrategy := false
	for _, f := range frames1 {
		for _, p := range frameMessageParts(f).GetParts() {
			if tc := p.GetToolCall(); tc != nil && tc.GetName() == "update_strategy" {
				if !strings.Contains(tc.GetArgsJson(), expectedPlannerStrategyText) {
					t.Errorf("update_strategy args_json = %q, want to contain %q", tc.GetArgsJson(), expectedPlannerStrategyText)
				}
				foundStrategy = true
			}
		}
	}
	if !foundStrategy {
		t.Fatal("no update_strategy tool_call frame seen in game 1 — the planner did not write a strategy")
	}

	// then: the planner's update_strategy tool loop closed with the
	// fake-LLM terminal text (sample_update_strategy_tools.yaml
	// update-strategy-success-text) — the planner turn ended deterministically
	// after the write.
	foundPlannerText := false
	for _, f := range frames1 {
		if f.GetAgent() != "planner" || !frameHasText(f) {
			continue
		}
		if strings.Contains(frameText(f), expectedPlannerUpdateText) {
			foundPlannerText = true
		}
	}
	if !foundPlannerText {
		t.Errorf("no planner text frame containing %q — the update_strategy tool loop did not close deterministically",
			expectedPlannerUpdateText)
	}

	// when: game 2 on the same session — the strategy written in game 1
	// stays readable (the planner's system context re-reads it per entry,
	// FR-014) and the flow completes normally.
	frames2 := playTeamGameUntilWait(t, conn, sessionID, buildSaoleiFlowResultScreenshot(saoleiBoardInitPNG),
		buildSaoleiFlowResultScreenshot(saoleiBoardLossPNG))
	if got := countUpdateStrategyCalls(frames2); got != 1 {
		t.Errorf("game 2: update_strategy calls = %d, want exactly 1 (strategy layer healthy after game 1)", got)
	}

	// when: a SECOND session runs its own game.
	otherSessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileID+"-b", "gpt-4", "gpt-4")
	connB := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, otherSessionID)
	defer connB.Close()
	framesB := playTeamGameUntilWait(t, connB, otherSessionID, buildSaoleiFlowResultScreenshot(saoleiBoardInitPNG),
		buildSaoleiFlowResultScreenshot(saoleiBoardLossPNG))

	// then: the second session's game is independent (its planner triggers
	// normally — the per-session strategy namespace is isolated, FR-013).
	if got := countUpdateStrategyCalls(framesB); got != 1 {
		t.Errorf("second session: update_strategy calls = %d, want exactly 1 (per-session strategy isolation — FR-013)", got)
	}
}

// TestTeamRefreshClearsShortTermKeepsStrategy verifies FR-018/D8: RefreshTeam
// clears the session's SHORT-TERM memory (both per-agent message channels —
// ListMessages partitions read empty afterwards) while the long-term
// strategy is unaffected — the next game's planner still triggers and the
// strategy flow continues.
func TestTeamRefreshClearsShortTermKeepsStrategy(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, "team-refresh-"+uniqueSuffix(), "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// given: one full game so both channels hold messages (player + planner).
	playTeamGameUntilWait(t, conn, sessionID, buildSaoleiFlowResultScreenshot(saoleiBoardInitPNG),
		buildSaoleiFlowResultScreenshot(saoleiBoardLossPNG))
	if got := len(listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "player").GetMessages()); got == 0 {
		t.Fatal("precondition: player partition is empty before RefreshTeam")
	}
	if got := len(listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "planner").GetMessages()); got == 0 {
		t.Fatal("precondition: planner partition is empty before RefreshTeam — the planner did not trigger")
	}

	// when: RefreshTeam (FR-018 — clears short-term memory).
	refreshTeam(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)

	// then: both partitions are empty (the channels were cleared).
	if got := len(listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "player").GetMessages()); got != 0 {
		t.Errorf("player partition after RefreshTeam = %d messages, want 0 (FR-018)", got)
	}
	if got := len(listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "planner").GetMessages()); got != 0 {
		t.Errorf("planner partition after RefreshTeam = %d messages, want 0 (FR-018)", got)
	}

	// then: the strategy is unaffected — the next game still triggers the
	// planner (which reads the persisted strategy as its system context,
	// FR-014) and the whole flow completes.
	frames := playTeamGameUntilWait(t, conn, sessionID, buildSaoleiFlowResultScreenshot(saoleiBoardInitPNG),
		buildSaoleiFlowResultScreenshot(saoleiBoardLossPNG))
	if got := countUpdateStrategyCalls(frames); got != 1 {
		t.Errorf("post-refresh game: update_strategy calls = %d, want exactly 1 (strategy survived RefreshTeam — FR-018)", got)
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
// (FR-002/FR-003 — the desktop dedups by frameId/messageId).
func TestTeamPlannerReviewRealtimeVisible(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, "team-review-live-"+uniqueSuffix(), "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// when: one full game — init recognizes an in-progress board, the first
	// click's reply is a terminal lost board (planner triggered).
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
	// tool, coordinates and status, plus the board renders (FR-002 — the
	// review input renders every gameLog entry, specs/036-team-mode-bugfix/
	// contracts/team-graph-fix-contract.md §2.2).
	for _, want := range []string{
		"1. saolei_init → playing",
		"2. saolei_click(3, 4) → lost",
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

// TestTeamCompressionAtFiveGames verifies US2 (specs/037-saolei-team-optimize/
// spec.md FR-006..FR-012/FR-015): after the 5th game's planner returns, the
// conditional edge routes to the compress node — each short-term channel is
// replaced by exactly ONE summary agent message (FR-008), live summary frames
// are emitted for both tabs (FR-011/SC-004), the player STOPS (FR-010 — the
// turn ends without opening another game), the strategy (long-term memory)
// survives (FR-009 — game 6 still triggers the planner from the summary
// context), and the live summary frame's frame_id equals the reloaded summary
// message's message_id (the desktop dedup anchor, data-model.md §4).
func TestTeamCompressionAtFiveGames(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, "team-compress-"+uniqueSuffix(), "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	initScreenshot := buildSaoleiFlowResultScreenshot(saoleiBoardInitPNG)
	terminalScreenshot := buildSaoleiFlowResultScreenshot(saoleiBoardLossPNG)

	// given: 4 completed games — the counter stays below 5, so NO compression
	// fires (FR-006) and the player keeps looping back after each planner.
	for i := 0; i < 4; i++ {
		frames := playTeamGameUntilWait(t, conn, sessionID, initScreenshot, terminalScreenshot)
		if got := countUpdateStrategyCalls(frames); got != 1 {
			t.Fatalf("game %d: update_strategy calls = %d, want 1 (planner trigger precondition)", i+1, got)
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
	if got := countUpdateStrategyCalls(frames5); got != 1 {
		t.Errorf("game 5: update_strategy calls = %d, want exactly 1 (the planner still reviewed the 5th game before compression)", got)
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

	// then: each channel was shrunk to exactly ONE summary message (FR-008)
	// and the player STOPPED — no new game was opened after the compression,
	// so the player partition holds only the summary (FR-010).
	playerLmr := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "player")
	if got := len(playerLmr.GetMessages()); got != 1 {
		t.Fatalf("player partition after compression = %d messages, want exactly 1 (FR-008/FR-010)", got)
	}
	if got := messageText(playerLmr.GetMessages()[0]); !strings.Contains(got, expectedPlayerCompressionSummary) {
		t.Errorf("player summary message = %q, want to contain %q (FR-008/FR-012)", got, expectedPlayerCompressionSummary)
	}
	plannerLmr := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "planner")
	if got := len(plannerLmr.GetMessages()); got != 1 {
		t.Fatalf("planner partition after compression = %d messages, want exactly 1 (FR-008)", got)
	}
	if got := messageText(plannerLmr.GetMessages()[0]); !strings.Contains(got, expectedPlannerCompressionSummary) {
		t.Errorf("planner summary message = %q, want to contain %q (FR-008/FR-012)", got, expectedPlannerCompressionSummary)
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

	// then: the strategy (long-term memory, StrategyStore) survived the
	// compression (FR-009) — the planner still triggers and writes the
	// strategy content (FR-012)...
	if got := countUpdateStrategyCalls(frames6); got != 1 {
		t.Errorf("game 6: update_strategy calls = %d, want exactly 1 (strategy survived compression — FR-009)", got)
	}
	foundStrategy := false
	for _, f := range frames6 {
		for _, p := range frameMessageParts(f).GetParts() {
			if tc := p.GetToolCall(); tc != nil && tc.GetName() == "update_strategy" && strings.Contains(tc.GetArgsJson(), expectedPlannerStrategyText) {
				foundStrategy = true
			}
		}
	}
	if !foundStrategy {
		t.Error("game 6: no update_strategy tool_call carrying the strategy content — the strategy layer did not survive compression (FR-009/FR-012)")
	}

	// ...and the player resumed from the summary context — the player
	// partition grew beyond the single summary message (FR-010/SC-003).
	if got := len(listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "player").GetMessages()); got <= 1 {
		t.Errorf("player partition after game 6 = %d messages, want > 1 (player resumed with the summary context — FR-010)", got)
	}
}

// TestTeamPlannerPromptToolDescriptions verifies US3 (specs/037-saolei-team-
// optimize/spec.md FR-016/FR-017/FR-018): the planner's system prompt
// carries the player tools' NAME + DESCRIPTION section, while the planner's
// actual tool set stays update_strategy-only.
//
// The FR-016 half (prompt content) is verified via LOG/TRACE: the deployed
// fake-llm logs every request's system messages at INFO ("system prompt
// received" — projects/game/fake-llm/service/handler.go logSystemPrompts),
// so after this test drives a game an operator can query the fake-llm logs
// (signoz) for the injected section: "## Player 可用工具" followed by the
// five player tools (saolei_init/saolei_click/saolei_flag/saolei_chord_click/
// saolei_remain) with their descriptions. The keyword matcher only reads user
// text (README.md §4), so the system content is unobservable in the response
// stream — the log is the verification channel the task requires.
//
// This test asserts the observable half — FR-018: the planner never calls a
// saolei_* tool (only update_strategy), proving the injection added
// descriptions, NOT tools.
func TestTeamPlannerPromptToolDescriptions(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, "team-tools-"+uniqueSuffix(), "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// when: one full game — the planner's createAgent request (carrying the
	// system prompt with the tool-description section) is sent to fake-llm.
	frames := playTeamGameUntilWait(t, conn, sessionID, buildSaoleiFlowResultScreenshot(saoleiBoardInitPNG),
		buildSaoleiFlowResultScreenshot(saoleiBoardLossPNG))

	// then: every planner tool_call in the live stream is update_strategy —
	// no player tool was injected into the planner's tool set (FR-018).
	for _, f := range frames {
		if f.GetAgent() != "planner" {
			continue
		}
		for _, p := range frameMessageParts(f).GetParts() {
			if tc := p.GetToolCall(); tc != nil && tc.GetName() != "update_strategy" {
				t.Errorf("planner live tool_call %q — the planner MUST hold only update_strategy (FR-018)", tc.GetName())
			}
		}
	}

	// then: the reloaded planner partition carries no saolei tool_call either
	// (FR-018 — the description injection did not add tools).
	plannerLmr := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "planner")
	for _, m := range plannerLmr.GetMessages() {
		for _, name := range messageToolCallNames(m) {
			if strings.HasPrefix(name, "saolei_") {
				t.Errorf("planner partition carries saolei tool_call %q — the planner MUST hold only update_strategy (FR-018)", name)
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
//   - operationCount = 1 — exactly one successful cell op (the first click
//     whose post-dispatch recognition is terminal; init/re-init and the
//     post-terminal rejected clicks never fire onMove — FR-027).
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

	// when: one full game (init in-progress → first click terminal loss).
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
